"""Root integration test using isolated client/WAN namespaces; no real LAN client is modified."""
import importlib.util
import json
import pathlib
import socket
import sqlite3
import subprocess
import time
import urllib.parse
import urllib.request

spec=importlib.util.spec_from_file_location('control','/opt/ngpanel/control.py')
control=importlib.util.module_from_spec(spec);spec.loader.exec_module(control)
base='http://192.168.1.84:8080'
def run(args,check=True):return subprocess.run(args,text=True,capture_output=True,timeout=15,check=check)
def post(action,**values):
    request=urllib.request.Request(base+'/action',data=urllib.parse.urlencode(dict(action=action,tab='status',**values)).encode(),headers={'Origin':base})
    with urllib.request.urlopen(request,timeout=15) as response:
        message=urllib.parse.parse_qs(urllib.parse.urlparse(response.url).query).get('message',[''])[0]
        if message.startswith('Ошибка:'):raise RuntimeError('Panel rejected '+action)
    if action in ('apply','check','stop','start','rollback'):
        for _ in range(100):
            if not pathlib.Path('/var/lib/ngpanel/jobs/control.request').exists():break
            time.sleep(.1)
        else:raise RuntimeError('System job timeout')
        state=json.loads(pathlib.Path('/var/lib/ngpanel/runtime.json').read_text())
        if state['State']!='ok' or state['Action']!=action:raise RuntimeError(state['Message'])
def client(args,check=True):return run(['ip','netns','exec','ngpanel-test-lan',*args],check)
def http(address='198.19.0.2',host='direct.test',ok=True):
    r=client(['curl','-sS','--max-time','3','-H','Host: '+host,'http://'+address+':8081'],False)
    if ok:assert r.returncode==0 and r.stdout=='ngpanel-fixture', (r.returncode,r.stderr)
    else:assert r.returncode!=0, 'Blocked connection leaked to direct'
def udp(address='198.19.0.2',ok=True):
    code="import socket;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(2);s.sendto(b'ngtest',('"+address+"',8082));assert s.recv(128)==b'ngtest'"
    r=client(['python3','-c',code],False)
    assert (r.returncode==0)==ok, 'Unexpected UDP routing'
for name in ('ngpanel-test-lan','ngpanel-test-wan'):
    if name in run(['ip','netns','list']).stdout:raise RuntimeError('Test namespace already exists')
for name in ('ngtest-lan','ngtest-wan'):
    if run(['ip','link','show',name],False).returncode==0:raise RuntimeError('Test interface already exists')
db=sqlite3.connect('/var/lib/ngpanel/db/panel.db')
saved=dict(db.execute("SELECT key,value FROM settings"))
saved_backups={name:pathlib.Path('/etc/ngpanel/'+name).read_bytes() if pathlib.Path('/etc/ngpanel/'+name).exists() else None for name in ('previous.json','previous-meta.json')}
process=None
fixture_id=None
test_rule_ids=[]
try:
    for side,network in [('lan','198.18.0.'),('wan','198.19.0.')]:
        ns='ngpanel-test-'+side;host='ngtest-'+side
        run(['ip','netns','add',ns]);run(['ip','link','add',host,'type','veth','peer','name','ngtest-peer'])
        run(['ip','link','set','ngtest-peer','netns',ns]);run(['ip','addr','add',network+'1/24','dev',host]);run(['ip','link','set',host,'up'])
        run(['ip','netns','exec',ns,'ip','addr','add',network+'2/24','dev','ngtest-peer']);run(['ip','netns','exec',ns,'ip','link','set','ngtest-peer','up']);run(['ip','netns','exec',ns,'ip','link','set','lo','up']);run(['ip','netns','exec',ns,'ip','route','add','default','via',network+'1'])
    run(['ip','netns','exec','ngpanel-test-wan','ip','addr','add','198.19.0.3/24','dev','ngtest-peer'])
    server='''import http.server,socket,threading
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers();self.wfile.write(b'ngpanel-fixture')
 def log_message(self,*args):pass
def echo():
 s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('0.0.0.0',8082))
 while True:
  b,addr=s.recvfrom(4096);s.sendto(b,addr)
threading.Thread(target=echo,daemon=True).start()
http.server.HTTPServer(('0.0.0.0',8081),Handler).serve_forever()
'''
    process=subprocess.Popen(['ip','netns','exec','ngpanel-test-wan','python3','-u','-c',server],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    time.sleep(.3)
    nft=control.nft_text().replace('table inet ngpanel','table inet ngpanel_test').replace('ens18','ngtest-lan').replace('192.168.1.0/24','198.18.0.0/24').replace('192.168.1.84','198.18.0.1')
    subprocess.run(['nft','-f','-'],input=nft,text=True,check=True,capture_output=True)
    http();udp();print('PASS: transparent IPv4 TCP and UDP Direct')
    for extra in ([],['+tcp']):
        result=client(['dig','@198.18.0.1','example.com','A','+short','+time=3','+tries=1',*extra])
        assert result.stdout.strip() and 'timed out' not in result.stdout, result.stdout
    print('PASS: UDP and TCP DNS redirection from port 53')
    post('rule-add',priority='1',name='TEST-only domain block',kind='domain',value='full:blocked.test',target='block')
    rid=db.execute("SELECT id FROM rules WHERE name='TEST-only domain block' ORDER BY id DESC LIMIT 1").fetchone()[0];test_rule_ids.append(rid)
    post('apply');http(host='blocked.test',ok=False);http();print('PASS: HTTP domain sniffing and Block; Direct remains available')
    post('rollback');http(host='blocked.test');print('PASS: configuration rollback restores traffic')
    post('rule-delete',id=str(rid));test_rule_ids.remove(rid)
    post('manual-node',uri='vless://fixture@127.0.0.1:1?type=tcp&security=none#TEST-unavailable')
    fixture_id=db.execute("SELECT id FROM nodes WHERE name='TEST-unavailable' ORDER BY id DESC LIMIT 1").fetchone()[0]
    post('select-node',id=str(fixture_id))
    post('rule-add',priority='1',name='TEST-only proxy failure',kind='ip',value='198.19.0.2',target='proxy')
    rid=db.execute("SELECT id FROM rules WHERE name='TEST-only proxy failure' ORDER BY id DESC LIMIT 1").fetchone()[0];test_rule_ids.append(rid)
    post('apply');http(ok=False);udp(ok=False);http('198.19.0.3');udp('198.19.0.3');print('PASS: unavailable VPN blocks assigned TCP/UDP with no Direct fallback')
    post('stop');http('198.19.0.3',ok=False)
    with urllib.request.urlopen(base) as response:assert response.status==200
    post('start');http('198.19.0.3');print('PASS: stopped core blocks intercepted TCP; panel remains available; start restores core')
finally:
    for rid in test_rule_ids:db.execute('DELETE FROM rules WHERE id=?',(rid,))
    if fixture_id is not None:db.execute('DELETE FROM nodes WHERE id=?',(fixture_id,))
    for key,value in saved.items():db.execute('UPDATE settings SET value=? WHERE key=?',(value,key))
    db.commit();db.close()
    try:
        post('apply')
        for name,data in saved_backups.items():
            path=pathlib.Path('/etc/ngpanel/'+name)
            if data is None:path.unlink(missing_ok=True)
            else:path.write_bytes(data)
    finally:
        run(['nft','delete','table','inet','ngpanel_test'],False)
        if process is not None:process.terminate();process.wait(timeout=3)
        for side in ('lan','wan'):
            run(['ip','netns','del','ngpanel-test-'+side],False)
            run(['ip','link','del','ngtest-'+side],False)
print('PASS: fixture resources removed and user configuration restored')

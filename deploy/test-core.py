"""Core configuration and DNS tests. Run as root on the isolated beta VM."""
import json
import pathlib
import sqlite3
import subprocess
import time
import urllib.parse
import urllib.request
base='http://192.168.1.84:8080'
def post(action,expect='ok',**kw):
    with urllib.request.urlopen(urllib.request.Request(base+'/action',data=urllib.parse.urlencode(dict(action=action,tab='status',**kw)).encode(),headers={'Origin':base})) as response:
        message=urllib.parse.parse_qs(urllib.parse.urlparse(response.url).query).get('message',[''])[0]
        if message.startswith('Ошибка:'):raise RuntimeError(message)
    if action in ('apply','check'):
        for _ in range(150):
            if not pathlib.Path('/var/lib/ngpanel/jobs/control.request').exists():break
            time.sleep(.1)
        else:raise RuntimeError('System job timeout')
        state=json.loads(pathlib.Path('/var/lib/ngpanel/runtime.json').read_text())
        assert state['Action']==action and state['State']==expect,state['Message']
db=sqlite3.connect('/var/lib/ngpanel/db/panel.db')
saved=dict(db.execute('SELECT key,value FROM settings'))
backups={name:pathlib.Path('/etc/ngpanel/'+name).read_bytes() if pathlib.Path('/etc/ngpanel/'+name).exists() else None for name in ('previous.json','previous-meta.json')}
fixture_id=None
try:
    post('settings',mode='proxy')
    ids=[r[0] for r in db.execute("SELECT id FROM nodes WHERE name NOT LIKE 'TEST%' ORDER BY id")]
    for node in ids:
        post('select-node',id=str(node));post('check')
    print('PASS core validation for all',len(ids),'imported VLESS transports')
    post('manual-node',uri='vless://fixture@127.0.0.1:443?security=reality&pbk=invalid#TEST-invalid-core')
    fixture_id=db.execute("SELECT id FROM nodes WHERE name='TEST-invalid-core' ORDER BY id DESC LIMIT 1").fetchone()[0]
    pid=subprocess.check_output(['systemctl','show','-p','MainPID','--value','xray']).strip()
    current=pathlib.Path('/etc/ngpanel/config.json').read_bytes()
    post('select-node',id=str(fixture_id));post('apply',expect='error')
    assert pid==subprocess.check_output(['systemctl','show','-p','MainPID','--value','xray']).strip()
    assert current==pathlib.Path('/etc/ngpanel/config.json').read_bytes()
    print('PASS invalid configuration preserves file and running process')
    post('select-node',id=str(ids[0]));post('settings',mode='direct')
    post('gateway-settings',dns='1.1.1.1',dns_mode='proxy',gateway='1');post('apply')
    config=json.loads(pathlib.Path('/etc/ngpanel/config.json').read_text())
    assert config['dns']['servers'][-1]['address']=='https://1.1.1.1/dns-query'
    for args in ([],['+tcp']):
        r=subprocess.run(['dig','@192.168.1.84','-p','1053','example.com','A','+short','+time=5','+tries=1',*args],text=True,capture_output=True,timeout=8)
        assert r.returncode==0 and r.stdout.strip() and 'timed out' not in r.stdout,r.stdout
    print('PASS real VPN DNS over HTTPS for UDP and TCP clients')
    r=subprocess.run(['dig','@192.168.1.84','-p','1053','example.com','AAAA','+short','+time=5','+tries=1'],text=True,capture_output=True,timeout=8)
    assert r.returncode==0 and not r.stdout.strip(),r.stdout
    print('PASS IPv6 DNS answers excluded')
finally:
    if fixture_id is not None:db.execute('DELETE FROM nodes WHERE id=?',(fixture_id,))
    for key,value in saved.items():db.execute('UPDATE settings SET value=? WHERE key=?',(value,key))
    db.commit();db.close()
    post('apply')
    for name,data in backups.items():
        path=pathlib.Path('/etc/ngpanel/'+name)
        if data is None:path.unlink(missing_ok=True)
        else:path.write_bytes(data)
print('PASS user settings and rollback backup restored')

"""Run with sudo unshare --net python3 test-traffic-lab.py fixture.nft.
All links, routes and firewall rules live inside disposable network namespaces.
No production panel database, core config or network settings are accessed.
"""
import json, pathlib, socket, subprocess, sys, tempfile, time

def run(args, **kw):
    try: return subprocess.run(args, check=True, capture_output=True, timeout=15, **kw)
    except subprocess.CalledProcessError as e:
        print(e.stderr.decode(errors="replace") if isinstance(e.stderr,bytes) else e.stderr,flush=True);raise

def ns(pid, args, **kw): return run(['nsenter','-t',str(pid),'-n',*args], **kw)
children=[]
with tempfile.TemporaryDirectory(prefix='ng-traffic-lab-') as directory:
    root=pathlib.Path(directory); root.chmod(0o755)
    try:
        run(['ip','link','set','lo','up'])
        for side,network in [('lan','198.18.0.'),('wan','198.19.0.')]:
            child=subprocess.Popen(['unshare','--net','sleep','180']);children.append(child);time.sleep(.1)
            run(['ip','link','add',side+'0','type','veth','peer','name',side+'peer'])
            run(['ip','link','set',side+'peer','netns',str(child.pid)])
            run(['ip','addr','add',network+'1/24','dev',side+'0']);run(['ip','link','set',side+'0','up'])
            ns(child.pid,['ip','addr','add',network+'2/24','dev',side+'peer']);ns(child.pid,['ip','link','set',side+'peer','up']);ns(child.pid,['ip','link','set','lo','up'])
            ns(child.pid,['ip','route','add','default','via',network+'1'])
        lan,wan=children[0].pid,children[1].pid
        ns(wan,['ip','addr','add','198.19.0.3/24','dev','wanpeer'])
        fixture='''import http.server,socket,threading,time
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.send_header('Content-Length',str(1048576));self.end_headers()
  for _ in range(64):self.wfile.write(b'd'*16384);self.wfile.flush();time.sleep(.0625)
 def do_POST(self):
  self.rfile.read(int(self.headers['Content-Length']));size=1 if self.path=='/upload' else 524288;self.send_response(200);self.send_header('Content-Length',str(size));self.end_headers();self.wfile.write(b'x'*size)
 def log_message(self,*args):pass
def echo(address):
 s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind((address,8082))
 while True:
  data,addr=s.recvfrom(65535);s.sendto(data+b'y'*1000,addr)
for address in ['198.19.0.2','198.19.0.3']:threading.Thread(target=echo,args=(address,),daemon=True).start()
http.server.HTTPServer(('0.0.0.0',8081),H).serve_forever()
'''
        children.append(subprocess.Popen(['nsenter','-t',str(wan),'-n','python3','-u','-c',fixture],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL))
        uuid='00000000-0000-4000-8000-000000000001'
        server={'log':{'loglevel':'none'},'inbounds':[{'listen':'0.0.0.0','port':16443,'protocol':'vless','settings':{'clients':[{'id':uuid}],'decryption':'none'}}],'outbounds':[{'protocol':'freedom'}]}
        (root/'server.json').write_text(json.dumps(server))
        children.append(subprocess.Popen(['nsenter','-t',str(wan),'-n','xray','run','-c',str(root/'server.json')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL))
        config={'log':{'loglevel':'info'},'api':{'listen':'127.0.0.1:10085','tag':'api','services':['StatsService']},'stats':{},'policy':{'system':{'statsOutboundUplink':True,'statsOutboundDownlink':True}},'inbounds':[{'tag':'tproxy-in','listen':'0.0.0.0','port':7895,'protocol':'dokodemo-door','settings':{'network':'tcp,udp','followRedirect':True},'streamSettings':{'sockopt':{'tproxy':'tproxy'}}}],'outbounds':[{'tag':'direct','protocol':'freedom','streamSettings':{'sockopt':{'mark':66202}}},{'tag':'node-1','protocol':'vless','streamSettings':{'sockopt':{'mark':131738}},'settings':{'vnext':[{'address':'198.19.0.2','port':16443,'users':[{'id':uuid,'encryption':'none'}]}]}}],'routing':{'rules':[{'type':'field','ip':['198.19.0.3'],'outboundTag':'node-1'}]}}
        (root/'gateway.json').write_text(json.dumps(config))
        gateway_log=(root/'gateway.log').open('w');children.append(subprocess.Popen(['xray','run','-c',str(root/'gateway.json')],stdout=gateway_log,stderr=gateway_log))
        for _ in range(50):
            try:
                with socket.create_connection(('127.0.0.1',10085),timeout=.1):break
            except OSError:time.sleep(.05)
        run(['ip','rule','add','fwmark','1','lookup','100']);run(['ip','route','add','local','0.0.0.0/0','dev','lo','table','100'])
        intercept='''table inet lab_tproxy { chain incoming {
 type filter hook prerouting priority -150; policy accept;
 iifname "lan0" meta nfproto ipv4 meta l4proto { tcp, udp } tproxy ip to :7895 meta mark set 1 accept
 }
}
'''
        run(['nft','-f','-'],input=intercept.encode())
        accounting=pathlib.Path(sys.argv[1]).read_bytes();run(['nft','-f','-'],input=accounting)
        def counters():
            raw=json.loads(run(['nft','-j','-a','list','table','inet','ngpanel_traffic']).stdout)
            (root/'last-nft.json').write_text(json.dumps(raw))
            output={}
            for item in raw['nftables']:
                if 'set' not in item or item['set']['name'] not in ('upload','download'):continue
                entry=item['set'];output[entry['name']]={}
                for element in entry.get('elem',[]):
                    element=element['elem'];output[entry['name']][element['val']]=element['counter']['bytes']
            return output,raw
        for target,tag in [('198.19.0.2','direct'),('198.19.0.3','node-1')]:
            before,_=counters()
            ns(lan,['curl','--noproxy','*','--fail','--silent','--show-error','--max-time','5','--data-binary','@-',f'http://{target}:8081','--output','/dev/null'],input=b'a'*262144)
            after,_=counters();up=after['upload']['198.18.0.2']-before['upload'].get('198.18.0.2',0);down=after['download']['198.18.0.2']-before['download'].get('198.18.0.2',0)
            assert 262144<up<300000 and 524288<down<600000,(up,down)
            stats=json.loads(run(['xray','api','statsquery','-s=127.0.0.1:10085','-pattern','outbound>>>']).stdout)
            tagged={v['name']:int(v.get('value',0)) for v in stats['stat']}
            # Core outbound counters measure the transport stream, not HTTP body size.
            assert 200000<tagged[f'outbound>>>{tag}>>>traffic>>>uplink']<320000
            assert tagged[f'outbound>>>{tag}>>>traffic>>>downlink']>=524288
            print('PASS TCP',tag,'device upload/download',up,down,flush=True)
            before,_=counters()
            code=f"import socket;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(5);s.sendto(b'u'*200,('{target}',8082));assert len(s.recv(4096))==1200"
            ns(lan,['python3','-c',code]);after,raw=counters();up=after['upload']['198.18.0.2']-before['upload']['198.18.0.2'];down=after['download']['198.18.0.2']-before['download']['198.18.0.2']
            assert 228<=up<1000 and 1228<=down<2000,(up,down)
            print('PASS UDP',tag,'device upload/download',up,down,flush=True)
        # One UDP source association keeps its first exit across destinations.
        udp_cases=[]
        for first,second,tag,port in [('198.19.0.2','198.19.0.3','direct',42001),('198.19.0.3','198.19.0.2','node-1',42002)]:
            def udp_stats():
                stats=json.loads(run(['xray','api','statsquery','-s=127.0.0.1:10085','-pattern','outbound>>>']).stdout)
                return {v['name']:int(v.get('value',0)) for v in stats['stat']}
            before=udp_stats()
            code=f"import socket,time;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('0.0.0.0',{port}));s.settimeout(5);s.sendto(b'u'*200,('{first}',8082));assert len(s.recv(4096))==1200;time.sleep(.1);s.sendto(b'v'*200,('{second}',8082));assert len(s.recv(4096))==1200"
            ns(lan,['python3','-c',code]);time.sleep(.1);after=udp_stats()
            for direction,amount in [('uplink',400),('downlink',2400)]:
                key=f'outbound>>>{tag}>>>traffic>>>{direction}'
                # VLESS/XUDP framing changes transport byte counts.
                assert after[key]-before.get(key,0)>=amount//2,(tag,direction,before,after)
                other='node-1' if tag=='direct' else 'direct'
                other_key=f'outbound>>>{other}>>>traffic>>>{direction}'
                assert after[other_key]==before.get(other_key,0),(tag,'wrong exit',direction)
            udp_cases.append(dict(SourcePort=str(port),Destination=second,Outbound=tag))
            print('PASS CONE UDP',first,'then',second,'keeps',tag,flush=True)
        pathlib.Path('/tmp/ng-udp-lab.json').write_text(json.dumps(udp_cases))
        # Sustained transfers must be visible before close, including Direct splice.
        # The stock Xray environment is deliberately untouched: splice stays enabled.
        trace=[]
        def pair(tag):
            c,raw=counters();v={}
            for item in raw['nftables']:
                entry=item.get('counter',{});v[entry.get('comment','')]=entry.get('bytes',0)
            return [c.get('upload',{}).get('198.18.0.2',0),c.get('download',{}).get('198.18.0.2',0),v.get('upload:'+tag,0),v.get('download:'+tag,0)]
        for target,tag in [('198.19.0.2','direct'),('198.19.0.3','node-1')]:
            for phase in ['download','upload']:
                before=pair(tag);began=time.monotonic()
                if phase=='download':
                    args=['curl','--noproxy','*','--fail','-s',f'http://{target}:8081/download','-o','/dev/null']
                else:
                    code=f"import socket,time;s=socket.create_connection(('{target}',8081));s.sendall(b'POST /upload HTTP/1.0\\r\\nContent-Length: 1048576\\r\\n\\r\\n');[(s.sendall(b'u'*16384),time.sleep(.0625)) for _ in range(64)];s.recv(1024)"
                    args=['python3','-c',code]
                client=subprocess.Popen(['nsenter','-t',str(lan),'-n',*args]);children.append(client)
                rates=[]
                for _ in range(5):
                    time.sleep(1);after=pair(tag);now=time.monotonic();row=[round((a-b)/(now-began),2) for a,b in zip(after,before)];rates.append(row);before=after;began=now
                assert client.wait(timeout=3)==0
                direction=0 if phase=='upload' else 1
                assert sum(r[direction]>150000 and r[direction+2]>150000 for r in rates[:4])>=3,(tag,phase,rates)
                assert sum(r[direction+2] for r in rates)>sum(r[3-direction] for r in rates)*20,(tag,phase,rates)
                trace.append(dict(tag=tag,phase=phase,rates=rates))
                print('PASS LIVE',tag,phase,'device/upstream Bps',rates,flush=True)
                time.sleep(1);before=pair(tag);time.sleep(1);after=pair(tag)
                assert max(a-b for a,b in zip(after,before))<4096,('post-close burst',tag,phase,before,after)
        pathlib.Path('/tmp/ng-linux-traffic-trace.json').write_text(json.dumps(trace))
        # No gateway-local test request must appear as another LAN client.
        before,_=counters();run(['curl','--noproxy','*','--silent','--data-binary','x','http://198.19.0.2:8081','-o','/dev/null']);after,raw=counters();assert before==after
        pathlib.Path('/tmp/ng-traffic-lab-counters.json').write_text(json.dumps(raw))
        pathlib.Path('/tmp/ng-route-lab.log').write_text((root/'gateway.log').read_text());print('PASS gateway-local traffic excluded; real nft JSON and route log exported',flush=True)
    finally:
        for child in reversed(children):
            if child.poll() is None:
                child.terminate()
                try:child.wait(timeout=3)
                except subprocess.TimeoutExpired:child.kill();child.wait()

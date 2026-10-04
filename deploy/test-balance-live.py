"""Isolated real-Xray test: latency change, failover, fail-closed, recovery.

Uses loopback VLESS servers and HTTP fixtures; does not touch the home gateway.
"""
import http.client
import http.server
import json
import os
import pathlib
import socket
import struct
import subprocess
import tempfile
import threading
import time

XRAY='/usr/local/bin/xray'
UUID='a4d22397-77f8-4e75-96c1-027304162e20'
delays={'a':0.01,'b':0.25}
stopping=threading.Event()

def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1',0));return s.getsockname()[1]

class Origin(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        name=self.server.name
        if self.path=='/probe':time.sleep(delays[name])
        self.send_response(200);self.send_header('Connection','close');self.end_headers()
        try:
            if self.path=='/stream':
                while not stopping.is_set():
                    self.wfile.write((name+'\n').encode());self.wfile.flush();time.sleep(.1)
            else:self.wfile.write(name.encode())
        except (BrokenPipeError,ConnectionResetError):pass

processes=[]
def launch(folder,name,config):
    path=folder/(name+'.json');path.write_text(json.dumps(config));path.chmod(0o600)
    process=subprocess.Popen([XRAY,'run','-c',str(path)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    processes.append(process);time.sleep(.3)
    assert process.poll() is None,name+' did not start'
    return process

def socks(portnum,destination):
    s=socket.create_connection(('127.0.0.1',portnum),3);s.settimeout(3)
    s.sendall(b'\x05\x01\x00');assert s.recv(2)==b'\x05\x00'
    s.sendall(b'\x05\x01\x00\x01'+socket.inet_aton('127.0.0.1')+struct.pack('!H',destination))
    reply=s.recv(10);assert len(reply)>=2 and reply[1]==0
    return s

with tempfile.TemporaryDirectory(prefix='ngpanel-balance-test-') as temporary:
    folder=pathlib.Path(temporary)
    api,socksport,destination=port(),port(),port()
    origins={};server_ports={'a':port(),'b':port()};server_configs={};servers={}
    try:
        for name in ('a','b'):
            origin=http.server.ThreadingHTTPServer(('127.0.0.1',0),Origin);origin.name=name
            origins[name]=origin;threading.Thread(target=origin.serve_forever,daemon=True).start()
            config={'log':{'loglevel':'none'},'inbounds':[{'listen':'127.0.0.1','port':server_ports[name],'protocol':'vless','settings':{'clients':[{'id':UUID}],'decryption':'none'}}], 'outbounds':[{'protocol':'freedom','settings':{'redirect':'127.0.0.1:'+str(origin.server_port)}}]}
            server_configs[name]=config;servers[name]=launch(folder,'server-'+name,config)
        out=[{'tag':'direct','protocol':'freedom'},{'tag':'block','protocol':'blackhole'}]
        for name in ('a','b'):
            out.append({'tag':'auto-vpn-'+name,'protocol':'vless','settings':{'vnext':[{'address':'127.0.0.1','port':server_ports[name],'users':[{'id':UUID,'encryption':'none'}]}]}})
        main=launch(folder,'client',{'log':{'loglevel':'none'},'api':{'tag':'api','listen':'127.0.0.1:'+str(api),'services':['RoutingService']},'inbounds':[{'tag':'socks','listen':'127.0.0.1','port':socksport,'protocol':'socks','settings':{'auth':'noauth'}}],'outbounds':out,'observatory':{'subjectSelector':['auto-vpn-'],'probeUrl':'http://127.0.0.1:'+str(destination)+'/probe','probeInterval':'1s','enableConcurrency':True},'routing':{'rules':[{'type':'field','inboundTag':['socks'],'balancerTag':'auto-vpn'}],'balancers':[{'tag':'auto-vpn','selector':['auto-vpn-'],'fallbackTag':'block','strategy':{'type':'leastPing'}}]}})
        def chosen():
            result=subprocess.run([XRAY,'api','bi','-json','-s=127.0.0.1:'+str(api),'-t=1','auto-vpn'],capture_output=True,text=True,timeout=3,check=True)
            info=json.loads(result.stdout)
            tags=info.get('balancer',{}).get('principleTarget',{}).get('tag',[])
            return tags[0] if tags else ''
        def wait_for(tag):
            deadline=time.monotonic()+20
            while time.monotonic()<deadline:
                if chosen()==tag:return
                time.sleep(.25)
            raise AssertionError('Balancer did not select '+repr(tag))
        def request():
            connection=http.client.HTTPConnection('127.0.0.1',destination,timeout=3)
            connection.sock=socks(socksport,destination)
            connection.request('GET','/identity');response=connection.getresponse()
            assert response.status==200
            body=response.read().decode();connection.close();return body
        wait_for('auto-vpn-a');assert request()=='a'
        stream=socks(socksport,destination);stream.sendall(b'GET /stream HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n')
        received=[];stream_errors=[]
        def read_stream():
            try:
                while not stopping.is_set():
                    data=stream.recv(4096)
                    if not data:raise RuntimeError('Existing connection closed')
                    received.append(data)
            except Exception as e:
                if not stopping.is_set():stream_errors.append(str(e))
        reader=threading.Thread(target=read_stream,daemon=True);reader.start();time.sleep(.4)
        initial=len(received)
        delays['a']=.35;delays['b']=.01
        wait_for('auto-vpn-b');assert request()=='b';time.sleep(.4)
        assert len(received)>initial and not stream_errors,'Live connection lost during latency switch'
        assert main.poll() is None
        print('Latency switch: A -> B; existing A stream remains open')
        servers['b'].terminate();servers['b'].wait(timeout=5)
        wait_for('auto-vpn-a');assert request()=='a'
        assert not stream_errors,'Unrelated live A connection was interrupted'
        print('Failed B: new connections use A, same Xray PID')
        stopping.set();stream.close();reader.join(timeout=4)
        servers['a'].terminate();servers['a'].wait(timeout=5)
        wait_for('')
        try:request()
        except (OSError,AssertionError,http.client.HTTPException):pass
        else:raise AssertionError('All VPNs down but traffic escaped block fallback')
        print('All VPNs down: traffic blocked')
        servers['a']=launch(folder,'server-a-recovered',server_configs['a'])
        wait_for('auto-vpn-a');assert request()=='a'
        assert main.poll() is None
        print('Recovery: A selected automatically, client PID '+str(main.pid)+' unchanged')
    finally:
        stopping.set()
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try:process.wait(timeout=5)
                except subprocess.TimeoutExpired:process.kill();process.wait()
        for origin in origins.values():origin.shutdown();origin.server_close()

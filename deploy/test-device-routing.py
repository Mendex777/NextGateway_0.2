#!/usr/bin/python3
"""Exercise source-address rules in a second temporary Xray, without gateway changes."""
import json, pathlib, socket, subprocess, tempfile, threading, time
server=socket.socket();server.bind(('127.0.0.1',0));server.listen();destination=server.getsockname()[1]
def serve():
    while True:
        try:client,_=server.accept()
        except OSError:return
        with client:
            client.settimeout(3)
            try:client.recv(4096);client.sendall(b'HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nOK')
            except OSError:pass
threading.Thread(target=serve,daemon=True).start()
probe=socket.socket();probe.bind(('127.0.0.1',0));port=probe.getsockname()[1];probe.close()
config={'log':{'loglevel':'error'},'inbounds':[{'listen':'127.0.0.1','port':port,'protocol':'socks','settings':{'auth':'noauth'}}],'outbounds':[{'tag':'direct','protocol':'freedom'},{'tag':'block','protocol':'blackhole'}],'routing':{'rules':[{'type':'field','source':['127.0.0.2'],'outboundTag':'block'},{'type':'field','network':'tcp,udp','outboundTag':'direct'}]}}
def request(source):
    with socket.socket() as client:
        client.settimeout(3);client.bind((source,0));client.connect(('127.0.0.1',port))
        client.sendall(b'\x05\x01\x00');assert client.recv(2)==b'\x05\x00'
        client.sendall(b'\x05\x01\x00\x01'+socket.inet_aton('127.0.0.1')+destination.to_bytes(2,'big'))
        response=client.recv(10)
        if len(response)<2 or response[1]!=0:return b''
        client.sendall(b'GET / HTTP/1.0\r\n\r\n')
        try:return client.recv(4096)
        except OSError:return b''
with tempfile.TemporaryDirectory() as folder:
    path=pathlib.Path(folder)/'config.json';path.write_text(json.dumps(config))
    process=subprocess.Popen(['/usr/local/bin/xray','run','-c',str(path)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    try:
        for _ in range(30):
            try:
                with socket.create_connection(('127.0.0.1',port),timeout=.2):break
            except OSError:time.sleep(.1)
        assert b'200 OK' in request('127.0.0.1'), 'Unrelated device blocked'
        assert not request('127.0.0.2'), 'Device source rule did not block'
        print('Actual Xray: selected source blocked; unrelated source works')
    finally:
        process.terminate();process.wait(timeout=5);server.close()

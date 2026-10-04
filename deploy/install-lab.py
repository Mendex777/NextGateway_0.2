#!/usr/bin/python3
"""Isolated, unprivileged 3x-ui reference installation; no host networking."""
import hashlib, io, json, pathlib, secrets, shutil, subprocess, tarfile, urllib.request
def run(*args): subprocess.run(args, check=True)
release=json.load(urllib.request.urlopen('https://api.github.com/repos/MHSanaei/3x-ui/releases/latest',timeout=30))
asset=next(a for a in release['assets'] if a['name']=='x-ui-linux-amd64.tar.gz')
data=urllib.request.urlopen(asset['browser_download_url'],timeout=120).read()
assert asset.get('digest')=='sha256:'+hashlib.sha256(data).hexdigest(), 'Release checksum mismatch'
base=pathlib.Path('/opt/3xui-lab'); base.mkdir(exist_ok=True)
with tarfile.open(fileobj=io.BytesIO(data)) as archive:
    for item in archive.getmembers():
        relative=pathlib.PurePosixPath(item.name)
        assert not relative.is_absolute() and '..' not in relative.parts
        assert item.isfile() or item.isdir(), 'Archive links refused'
    archive.extractall(base,filter='data')
if subprocess.run(['id','ng3xui'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:
    run('useradd','--system','--home','/var/lib/3xui-lab','--shell','/usr/sbin/nologin','ng3xui')
state=pathlib.Path('/var/lib/3xui-lab'); state.mkdir(exist_ok=True)
shutil.copytree(base/'x-ui/bin',state/'bin',dirs_exist_ok=True)
for folder in ('db','log'): (state/folder).mkdir(exist_ok=True)
run('chown','-R','ng3xui:ng3xui',str(state))
password=secrets.token_urlsafe(18)
env={'XUI_DB_FOLDER':str(state/'db'),'XUI_BIN_FOLDER':str(state/'bin'),'XUI_LOG_FOLDER':str(state/'log'),'XUI_ENABLE_FAIL2BAN':'false'}
import os
subprocess.run(['runuser','-u','ng3xui','--',str(base/'x-ui/x-ui'),'setting','-username','lab','-password',password,'-port','2053','-webBasePath','/'],env={**os.environ,**env},check=True,stdout=subprocess.DEVNULL)
credentials=pathlib.Path('/home/tdcadmin/3xui-lab-credentials.txt')
credentials.write_text('http://192.168.1.84:2053/\nUsername: lab\nPassword: '+password+'\n');credentials.chmod(0o600)
run('chown','tdcadmin:tdcadmin',str(credentials))
unit='''[Unit]
Description=Isolated 3x-ui reference lab
[Service]
User=ng3xui
WorkingDirectory=/var/lib/3xui-lab
ExecStart=/opt/3xui-lab/x-ui/x-ui
PrivateNetwork=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/3xui-lab
InaccessiblePaths=/etc/ngpanel /var/lib/ngpanel
NoNewPrivileges=yes
CapabilityBoundingSet=
MemoryMax=512M
CPUQuota=50%
Restart=on-failure
'''+''.join('Environment='+k+'='+v+'\n' for k,v in env.items())+'''[Install]
WantedBy=multi-user.target
'''
pathlib.Path('/etc/systemd/system/3xui-lab.service').write_text(unit)
pathlib.Path('/etc/systemd/system/3xui-lab-proxy.socket').write_text('''[Socket]
ListenStream=192.168.1.84:2053
[Install]
WantedBy=sockets.target
''')
pathlib.Path('/etc/systemd/system/3xui-lab-proxy.service').write_text('''[Unit]
Requires=3xui-lab.service
After=3xui-lab.service
JoinsNamespaceOf=3xui-lab.service
[Service]
User=ng3xui
ExecStart=/usr/lib/systemd/systemd-socket-proxyd 127.0.0.1:2053
PrivateNetwork=yes
PrivateTmp=yes
NoNewPrivileges=yes
CapabilityBoundingSet=
''')
run('systemctl','daemon-reload')
run('systemctl','enable','--now','3xui-lab.service','3xui-lab-proxy.socket')
print('Installed isolated reference:',release['tag_name'])

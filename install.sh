#!/bin/sh
# Install or repair the complete NGPanel service, preserving existing settings.
set -eu
[ "$(id -u)" = 0 ] || { echo "Run this installer as root (sudo bash)." >&2; exit 1; }
. /etc/os-release
[ "$ID" = ubuntu ] || { echo "Supported OS: Ubuntu" >&2; exit 1; }
[ "$(dpkg --print-architecture)" = amd64 ] || { echo "Beta supports amd64" >&2; exit 1; }
exec 9>/run/ngpanel-installer.lock
flock -n 9 || { echo "Another NGPanel installer is running." >&2; exit 1; }
echo "[1/5] Checking system and downloading the verified stable release"
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates python3
umask 077
NG_INSTALL_STAGE=$(mktemp -d /tmp/ngpanel-install.XXXXXX)
trap 'rm -rf "$NG_INSTALL_STAGE"' EXIT HUP INT TERM
python3 - "$NG_INSTALL_STAGE" <<'PY'
import hashlib,io,json,pathlib,re,tarfile,urllib.request,sys,subprocess
repo='Mendex777/NextGateway_0.2'
stage=pathlib.Path(sys.argv[1])
def download(url,limit):
    request=urllib.request.Request(url,headers={'User-Agent':'NGPanel-installer'})
    with urllib.request.urlopen(request,timeout=90) as response:
        data=response.read(limit+1)
    if len(data)>limit: raise ValueError('Download exceeds size limit')
    return data
release=json.loads(download('https://api.github.com/repos/'+repo+'/releases/latest',2*1024*1024))
tag=release['tag_name']
if release.get('draft') or release.get('prerelease') or not re.fullmatch(r'v[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}',tag):
    raise ValueError('Invalid stable release')
def asset(name,limit):
    matches=[a for a in release['assets'] if a['name']==name]
    if len(matches)!=1: raise ValueError('Release asset missing: '+name)
    item=matches[0]
    digest=item.get('digest','')
    if not re.fullmatch(r'sha256:[0-9a-f]{64}',digest): raise ValueError('GitHub SHA-256 unavailable')
    url=item['browser_download_url']
    if url!='https://github.com/'+repo+'/releases/download/'+tag+'/'+name: raise ValueError('Unexpected asset URL')
    data=download(url,limit)
    if hashlib.sha256(data).hexdigest()!=digest[7:]: raise ValueError('Asset checksum mismatch')
    return data
archive=asset('ngpanel-linux-amd64.tar.gz',40*1024*1024)
installer=asset('ngpanel-install.sh',128*1024)
files={'ngpanel','web.html','ui.js','control.py','install-xray.py','index-geodata.py','update-geodata.py','update-panel.py','manifest.json'}
found=set()
with tarfile.open(fileobj=io.BytesIO(archive),mode='r:gz') as bundle:
    for item in bundle:
        if item.name not in files or item.name in found or not item.isfile() or item.size>40*1024*1024:
            raise ValueError('Invalid archive member')
        found.add(item.name)
        (stage/item.name).write_bytes(bundle.extractfile(item).read())
if found!=files: raise ValueError('Incomplete release archive')
manifest=json.loads((stage/'manifest.json').read_text())
if manifest.get('schema')!=1 or manifest.get('version')!=tag: raise ValueError('Invalid release manifest')
for name in files-{'manifest.json'}:
    if hashlib.sha256((stage/name).read_bytes()).hexdigest()!=manifest.get('sha256',{}).get(name):
        raise ValueError('File checksum mismatch')
(stage/'ngpanel').chmod(0o755)
version=subprocess.check_output([str(stage/'ngpanel'),'--version'],text=True,timeout=10).split()
if len(version)<2 or version[1]!=tag: raise ValueError('Binary version mismatch')
(stage/'deploy').mkdir()
for name in files:
    if name.endswith('.py'): (stage/name).rename(stage/'deploy'/name)
(stage/'deploy'/'install-panel.sh').write_bytes(installer)
print('Verified stable release: '+tag,flush=True)
PY
NG_PREBUILT=1 sh "$NG_INSTALL_STAGE/deploy/install-panel.sh"
echo "NGPanel installation completed."

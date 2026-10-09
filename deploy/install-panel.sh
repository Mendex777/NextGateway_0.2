#!/bin/sh
set -eu
test "$(id -u)" = 0
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
cd "$PROJECT_DIR"
. /etc/os-release
[ "$ID" = ubuntu ] || { echo "Supported OS: Ubuntu" >&2; exit 1; }
[ "$(dpkg --print-architecture)" = amd64 ] || { echo "Beta supports amd64" >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive
echo "[2/5] Installing service dependencies"
apt-get update
if [ "${NG_PREBUILT:-0}" = 1 ]; then
 apt-get install -y --no-install-recommends ca-certificates python3
else
 apt-get install -y --no-install-recommends ca-certificates golang-go build-essential python3
fi
apt-get install -y --no-install-recommends nftables iproute2 curl avahi-utils ieee-data
umask 077
if [ "${NG_PREBUILT:-0}" != 1 ]; then
test -f frontend/dist/index.html || { echo "Build frontend first: npm ci --prefix frontend && npm run build --prefix frontend" >&2; exit 1; }
PANEL_VERSION=${PANEL_VERSION:-$(cat VERSION)}
PANEL_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo source)
go build -trimpath -ldflags "-X main.panelVersion=$PANEL_VERSION -X main.panelCommit=$PANEL_COMMIT" -o ngpanel .
fi
if [ -f /etc/systemd/system/ngpanel.service ]; then
 NG_LISTEN=$(systemctl show ngpanel --property=Environment --value | python3 -c 'import shlex,sys; print(next((v.split("=",1)[1] for v in shlex.split(sys.stdin.read()) if v.startswith("NG_LISTEN=")),"0.0.0.0:8080"))')
fi
NG_LISTEN=${NG_LISTEN:-0.0.0.0:8080}
case "$NG_LISTEN" in *[!a-zA-Z0-9.:_-]*|"") echo "Invalid NG_LISTEN" >&2; exit 1;; esac
for NG_JOB in ngpanel-control.service ngpanel-update.service ngpanel-install.service; do
 if systemctl is-active --quiet "$NG_JOB"; then
  echo "NGPanel is busy: $NG_JOB. Retry after the operation completes." >&2; exit 1
 fi
done
if ! systemctl is-active --quiet ngpanel; then
 python3 - "$NG_LISTEN" <<'PY'
import socket,sys
host,port=sys.argv[1].rsplit(':',1)
with socket.socket(socket.AF_INET,socket.SOCK_STREAM) as sock:
    try:sock.bind((host,int(port)))
    except OSError:raise RuntimeError('Panel port is occupied: '+sys.argv[1])
PY
fi
if [ -x /opt/ngpanel/ngpanel ]; then
 python3 - <<'PY'
import re,subprocess
def version(binary):
    value=subprocess.check_output([binary,'--version'],text=True).split()[1]
    if not re.fullmatch(r'v\d+\.\d+\.\d+',value):raise ValueError('Invalid panel version')
    return value,tuple(map(int,value[1:].split('.')))
old,a=version('/opt/ngpanel/ngpanel');new,b=version('./ngpanel')
if a>b:raise RuntimeError('Installed '+old+' is newer than '+new+'; downgrade refused')
print('NGPanel '+old+' already installed. '+('Checking and repairing components.' if a==b else 'Updating to '+new+'. Settings will be preserved.'),flush=True)
PY
fi
if [ -f /etc/systemd/system/xray.service ] && ! grep -q 'Xray managed by NGPanel' /etc/systemd/system/xray.service; then
 echo "Existing unrelated xray.service; installation refused" >&2; exit 1
fi
echo "[3/5] Saving existing installation and installing panel services"
NG_INSTALL_BACKUP=$(mktemp -d /var/tmp/ngpanel-before-install.XXXXXX)
export NG_INSTALL_BACKUP
python3 - <<'PY'
import os,pathlib,shutil,sqlite3
saved=pathlib.Path(os.environ['NG_INSTALL_BACKUP'])
app=pathlib.Path('/opt/ngpanel')
database=pathlib.Path('/var/lib/ngpanel/db/panel.db')
if app.exists():shutil.copytree(app,saved/'app')
units=list(pathlib.Path('/etc/systemd/system').glob('ngpanel*'))
units.append(pathlib.Path('/etc/systemd/system/xray.service'))
(saved/'units').mkdir()
for unit in units:
    if unit.is_file() and not unit.is_symlink():shutil.copy2(unit,saved/'units'/unit.name)
if database.exists():
    with sqlite3.connect('file:'+str(database)+'?mode=ro',uri=True) as src,sqlite3.connect(saved/'panel.db') as dst:src.backup(dst)
PY
rollback_install() {
 echo "Installation failed. Saved configuration: $NG_INSTALL_BACKUP" >&2
 if [ -f "$NG_INSTALL_BACKUP/app/ngpanel" ] && [ -f "$NG_INSTALL_BACKUP/panel.db" ]; then
  systemctl stop ngpanel || true
  cp -a "$NG_INSTALL_BACKUP/app/." /opt/ngpanel/
  cp -a "$NG_INSTALL_BACKUP/units/." /etc/systemd/system/
  rm -f /var/lib/ngpanel/db/panel.db-wal /var/lib/ngpanel/db/panel.db-shm
  install -m 600 -o ngpanel -g ngpanel "$NG_INSTALL_BACKUP/panel.db" /var/lib/ngpanel/db/panel.db
  systemctl daemon-reload
  systemctl start ngpanel || true
  echo "Previous panel and database restored." >&2
 fi
}
trap 'rollback_install' EXIT
trap 'exit 1' HUP INT TERM
id ngpanel >/dev/null 2>&1 || useradd --system --home /var/lib/ngpanel --shell /usr/sbin/nologin ngpanel
id ngxray >/dev/null 2>&1 || useradd --system --home /var/lib/ngxray --shell /usr/sbin/nologin ngxray
install -d -m 755 /opt/ngpanel
install -d -m 700 -o ngpanel -g ngpanel /var/lib/ngpanel
chown root:ngpanel /var/lib/ngpanel
chmod 750 /var/lib/ngpanel
install -d -m 700 -o ngpanel -g ngpanel /var/lib/ngpanel/jobs
install -d -m 700 -o ngpanel -g ngpanel /var/lib/ngpanel/db
install -m 755 deploy/install-xray.py /opt/ngpanel/install-xray.py
install -m 755 deploy/control.py /opt/ngpanel/control.py
install -m 755 deploy/index-geodata.py /opt/ngpanel/index-geodata.py
install -m 755 deploy/update-geodata.py /opt/ngpanel/update-geodata.py
install -m 755 deploy/update-panel.py /opt/ngpanel/update-panel.py
install -d -m 750 -o root -g ngxray /etc/ngpanel
install -d -m 750 -o ngxray -g ngxray /var/lib/ngxray
install -m 755 ngpanel /opt/ngpanel/ngpanel
install -m 644 web.html /opt/ngpanel/web.html
install -m 644 ui.js /opt/ngpanel/ui.js
cat > /etc/systemd/system/ngpanel.service <<EOF
[Unit]
Description=NGPanel web management beta
After=network-online.target
Wants=network-online.target
[Service]
User=ngpanel
Group=ngpanel
WorkingDirectory=/opt/ngpanel
Environment=NG_LISTEN=$NG_LISTEN
Environment=NG_DB=/var/lib/ngpanel/db/panel.db
ExecStart=/opt/ngpanel/ngpanel
Restart=on-failure
UMask=0077
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/ngpanel
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-install.path <<'EOF'
[Unit]
Description=NGPanel Xray install request
[Path]
PathExists=/var/lib/ngpanel/jobs/install.request
Unit=ngpanel-install.service
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-install.service <<'EOF'
[Unit]
Description=Install official Xray binary for NGPanel
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /opt/ngpanel/install-xray.py
TimeoutStartSec=180
EOF
cat > /etc/systemd/system/xray.service <<'EOF'
[Unit]
Description=Xray managed by NGPanel
After=network-online.target
Wants=network-online.target
ConditionPathExists=/etc/ngpanel/config.json
[Service]
User=ngxray
Group=ngxray
ExecStart=/usr/local/bin/xray run -c /etc/ngpanel/config.json
Environment=XRAY_LOCATION_ASSET=/usr/local/share/ngpanel-geodata
Restart=on-failure
RestartSec=2
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
UMask=0077
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-control.path <<'EOF'
[Unit]
Description=NGPanel system request
[Path]
PathExists=/var/lib/ngpanel/jobs/control.request
Unit=ngpanel-control.service
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-control.service <<'EOF'
[Unit]
Description=NGPanel fixed system operations
StartLimitIntervalSec=0
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /opt/ngpanel/control.py
TimeoutStartSec=300
EOF
cat > /etc/systemd/system/ngpanel-gateway.service <<'EOF'
[Unit]
Description=Restore NGPanel TPROXY rules
After=network-online.target xray.service
Wants=network-online.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/python3 /opt/ngpanel/control.py gateway-restore
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-update.path <<'EOF'
[Unit]
Description=NGPanel release update request
[Path]
PathExists=/var/lib/ngpanel/jobs/panel-update.request
Unit=ngpanel-update.service
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/ngpanel-update.service <<'EOF'
[Unit]
Description=NGPanel fixed release update
StartLimitIntervalSec=0
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /opt/ngpanel/update-panel.py
TimeoutStartSec=300
EOF
systemctl daemon-reload
systemctl enable --now ngpanel ngpanel-install.path ngpanel-control.path ngpanel-update.path
systemctl restart ngpanel

echo "[4/5] Preparing Xray, geo databases and gateway"
python3 - "$NG_LISTEN" <<'PY'
import ipaddress,json,pathlib,sqlite3,subprocess,sys,time,urllib.request,urllib.parse
def run(args):subprocess.run(args,check=True)
listen=sys.argv[1];host,port=listen.rsplit(':',1)
origin='http://'+('127.0.0.1' if host=='0.0.0.0' else host)+':'+port
expected=subprocess.check_output(['/opt/ngpanel/ngpanel','--version'],text=True).split()[1]
def healthy():
    for _ in range(30):
        try:
            with urllib.request.urlopen(origin+'/health',timeout=2) as response:state=json.load(response)
            if state.get('version')==expected:return
        except Exception:time.sleep(1)
    raise RuntimeError('Panel did not pass startup check')
healthy()
if not pathlib.Path('/usr/local/bin/xray').exists():
    run(['systemctl','stop','xray'])
    run(['python3','/opt/ngpanel/install-xray.py'])
run(['/usr/local/bin/xray','version'])
geo=pathlib.Path('/usr/local/share/ngpanel-geodata')
if not all((geo/name).is_file() for name in ('geoip.dat','geosite.dat')):run(['python3','/opt/ngpanel/update-geodata.py'])
healthy()
database='/var/lib/ngpanel/db/panel.db'
with sqlite3.connect(database) as db:
    settings=dict(db.execute('SELECT key,value FROM settings'))
    # Only an installation without an applied config needs its initial gateway setup.
    initial=not pathlib.Path('/etc/ngpanel/config.json').exists()
    if initial:
        if not settings.get('gateway_network'):
            routes=json.loads(subprocess.check_output(['ip','-j','-4','route','show','default']))
            route=min((r for r in routes if r.get('gateway') and r.get('dev')!='lo'),key=lambda r:r.get('metric',0))
            links=json.loads(subprocess.check_output(['ip','-j','-4','addr','show','dev',route['dev']]))
            address=next(a for link in links for a in link.get('addr_info',[]) if ipaddress.ip_address(route['gateway']) in ipaddress.ip_network(str(a['local'])+'/'+str(a['prefixlen']),strict=False))
            network=dict(interface=route['dev'],address=address['local'],cidr=str(ipaddress.ip_network(str(address['local'])+'/'+str(address['prefixlen']),strict=False)),router=route['gateway'])
            db.execute("INSERT OR REPLACE INTO settings VALUES('gateway_network',?)",(json.dumps(network),))
        db.execute("INSERT OR REPLACE INTO settings VALUES('gateway_enabled','1')")
    db.execute("INSERT OR REPLACE INTO settings VALUES('setup_skipped','1')")
if initial:
    request=urllib.request.Request(origin+'/action',data=urllib.parse.urlencode({'action':'apply'}).encode(),headers={'Origin':origin,'Accept':'application/json'})
    with urllib.request.urlopen(request,timeout=30) as response:
        result=json.load(response)
    if not result.get('ok'):raise RuntimeError(result.get('message','Initial configuration failed'))
    runtime=pathlib.Path('/var/lib/ngpanel/runtime.json')
    for _ in range(90):
        state=json.loads(runtime.read_text()) if runtime.exists() else {}
        if state.get('Action')=='apply' and state.get('Updated','')>=result.get('since','') and state.get('State') in ('ok','error'):
            if state['State']!='ok':raise RuntimeError(state.get('Message','Gateway initialization failed'))
            break
        time.sleep(1)
    else:raise RuntimeError('Gateway startup timed out')
else:
    run(['systemctl','enable','--now','xray','ngpanel-gateway'])
run(['systemctl','is-active','--quiet','ngpanel','xray'])
healthy()
with sqlite3.connect(database) as db:
    network=dict(db.execute('SELECT key,value FROM settings')).get('gateway_network','{}')
address=json.loads(network).get('address')
print('[5/5] NGPanel ready: http://'+(address or host)+':'+port+'/',flush=True)
PY
trap - EXIT HUP INT TERM
echo "Saved configuration backup: $NG_INSTALL_BACKUP"

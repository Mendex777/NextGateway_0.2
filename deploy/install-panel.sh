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
apt-get update
if [ "${NG_PREBUILT:-0}" = 1 ]; then
 apt-get install -y --no-install-recommends ca-certificates python3
else
 apt-get install -y --no-install-recommends ca-certificates golang-go build-essential python3
fi
umask 077
if [ "${NG_PREBUILT:-0}" != 1 ]; then
test -f frontend/dist/index.html || { echo "Build frontend first: npm ci --prefix frontend && npm run build --prefix frontend" >&2; exit 1; }
PANEL_VERSION=${PANEL_VERSION:-$(cat VERSION)}
PANEL_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo source)
go build -trimpath -ldflags "-X main.panelVersion=$PANEL_VERSION -X main.panelCommit=$PANEL_COMMIT" -o ngpanel .
fi
NG_LISTEN=${NG_LISTEN:-0.0.0.0:8080}
case "$NG_LISTEN" in *[!a-zA-Z0-9.:_-]*|"") echo "Invalid NG_LISTEN" >&2; exit 1;; esac
if [ -f /etc/systemd/system/xray.service ] && ! grep -q 'Xray managed by NGPanel' /etc/systemd/system/xray.service; then
 echo "Existing unrelated xray.service; installation refused" >&2; exit 1
fi
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

echo "NGPanel ready: http://<VM-IPv4>:${NG_LISTEN##*:}/ — install gateway components from the web panel. Xray and interception have not been started by this installer."

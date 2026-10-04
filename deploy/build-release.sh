#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
VERSION=${1:-$(cat VERSION)}
COMMIT=${SOURCE_COMMIT:-$(git rev-parse --short HEAD)}
mkdir -p dist
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.panelVersion=$VERSION -X main.panelCommit=$COMMIT" -o "$STAGE/ngpanel" .
cp web.html ui.js "$STAGE/"
for file in control.py install-xray.py index-geodata.py update-geodata.py update-panel.py; do cp "deploy/$file" "$STAGE/$file"; done
python3 - "$STAGE" "$VERSION" "$COMMIT" <<'PY'
import hashlib,json,pathlib,sys
folder=pathlib.Path(sys.argv[1]);files=sorted(p.name for p in folder.iterdir())
manifest={'schema':1,'version':sys.argv[2],'commit':sys.argv[3],'sha256':{n:hashlib.sha256((folder/n).read_bytes()).hexdigest() for n in files}}
(folder/'manifest.json').write_text(json.dumps(manifest,sort_keys=True))
PY
tar -czf dist/ngpanel-linux-amd64.tar.gz -C "$STAGE" ngpanel web.html ui.js control.py install-xray.py index-geodata.py update-geodata.py update-panel.py manifest.json
sha256sum dist/ngpanel-linux-amd64.tar.gz

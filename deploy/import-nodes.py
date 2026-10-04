"""Import a JSON list of VLESS URIs from stdin without echoing credentials."""
import json
import sys
import urllib.parse
import urllib.request
base = 'http://192.168.1.84:8080'
links = json.load(sys.stdin)
for uri in links:
    request = urllib.request.Request(base + '/action', data=urllib.parse.urlencode({'action':'manual-node','tab':'nodes','uri':uri}).encode(), headers={'Origin':base})
    with urllib.request.urlopen(request) as response:
        if 'Ошибка:' in response.read().decode():
            raise RuntimeError('Import failed')
print('Imported VLESS nodes:', len(links))

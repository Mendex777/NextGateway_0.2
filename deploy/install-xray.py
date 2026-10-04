#!/usr/bin/python3
"""Fixed privileged job: install official Xray amd64 binary, without networking changes."""
import hashlib, io, json, os, pathlib, subprocess, urllib.request, zipfile
import fcntl

status = pathlib.Path('/var/lib/ngpanel/install-status')
request = pathlib.Path('/var/lib/ngpanel/jobs/install.request')
def report(text):
    # Replace atomically in a root-owned directory; never follow a panel-created symlink.
    tmp = status.with_suffix('.tmp')
    tmp.write_text(text, encoding='utf-8')
    tmp.chmod(0o644)
    os.replace(tmp, status)
def download(url, limit):
    req = urllib.request.Request(url, headers={'User-Agent': 'NGPanel/0.1'})
    with urllib.request.urlopen(req, timeout=60) as response:
        data = response.read(limit + 1)
    if len(data) > limit:
        raise ValueError('Download exceeds size limit')
    return data
try:
    lock = open('/run/ngpanel-control.lock', 'w')
    fcntl.flock(lock, fcntl.LOCK_EX)
    if subprocess.run(['systemctl','is-active','--quiet','xray']).returncode == 0:
        raise ValueError('Сначала остановите Xray; обновление работающего ядра отключено')
    report('Получение официального релиза Xray...')
    release = json.loads(download('https://api.github.com/repos/XTLS/Xray-core/releases/latest', 2 * 1024 * 1024))
    asset = next(a for a in release['assets'] if a['name'] == 'Xray-linux-64.zip')
    digest = asset.get('digest', '')
    if not digest.startswith('sha256:') or len(digest) != 71:
        raise ValueError('Release does not publish a SHA256 digest; installation refused')
    url = asset['browser_download_url']
    if not url.startswith('https://github.com/XTLS/Xray-core/releases/download/'):
        raise ValueError('Unexpected release URL')
    data = download(url, 100 * 1024 * 1024)
    if hashlib.sha256(data).hexdigest() != digest[7:]:
        raise ValueError('SHA256 mismatch')
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        info = archive.getinfo('xray')
        if info.file_size > 150 * 1024 * 1024:
            raise ValueError('Binary too large')
        binary = archive.read(info)
    target = pathlib.Path('/usr/local/bin/xray')
    tmp = target.with_suffix('.ng-new')
    tmp.write_bytes(binary)
    tmp.chmod(0o755)
    subprocess.run([str(tmp), 'version'], check=True, timeout=15, capture_output=True)
    os.replace(tmp, target)
    report('Установлен Xray ' + release['tag_name'] + '.')
except Exception as exc:
    report('Ошибка установки: ' + str(exc))
    raise
finally:
    request.unlink(missing_ok=True)

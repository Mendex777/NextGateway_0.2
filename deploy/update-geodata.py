#!/usr/bin/python3
"""Verified geodata update; invoked under the controller's exclusive lock."""
import datetime, hashlib, json, os, pathlib, shutil, sqlite3, subprocess, tempfile, urllib.request

CURRENT=pathlib.Path('/usr/local/share/ngpanel-geodata')
VERSIONS=pathlib.Path('/usr/local/share/ngpanel-geodata-versions')
def download(url,limit):
    with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'NGPanel-beta'}),timeout=45) as response:
        data=response.read(limit+1)
    if len(data)>limit:raise ValueError('Geo download exceeds size limit')
    return data
def command(args,**kwargs):return subprocess.run(args,check=True,timeout=45,capture_output=True,text=True,**kwargs)
def activate(path):
    link=CURRENT.with_name(CURRENT.name+'.new')
    link.unlink(missing_ok=True);link.symlink_to(path,target_is_directory=True);os.replace(link,CURRENT)
def update():
    VERSIONS.mkdir(exist_ok=True)
    stage=pathlib.Path(tempfile.mkdtemp(prefix='release-',dir=VERSIONS));stage.chmod(0o755)
    installed=False
    try:
        release=json.loads(download('https://api.github.com/repos/Loyalsoldier/v2ray-rules-dat/releases/latest',2*1024*1024))
        for name in ('geoip.dat','geosite.dat'):
            asset=next(a for a in release['assets'] if a['name']==name)
            url=asset['browser_download_url']
            if not url.startswith('https://github.com/Loyalsoldier/v2ray-rules-dat/releases/download/'):
                raise ValueError('Unexpected geodata source')
            content=download(url,64*1024*1024)
            if asset.get('digest')!='sha256:'+hashlib.sha256(content).hexdigest():
                raise ValueError('Geo SHA-256 mismatch; old databases preserved')
            (stage/name).write_bytes(content)
        command(['/usr/bin/python3','/opt/ngpanel/index-geodata.py',str(stage),str(stage)])
        categories=json.loads((stage/'index.json').read_text())
        tokens={c['Kind']+':'+c['Code'] for c in categories if c['Count']>0}
        # Validate saved rules as well as applied and rollback configs.
        with sqlite3.connect('file:/var/lib/ngpanel/db/panel.db?mode=ro',uri=True) as db:
            for value, in db.execute('SELECT value FROM rules'):
                for token in value.replace(',','\n').splitlines():
                    token=token.strip()
                    if token.startswith(('geosite:','geoip:')) and token not in tokens:
                        raise ValueError('Базы не заменены: сохранённая категория отсутствует: '+token[:160])
        environment={**os.environ,'XRAY_LOCATION_ASSET':str(stage)}
        for filename in ('config.json','previous.json'):
            config=pathlib.Path('/etc/ngpanel')/filename
            if not config.exists():continue
            # previous.json may be root-only; use a bounded, group-readable test copy.
            candidate=pathlib.Path('/etc/ngpanel/geodata-candidate.json')
            import pwd
            candidate.write_bytes(config.read_bytes());candidate.chmod(0o640);os.chown(candidate,0,pwd.getpwnam('ngxray').pw_gid)
            try:
                result=subprocess.run(['runuser','-u','ngxray','--','/usr/local/bin/xray','run','-test','-c',str(candidate)],env=environment,timeout=45,capture_output=True)
                if result.returncode:raise ValueError('Новые geo-базы несовместимы с применённой или предыдущей конфигурацией; старые базы сохранены')
            finally:candidate.unlink(missing_ok=True)
        (stage/'source.txt').write_text('Loyalsoldier/v2ray-rules-dat '+release['tag_name']+'; SHA-256 verified; '+datetime.datetime.now(datetime.timezone.utc).isoformat())
        if CURRENT.is_symlink():previous=CURRENT.resolve()
        elif CURRENT.exists():
            previous=VERSIONS/'initial-bundle'
            if previous.exists():raise ValueError('Initial geodata backup already exists')
            CURRENT.rename(previous)
            activate(previous)
        else:previous=None
        running=subprocess.run(['systemctl','is-active','--quiet','xray']).returncode==0
        try:
            activate(stage)
            if running:
                command(['systemctl','restart','xray'])
                import time
                time.sleep(2)
                command(['systemctl','is-active','--quiet','xray'])
        except Exception:
            if previous:activate(previous)
            else:CURRENT.unlink(missing_ok=True)
            if running:command(['systemctl','restart','xray'])
            raise ValueError('Обновление отменено; восстановлены предыдущие geo-базы')
        installed=True
        command(['systemctl','restart','ngpanel'])
        print('Geo-базы обновлены: '+release['tag_name']+'. Проверены SHA-256 и совместимость; маршруты сохранены.')
    finally:
        if not installed:shutil.rmtree(stage)
if __name__=='__main__':update()

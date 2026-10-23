#!/usr/bin/python3
"""Verified geodata update; invoked under the controller's exclusive lock."""
import stat, datetime, hashlib, json, os, pathlib, shutil, sqlite3, subprocess, tempfile, urllib.request

CURRENT=pathlib.Path('/usr/local/share/ngpanel-geodata')
VERSIONS=pathlib.Path('/usr/local/share/ngpanel-geodata-versions')
CONFIG_DIR=pathlib.Path('/etc/ngpanel')
def download(url,limit):
    with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'NGPanel-beta'}),timeout=45) as response:
        data=response.read(limit+1)
    if len(data)>limit:raise ValueError('Geo download exceeds size limit')
    return data
def command(args,**kwargs):return subprocess.run(args,check=True,timeout=45,capture_output=True,text=True,**kwargs)
def activate(path):
    link=CURRENT.with_name(CURRENT.name+'.new')
    link.unlink(missing_ok=True);link.symlink_to(path,target_is_directory=True);os.replace(link,CURRENT)
def compatible_rollback(raw):
    """Only disambiguate generated metadata; routing matches and targets stay intact."""
    config=json.loads(raw);rules=config.get('routing',{}).get('rules',[])
    reserved={r.get('ruleTag') for r in rules if isinstance(r.get('ruleTag'),str)};seen=set();changed=False
    for index,rule in enumerate(rules):
        tag=rule.get('ruleTag')
        if not isinstance(tag,str) or not tag:continue
        if tag in seen and tag.startswith(('ngpanel-system:','ngpanel-rule:')):
            candidate=tag+':ngpanel-'+str(index+1)
            while candidate in reserved:candidate+='-'
            rule['ruleTag']=candidate;reserved.add(candidate);changed=True
        seen.add(tag)
    return (json.dumps(config,ensure_ascii=False,separators=(',',':'))+'\n').encode() if changed else raw

def replace_config(path,content,metadata):
    temporary=path.with_name(path.name+'.geodata-new')
    try:
        temporary.write_bytes(content);temporary.chmod(stat.S_IMODE(metadata.st_mode));os.chown(temporary,metadata.st_uid,metadata.st_gid)
        os.replace(temporary,path)
    finally:temporary.unlink(missing_ok=True)

def repair_rollback(path,original,prepared):
    if path.read_bytes()!=original:raise ValueError('Конфигурация отката изменилась во время проверки; повторите обновление')
    metadata=path.stat();backup=path.with_name('previous-before-geodata-tags.json')
    if backup.exists() and backup.read_bytes()!=original:raise ValueError('Сохраните существующую резервную копию previous-before-geodata-tags.json перед повторным ремонтом')
    if not backup.exists():
        with backup.open('xb') as stream:
            os.fchmod(stream.fileno(),0o600);stream.write(original)
    replace_config(path,prepared,metadata)
    return metadata

def update():
    VERSIONS.mkdir(exist_ok=True)
    # The installer uses umask 077; both ngpanel and ngxray need traversal.
    VERSIONS.chmod(0o755)
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
        repairs=[]
        for filename in ('config.json','previous.json'):
            config=CONFIG_DIR/filename
            if not config.exists():continue
            # previous.json may be root-only; use a bounded, group-readable test copy.
            candidate=CONFIG_DIR/'geodata-candidate.json'
            import pwd
            original=config.read_bytes();prepared=compatible_rollback(original) if filename=='previous.json' else original
            candidate.write_bytes(prepared);candidate.chmod(0o640);os.chown(candidate,0,pwd.getpwnam('ngxray').pw_gid)
            try:
                result=subprocess.run(['runuser','-u','ngxray','--','/usr/local/bin/xray','run','-test','-c',str(candidate)],env=environment,timeout=45,capture_output=True)
                if result.returncode:raise ValueError('Новые geo-базы не прошли проверку '+filename+'; прежние базы и конфигурация сохранены')
                if prepared!=original:repairs.append((config,original,prepared))
            finally:candidate.unlink(missing_ok=True)
        (stage/'source.txt').write_text('Loyalsoldier/v2ray-rules-dat '+release['tag_name']+'; SHA-256 verified; '+datetime.datetime.now(datetime.timezone.utc).isoformat())
        (stage/'source.txt').chmod(0o644)
        if CURRENT.is_symlink():previous=CURRENT.resolve()
        elif CURRENT.exists():
            previous=VERSIONS/'initial-bundle'
            if previous.exists():raise ValueError('Initial geodata backup already exists')
            CURRENT.rename(previous)
            activate(previous)
        else:previous=None
        running=subprocess.run(['systemctl','is-active','--quiet','xray']).returncode==0
        repaired=[]
        try:
            for config,original,prepared in repairs:
                metadata=repair_rollback(config,original,prepared);repaired.append((config,original,metadata))
            activate(stage)
            if running:
                command(['systemctl','restart','xray'])
                import time
                time.sleep(2)
                command(['systemctl','is-active','--quiet','xray'])
        except Exception:
            for config,original,metadata in reversed(repaired):replace_config(config,original,metadata)
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

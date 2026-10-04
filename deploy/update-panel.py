#!/usr/bin/python3
"""Fixed public GitHub release updater. Never accepts a URL or shell command from UI."""
import datetime,fcntl,hashlib,io,json,os,pathlib,pwd,re,shlex,shutil,sqlite3,stat,subprocess,tarfile,tempfile,time,urllib.request
ROOT=pathlib.Path('/var/lib/ngpanel')
APP=pathlib.Path('/opt/ngpanel')
DB=ROOT/'db/panel.db'
UPDATES=ROOT/'updates'
PREVIOUS=UPDATES/'previous'
STATUS=ROOT/'panel-update.json'
REQUEST=ROOT/'jobs/panel-update.request'
REPO='Mendex777/NextGateway_0.2'
FILES=('ngpanel','web.html','ui.js','control.py','install-xray.py','index-geodata.py','update-geodata.py','update-panel.py')
ASSET='ngpanel-linux-amd64.tar.gz'

def run(args,**kw):return subprocess.run(args,check=True,timeout=kw.pop('timeout',30),capture_output=True,text=True,**kw)
def atomic(path,data,mode=0o644):
    tmp=path.with_name(path.name+'.tmp');tmp.write_text(json.dumps(data,ensure_ascii=False));tmp.chmod(mode);os.replace(tmp,path)
def report(state,message,**fields):
    current=json.loads(STATUS.read_text()) if STATUS.exists() else {}
    current.update(State=state,Message=message,Updated=datetime.datetime.now(datetime.timezone.utc).isoformat(),CanRollback=PREVIOUS.exists(),**fields)
    atomic(STATUS,current)
def download(url,limit):
    if not url.startswith(('https://api.github.com/repos/'+REPO+'/', 'https://github.com/'+REPO+'/releases/download/')):raise ValueError('Unexpected release URL')
    req=urllib.request.Request(url,headers={'User-Agent':'NGPanel-updater','Accept':'application/vnd.github+json' if 'api.github.com' in url else 'application/octet-stream'})
    with urllib.request.urlopen(req,timeout=60) as r:data=r.read(limit+1)
    if len(data)>limit:raise ValueError('Release exceeds size limit')
    return data

def version(value):
    if not re.fullmatch(r'v[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}',value):raise ValueError('Invalid release version')
    return tuple(map(int,value[1:].split('.')))
def installed():
    text=run([str(APP/'ngpanel'),'--version']).stdout.split()
    if len(text)<2:raise ValueError('Panel version unavailable')
    version(text[1]);return text[1]
def latest():
    data=json.loads(download('https://api.github.com/repos/'+REPO+'/releases/latest',2*1024*1024))
    tag=data['tag_name'];version(tag)
    if data.get('draft') or data.get('prerelease'):raise ValueError('Not a stable release')
    matches=[a for a in data['assets'] if a['name']==ASSET]
    if len(matches)!=1:raise ValueError('Release archive missing')
    asset=matches[0];digest=asset.get('digest','')
    if not re.fullmatch(r'sha256:[0-9a-f]{64}',digest):raise ValueError('GitHub SHA-256 unavailable')
    return tag,asset,digest[7:]

def unpack(data,destination,tag):
    destination.mkdir(mode=0o755)
    found=set()
    with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
        for item in archive:
            if item.name not in (*FILES,'manifest.json') or item.name in found or not item.isfile() or item.size>40*1024*1024:raise ValueError('Invalid archive member')
            content=archive.extractfile(item).read();found.add(item.name)
            (destination/item.name).write_bytes(content)
    if found!=set((*FILES,'manifest.json')):raise ValueError('Incomplete release')
    manifest=json.loads((destination/'manifest.json').read_text())
    if manifest.get('version')!=tag or manifest.get('schema')!=1:raise ValueError('Unsupported manifest')
    for name in FILES:
        path=destination/name
        if hashlib.sha256(path.read_bytes()).hexdigest()!=manifest.get('sha256',{}).get(name):raise ValueError('File checksum mismatch')
        path.chmod(0o755 if name=='ngpanel' or name.endswith('.py') else 0o644)
    text=run(['runuser','-u','ngpanel','--',str(destination/'ngpanel'),'--version']).stdout.split()
    if len(text)<2 or text[1]!=tag:raise ValueError('Binary version mismatch')

def snapshot(destination):
    destination.mkdir(mode=0o700)
    for name in FILES:shutil.copy2(APP/name,destination/name)
    with sqlite3.connect('file:'+str(DB)+'?mode=ro',uri=True) as src,sqlite3.connect(destination/'panel.db') as dst:src.backup(dst)
    (destination/'panel.db').chmod(0o600)

def replace_files(source):
    for name in FILES:
        tmp=APP/(name+'.update-new');shutil.copy2(source/name,tmp);os.chown(tmp,0,0);os.replace(tmp,APP/name)

def restore_db(source):
    target=DB.with_suffix('.restore');shutil.copy2(source/'panel.db',target)
    user=pwd.getpwnam('ngpanel');os.chown(target,user.pw_uid,user.pw_gid);target.chmod(0o600)
    for suffix in ('-wal','-shm'):pathlib.Path(str(DB)+suffix).unlink(missing_ok=True)
    os.replace(target,DB)

def healthy(expected):
    env=run(['systemctl','show','ngpanel','--property=Environment','--value']).stdout
    listen=next((s.split('=',1)[1] for s in shlex.split(env) if s.startswith('NG_LISTEN=')),'127.0.0.1:8080')
    host,port=listen.rsplit(':',1)
    if host=='0.0.0.0':host='127.0.0.1'
    for _ in range(12):
        try:
            with urllib.request.urlopen('http://'+host+':'+port+'/health',timeout=1) as r:data=json.load(r)
            if data.get('version')==expected:return True
        except Exception:pass
        time.sleep(1)
    return False

def install_release(tag,asset,digest):
    report('running','Скачивание и проверка релиза '+tag,Available=False)
    data=download(asset['browser_download_url'],40*1024*1024)
    if hashlib.sha256(data).hexdigest()!=digest:raise ValueError('Release checksum mismatch')
    stage=APP/('.update-'+str(time.time_ns()))
    saved=UPDATES/('backup-'+str(time.time_ns()))
    stopped=False
    try:
        unpack(data,stage,tag)
        run(['systemctl','stop','ngpanel']);stopped=True
        snapshot(saved)
        old=run([str(APP/'ngpanel'),'--version']).stdout.split()[1]
        try:
            replace_files(stage);run(['systemctl','start','ngpanel'])
            if not healthy(tag):raise ValueError('Новая панель не прошла проверку запуска')
        except Exception:
            run(['systemctl','stop','ngpanel']);replace_files(saved);restore_db(saved);run(['systemctl','start','ngpanel'])
            if not healthy(old):raise ValueError('Ошибка отката; резервная копия сохранена в '+str(saved))
            raise ValueError('Обновление отменено; предыдущая панель и база восстановлены')
        if PREVIOUS.exists():shutil.rmtree(PREVIOUS)
        saved.rename(PREVIOUS)
        report('ok','Панель обновлена до '+tag+'; база сохранена',Latest=tag,Available=False)
    finally:
        if stage.exists():shutil.rmtree(stage)
        if stopped and subprocess.run(['systemctl','is-active','--quiet','ngpanel']).returncode:subprocess.run(['systemctl','start','ngpanel'])

def rollback():
    if not PREVIOUS.exists():raise ValueError('Нет предыдущей версии')
    tag=run([str(PREVIOUS/'ngpanel'),'--version']).stdout.split()[1]
    saved=UPDATES/('before-rollback-'+str(time.time_ns()))
    run(['systemctl','stop','ngpanel']);snapshot(saved)
    try:
        replace_files(PREVIOUS);restore_db(PREVIOUS);run(['systemctl','start','ngpanel'])
        if not healthy(tag):raise ValueError('Не удалось запустить предыдущую панель')
    except Exception:
        run(['systemctl','stop','ngpanel']);replace_files(saved);restore_db(saved);run(['systemctl','start','ngpanel']);raise
    report('ok','Панель и база возвращены к '+tag,Available=True)

def main():
    UPDATES.mkdir(mode=0o700,exist_ok=True)
    with open('/run/ngpanel-control.lock','w') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        fd=os.open(REQUEST,os.O_RDONLY|os.O_NOFOLLOW)
        with os.fdopen(fd,'rb') as f:
            info=os.fstat(f.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid!=pwd.getpwnam('ngpanel').pw_uid or info.st_size>512:raise ValueError('Invalid update request')
            action=json.load(f).get('action')
        try:
            report('running','Выполняется '+str(action))
            if action=='rollback':rollback()
            elif action in ('check','install'):
                tag,asset,digest=latest();available=version(tag)>version(installed())
                if action=='check':report('ok','Доступна новая версия' if available else 'Установлена последняя версия',Latest=tag,Available=available)
                elif available:install_release(tag,asset,digest)
                else:report('ok','Установлена последняя версия',Latest=tag,Available=False)
            else:raise ValueError('Unknown update action')
        except Exception as e:
            message=str(e) if isinstance(e,ValueError) else 'Не удалось выполнить обновление; подробности в журнале ngpanel-update'
            report('error',message)
            print(str(e),file=__import__('sys').stderr)
        finally:REQUEST.unlink(missing_ok=True)
if __name__=='__main__':main()

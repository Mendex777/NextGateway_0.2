#!/usr/bin/python3
"""Fixed official Xray install/update job, with validation and rollback; no routing changes."""
import fcntl,hashlib,io,json,os,pathlib,pwd,shutil,stat,subprocess,time,urllib.request,zipfile
status=pathlib.Path('/var/lib/ngpanel/install-status')
request=pathlib.Path('/var/lib/ngpanel/jobs/install.request')
target=pathlib.Path('/usr/local/bin/xray')
config=pathlib.Path('/etc/ngpanel/config.json')
def report(text):
 tmp=status.with_suffix('.tmp');tmp.write_text(text,encoding='utf-8');tmp.chmod(0o644);os.replace(tmp,status)
def command(args,**kw):return subprocess.run(args,check=True,capture_output=True,timeout=kw.pop('timeout',15),**kw)
def active():return subprocess.run(['systemctl','is-active','--quiet','xray'],capture_output=True).returncode==0
def download(url,limit):
 with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'NGPanel'}),timeout=45) as response:data=response.read(limit+1)
 if len(data)>limit:raise ValueError('Download exceeds size limit')
 return data

def install(update=False):
 running=active()
 if running and not update:raise ValueError('Сначала остановите Xray; используйте обновление ядра на главной')
 release=json.loads(download('https://api.github.com/repos/XTLS/Xray-core/releases/latest',2*1024*1024))
 if release.get('draft') or release.get('prerelease'):raise ValueError('Ожидается стабильный выпуск')
 asset=next(a for a in release['assets'] if a['name']=='Xray-linux-64.zip');digest=asset.get('digest','');url=asset['browser_download_url']
 if not digest.startswith('sha256:') or len(digest)!=71:raise ValueError('Release does not publish SHA-256')
 if not url.startswith('https://github.com/XTLS/Xray-core/releases/download/'):raise ValueError('Unexpected release URL')
 data=download(url,100*1024*1024)
 if hashlib.sha256(data).hexdigest()!=digest[7:]:raise ValueError('SHA-256 mismatch')
 with zipfile.ZipFile(io.BytesIO(data)) as archive:
  info=archive.getinfo('xray')
  if info.file_size>150*1024*1024:raise ValueError('Binary too large')
  binary=archive.read(info)
 candidate=target.with_suffix('.ng-new');backup=target.with_suffix('.ng-previous');had_binary=target.exists();changed=False
 try:
  candidate.write_bytes(binary);candidate.chmod(0o755)
  version=command([str(candidate),'version']).stdout.decode().split()
  if len(version)<2 or version[0]!='Xray' or version[1]!=str(release['tag_name']).removeprefix('v'):raise ValueError('Binary version mismatch')
  if update and config.exists():
   command(['runuser','-u','ngxray','--',str(candidate),'run','-test','-c',str(config)],env={**os.environ,'XRAY_LOCATION_ASSET':'/usr/local/share/ngpanel-geodata'},timeout=30)
  if had_binary:shutil.copy2(target,backup)
  try:
   if running:command(['systemctl','stop','xray'])
   os.replace(candidate,target);changed=True
   if running:
    command(['systemctl','start','xray']);time.sleep(2)
    if not active():raise ValueError('Новый Xray не запустился')
  except Exception:
   if changed:
    if had_binary:os.replace(backup,target)
    else:target.unlink(missing_ok=True)
   if running:command(['systemctl','restart','xray'])
   raise ValueError('Обновление отменено, восстановлено прежнее ядро')
  return 'Установлен Xray '+release['tag_name']+('. Служба работает.' if running else '. Состояние службы сохранено: остановлена.')
 finally:candidate.unlink(missing_ok=True)

def main():
 try:
  with open('/run/ngpanel-control.lock','w') as lock:
   fcntl.flock(lock,fcntl.LOCK_EX)
   update=False
   if request.exists():
    fd=os.open(request,os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(fd,'rb') as stream:
     info=os.fstat(stream.fileno())
     if not stat.S_ISREG(info.st_mode) or info.st_size>1024 or info.st_uid!=pwd.getpwnam('ngpanel').pw_uid:raise ValueError('Invalid install request')
     raw=stream.read();job=json.loads(raw) if raw else {}
     if job not in ({},{'update':True}):raise ValueError('Unknown install operation')
     update=job.get('update') is True
   report('Получение и проверка официального релиза Xray...')
   report(install(update))
 except Exception as exc:
  report('Ошибка установки: '+str(exc));raise
 finally:request.unlink(missing_ok=True)
if __name__=='__main__':main()

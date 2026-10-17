#!/usr/bin/env python3
"""Transfer one complete, verified Vite build. Never merge files into dist."""
import argparse,hashlib,json,os,pathlib,re,shutil,subprocess,tarfile,tempfile,uuid
MANIFEST='dist-manifest.json'
def inventory(folder):
 if not folder.is_dir() or folder.is_symlink():raise ValueError('Expected a real frontend/dist directory')
 files={}
 for p in sorted(folder.rglob('*')):
  if p.is_symlink():raise ValueError('Symlinks are not allowed in frontend artifacts')
  if p.is_file():files[p.relative_to(folder).as_posix()]=hashlib.sha256(p.read_bytes()).hexdigest()
 if 'index.html' not in files:raise ValueError('Missing frontend index.html; run npm build first')
 return files

def verify(workspace):
 root=pathlib.Path(workspace).resolve();manifest=json.loads((root/'frontend'/MANIFEST).read_text(encoding='utf-8'))
 if manifest.get('schema')!=1 or not isinstance(manifest.get('files'),dict):raise ValueError('Invalid frontend manifest')
 actual=inventory(root/'frontend/dist')
 if actual!=manifest['files']:raise ValueError('Frontend files differ from the Vite manifest (extra, missing or modified files). Rebuild and sync the complete artifact.')
 return manifest

def pack(workspace,archive):
 root=pathlib.Path(workspace).resolve();manifest=verify(root)
 with tarfile.open(archive,'w:gz') as tar:
  tar.add(root/'frontend'/MANIFEST,arcname=MANIFEST,recursive=False)
  for name in manifest['files']:tar.add(root/'frontend/dist'/name,arcname='dist/'+name,recursive=False)
 return hashlib.sha256(pathlib.Path(archive).read_bytes()).hexdigest()

def apply(workspace,archive,digest):
 import fcntl
 root=pathlib.Path(workspace).resolve();frontend=root/'frontend';archive=pathlib.Path(archive)
 if not (root/'main.go').is_file() or frontend.is_symlink() or not frontend.is_dir():raise ValueError('Expected the NGPanel source workspace')
 if archive.parent.resolve()!=frontend.resolve() or not re.fullmatch(r'dist-upload-[0-9a-f]{32}\.tar\.gz',archive.name) or archive.is_symlink():raise ValueError('Unexpected upload path')
 if hashlib.sha256(archive.read_bytes()).hexdigest()!=digest:
  archive.unlink();raise ValueError('Upload SHA-256 mismatch')
 with open(frontend/'.dist-sync.lock','a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX)
  stage=pathlib.Path(tempfile.mkdtemp(prefix='.dist-stage-',dir=frontend));old=frontend/('.dist-old-'+uuid.uuid4().hex);target=frontend/'dist';swapped=False
  try:
   if target.is_symlink():raise ValueError('Refusing to replace a symlink')
   with tarfile.open(archive,'r:gz') as tar:
    names=set();size=0
    for member in tar:
     path=pathlib.PurePosixPath(member.name)
     if not member.isfile() or path.is_absolute() or '..' in path.parts or member.name in names or not (member.name==MANIFEST or path.parts[0]=='dist'):raise ValueError('Invalid frontend archive member')
     names.add(member.name);size+=member.size
     if size>50*1024*1024 or len(names)>2048:raise ValueError('Frontend artifact exceeds limit')
     destination=stage.joinpath(*path.parts);destination.parent.mkdir(parents=True,exist_ok=True)
     with tar.extractfile(member) as source,open(destination,'wb') as output:shutil.copyfileobj(source,output)
     destination.chmod(0o644)
   manifest=json.loads((stage/MANIFEST).read_text(encoding='utf-8'))
   if manifest.get('schema')!=1 or inventory(stage/'dist')!=manifest.get('files'):raise ValueError('Frontend archive does not match its manifest')
   if target.exists():os.replace(target,old)
   try:
    os.replace(stage/'dist',target);swapped=True
    os.replace(stage/MANIFEST,frontend/MANIFEST)
   except Exception:
    if swapped:shutil.rmtree(target)
    if old.exists():os.replace(old,target)
    raise
   if old.exists():shutil.rmtree(old)
   verify(root)
   print('Frontend replaced:',len(manifest['files']),'files,',sum(p.stat().st_size for p in target.rglob('*') if p.is_file()),'bytes')
  finally:
   if stage.exists():shutil.rmtree(stage)
   archive.unlink(missing_ok=True)

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--verify',action='store_true');parser.add_argument('--apply',action='store_true');parser.add_argument('--workspace');parser.add_argument('--archive');parser.add_argument('--sha256');parser.add_argument('--host',default='tdcadmin@192.168.1.84');args=parser.parse_args()
 local=pathlib.Path(__file__).resolve().parent.parent
 if args.verify:verify(args.workspace or local);print('Frontend manifest verified');return
 if args.apply:apply(args.workspace,args.archive,args.sha256);return
 remote=args.workspace or '/home/tdcadmin/ngpanel-home-review'
 if not re.fullmatch(r'[a-zA-Z0-9_.@-]+',args.host) or not re.fullmatch(r'/home/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+',remote):raise ValueError('Unexpected SSH host or source workspace')
 with tempfile.TemporaryDirectory(prefix='ngpanel-frontend-') as folder:
  archive=pathlib.Path(folder)/('dist-upload-'+uuid.uuid4().hex+'.tar.gz');digest=pack(local,archive);destination=remote+'/frontend/'+archive.name
  subprocess.run(['scp',str(pathlib.Path(__file__).resolve()),args.host+':'+remote+'/deploy/sync-frontend.py'],check=True)
  subprocess.run(['scp',str(archive),args.host+':'+destination],check=True)
  subprocess.run(['ssh',args.host,'python3',remote+'/deploy/sync-frontend.py','--apply','--workspace',remote,'--archive',destination,'--sha256',digest],check=True)
if __name__=='__main__':main()

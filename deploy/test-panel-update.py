#!/usr/bin/python3
import hashlib,importlib.util,io,json,pathlib,sqlite3,tarfile,tempfile,types,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('updater',pathlib.Path(__file__).with_name('update-panel.py'));u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)
def archive(tag='v0.2.1',extra=None):
    files={n:b'new '+n.encode() for n in u.FILES}
    manifest={'version':tag,'schema':1,'sha256':{n:hashlib.sha256(b).hexdigest() for n,b in files.items()}}
    files['manifest.json']=json.dumps(manifest).encode()
    if extra:files.update(extra)
    out=io.BytesIO()
    with tarfile.open(fileobj=out,mode='w:gz') as tar:
        for n,b in files.items():
            item=tarfile.TarInfo(n);item.size=len(b);tar.addfile(item,io.BytesIO(b))
    return out.getvalue()
class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=pathlib.Path(self.temp.name);self.app=self.root/'app';self.app.mkdir();self.updates=self.root/'updates';self.updates.mkdir();self.db=self.root/'panel.db'
        with sqlite3.connect(self.db) as db:db.execute('CREATE TABLE example(value TEXT)');db.execute("INSERT INTO example VALUES('private subscription')")
        for n in u.FILES:(self.app/n).write_bytes(b'old '+n.encode())
        self.patches=[patch.object(u,n,v) for n,v in {'APP':self.app,'DB':self.db,'UPDATES':self.updates,'PREVIOUS':self.updates/'previous'}.items()]
        for p in self.patches:p.start();self.addCleanup(p.stop)
    def test_checksum_failure_keeps_running_panel(self):
        with patch.object(u,'download',return_value=b'corrupt'),patch.object(u,'report'),patch.object(u,'run') as run:
            with self.assertRaises(ValueError):u.install_release('v0.2.1',{'browser_download_url':'url'},'0'*64)
            run.assert_not_called()
        self.assertEqual((self.app/'ngpanel').read_bytes(),b'old ngpanel')
    def test_archive_traversal_rejected(self):
        with patch.object(u,'run'):
            with self.assertRaises(ValueError):u.unpack(archive(extra={'../escape':b'bad'}),self.app/'stage','v0.2.1')
        self.assertFalse((self.root/'escape').exists())
    def test_failed_start_restores_files_and_database(self):
        data=archive();calls=[]
        def run(args,**kw):
            calls.append(args)
            text='NGPanel v0.2.1 commit' if 'runuser'==args[0] else 'NGPanel v0.2.0 old'
            return types.SimpleNamespace(stdout=text)
        with patch.object(u,'download',return_value=data),patch.object(u,'report'),patch.object(u,'run',side_effect=run),patch.object(u,'healthy',side_effect=[False,True]),patch.object(u.os,'chown'),patch.object(u,'restore_db',side_effect=lambda src:__import__('shutil').copy2(src/'panel.db',self.db)),patch.object(u.subprocess,'run',return_value=types.SimpleNamespace(returncode=0)):
            with self.assertRaisesRegex(ValueError,'предыдущая панель и база восстановлены'):u.install_release('v0.2.1',{'browser_download_url':'url'},hashlib.sha256(data).hexdigest())
        for n in u.FILES:self.assertEqual((self.app/n).read_bytes(),b'old '+n.encode())
        with sqlite3.connect(self.db) as db:self.assertEqual(db.execute('SELECT value FROM example').fetchone()[0],'private subscription')
        self.assertIn(['systemctl','start','ngpanel'],calls)
if __name__=='__main__':unittest.main()

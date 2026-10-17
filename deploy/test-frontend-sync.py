#!/usr/bin/env python3
import hashlib,importlib.util,json,pathlib,sys,tarfile,tempfile,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('sync',pathlib.Path(__file__).with_name('sync-frontend.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class SyncTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=pathlib.Path(self.temp.name)
  for name in ('source','target'):
   root=self.root/name;(root/'frontend/dist/assets').mkdir(parents=True);(root/'main.go').write_text('package main')
   (root/'frontend/dist/index.html').write_text('<script src="/assets/app.js"></script>');(root/'frontend/dist/assets/app.js').write_text(name)
   self.manifest(root)
 def manifest(self,root):
  (root/'frontend'/m.MANIFEST).write_text(json.dumps({'schema':1,'files':m.inventory(root/'frontend/dist')}))
 def bundle(self):
  target=self.root/'target';archive=target/'frontend'/('dist-upload-'+'a'*32+'.tar.gz');digest=m.pack(self.root/'source',archive);return target,archive,digest
 def test_verify_rejects_extra_and_modified_files(self):
  root=self.root/'source';(root/'frontend/dist/assets/old.js').write_text('stale')
  with self.assertRaises(ValueError):m.verify(root)
  (root/'frontend/dist/assets/old.js').unlink();(root/'frontend/dist/assets/app.js').write_text('changed')
  with self.assertRaises(ValueError):m.verify(root)
 @unittest.skipIf(sys.platform=='win32','Remote atomic replacement runs on Linux')
 def test_transfer_replaces_instead_of_merging(self):
  target,archive,digest=self.bundle();(target/'frontend/dist/assets/stale.js').write_text('orphan')
  m.apply(target,archive,digest);self.assertFalse((target/'frontend/dist/assets/stale.js').exists());self.assertEqual(m.verify(target),m.verify(self.root/'source'));self.assertFalse(archive.exists())
 @unittest.skipIf(sys.platform=='win32','Remote atomic replacement runs on Linux')
 def test_corrupt_upload_preserves_existing_dist(self):
  target,archive,digest=self.bundle();archive.write_bytes(b'broken')
  with self.assertRaises(ValueError):m.apply(target,archive,digest)
  self.assertEqual((target/'frontend/dist/assets/app.js').read_text(),'target');self.assertFalse(archive.exists())
 @unittest.skipIf(sys.platform=='win32','Remote atomic replacement runs on Linux')
 def test_failed_manifest_swap_restores_previous_dist(self):
  target,archive,digest=self.bundle();replace=m.os.replace
  def fail_manifest(src,dst):
   if pathlib.Path(src).name==m.MANIFEST:raise OSError('simulated failure')
   return replace(src,dst)
  with patch.object(m.os,'replace',side_effect=fail_manifest):
   with self.assertRaises(OSError):m.apply(target,archive,digest)
  self.assertEqual((target/'frontend/dist/assets/app.js').read_text(),'target');m.verify(target)
 @unittest.skipIf(sys.platform=='win32','Remote atomic replacement runs on Linux')
 def test_repeated_transfer_does_not_accumulate_files(self):
  for _ in range(2):
   target,archive,digest=self.bundle();m.apply(target,archive,digest)
  self.assertEqual(len(m.inventory(target/'frontend/dist')),2)
  self.assertFalse(list((target/'frontend').glob('.dist-old-*')))
  self.assertFalse(list((target/'frontend').glob('.dist-stage-*')))
 @unittest.skipIf(sys.platform=='win32','Remote atomic replacement runs on Linux')
 def test_archive_traversal_is_rejected(self):
  target,archive,_=self.bundle()
  import io
  with tarfile.open(archive,'w:gz') as tar:
   entry=tarfile.TarInfo('../escape');entry.size=4;tar.addfile(entry,io.BytesIO(b'evil'))
  with self.assertRaises(ValueError):m.apply(target,archive,hashlib.sha256(archive.read_bytes()).hexdigest())
  self.assertFalse((target/'escape').exists());self.assertEqual((target/'frontend/dist/assets/app.js').read_text(),'target')
if __name__=='__main__':unittest.main()

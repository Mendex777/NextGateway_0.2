#!/usr/bin/python3
import copy,hashlib,importlib.util,json,os,pathlib,sqlite3,tempfile,types,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('geo',pathlib.Path(__file__).with_name('update-geodata.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class GeoRollbackTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=pathlib.Path(self.temp.name);self.config=self.root/'config';self.config.mkdir();self.versions=self.root/'versions';self.versions.mkdir();self.old=self.versions/'old';self.old.mkdir();self.current=self.root/'current';self.current.symlink_to(self.old,target_is_directory=True)
  self.tag='ngpanel-system:RE5T';self.value={'routing':{'rules':[{'ruleTag':self.tag,'inboundTag':['dns-in'],'outboundTag':'direct'},{'ruleTag':self.tag,'inboundTag':['dns-upstream'],'outboundTag':'block'},{'ruleTag':self.tag+':ngpanel-2','outboundTag':'direct'}]}}
  self.original=json.dumps(self.value).encode();(self.config/'previous.json').write_bytes(self.original);(self.config/'config.json').write_text('{}')
  self.db=sqlite3.connect(':memory:');self.db.execute('CREATE TABLE rules(value TEXT)');self.addCleanup(self.db.close)
  release={'tag_name':'20261022','assets':[{'name':name,'browser_download_url':'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/download/test/'+name,'digest':'sha256:'+hashlib.sha256(b'new geo').hexdigest()} for name in ['geoip.dat','geosite.dat']]}
  def download(url,limit):return json.dumps(release).encode() if '/latest' in url else b'new geo'
  self.fail_restart=False;self.fail_validation=False
  def command(args,**kw):
   if 'index-geodata.py' in args[1]:(pathlib.Path(args[2])/'index.json').write_text('[]')
   if args==['systemctl','restart','xray'] and self.fail_restart:self.fail_restart=False;raise RuntimeError('restart failed')
   return types.SimpleNamespace(returncode=0)
  def run(args,**kw):return types.SimpleNamespace(returncode=1 if args[0]=='runuser' and self.fail_validation else 0)
  for p in [patch.object(m,'CURRENT',self.current),patch.object(m,'VERSIONS',self.versions),patch.object(m,'CONFIG_DIR',self.config),patch.object(m,'download',side_effect=download),patch.object(m,'command',side_effect=command),patch.object(m.subprocess,'run',side_effect=run),patch.object(m.sqlite3,'connect',return_value=self.db),patch('pwd.getpwnam',return_value=types.SimpleNamespace(pw_gid=os.getgid())),patch('time.sleep'),patch.object(m.os,'chown')]:p.start();self.addCleanup(p.stop)
 def test_success_repairs_only_tags_and_preserves_original(self):
  m.update();fixed=json.loads((self.config/'previous.json').read_bytes());tags=[r['ruleTag'] for r in fixed['routing']['rules']];self.assertEqual(len(tags),len(set(tags)));self.assertEqual((self.config/'previous-before-geodata-tags.json').read_bytes(),self.original)
  for r in fixed['routing']['rules']:r.pop('ruleTag')
  expected=copy.deepcopy(self.value)
  for r in expected['routing']['rules']:r.pop('ruleTag')
  self.assertEqual(fixed,expected);self.assertNotEqual(self.current.resolve(),self.old);self.assertEqual((self.config/'config.json').read_text(),'{}')
 def test_failed_validation_preserves_rollback_and_geo(self):
  self.fail_validation=True
  with self.assertRaisesRegex(ValueError,'config.json'):m.update()
  self.assertEqual((self.config/'previous.json').read_bytes(),self.original);self.assertEqual(self.current.resolve(),self.old);self.assertEqual(list(self.versions.iterdir()),[self.old])
 def test_failed_restart_restores_rollback_and_geo(self):
  self.fail_restart=True
  with self.assertRaisesRegex(ValueError,'восстановлены'):m.update()
  self.assertEqual((self.config/'previous.json').read_bytes(),self.original);self.assertEqual(self.current.resolve(),self.old);self.assertEqual(list(self.versions.iterdir()),[self.old])
 def test_idempotent_and_custom_tags_preserved(self):
  repaired=m.compatible_rollback(self.original);self.assertEqual(m.compatible_rollback(repaired),repaired)
  custom=b'{"routing":{"rules":[{"ruleTag":"custom"},{"ruleTag":"custom"}]}}';self.assertEqual(m.compatible_rollback(custom),custom)
 def test_concurrent_config_edit_is_not_overwritten(self):
  path=self.config/'previous.json';path.write_bytes(b'new config')
  with self.assertRaisesRegex(ValueError,'изменилась'):m.repair_rollback(path,self.original,m.compatible_rollback(self.original))
  self.assertEqual(path.read_bytes(),b'new config')
if __name__=='__main__':unittest.main()

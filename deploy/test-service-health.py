#!/usr/bin/python3
"""Collector installation never changes the live network or blesses drifted rules."""
import importlib.util,pathlib,tempfile,types,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('control',pathlib.Path(__file__).with_name('control.py'));c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)
class HealthInstallTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=pathlib.Path(self.temp.name)
  for key,value in {'HEALTH_UNIT':self.root/'health.service','HEALTH_BASELINE':self.root/'baseline.json','NFT':self.root/'gateway.nft'}.items():
   p=patch.object(c,key,value);p.start();self.addCleanup(p.stop)
  p=patch.object(c.os,'geteuid',return_value=0);p.start();self.addCleanup(p.stop)
 def test_baseline_is_generated_in_separate_namespace(self):
  c.NFT.write_text('table inet ngpanel {}')
  with patch.object(c,'run',return_value=types.SimpleNamespace(returncode=0,stdout='{"nftables":[]}')) as run:
   c.install_health()
  args=[call.args[0] for call in run.call_args_list]
  self.assertEqual(args[0][:2],['unshare','--net']);self.assertNotIn(['nft','-f',str(c.NFT)],args)
  self.assertTrue(c.HEALTH_BASELINE.exists());self.assertEqual(c.HEALTH_UNIT.read_text(),c.HEALTH_SERVICE)
 def test_existing_baseline_is_preserved(self):
  c.HEALTH_BASELINE.write_text('expected, never live');c.NFT.write_text('modified')
  with patch.object(c,'run') as run:c.install_health()
  self.assertFalse(any(call.args[0][0]=='unshare' for call in run.call_args_list));self.assertEqual(c.HEALTH_BASELINE.read_text(),'expected, never live')
 def test_masked_collector_is_not_overwritten(self):
  c.HEALTH_UNIT.symlink_to('/dev/null')
  with patch.object(c,'run') as run:
   with self.assertRaises(ValueError):c.install_health()
  run.assert_not_called()
if __name__=='__main__':unittest.main()

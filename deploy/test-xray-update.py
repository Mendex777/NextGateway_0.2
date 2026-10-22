#!/usr/bin/python3
import hashlib,importlib.util,io,json,pathlib,tempfile,types,unittest,zipfile
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('core',pathlib.Path(__file__).with_name('install-xray.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class UpdateTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);root=pathlib.Path(self.temp.name)
  for k,v in {'target':root/'xray','config':root/'config.json'}.items():
   p=patch.object(m,k,v);p.start();self.addCleanup(p.stop)
  m.target.write_bytes(b'old');m.config.write_text('{}')
  out=io.BytesIO()
  with zipfile.ZipFile(out,'w') as z:z.writestr('xray',b'new')
  self.data=out.getvalue();self.release={'tag_name':'v26.3.28','assets':[{'name':'Xray-linux-64.zip','digest':'sha256:'+hashlib.sha256(self.data).hexdigest(),'browser_download_url':'https://github.com/XTLS/Xray-core/releases/download/v26.3.28/Xray-linux-64.zip'}]}
  p=patch.object(m,'download',side_effect=lambda url,limit:json.dumps(self.release).encode() if '/latest' in url else self.data);p.start();self.addCleanup(p.stop)
  p=patch.object(m.time,'sleep');p.start();self.addCleanup(p.stop)
 def command(self,args,**kw):return types.SimpleNamespace(stdout=b'Xray 26.3.28')
 def test_download_corruption_does_not_stop_service(self):
  self.release['assets'][0]['digest']='sha256:'+'0'*64
  with patch.object(m,'active',return_value=True),patch.object(m,'command') as command:
   with self.assertRaises(ValueError):m.install(True)
  command.assert_not_called();self.assertEqual(m.target.read_bytes(),b'old')
 def test_incompatible_config_does_not_stop_service(self):
  def cmd(args,**kw):
   if 'runuser'==args[0]:raise RuntimeError('Invalid config')
   return self.command(args,**kw)
  with patch.object(m,'active',return_value=True),patch.object(m,'command',side_effect=cmd) as command:
   with self.assertRaises(RuntimeError):m.install(True)
  self.assertFalse(any(c.args[0][:2]==['systemctl','stop'] for c in command.call_args_list));self.assertEqual(m.target.read_bytes(),b'old')
 def test_config_failure_reports_reason_without_secrets(self):
  import subprocess
  m.config.write_text(json.dumps({'outbounds':[{'settings':{'id':'private-uuid','pass':'secret-password'}}]}))
  def cmd(args,**kw):
   if args[0]=='runuser':raise subprocess.CalledProcessError(23,args,output=b'Failed to start: duplicate ruleTag private-uuid secret-password',stderr=b'')
   return self.command(args,**kw)
  with patch.object(m,'active',return_value=True),patch.object(m,'command',side_effect=cmd) as command:
   with self.assertRaises(ValueError) as caught:m.install(True)
  message=str(caught.exception)
  self.assertIn('duplicate ruleTag',message);self.assertNotIn('private-uuid',message);self.assertNotIn('secret-password',message)
  self.assertFalse(any(c.args[0][:2]==['systemctl','stop'] for c in command.call_args_list));self.assertEqual(m.target.read_bytes(),b'old')
 def test_failed_start_restores_old_core(self):
  with patch.object(m,'active',side_effect=[True,False]),patch.object(m,'command',side_effect=self.command) as command:
   with self.assertRaisesRegex(ValueError,'прежнее ядро'):m.install(True)
  self.assertEqual(m.target.read_bytes(),b'old');self.assertIn(['systemctl','restart','xray'],[c.args[0] for c in command.call_args_list])
 def test_success_restores_running_state(self):
  with patch.object(m,'active',return_value=True),patch.object(m,'command',side_effect=self.command) as command:result=m.install(True)
  self.assertEqual(m.target.read_bytes(),b'new');self.assertIn('v26.3.28',result);self.assertIn(['systemctl','start','xray'],[c.args[0] for c in command.call_args_list])
 def test_stopped_core_stays_stopped(self):
  with patch.object(m,'active',return_value=False),patch.object(m,'command',side_effect=self.command) as command:m.install(True)
  self.assertFalse(any(c.args[0][0]=='systemctl' for c in command.call_args_list))
if __name__=='__main__':unittest.main()

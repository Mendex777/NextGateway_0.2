#!/usr/bin/env python3
"""Exercise installer initialization on real temporary SQLite databases, no host changes."""
import io,json,pathlib,sqlite3,subprocess,sys,tempfile,unittest,unittest.mock as mock
from contextlib import closing

SCRIPT=pathlib.Path(__file__).with_name('install-panel.sh').read_text().rsplit('python3 - "$NG_LISTEN" <<\'PY\'\n',1)[1].split('\nPY\n',1)[0]

class SetupTests(unittest.TestCase):
    @unittest.skipIf(sys.platform=='win32','Rollback shell test runs on Linux')
    def test_rollback_restores_binary_database_and_unit(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory);backup=root/'saved';backup.mkdir()
            for folder in ('saved/app','saved/units','opt/ngpanel','etc/systemd/system','var/lib/ngpanel/db','bin'):(root/folder).mkdir(parents=True,exist_ok=True)
            (backup/'app/ngpanel').write_text('previous binary');(root/'opt/ngpanel/ngpanel').write_text('bad binary')
            (backup/'units/ngpanel.service').write_text('previous port and unit')
            (backup/'panel.db').write_text('previous database');(root/'var/lib/ngpanel/db/panel.db').write_text('bad database')
            command=root/'bin/systemctl';command.write_text('#!/bin/sh\nexit 0\n');command.chmod(0o755)
            source=pathlib.Path(__file__).with_name('install-panel.sh').read_text()
            function='rollback_install() {'+source.split('rollback_install() {',1)[1].split("\ntrap 'rollback_install'",1)[0]
            for prefix in ('/var/lib/ngpanel','/etc/systemd/system','/opt/ngpanel'):function=function.replace(prefix,str(root/prefix.lstrip('/')))
            function=function.replace('-o ngpanel -g ngpanel','')
            import os
            subprocess.run(['sh','-c',function+'\nrollback_install'],check=True,env={**os.environ,'NG_INSTALL_BACKUP':str(backup),'PATH':str(root/'bin')+':'+os.environ['PATH']},capture_output=True)
            self.assertEqual((root/'opt/ngpanel/ngpanel').read_text(),'previous binary')
            self.assertEqual((root/'var/lib/ngpanel/db/panel.db').read_text(),'previous database')
            self.assertEqual((root/'etc/systemd/system/ngpanel.service').read_text(),'previous port and unit')
    def exercise(self,existing=False,broken=False,fail=False):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            def path(name):return root/name.lstrip('/')
            database=path('/var/lib/ngpanel/db/panel.db');database.parent.mkdir(parents=True)
            resolver=path('/run/systemd/resolve/resolv.conf');resolver.parent.mkdir(parents=True);resolver.write_text('nameserver 127.0.0.53\nnameserver 192.168.50.2\n')
            network={'interface':'eth0','address':'192.168.50.8','cidr':'192.168.50.0/24','router':'192.168.50.2'}
            with closing(sqlite3.connect(database)) as db, db:
                db.execute('CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT)')
                db.executemany('INSERT INTO settings VALUES(?,?)',[('gateway_enabled','0'),('custom_setting','keep'),('selected_node','42')])
                if existing:db.execute('INSERT INTO settings VALUES(?,?)',('gateway_network',json.dumps(network)))
            if existing:
                config=path('/etc/ngpanel/config.json');config.parent.mkdir(parents=True);config.write_text('keep applied config')
            def components():
                for name in ('/usr/local/bin/xray','/usr/local/share/ngpanel-geodata/geoip.dat','/usr/local/share/ngpanel-geodata/geosite.dat'):
                    file=path(name);file.parent.mkdir(parents=True,exist_ok=True);file.touch()
            if not broken:components()
            calls=[];posts=[]
            def command(args,**kwargs):
                calls.append(args)
                if args[-1].endswith('install-xray.py'):
                    binary=path('/usr/local/bin/xray');binary.parent.mkdir(parents=True,exist_ok=True);binary.touch()
                if args[-1].endswith('update-geodata.py'):components()
                return subprocess.CompletedProcess(args,0)
            def output(args,**kwargs):
                if args[-1]=='--version':return 'NGPanel v0.3.23 test'
                if 'route' in args:return json.dumps([{'dev':'eth0','gateway':'192.168.50.2','metric':10}]).encode()
                return json.dumps([{'addr_info':[{'local':'192.168.50.8','prefixlen':24}]}]).encode()
            def response(request,**kwargs):
                if isinstance(request,str):return io.BytesIO(b'{"version":"v0.3.23"}')
                posts.append(request)
                state=path('/var/lib/ngpanel/runtime.json');state.parent.mkdir(parents=True,exist_ok=True)
                state.write_text(json.dumps({'Action':'apply','State':'error' if fail else 'ok','Message':'test failure','Updated':'9999'}))
                return io.BytesIO(b'{"ok":true,"since":"2026"}')
            code=SCRIPT.replace('import ipaddress,json,pathlib,sqlite3,subprocess,sys,time,urllib.request,urllib.parse','import ipaddress,json,pathlib,sqlite3,subprocess,sys,time,urllib.request,urllib.parse\nfrom contextlib import closing').replace('with sqlite3.connect(database) as db:', 'with closing(sqlite3.connect(database)) as db, db:')
            for prefix in ('/var/lib/ngpanel','/etc/ngpanel','/usr/local','/opt/ngpanel','/run/systemd/resolve','/etc/resolv.conf'):
                code=code.replace(prefix,str(path(prefix)).replace('\\','/'))
            with mock.patch.object(sys,'argv',['setup','0.0.0.0:8181']),mock.patch('subprocess.run',command),mock.patch('subprocess.check_output',output),mock.patch('urllib.request.urlopen',response),mock.patch('time.sleep'),mock.patch('sys.stdout',io.StringIO()):
                if fail:
                    with self.assertRaisesRegex(RuntimeError,'test failure'):exec(compile(code,'installer setup','exec'),{})
                else:exec(compile(code,'installer setup','exec'),{})
            with closing(sqlite3.connect(database)) as db:settings=dict(db.execute('SELECT key,value FROM settings'))
            self.assertEqual(settings['custom_setting'],'keep');self.assertEqual(settings['selected_node'],'42')
            self.assertEqual(json.loads(settings['gateway_network']),network)
            self.assertEqual(settings['gateway_enabled'],'0' if existing else '1')
            if not existing:self.assertEqual(settings['dns_direct'],'192.168.50.2')
            self.assertEqual(len(posts),0 if existing else 1)
            if existing:self.assertEqual(path('/etc/ngpanel/config.json').read_text(),'keep applied config')
            if broken:self.assertTrue(any(a[-1].endswith('install-xray.py') for a in calls));self.assertTrue(any(a[-1].endswith('update-geodata.py') for a in calls))
    def test_fresh_detects_network_and_applies(self):self.exercise()
    def test_repeat_preserves_settings_and_does_not_apply(self):self.exercise(existing=True)
    def test_repeat_repairs_components(self):self.exercise(existing=True,broken=True)
    def test_failed_apply_is_reported(self):self.exercise(fail=True)

if __name__=='__main__':unittest.main()

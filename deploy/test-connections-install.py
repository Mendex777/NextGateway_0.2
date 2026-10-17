#!/usr/bin/python3
import importlib.util,pathlib,tempfile,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('control',pathlib.Path(__file__).with_name('control.py'))
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)

class CollectorInstallTests(unittest.TestCase):
    def test_existing_release_upgrades_without_xray_restart(self):
        with tempfile.TemporaryDirectory() as root:
            unit=pathlib.Path(root)/'ngpanel-connections.service'
            with patch.object(c,'CONNECTIONS_UNIT',unit),patch.object(c.os,'geteuid',return_value=0),patch.object(c,'run') as run:
                c.install_connections()
                self.assertEqual(unit.read_text(),pathlib.Path(__file__).with_name('ngpanel-connections.service').read_text())
                commands=[call.args[0] for call in run.call_args_list]
                self.assertIn(['systemctl','daemon-reload'],commands)
                self.assertIn(['systemctl','enable','ngpanel-connections.service'],commands)
                self.assertIn(['systemctl','restart','ngpanel-connections.service'],commands)
                self.assertTrue(all('xray' not in arg for command in commands for arg in command))
                run.reset_mock();c.install_connections()
                self.assertNotIn(['systemctl','daemon-reload'],[call.args[0] for call in run.call_args_list])
    def test_legacy_update_operation_installs_all_collectors(self):
        with patch.object(c,'install_connections') as connections,patch.object(c,'install_health') as health,patch.object(c,'install_traffic') as traffic:
            c.install_collectors()
        connections.assert_called_once_with();health.assert_called_once_with();traffic.assert_called_once_with()

    def test_web_user_cannot_provision_service(self):
        with patch.object(c.os,'geteuid',return_value=999),patch.object(c,'run') as run:
            with self.assertRaises(ValueError):c.install_connections()
            run.assert_not_called()
    def test_administrator_mask_is_preserved(self):
        with tempfile.TemporaryDirectory() as root:
            unit=pathlib.Path(root)/'ngpanel-connections.service';unit.symlink_to('/dev/null')
            with patch.object(c,'CONNECTIONS_UNIT',unit),patch.object(c.os,'geteuid',return_value=0),patch.object(c,'run') as run:
                with self.assertRaises(ValueError):c.install_connections()
                self.assertTrue(unit.is_symlink());run.assert_not_called()

if __name__=='__main__':unittest.main()

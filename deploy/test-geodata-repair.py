#!/usr/bin/python3
"""Button upgrades repair local datasets; absent datasets use the verified updater."""
import importlib.util
import pathlib
import json
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('control', pathlib.Path(__file__).with_name('control.py'))
c = importlib.util.module_from_spec(spec)
spec.loader.exec_module(c)


class GeoRepairTests(unittest.TestCase):
    @unittest.skipIf(sys.platform == 'win32', 'POSIX file permissions')
    def test_released_updater_hook_repairs_private_files_and_missing_index(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            domain = b'\x08\x02\x12\x0byoutube.com'
            category = b'\x0a\x07youtube' + b'\x12' + bytes([len(domain)]) + domain
            data = b'\x0a' + bytes([len(category)]) + category
            (root / 'geosite.dat').write_bytes(data)
            (root / 'geoip.dat').write_bytes(b'')
            for file in root.iterdir(): file.chmod(0o600)
            def command(args, **kwargs):
                self.assertEqual(args[1], '/opt/ngpanel/index-geodata.py')
                subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name('index-geodata.py')), *args[2:]], check=True)
            with patch.object(c, 'GEODATA', root), patch.object(c.os, 'geteuid', return_value=0), patch.object(c, 'run', side_effect=command), patch.object(c, 'install_connections'), patch.object(c, 'install_health'), patch.object(c, 'install_traffic'):
                c.install_collectors()
            for name in ('geosite.dat', 'geoip.dat', 'index.json'):
                self.assertEqual(stat.S_IMODE((root / name).stat().st_mode), 0o644)
            self.assertEqual((root / 'geosite.dat').read_bytes(), data)
            self.assertEqual(json.loads((root / 'index.json').read_text())[0]['Code'], 'youtube')

    def test_existing_datasets_reindexed_without_download_or_restart(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            for name in ('geoip.dat', 'geosite.dat', 'source.txt'):
                (root / name).write_bytes(b'verified existing data')
            with patch.object(c, 'GEODATA', root), patch.object(c.os, 'geteuid', return_value=0), patch.object(c, 'run') as run:
                c.repair_geodata()
            run.assert_called_once_with(['python3', '/opt/ngpanel/index-geodata.py', str(root), str(root)], timeout=120)
            self.assertEqual((root / 'geoip.dat').read_bytes(), b'verified existing data')

    def test_missing_dataset_downloaded_before_indexing(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            with patch.object(c, 'GEODATA', root), patch.object(c.os, 'geteuid', return_value=0), patch.object(c, 'run') as run:
                c.repair_geodata()
            self.assertEqual([call.args[0] for call in run.call_args_list], [
                ['python3', '/opt/ngpanel/update-geodata.py'],
                ['python3', '/opt/ngpanel/index-geodata.py', str(root), str(root)]])

    def test_download_failure_prevents_collector_provisioning(self):
        with patch.object(c, 'repair_geodata', side_effect=ValueError('checksum mismatch')), patch.object(c, 'install_connections') as install:
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                c.install_collectors()
            install.assert_not_called()

    def test_unprivileged_repair_rejected(self):
        with patch.object(c.os, 'geteuid', return_value=999), patch.object(c, 'run') as run:
            with self.assertRaises(ValueError):
                c.repair_geodata()
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()

#!/usr/bin/python3
"""Geo indexing must remain service-readable under the installer's private umask."""
import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import unittest


@unittest.skipIf(sys.platform == 'win32', 'POSIX file permissions')
class GeoPermissionsTests(unittest.TestCase):
    def test_index_repairs_private_datasets_and_missing_index(self):
        with tempfile.TemporaryDirectory() as folder:
            target = pathlib.Path(folder) / 'geo'
            target.mkdir(mode=0o700)
            # One geosite category: youtube, containing domain:youtube.com.
            domain = b'\x08\x02\x12\x0byoutube.com'
            category = b'\x0a\x07youtube' + b'\x12' + bytes([len(domain)]) + domain
            (target / 'geosite.dat').write_bytes(b'\x0a' + bytes([len(category)]) + category)
            (target / 'geoip.dat').write_bytes(b'')
            for file in target.iterdir():
                file.chmod(0o600)
            old_umask = os.umask(0o077)
            try:
                subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name('index-geodata.py')),
                                str(target), str(target)], check=True)
            finally:
                os.umask(old_umask)
            self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)
            for name in ('geosite.dat', 'geoip.dat', 'index.json'):
                self.assertEqual(stat.S_IMODE((target / name).stat().st_mode), 0o644, name)
            category = json.loads((target / 'index.json').read_text())[0]
            self.assertEqual((category['Code'], category['Count']), ('youtube', 1))


if __name__ == '__main__':
    unittest.main()

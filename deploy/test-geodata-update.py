#!/usr/bin/python3
"""A corrupted download must leave the live files and remove staging data."""
import importlib.util, json, pathlib, tempfile
spec=importlib.util.spec_from_file_location('updater',pathlib.Path(__file__).with_name('update-geodata.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
with tempfile.TemporaryDirectory() as folder:
    root=pathlib.Path(folder);module.CURRENT=root/'current';module.CURRENT.mkdir()
    (module.CURRENT/'geoip.dat').write_bytes(b'original')
    module.VERSIONS=root/'versions'
    release={'assets':[{'name':'geoip.dat','browser_download_url':'https://github.com/Loyalsoldier/v2ray-rules-dat/releases/download/test/geoip.dat','digest':'sha256:'+'0'*64}]}
    module.download=lambda url,limit:json.dumps(release).encode() if '/latest' in url else b'corrupted'
    try:module.update()
    except ValueError as error:assert 'SHA-256' in str(error)
    else:raise AssertionError('Corrupted download accepted')
    assert (module.CURRENT/'geoip.dat').read_bytes()==b'original'
    assert not list(module.VERSIONS.iterdir())
print('Corrupt geo download rejected; original files preserved; staging cleaned')

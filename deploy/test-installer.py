#!/usr/bin/python3
"""Verify bootstrap downloads/extraction without installing services or packages."""
import hashlib,io,json,pathlib,sys,tarfile,tempfile,unittest.mock
root=pathlib.Path(__file__).resolve().parent.parent
code=(root/'install.sh').read_text().split("<<'PY'\n",1)[1].split('\nPY\n',1)[0]
archive=(root/'dist/ngpanel-linux-amd64.tar.gz').read_bytes()
installer=(root/'dist/ngpanel-install.sh').read_bytes()
tag=(root/'VERSION').read_text().strip()
repo='Mendex777/NextGateway_0.2'
def check(bundle, corrupt=False, reject=False):
    assets={'ngpanel-linux-amd64.tar.gz':bundle,'ngpanel-install.sh':installer}
    release={'tag_name':tag,'assets':[{'name':n,'digest':'sha256:'+hashlib.sha256(b).hexdigest(),'browser_download_url':'https://github.com/'+repo+'/releases/download/'+tag+'/'+n} for n,b in assets.items()]}
    def response(request,timeout):
        if request.full_url.endswith('/releases/latest'): return io.BytesIO(json.dumps(release).encode())
        data=assets[request.full_url.rsplit('/',1)[1]]
        if corrupt: data=b'corrupted download'
        return io.BytesIO(data)
    with tempfile.TemporaryDirectory() as stage, unittest.mock.patch('urllib.request.urlopen',response), unittest.mock.patch.object(sys,'argv',['bootstrap',stage]):
        try: exec(compile(code,'install.sh download','exec'),{})
        except ValueError:
            if not reject: raise
        else:
            if reject: raise AssertionError('Unsafe archive accepted')
            assert (pathlib.Path(stage)/'deploy/install-panel.sh').read_bytes()==installer
            assert (pathlib.Path(stage)/'ngpanel').is_file()
check(archive)
check(archive,corrupt=True,reject=True)
for kind in ('traversal','symlink','duplicate'):
    output=io.BytesIO()
    with tarfile.open(fileobj=output,mode='w:gz') as target:
        if kind=='duplicate':
            with tarfile.open(fileobj=io.BytesIO(archive),mode='r:gz') as original:
                for item in original: target.addfile(item,original.extractfile(item))
            info=tarfile.TarInfo('manifest.json');info.size=2;target.addfile(info,io.BytesIO(b'{}'))
        else:
            info=tarfile.TarInfo('../escaped' if kind=='traversal' else 'ngpanel')
            if kind=='symlink':info.type=tarfile.SYMTYPE;info.linkname='/etc/passwd'
            target.addfile(info)
    check(output.getvalue(),reject=True)
print('Installer verification passed: valid release, checksum rejection, traversal, symlink, duplicate rejection. No installation performed.')

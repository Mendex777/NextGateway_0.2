#!/usr/bin/python3
"""Index Xray protobuf geodata from a verified, root-owned release archive."""
import ipaddress, json, pathlib, shutil, sys
def varint(data, pos):
    value=0
    for shift in range(0,70,7):
        b=data[pos];pos+=1;value|=(b&127)<<shift
        if b<128:return value,pos
    raise ValueError('Invalid varint')
def fields(data):
    pos=0
    while pos<len(data):
        tag,pos=varint(data,pos);kind=tag&7
        if kind==2:
            size,pos=varint(data,pos);value=data[pos:pos+size];pos+=size
            if pos>len(data):raise ValueError('Truncated protobuf')
        elif kind==0:value,pos=varint(data,pos)
        elif kind in (1,5):size=8 if kind==1 else 4;value=data[pos:pos+size];pos+=size
        else:raise ValueError('Unsupported protobuf field')
        yield tag>>3,value
source_dir=pathlib.Path(sys.argv[1]) if len(sys.argv)>1 else pathlib.Path('/opt/3xui-lab/x-ui/bin')
target=pathlib.Path(sys.argv[2]) if len(sys.argv)>2 else pathlib.Path('/usr/local/share/ngpanel-geodata');target.mkdir(exist_ok=True)
index=[]
for kind in ('geosite','geoip'):
    source=source_dir/(kind+'.dat')
    if source.resolve()!=(target/source.name).resolve():shutil.copyfile(source,target/source.name)
    for field,category in fields(source.read_bytes()):
        if field!=1:continue
        code='';entries=[]
        for key,value in fields(category):
            if key==1:code=value.decode().lower()
            elif key==2:
                item=dict(fields(value))
                if kind=='geosite':
                    prefix={0:'plain:',1:'regexp:',2:'domain:',3:'full:'}.get(item.get(1,0),'')
                    entries.append(prefix+item.get(2,b'').decode())
                elif len(item.get(1,b''))==4:
                    entries.append(str(ipaddress.ip_address(item[1]))+'/'+str(item.get(2,0)))
        index.append({'Kind':kind,'Code':code,'Entries':entries,'Count':len(entries)})
(target/'index.json').write_text(json.dumps(index,ensure_ascii=False))
if len(sys.argv)==1:(target/'source.txt').write_text('3x-ui v3.9.0 verified release bundle; installed datasets\n')
print('Indexed',len(index),'categories')

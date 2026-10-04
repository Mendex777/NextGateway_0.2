#!/usr/bin/python3
"""Bounded system jobs. No shell command or filesystem target comes from the web UI."""
import datetime
import fcntl
import hashlib
import ipaddress
import json
import os
import pathlib
import pwd
import re
import shutil
import stat
import subprocess
import sys
import time

ROOT = pathlib.Path('/var/lib/ngpanel')
os.environ['XRAY_LOCATION_ASSET'] = '/usr/local/share/ngpanel-geodata'
CONF = pathlib.Path('/etc/ngpanel/config.json')
BACKUP = pathlib.Path('/etc/ngpanel/previous.json')
META = pathlib.Path('/etc/ngpanel/applied.json')
PREVIOUS_META = pathlib.Path('/etc/ngpanel/previous-meta.json')
NFT = pathlib.Path('/etc/ngpanel/gateway.nft')
MARK_RULE = ['ip', '-4', 'rule', 'add', 'priority', '11000', 'fwmark', '1', 'lookup', '100']

def run(args, check=True, timeout=30, input=None):
    return subprocess.run(args, check=check, timeout=timeout, input=input, text=True, capture_output=True)

def atomic(path, data, mode=0o644, group=None):
    temporary = path.with_suffix(path.suffix + '.new')
    temporary.write_text(data, encoding='utf-8')
    temporary.chmod(mode)
    if group is not None:
        os.chown(temporary, 0, group)
    os.replace(temporary, path)

def load(path, default=None):
    if not path.exists():
        return {} if default is None else default
    return json.loads(path.read_text())

def active():
    return run(['systemctl', 'is-active', '--quiet', 'xray'], check=False).returncode == 0

def validate(config):
    if not isinstance(config, dict) or not {'log','dns','inbounds','outbounds','routing'}<=set(config) or set(config)-{'log','dns','inbounds','outbounds','routing','observatory','api'}:
        raise ValueError('Unexpected configuration schema')
    if config['log'] != {'loglevel':'warning'}:
        raise ValueError('Only journal logging is allowed')
    expected = {'tproxy-in':('0.0.0.0',7895,'dokodemo-door'), 'dns-in':('0.0.0.0',1053,'dokodemo-door'), 'test-socks':('127.0.0.1',1080,'socks')}
    if len(config['inbounds']) != 3:
        raise ValueError('Unexpected inbound count')
    seen = set()
    for inbound in config['inbounds']:
        tag = inbound.get('tag')
        if tag not in expected or tag in seen or tuple(inbound.get(k) for k in ('listen','port','protocol')) != expected[tag]:
            raise ValueError('Unexpected inbound')
        seen.add(tag)
    if len(config['outbounds']) > 256:
        raise ValueError('Unexpected outbound count')
    for outbound in config['outbounds']:
        if outbound.get('protocol') not in ('vless','hysteria','freedom','blackhole','dns'):
            raise ValueError('Unexpected outbound protocol')
    has_balance='observatory' in config or 'api' in config or bool(config['routing'].get('balancers'))
    if has_balance:
        api={'tag':'balance-api','listen':'127.0.0.1:10085','services':['RoutingService']}
        if config.get('api')!=api:raise ValueError('Only local balance API is allowed')
        obs=config.get('observatory',{})
        if not isinstance(obs,dict) or set(obs)!={'subjectSelector','probeUrl','probeInterval','enableConcurrency'} or obs.get('subjectSelector')!=['auto-vpn-'] or obs.get('probeUrl')!='https://www.google.com/generate_204' or obs.get('enableConcurrency') is not True:
            raise ValueError('Unexpected observatory settings')
        interval=obs.get('probeInterval','')
        if not isinstance(interval,str) or not re.fullmatch(r'[0-9]{2,3}s',interval) or not 10<=int(interval[:-1])<=600:raise ValueError('Unexpected probe interval')
        balancers=config['routing'].get('balancers',[])
        if not isinstance(balancers,list) or not 1<=len(balancers)<=16:raise ValueError('Unexpected group count')
        seen=set(); member_tags=set()
        for bal in balancers:
            tag=bal.get('tag','')
            if not re.fullmatch(r'group-[1-9][0-9]*',tag) or tag in seen:raise ValueError('Unexpected group tag')
            seen.add(tag); prefix='auto-vpn-'+tag[6:]+'-'
            expected={'tag':tag,'selector':[prefix],'fallbackTag':'block','strategy':{'type':'leastPing'}}
            if bal!=expected:raise ValueError('VPN fallback must block traffic')
            members=[o for o in config['outbounds'] if str(o.get('tag','')).startswith(prefix)]
            if not 2<=len(members)<=8:raise ValueError('Unexpected balance group size')
            for o in members:
                if not re.fullmatch(re.escape(prefix)+r'[1-9][0-9]*-',o['tag']) or o.get('protocol') not in ('vless','hysteria') or o['tag'] in member_tags:raise ValueError('Unexpected balance member')
                member_tags.add(o['tag'])
        if {o['tag'] for o in config['outbounds'] if str(o.get('tag','')).startswith('auto-vpn-')}!=member_tags:raise ValueError('Unassigned balance member')
    # No external file reads/writes through core config, even with a forged inbox job.
    forbidden = {'access','error','certificates','certificateFile','keyFile','file','files','configFile','privateKey','masterKeyLog'}
    def walk(value):
        if isinstance(value, dict):
            if set(value) & forbidden:
                raise ValueError('External file or server key settings are forbidden')
            for child in value.values(): walk(child)
        elif isinstance(value, list):
            for child in value: walk(child)
    walk(config)

def validate_network(n, live=False):
    if not isinstance(n,dict) or set(n)!={'interface','address','cidr','router'}:
        raise ValueError('Укажите параметры сети на странице DNS и шлюз')
    iface=n['interface']
    if not isinstance(iface,str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,15}',iface) or iface=='lo':
        raise ValueError('Некорректный интерфейс LAN')
    subnet=ipaddress.IPv4Network(n['cidr'],strict=True)
    address=ipaddress.IPv4Address(n['address']);router=ipaddress.IPv4Address(n['router'])
    if not 1<=subnet.prefixlen<=30 or address==router or any(ip not in subnet or ip in (subnet.network_address,subnet.broadcast_address) for ip in (address,router)):
        raise ValueError('Некорректные адреса LAN или роутера')
    if live:
        links=json.loads(run(['ip','-j','-4','addr','show','dev',iface]).stdout)
        if not any(a.get('local')==str(address) and a.get('prefixlen')==subnet.prefixlen for l in links for a in l.get('addr_info',[])):
            raise ValueError('Выбранный адрес и подсеть отсутствуют на интерфейсе ВМ; определите сеть заново')
    return n

def verify_default(n):
    routes=json.loads(run(['ip','-j','-4','route','show','default']).stdout)
    routes=sorted((r for r in routes if r.get('gateway')),key=lambda r:r.get('metric',0))
    if not routes or routes[0].get('gateway')!=n['router'] or routes[0].get('dev')!=n['interface']:
        raise ValueError('Выход ВМ не соответствует выбранному роутеру; примените выход и подтвердите доступность панели')

def applied_network(meta):
    n=meta.get('network')
    if n: return validate_network(n,True)
    # Migration of older installations: use the actual interface and route.
    routes=json.loads(run(['ip','-j','-4','route','show','default']).stdout)
    for r in sorted(routes,key=lambda r:r.get('metric',0)):
        if not r.get('gateway'):continue
        links=json.loads(run(['ip','-j','-4','addr','show','dev',r['dev']]).stdout)
        for l in links:
            for a in l.get('addr_info',[]):
                subnet=ipaddress.IPv4Network(str(a['local'])+'/'+str(a['prefixlen']),strict=False)
                if ipaddress.IPv4Address(r['gateway']) in subnet:
                    return validate_network(dict(interface=r['dev'],address=a['local'],cidr=str(subnet),router=r['gateway']),True)
    raise ValueError('Не удалось определить применённую сеть')

def migrate_network():
    meta=load(META)
    if meta.get('gateway') and not meta.get('network'):
        meta['network']=applied_network(meta)
        atomic(META,json.dumps(meta),0o600)
    return meta

def restore_gateway_on_boot():
    if not load(META).get('gateway'):return
    # netplan apply can return before DHCP has supplied an address again.
    for attempt in range(30):
        try:
            n=applied_network(migrate_network())
            break
        except (ValueError,subprocess.CalledProcessError):
            if attempt==29:raise
            time.sleep(1)
    enable_gateway(n)

def nft_text(n):
    n=validate_network(n)
    text = '''table inet ngpanel {
 chain transparent_in {
  type filter hook prerouting priority mangle; policy accept;
  iifname != "__INTERFACE__" return
  meta nfproto != ipv4 return
  ip saddr != __CIDR__ return
  meta mark 666 return
  meta l4proto { tcp, udp } th dport 53 return
  fib daddr type local return
  ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/3 } return
  meta l4proto tcp socket transparent 1 meta mark set 1 accept
  meta l4proto { tcp, udp } tproxy ip to :7895 meta mark set 1 accept
 }
 chain dns {
  type nat hook prerouting priority dstnat; policy accept;
  iifname "__INTERFACE__" ip saddr __CIDR__ meta l4proto { tcp, udp } th dport 53 redirect to :1053
 }
 chain local_input {
  type filter hook input priority filter; policy accept;
  iifname "__INTERFACE__" ip saddr != __CIDR__ meta l4proto { tcp, udp } th dport { 1053, 7895 } reject
  iifname "__INTERFACE__" ip daddr __ADDRESS__ meta l4proto { tcp, udp } th dport 7895 reject
 }
 chain forward_guard {
  type filter hook forward priority filter; policy accept;
  iifname != "__INTERFACE__" return
  meta nfproto ipv6 reject
  ip saddr != __CIDR__ return
  ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/3 } return
  meta l4proto { tcp, udp } reject
 }
}'''

    return text.replace("__INTERFACE__",n["interface"]).replace("__CIDR__",n["cidr"]).replace("__ADDRESS__",n["address"])

def disable_gateway():
    if run(['nft','list','table','inet','ngpanel'],check=False).returncode == 0:
        run(['nft','delete','table','inet','ngpanel'])
    run(['ip','-4','rule','del','priority','11000','fwmark','1','lookup','100'],check=False)
    run(['ip','-4','route','del','local','0.0.0.0/0','dev','lo','table','100'],check=False)

def enable_gateway(n):
    n=validate_network(n,True)
    # Preserve our policy rule across networkd restarts and DHCP reconfiguration.
    networkd_dir=pathlib.Path('/etc/systemd/networkd.conf.d')
    networkd_dir.mkdir(mode=0o755,exist_ok=True)
    networkd_config=networkd_dir/'90-ngpanel.conf'
    networkd_text='[Network]\nManageForeignRoutingPolicyRules=no\nManageForeignRoutes=no\n'
    if not networkd_config.exists() or networkd_config.read_text()!=networkd_text:
        atomic(networkd_config,networkd_text)
        run(['systemctl','restart','systemd-networkd'])
    # Refuse to take over another application's policy routing resources.
    rules = json.loads(run(['ip','-j','-4','rule']).stdout)
    for rule in rules:
        if rule.get('priority') == 11000 and (str(rule.get('table')) != '100' or str(rule.get('fwmark')) not in ('1','0x1')):
            raise ValueError('Policy routing priority is occupied')
    route_result = run(['ip','-j','-4','route','show','table','100'],check=False)
    routes = json.loads(route_result.stdout or '[]') if route_result.returncode == 0 else []
    if any(r.get('type') != 'local' or r.get('dst') != 'default' or r.get('dev') != 'lo' for r in routes):
        raise ValueError('Policy routing table 100 is occupied')
    text = nft_text(n)
    prefix = 'delete table inet ngpanel\n' if run(['nft','list','table','inet','ngpanel'],check=False).returncode == 0 else ''
    run(['nft','-c','-f','-'],input=prefix + text)
    if not any(r.get('priority') == 11000 for r in rules): run(MARK_RULE)
    run(['ip','-4','route','replace','local','0.0.0.0/0','dev','lo','table','100'])
    run(['nft','-f','-'],input=prefix + text)
    run(['sysctl','-w','net.ipv4.ip_forward=1','net.ipv4.conf.all.rp_filter=2'])
    pathlib.Path('/proc/sys/net/ipv4/conf',n['interface'],'rp_filter').write_text('2')
    atomic(NFT,text)

def restore(config_bytes, meta, was_active):
    if config_bytes is not None:
        atomic(CONF,config_bytes,0o640,pwd.getpwnam('ngxray').pw_gid)
    else:
        CONF.unlink(missing_ok=True)
    if was_active and config_bytes is not None:
        run(['systemctl','restart','xray'])
    else:
        run(['systemctl','stop','xray'],check=False)
    if meta.get('gateway'):
        enable_gateway(applied_network(meta))
    else:
        disable_gateway()

def apply(config, gateway, config_hash, n):
    validate(config)
    gid = pwd.getpwnam('ngxray').pw_gid
    candidate = pathlib.Path('/etc/ngpanel/candidate.json')
    atomic(candidate,json.dumps(config),0o640,gid)
    result = run(['runuser','-u','ngxray','--','/usr/local/bin/xray','run','-test','-c',str(candidate)],check=False)
    if result.returncode:
        candidate.unlink(missing_ok=True)
        details=(result.stdout+'\n'+result.stderr).strip()
        def redact(value):
            nonlocal details
            if isinstance(value,dict):
                for key,child in value.items():
                    if key in ('id','password','publicKey','shortId') and isinstance(child,str) and child:
                        details=details.replace(child,'[REDACTED]')
                    else: redact(child)
            elif isinstance(value,list):
                for child in value: redact(child)
        redact(config)
        print(details[-1500:],file=sys.stderr)
        reason=details.splitlines()[-1] if details else 'ошибка проверки'
        raise ValueError('Конфигурация отклонена Xray; рабочая версия сохранена. '+reason[:800])
    if action == 'check':
        candidate.unlink(missing_ok=True)
        return 'Конфигурация прошла проверку Xray; трафик не изменён'
    if gateway:
        if pathlib.Path('/etc/ngpanel/network-backup.json').exists():
            raise ValueError('Подтвердите доступность панели после изменения сети перед включением шлюза')
        n=validate_network(n,True)
        verify_default(n)
        text=nft_text(n)
        prefix='delete table inet ngpanel\n' if run(['nft','list','table','inet','ngpanel'],check=False).returncode==0 else ''
        run(['nft','-c','-f','-'],input=prefix+text)
    previous = CONF.read_text() if CONF.exists() else None
    old_meta = migrate_network()
    was_active = active()
    try:
        os.replace(candidate,CONF)
        run(['systemctl','restart','xray'])
        time.sleep(1)
        if not active(): raise ValueError('Xray did not remain running')
        if gateway: enable_gateway(n)
        else: disable_gateway()
    except Exception:
        restore(previous,old_meta,was_active)
        raise
    if previous is not None:
        atomic(BACKUP,previous,0o640,gid)
        atomic(PREVIOUS_META,json.dumps(old_meta),0o600)
    meta={'gateway':gateway,'hash':config_hash,'network':n if gateway else {},'balance':'observatory' in config}
    atomic(META,json.dumps(meta),0o600)
    run(['systemctl','enable','xray','ngpanel-gateway'],check=True)
    return 'Конфигурация применена. Xray запущен; шлюз ' + ('включён' if gateway else 'выключен')

def install_network_recovery():
    # Transient timers disappear on reboot. Recover an unconfirmed Netplan
    # change before either the panel or gateway starts after a reboot.
    unit=pathlib.Path('/etc/systemd/system/ngpanel-network-recovery.service')
    text='''[Unit]
Description=Restore unconfirmed NGPanel network changes
After=network.target
Before=ngpanel.service ngpanel-gateway.service

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /opt/ngpanel/control.py network-revert

[Install]
WantedBy=multi-user.target
'''
    if not unit.exists() or unit.read_text()!=text:
        atomic(unit,text,0o644)
        run(['systemctl','daemon-reload'])
    run(['systemctl','enable','ngpanel-network-recovery.service'])

def network(n):
    n=validate_network(n,True)
    migrate_network()
    target=pathlib.Path('/etc/netplan/90-ngpanel.yaml')
    backup=pathlib.Path('/etc/ngpanel/network-backup.json')
    if backup.exists():
        raise ValueError('Сначала подтвердите или дождитесь отката предыдущего изменения сети')
    install_network_recovery()
    atomic(backup,json.dumps({'content':target.read_text() if target.exists() else None,'network':n}),0o600)
    config='''network:
  version: 2
  ethernets:
    __INTERFACE__:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: []
      dhcp4-overrides:
        use-routes: false
        use-dns: false
      routes:
        - to: default
          via: __ROUTER__
      nameservers:
        addresses: [1.1.1.1, 8.8.8.8]
'''
    config=config.replace("__INTERFACE__",json.dumps(n["interface"])).replace("__ROUTER__",n["router"])
    atomic(target,config,0o600)
    try:
        run(['netplan','generate'])
        run(['systemd-run','--unit=ngpanel-network-revert','--on-active=120s','/usr/bin/python3','/opt/ngpanel/control.py','network-revert'])
        run(['netplan','apply'],timeout=60)
    except Exception:
        network_revert()
        raise
    return 'Выход через выбранный роутер применён временно. Подтвердите доступность панели в течение 120 секунд, иначе сеть откатится.'

def network_revert():
    backup=pathlib.Path('/etc/ngpanel/network-backup.json')
    if not backup.exists(): return
    saved=load(backup)
    target=pathlib.Path('/etc/netplan/90-ngpanel.yaml')
    if saved['content'] is None: target.unlink(missing_ok=True)
    else: atomic(target,saved['content'],0o600)
    run(['netplan','apply'],timeout=60)
    backup.unlink(missing_ok=True)
    runtime=load(ROOT/'runtime.json')
    runtime.update(State='ok',Action='network-revert',Network='reverted',Message='Изменение сети автоматически отменено: подтверждение не получено',Updated=datetime.datetime.now(datetime.timezone.utc).isoformat())
    atomic(ROOT/'runtime.json',json.dumps(runtime,ensure_ascii=False))

if __name__ == '__main__':
    if len(sys.argv)>1:
        if sys.argv[1]=='network-revert':
            with open('/run/ngpanel-control.lock','w') as lock:
                fcntl.flock(lock,fcntl.LOCK_EX)
                network_revert()
        elif sys.argv[1]=='gateway-restore':
            restore_gateway_on_boot()
        else: raise ValueError('Unknown fixed operation')
        sys.exit(0)
    with open('/run/ngpanel-control.lock','w') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        request=ROOT/'jobs/control.request'
        runtime=load(ROOT/'runtime.json')
        action=''
        try:
            fd=os.open(request,os.O_RDONLY|os.O_NOFOLLOW)
            with os.fdopen(fd,'rb') as file:
                info=os.fstat(file.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_uid!=pwd.getpwnam('ngpanel').pw_uid or info.st_size>2*1024*1024: raise ValueError('Invalid request')
                job=json.load(file)
            action=job.get('Action','')
            if action not in ('check','apply','start','stop','rollback','network','network-confirm','logs','geodata','dependencies'): raise ValueError('Unknown action')
            runtime.update(State='running',Action=action,Message='Выполняется задание')
            atomic(ROOT/'runtime.json',json.dumps(runtime,ensure_ascii=False))
            message=''
            if action in ('check','apply'): message=apply(job['config'],job.get('gateway') is True,job.get('config_hash',''),job.get('network'))
            elif action=='dependencies':
                run(['apt-get','update'],timeout=120)
                run(['apt-get','install','-y','--no-install-recommends','nftables','iproute2','curl','ca-certificates','avahi-utils','ieee-data'],timeout=150)
                message='Компоненты шлюза установлены'
            elif action=='geodata':
                result=run(['/usr/bin/python3','/opt/ngpanel/update-geodata.py'],check=False,timeout=270)
                if result.returncode:raise ValueError('Обновление geo-баз не завершено. '+(result.stderr.splitlines()[-1] if result.stderr else 'Подробности в журнале')[:500])
                message=result.stdout.strip()
            elif action=='start':
                if not CONF.exists():raise ValueError('Сначала примените конфигурацию')
                run(['systemctl','enable','--now','xray']);time.sleep(1)
                if not active():raise ValueError('Xray failed to start')
                message='Xray запущен'
            elif action=='stop':
                run(['systemctl','disable','--now','xray']);message='Xray остановлен, автозапуск отключён. При включённом шлюзе перехват сохраняется и блокирует TCP/UDP.'
            elif action=='rollback':
                if not BACKUP.exists():raise ValueError('Нет предыдущей конфигурации')
                old_meta=load(PREVIOUS_META)
                restore(BACKUP.read_text(),old_meta,True)
                atomic(META,json.dumps(old_meta),0o600)
                message='Предыдущая конфигурация восстановлена'
            elif action=='network': message=network(job.get('network'));runtime['Network']='pending'
            elif action=='network-confirm':
                if not pathlib.Path('/etc/ngpanel/network-backup.json').exists():raise ValueError('Нет ожидающего изменения сети')
                pending=load(pathlib.Path('/etc/ngpanel/network-backup.json'))
                if pending.get('network'):
                    validate_network(pending['network'],True)
                    verify_default(pending['network'])
                run(['systemctl','stop','ngpanel-network-revert.timer'],check=False)
                pathlib.Path('/etc/ngpanel/network-backup.json').unlink(missing_ok=True)
                runtime['Network']='direct';message='Выход ВМ через роутер подтверждён'
            elif action=='logs':
                text=run(['journalctl','-u','xray','-n','60','--no-pager']).stdout
                text=re.sub(r'(?i)(?:vless|vmess|trojan)://\S+','[REDACTED]',text)
                text=re.sub(r'[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}','[UUID]',text)
                atomic(ROOT/'xray-log',text);message='Журнал Xray обновлён'
            runtime.update(State='ok',Message=message)
        except Exception as exc:
            print(str(exc),file=sys.stderr)
            runtime.update(State='error',Message=str(exc) if isinstance(exc,ValueError) else 'Системное задание завершилось ошибкой; рабочие настройки сохранены, подробности в журнале ngpanel-control')
        finally:
            meta=load(META)
            runtime['AppliedNetwork']=meta.get('network',{})
            runtime['Balance']=bool(meta.get('balance'))
            runtime.update(Action=action,Gateway=bool(meta.get('gateway')),ConfigHash=meta.get('hash',''),Updated=datetime.datetime.now(datetime.timezone.utc).isoformat())
            atomic(ROOT/'runtime.json',json.dumps(runtime,ensure_ascii=False))
            request.unlink(missing_ok=True)

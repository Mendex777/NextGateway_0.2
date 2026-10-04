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
    if not isinstance(config, dict) or set(config) != {'log','dns','inbounds','outbounds','routing'}:
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
    if len(config['outbounds']) > 4:
        raise ValueError('Unexpected outbound count')
    for outbound in config['outbounds']:
        if outbound.get('protocol') not in ('vless','freedom','blackhole','dns'):
            raise ValueError('Unexpected outbound protocol')
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

def nft_text():
    return '''table inet ngpanel {
 chain transparent_in {
  type filter hook prerouting priority mangle; policy accept;
  iifname != "ens18" return
  meta nfproto != ipv4 return
  ip saddr != 192.168.1.0/24 return
  meta mark 666 return
  meta l4proto { tcp, udp } th dport 53 return
  fib daddr type local return
  ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/3 } return
  meta l4proto tcp socket transparent 1 meta mark set 1 accept
  meta l4proto { tcp, udp } tproxy ip to :7895 meta mark set 1 accept
 }
 chain dns {
  type nat hook prerouting priority dstnat; policy accept;
  iifname "ens18" ip saddr 192.168.1.0/24 meta l4proto { tcp, udp } th dport 53 redirect to :1053
 }
 chain local_input {
  type filter hook input priority filter; policy accept;
  iifname "ens18" ip saddr != 192.168.1.0/24 meta l4proto { tcp, udp } th dport { 1053, 7895 } reject
  iifname "ens18" ip daddr 192.168.1.84 meta l4proto { tcp, udp } th dport 7895 reject
 }
 chain forward_guard {
  type filter hook forward priority filter; policy accept;
  iifname != "ens18" return
  meta nfproto ipv6 reject
  ip saddr != 192.168.1.0/24 return
  ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/3 } return
  meta l4proto { tcp, udp } reject
 }
}'''

def disable_gateway():
    if run(['nft','list','table','inet','ngpanel'],check=False).returncode == 0:
        run(['nft','delete','table','inet','ngpanel'])
    run(['ip','-4','rule','del','priority','11000','fwmark','1','lookup','100'],check=False)
    run(['ip','-4','route','del','local','0.0.0.0/0','dev','lo','table','100'],check=False)

def enable_gateway():
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
    text = nft_text()
    prefix = 'delete table inet ngpanel\n' if run(['nft','list','table','inet','ngpanel'],check=False).returncode == 0 else ''
    run(['nft','-c','-f','-'],input=prefix + text)
    if not any(r.get('priority') == 11000 for r in rules): run(MARK_RULE)
    run(['ip','-4','route','replace','local','0.0.0.0/0','dev','lo','table','100'])
    run(['nft','-f','-'],input=prefix + text)
    run(['sysctl','-w','net.ipv4.ip_forward=1','net.ipv4.conf.all.rp_filter=2','net.ipv4.conf.ens18.rp_filter=2'])
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
        enable_gateway()
    else:
        disable_gateway()

def apply(config, gateway, config_hash):
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
        default = run(['ip','-4','route','show','default']).stdout
        if 'via 192.168.1.1 ' not in default:
            raise ValueError('Сначала настройте выход ВМ через роутер 192.168.1.1')
        text=nft_text()
        prefix='delete table inet ngpanel\n' if run(['nft','list','table','inet','ngpanel'],check=False).returncode==0 else ''
        run(['nft','-c','-f','-'],input=prefix+text)
    previous = CONF.read_text() if CONF.exists() else None
    old_meta = load(META)
    was_active = active()
    try:
        os.replace(candidate,CONF)
        run(['systemctl','restart','xray'])
        time.sleep(1)
        if not active(): raise ValueError('Xray did not remain running')
        if gateway: enable_gateway()
        else: disable_gateway()
    except Exception:
        restore(previous,old_meta,was_active)
        raise
    if previous is not None:
        atomic(BACKUP,previous,0o640,gid)
        atomic(PREVIOUS_META,json.dumps(old_meta),0o600)
    meta={'gateway':gateway,'hash':config_hash}
    atomic(META,json.dumps(meta),0o600)
    run(['systemctl','enable','xray','ngpanel-gateway'],check=True)
    return 'Конфигурация применена. Xray запущен; шлюз ' + ('включён' if gateway else 'выключен')

def network():
    target=pathlib.Path('/etc/netplan/90-ngpanel.yaml')
    backup=pathlib.Path('/etc/ngpanel/network-backup.json')
    if backup.exists():
        raise ValueError('Сначала подтвердите или дождитесь отката предыдущего изменения сети')
    atomic(backup,json.dumps({'content':target.read_text() if target.exists() else None}),0o600)
    config='''network:
  version: 2
  ethernets:
    ens18:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: []
      dhcp4-overrides:
        use-routes: false
        use-dns: false
      routes:
        - to: default
          via: 192.168.1.1
      nameservers:
        addresses: [1.1.1.1, 8.8.8.8]
'''
    atomic(target,config,0o600)
    try:
        run(['netplan','generate'])
        run(['systemd-run','--unit=ngpanel-network-revert','--on-active=120s','/usr/bin/python3','/opt/ngpanel/control.py','network-revert'])
        run(['netplan','apply'],timeout=60)
    except Exception:
        network_revert()
        raise
    return 'Выход через 192.168.1.1 применён временно. Подтвердите доступность панели в течение 120 секунд, иначе сеть откатится.'

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
        if sys.argv[1]=='network-revert': network_revert()
        elif sys.argv[1]=='gateway-restore':
            if load(META).get('gateway'): enable_gateway()
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
            if action in ('check','apply'): message=apply(job['config'],job.get('gateway') is True,job.get('config_hash',''))
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
            elif action=='network': message=network();runtime['Network']='pending'
            elif action=='network-confirm':
                if not pathlib.Path('/etc/ngpanel/network-backup.json').exists():raise ValueError('Нет ожидающего изменения сети')
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
            runtime.update(Action=action,Gateway=bool(meta.get('gateway')),ConfigHash=meta.get('hash',''),Updated=datetime.datetime.now(datetime.timezone.utc).isoformat())
            atomic(ROOT/'runtime.json',json.dumps(runtime,ensure_ascii=False))
            request.unlink(missing_ok=True)

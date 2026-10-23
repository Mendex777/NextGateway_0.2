import {metricsFresh} from "./metrics-freshness";
import {useEffect,useState} from "react";
import {App,Button,Popover,Tooltip} from "antd";
import {ArrowUpCircle,RefreshCw,FileJson,Archive,RotateCw,Square,Play} from "lucide-react";
import {getJSON} from "./api";
import type {Page} from "./types";
import type {Run} from "./common";
import type {Metrics} from "./Dashboard";
import {SizeFormatter} from "./reference/utils";
type Check={ID:string;Name:string;State:string;Summary:string;Expected:string;Actual:string};
type Health={Updated:string;Checks:Check[];Message:string};
type Update={ID:string;Current:string;Latest:string;Available:boolean;Error:string};
type Updates={Updated:string;Busy:boolean;Components:Update[]};
const components=[['xray','Xray'],['config','Конфигурация'],['network','Сеть'],['nft','nftables'],['routing','Маршруты'],['kernel','Linux'],['listeners','Входы'],['dns','DNS'],['geo','Geo-базы'],['autostart','Автозапуск']];
const labels:Record<string,string>={good:'Готов',bad:'Сбой',warn:'Проверить',unknown:'Нет данных',neutral:'Не требуется'};
const geoDate=(v:string)=>/^\d{8}/.test(v)?`${v.slice(6,8)}.${v.slice(4,6)}.${v.slice(0,4)}`:v;
export default function ServiceHealth({p,run,working,metrics,onTool}:{p:Page;run:Run;working:boolean;metrics?:Metrics;onTool:(tool:string)=>void}){
 const {modal}=App.useApp();
 const active=p.Service.trim()==="active";
 const fresh=metrics&&metricsFresh(metrics)?metrics:undefined;
 const [health,setHealth]=useState<Health>();const [updates,setUpdates]=useState<Updates>();
 const [failed,setFailed]=useState(false);const [now,setNow]=useState(Date.now());
 useEffect(()=>{
  const abort=new AbortController();let busy=false;
  const load=async()=>{setNow(Date.now());if(busy||document.hidden)return;busy=true;await Promise.allSettled([
   getJSON<Health>('/api/service-health',abort.signal).then(h=>{if(!abort.signal.aborted){setHealth(h);setFailed(false);}}).catch(()=>{if(!abort.signal.aborted)setFailed(true);}),
   getJSON<Updates>('/api/component-updates',abort.signal).then(u=>{if(!abort.signal.aborted)setUpdates(u);})
  ]);busy=false;};
  void load();const timer=setInterval(load,5000);document.addEventListener('visibilitychange',load);
  return()=>{abort.abort();clearInterval(timer);document.removeEventListener('visibilitychange',load);};
 },[]);
 const stamp=Date.parse(health?.Updated||'');const stale=failed||!Number.isFinite(stamp)||now-stamp>45000||stamp-now>5000;
 const checks=components.map(([ID,Name])=>{const c=health?.Checks?.find(c=>c.ID===ID);return !c||stale?{ID,Name,State:'unknown',Summary:'Нет актуальной проверки',Expected:c?.Expected||'Актуальный отчёт',Actual:'Ожидается ответ службы проверки'}:{...c,Name};});
 const broken=checks.some(c=>c.State==='bad');const problems=checks.filter(c=>c.State==='bad'||c.State==='warn');const unknown=checks.some(c=>c.State==='unknown');
 const detail=(c:Check,u?:Update)=><div className="service-popover"><p>{c.Summary}</p>{c.ID==='xray'&&<p>Память процесса: {fresh&&fresh.xrayPID>0?SizeFormatter.sizeFormat(fresh.xrayMemory):'—'}</p>}<dl><dt>Ожидается</dt><dd>{c.Expected}</dd><dt>Проверено</dt><dd>{c.Actual}</dd></dl>{u&&<p>{u.Error||`Последний выпуск: ${u.Latest||'ещё не проверен'}`}</p>}</div>;
 const update=(id:string,u:Update)=>modal.confirm({title:`Обновить ${id==='panel'?'NGPanel':id==='xray'?'Xray':'geo-базы'} до ${u.Latest}?`,content:id==='panel'?'Панель перезапустится. Xray продолжит работу.':id==='xray'?'Новый файл будет проверен перед заменой. Текущие соединения кратковременно прервутся при перезапуске Xray.':'Базы будут проверены на совместимость с вашими правилами. Затем Xray перезапустится.',okText:'Обновить',cancelText:'Отмена',onOk:()=>run(id==='panel'?'panel-update-install':id==='xray'?'xray-update':'geodata')});
 return <section className="home-service" aria-label="Состояние сервиса">
  <header><h2>Сервис</h2><span role="status" className={'service-overall '+(broken?'bad':problems.length?'warn':unknown?'unknown':'good')}>{problems.length?`Требуют внимания: ${problems.length}`:unknown?'Нет полной проверки':'Работает'}</span><Tooltip title="Проверить обновления Xray, geo-баз и панели"><Button type="text" size="small" aria-label="Проверить обновления компонентов" disabled={working||updates?.Busy} icon={<RefreshCw size={13} className={updates?.Busy?'service-refreshing':''}/>} onClick={()=>void run('component-updates-check').catch(()=>{})}/></Tooltip></header>
  <div className="service-software">{['panel','xray','geo'].map(id=>{
   const u=updates?.Components?.find(c=>c.ID===id);const c=id==='panel'?{ID:id,Name:'NGPanel',State:'good',Summary:'Панель работает',Expected:'Доступная веб-панель',Actual:p.PanelVersion}:checks.find(c=>c.ID===id)!;
   const version=id==='panel'?p.PanelVersion:id==='xray'?(p.Version.match(/\d+\.\d+\.\d+/)?.[0]||'Не установлен'):geoDate(u?.Current||'geosite / geoip');
   return <div className="service-software-item" key={id}><Popover trigger="click" title={c.Name} content={detail(c,u)}><button className={'service-chip '+c.State} aria-label={`${c.Name}: ${labels[c.State]}. Подробнее`}><i/><b>{c.Name}</b><span className="service-version">{version}</span>{id==='xray'&&fresh&&fresh.xrayPID>0&&<span className="service-memory" title="Память процесса Xray">{SizeFormatter.sizeFormat(fresh.xrayMemory)}</span>}</button></Popover>{u?.Available&&!u.Error&&<Tooltip title={`Доступен выпуск ${u.Latest}`}><Button size="small" type="text" className="service-update" icon={<ArrowUpCircle size={14}/>} disabled={working} onClick={()=>update(id,u)}>Обновить</Button></Tooltip>}</div>;
  })}</div>
  <div className="service-actions">
   <Button size="small" icon={<FileJson size={13}/>} onClick={()=>onTool('config')}>Конфигурация</Button>
   <Button size="small" icon={<Archive size={13}/>} onClick={()=>onTool('backup')}>Бэкап</Button>
   <Button size="small" disabled={working} icon={active?<RotateCw size={13}/>:<Play size={13}/>} onClick={()=>onTool(active?'restart':'start')}>{active?'Перезапустить Xray':'Запустить Xray'}</Button>
   <Button size="small" danger disabled={working||!active} icon={<Square size={12}/>} onClick={()=>onTool('stop')}>Остановить Xray</Button>
  </div>
  <div className="service-checks">{checks.filter(c=>c.ID!=='xray'&&c.ID!=='geo').map(c=><Popover key={c.ID} trigger="click" title={c.Name} content={detail(c)}><button className={'service-chip '+c.State} aria-label={`${c.Name}: ${labels[c.State]}. Подробнее`}><i/>{c.Name}{c.State!=='good'&&c.State!=='neutral'&&<em>{labels[c.State]}</em>}</button></Popover>)}</div>
  {(problems.length>0||failed||p.ConfigError||p.Pending)&&<div className="service-issues" role="status">{problems.map(c=><p key={c.ID} className={c.State}>{c.Name}: {c.Summary}</p>)}{failed&&<p>Проверка компонентов недоступна</p>}{(p.ConfigError||p.Pending)&&<p className="warn">{p.ConfigError?'Ошибка редактируемой конфигурации':'Есть неприменённые изменения'}</p>}</div>}
 </section>;
}

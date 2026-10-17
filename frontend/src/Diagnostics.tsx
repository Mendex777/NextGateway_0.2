import {useEffect,useState} from "react";
import {Alert,Button,Collapse,Tabs} from "antd";
import {getJSON} from "./api";
import type {Page} from "./types";
import type {Run} from "./common";
import LogViewer from "./reference/LogViewer";
import "./diagnostics.css";

type Check={ID:string;Name:string;State:string;Summary:string;Expected:string;Actual:string};
type Health={Updated:string;Checks:Check[];Message:string};
const states:Record<string,string>={good:'Готов',bad:'Сбой',warn:'Проверить',unknown:'Нет данных',neutral:'Не требуется'};
const networkIDs=['network','nft','routing','kernel','listeners','dns'];

export default function Diagnostics({p,run,working,view,navigate}:{p:Page;run:Run;working:boolean;view:string;navigate:(tab:string)=>void}){
 const selected=['logs','checks','network'].includes(view)?view:'logs';
 const [health,setHealth]=useState<Health>(),[error,setError]=useState(''),[now,setNow]=useState(Date.now());
 useEffect(()=>{
  if(selected==='logs')return;
  const abort=new AbortController();let busy=false;
  const load=async()=>{
   if(busy||document.hidden)return;busy=true;setNow(Date.now());
   try{const result=await getJSON<Health>('/api/service-health',abort.signal);if(!abort.signal.aborted){setHealth(result);setError('');}}
   catch{if(!abort.signal.aborted)setError('Не удалось получить проверку компонентов');}
   finally{busy=false;}
  };
  void load();const timer=setInterval(load,10000);document.addEventListener('visibilitychange',load);
  return()=>{abort.abort();clearInterval(timer);document.removeEventListener('visibilitychange',load);};
 },[selected]);
 const stamp=Date.parse(health?.Updated||'');
 const stale=!!error||!Number.isFinite(stamp)||now-stamp>45000||stamp-now>5000;
 const checks=(health?.Checks||[]).map(c=>stale?{...c,State:'unknown',Summary:'Нет актуальной проверки'}:c);
 const problems=checks.filter(c=>c.State==='bad'||c.State==='warn');
 const details=(rows:Check[])=><Collapse className="diagnostic-checks" items={rows.map(c=>({key:c.ID,label:<span className="diagnostic-check-label"><span className={'diagnostic-state '+c.State}>{states[c.State]||c.State}</span><b>{c.Name}</b><span>{c.Summary}</span></span>,children:<dl className="diagnostic-details"><dt>Ожидается</dt><dd>{c.Expected}</dd><dt>{stale?'Последняя проверка':'Проверено'}</dt><dd>{c.Actual}</dd></dl>}))}/>;
 const content=<section className="diagnostic-panel">
   {selected==='logs'?<LogViewer/>:<>
    <div className="diagnostic-toolbar"><h2>{selected==='checks'?'Проверки сервиса':'Сеть шлюза'}</h2>{selected==='checks'&&<Button size="small" loading={working} onClick={()=>void run('check').catch(()=>{})}>Проверить конфигурацию</Button>}<span className="diagnostic-muted" role="status">{health?.Updated?'Проверено '+new Date(health.Updated).toLocaleTimeString('ru-RU'):'Ожидаем отчёт'}</span></div>
    {error&&<Alert type="warning" title={error}/>}
    {stale&&<Alert type="warning" title={health?.Message||'Нет актуального отчёта службы проверки компонентов'}/>}
    {selected==='checks'?<>
     <div className="diagnostic-operation"><h3>Последняя операция</h3><Alert type={p.Runtime.State==='error'?'error':p.Runtime.State==='ok'?'success':'info'} title={p.Runtime.Message||'Операций пока нет'}/></div>
     {p.ConfigError&&<Alert type="error" title={p.ConfigError}/>}
     {p.Pending&&<Alert type="warning" title="Есть неприменённые изменения. Проверка работающего сервиса относится к применённой конфигурации."/>}
     {checks.length>0&&<><p className="diagnostic-muted">{stale?'Статус компонентов неизвестен':problems.length?`Требуют внимания: ${problems.length}`:'Компоненты работают штатно'}. Нажмите на компонент для подробностей.</p>{details(checks)}</>}
    </>:<>
     <dl className="diagnostic-network">
      <div><dt>Адрес ВМ</dt><dd>{p.DetectedNetwork.address||'—'}</dd></div>
      <div><dt>Интерфейс</dt><dd>{p.DetectedNetwork.interface||'—'}</dd></div>
      <div><dt>Подсеть</dt><dd>{p.DetectedNetwork.cidr||'—'}</dd></div>
      <div><dt>Роутер</dt><dd>{p.DetectedNetwork.router||'—'}</dd></div>
      <div><dt>DNS Direct · сохранённый</dt><dd>{p.DNSDirectServers||'Не задан'}</dd></div>
      <div><dt>DNS VPN · сохранённый</dt><dd>{p.DNSVPNServers||'Не задан'}</dd></div>
     </dl>
     {details(checks.filter(c=>networkIDs.includes(c.ID)))}
     <h3>Политики маршрутизации Linux</h3><pre className="diagnostic-code">{p.Routes||'Сведения о маршрутизации недоступны'}</pre>
    </>}
   </>}
  </section>;
 return <div className="diagnostics-page">
  <header className="diagnostic-heading"><h1>Диагностика</h1></header>
  <Tabs activeKey={selected} destroyOnHidden onChange={key=>navigate('diagnostics&view='+key)} items={[{key:'logs',label:'Журналы'},{key:'checks',label:'Проверки'},{key:'network',label:'Сеть'}].map(item=>({...item,children:item.key===selected?content:null}))}/>
 </div>;
}

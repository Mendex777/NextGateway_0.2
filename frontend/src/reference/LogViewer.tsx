// Adapted from 3x-ui v3.9.0 (GPL-3.0); NGPanel journal adapter.
import {useEffect,useMemo,useRef,useState} from 'react';
import {Alert,Button,Checkbox,Grid,Select} from 'antd';
import {DownloadOutlined,SyncOutlined} from '../icons';
import {getJSON,action} from '../api';
import {parseLogLine} from './logParse';
import './LogViewer.css';

export default function LogViewer(){
 const isMobile=!Grid.useBreakpoint().md;
 const [rows,setRows]=useState('100'),[level,setLevel]=useState('info'),[source,setSource]=useState('panel');
 const [autoUpdate,setAutoUpdate]=useState(false),[loading,setLoading]=useState(false),[text,setText]=useState(''),[error,setError]=useState('');
 const refreshRef=useRef<()=>void>(()=>{});
 useEffect(()=>{
  let stopped=false,busy=false;const abort=new AbortController();
  const load=async()=>{
   if(busy||stopped)return;busy=true;setLoading(true);
   try{
    const request=await action('logs','diagnostics');
    if(stopped)return;
    if(request.operation){
     let done=false;
     for(let attempt=0;attempt<30&&!stopped;attempt++){
      const status=await getJSON<{Done:boolean;State:string;Message:string}>('/operation-status?'+new URLSearchParams({action:'logs',since:request.since}),abort.signal);
      if(status.State==='error')throw new Error(status.Message);
      if(status.Done){done=true;break;}
      await new Promise(resolve=>setTimeout(resolve,1000));
     }
     if(stopped)return;
     if(!done)throw new Error('Обновление журнала ещё выполняется. Повторите запрос.');
    }
    const result=await getJSON<{text:string}>('/api/dashboard/logs?service='+source,abort.signal);
    if(!stopped){setText(result.text);setError('');}
   }catch(e){
    if(!stopped){
     setError('Не удалось обновить журнал: '+(e instanceof Error?e.message:'служба недоступна'));
     try{const saved=await getJSON<{text:string}>('/api/dashboard/logs?service='+source,abort.signal);if(!stopped)setText(saved.text);}catch{/* Keep the last visible snapshot when collection is unavailable. */}
    }
   }
   finally{busy=false;if(!stopped)setLoading(false);}
  };
  setText('');setError('');refreshRef.current=()=>void load();void load();
  return()=>{stopped=true;abort.abort();refreshRef.current=()=>{};};
 },[source]);
 useEffect(()=>{
  if(!autoUpdate)return;
  const timer=setInterval(()=>{if(!document.hidden)refreshRef.current();},5000);
  return()=>clearInterval(timer);
 },[autoUpdate]);
 const logs=useMemo(()=>{
  const rank=['debug','info','notice','warning','err'].indexOf(level);
  const timestamp=(line:string)=>{const stamp=parseLogLine(line).stamp;const date=Date.parse(stamp+' '+new Date().getFullYear());return Number.isFinite(date)?date:0;};
  return text.split('\n').filter(line=>{
   if(!line.trim())return false;
   const severity=['DEBUG','INFO','NOTICE','WARNING','ERROR'].indexOf(parseLogLine(line).levelText);
   return severity<0||severity>=rank;
  }).sort((a,b)=>timestamp(a)-timestamp(b)).slice(-Number(rows));
 },[text,rows,level]);
 const parsedLogs=useMemo(()=>logs.map(parseLogLine),[logs]);
 const download=()=>{
  const url=URL.createObjectURL(new Blob([logs.join('\n')],{type:'text/plain;charset=utf-8'}));
  const link=document.createElement('a');link.href=url;link.download=`ngpanel-${source}.log`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
 };
 return <section className="diagnostic-logs" aria-label="Журналы">
  <div className="log-toolbar">
   <Select aria-label="Источник журнала" size="small" value={source} onChange={setSource} style={{width:154}} options={[{value:'panel',label:'NGPanel'},{value:'xray',label:'Xray'},{value:'system',label:'NGPanel и Xray'}]}/>
   <Select aria-label="Количество записей" size="small" value={rows} onChange={setRows} style={{width:112}} options={['20','50','100','500','1000'].map(value=>({value,label:value+' записей'}))}/>
   <Select aria-label="Уровень журнала" size="small" value={level} onChange={setLevel} style={{width:148}} options={[{value:'debug',label:'Отладка'},{value:'info',label:'Информация'},{value:'notice',label:'Уведомление'},{value:'warning',label:'Предупреждение'},{value:'err',label:'Ошибка'}]}/>
   <Checkbox checked={autoUpdate} onChange={e=>setAutoUpdate(e.target.checked)}>Автообновление</Checkbox>
   <div className="log-actions"><Button size="small" loading={loading} icon={<SyncOutlined/>} onClick={()=>refreshRef.current()}>Обновить</Button><Button size="small" disabled={!logs.length} icon={<DownloadOutlined/>} onClick={download}>Скачать</Button></div>
  </div>
  {error&&<Alert type="warning" title={error}/>}
  {loading&&!text&&<p className="diagnostic-muted" role="status">Получаем журнал…</p>}
      <div className={`log-container ${isMobile ? 'log-container-mobile' : ''}`} aria-label="Записи журнала" aria-busy={loading}>
        {parsedLogs.length === 0 ? (
          <div className="log-empty">Нет записей</div>
        ) : isMobile ? (
          parsedLogs.map((log, idx) => (
            <div key={idx} className="log-card">
              <div className="log-card-head">
                {log.stamp && (
                  <span className="log-time">
                    {log.time && <span>{log.time}</span>}
                    {log.time && log.date ? ' ' : ''}
                    {log.date && <span className="log-date">{log.date}</span>}
                  </span>
                )}
                {log.levelText && (
                  <span className={`log-level-badge ${log.levelClass}`}>{log.levelText}</span>
                )}
              </div>
              {(log.body || log.service) && (
                <div className="log-body">
                  {log.service && <b>{log.service}</b>}
                  {log.service && log.body ? ' ' : ''}
                  {log.body && <span className="log-body-text">{log.body}</span>}
                </div>
              )}
            </div>
          ))
        ) : (
          parsedLogs.map((log, idx) => (
            <div key={idx} className="log-line">
              {log.stamp && <span className="log-stamp">{log.stamp}</span>}
              {log.stamp && log.levelText ? ' ' : ''}
              {log.levelText && (
                <span className={`log-level ${log.levelClass}`}>{log.levelText}</span>
              )}
              {(log.body || log.service) && (
                <>
                  {(log.stamp || log.levelText) && <span> - </span>}
                  {log.service && <b>{log.service}</b>}
                  {log.service && log.body ? ' ' : ''}
                  <span>{log.body}</span>
                </>
              )}
            </div>
          ))
        )}
      </div>
 </section>;
}

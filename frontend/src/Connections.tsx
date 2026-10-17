import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Input, Select, Space, Segmented, Switch, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { getJSON } from "./api";
import { ReloadOutlined, SearchOutlined } from "./icons";
import { FlagText } from "./FlagText";
interface Event {
 TCPState?: string; StartKnown?: boolean; ClosedAt?: string; Rule?: string; Domain?: string; ID: string; Time: string; Source: string; SourcePort: string; Device: string;
 Destination: string; Port: string; Network: string; Inbound: string; Outbound: string; OutboundLabel: string;
}
interface Snapshot { TCPAvailable?: boolean; Updated: string; Events: Event[]; Message: string }
export default function Connections({ active }: { active: boolean }) {
 const [snapshot, setSnapshot] = useState<Snapshot>({ Updated: "", Events: [], Message: "" });
 const [view,setView]=useState("active");
 const [current,setCurrent]=useState(1),[pageSize,setPageSize]=useState(100);
 const [error, setError] = useState("");
 const [paused, setPaused] = useState(false), [query, setQuery] = useState("");
 const [source, setSource] = useState<string|undefined>(()=>new URLSearchParams(location.search).get("source")||undefined), [outbound, setOutbound] = useState<string>();
 const [internal, setInternal] = useState(false), [refresh, setRefresh] = useState(0);
 const [loading, setLoading] = useState(false);
 useEffect(() => {
  if (!active || paused) {setLoading(false); return;}
  const abort = new AbortController(); let busy = false;
  const load = async () => {
   if (busy || document.hidden) return; busy = true;
   try { const data = await getJSON<Snapshot>("/api/connections", abort.signal); if (!abort.signal.aborted) {setSnapshot(data); setError("");} }
   catch {if (!abort.signal.aborted) setError("Не удалось получить журнал соединений");}
   finally {busy = false; if (!abort.signal.aborted) setLoading(false);}
  };
  setLoading(true); void load(); const timer = setInterval(() => void load(), 500);
  return () => {abort.abort(); clearInterval(timer);};
 }, [active, paused, refresh]);
 const events = useMemo(() => (snapshot.Events || []).filter((e) => internal || e.Inbound === "tproxy-in"), [snapshot.Events, internal]);
 const counts=useMemo(()=>({active:events.filter(e=>e.Network==="tcp"&&e.TCPState==="active").length,closed:events.filter(e=>e.Network==="tcp"&&e.TCPState==="closed").length,udp:events.filter(e=>e.Network==="udp").length,unknown:events.filter(e=>e.Network==="tcp"&&!e.TCPState).length}),[events]);
 const visible=useMemo(()=>events.filter(e=>view==="udp"?e.Network==="udp":e.Network==="tcp"&&e.TCPState===view&&(view!=="active"||(snapshot.TCPAvailable&&!error))),[events,view,snapshot.TCPAvailable,error]);
 useEffect(()=>{setCurrent(1)},[view,query,source,outbound,internal]);
 const sources = useMemo(() => Array.from(new Map(events.map((e) => [e.Source, {value: e.Source, label: e.Device && e.Device !== e.Source ? `${e.Device} · ${e.Source}` : e.Source}])).values()).sort((a,b) => a.label.localeCompare(b.label)), [events]);
 const outbounds = useMemo(() => Array.from(new Map(events.map((e) => [e.Outbound, {value:e.Outbound,label:e.OutboundLabel}])).values()), [events]);
 const filtered = useMemo(() => {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return visible.filter((e) => (!source || e.Source === source) && (!outbound || e.Outbound === outbound) && words.every((word) => [e.Rule,e.Domain,e.Device,e.Source,e.Destination,e.Port,e.Network,e.Outbound,e.OutboundLabel].join(" ").toLocaleLowerCase().includes(word))).slice().sort((a,b)=>new Date(view==="closed"?b.ClosedAt!:b.Time).getTime()-new Date(view==="closed"?a.ClosedAt!:a.Time).getTime());
 }, [visible,source,outbound,query,view]);
 const endpoint = (host: string, port: string) => `${host.includes(":") ? `[${host}]` : host}:${port}`;
 const columns: ColumnsType<Event> = [
  {title:view==="closed"?"Закрыто":"Начало",width:86,render:(_,e)=><Tooltip title={view==="closed" ? "Исчезновение сокета зафиксировано: "+new Date(e.ClosedAt!).toLocaleString("ru-RU") : (e.StartKnown ? "Начало: " : "Начало неизвестно. Впервые обнаружено: ")+new Date(e.Time).toLocaleString("ru-RU")}><span className="connection-time">{new Date(view==="closed"?e.ClosedAt!:e.Time).toLocaleTimeString("ru-RU")}{view!=="closed"&&!e.StartKnown?" *":""}</span></Tooltip>},
  {title:"Устройство",width:160,ellipsis:true,render:(_,e)=><Tooltip title={`${e.Device || e.Source} · ${e.Source}:${e.SourcePort}`}><span>{e.Device && e.Device!==e.Source ? <><FlagText text={e.Device}/><span className="connection-secondary">{e.Source}</span></> : e.Source}</span></Tooltip>},
  {title:"Назначение",width:260,render:(_,e)=><div className="connection-destination"><Tooltip title={e.Domain ? `Распознано Xray: ${e.Domain} · Исходный адрес: ${endpoint(e.Destination,e.Port)}` : endpoint(e.Destination,e.Port)}><span className="connection-host">{endpoint(e.Domain || e.Destination,e.Port)}</span></Tooltip>{e.Domain && e.Domain!==e.Destination && <span className="connection-secondary">{endpoint(e.Destination,e.Port)}</span>}</div>},
  {title:"Тип",width:64,render:(_,e)=><span className="connection-protocol">{e.Network.toUpperCase()}</span>},
  {title:"Правило",dataIndex:"Rule",width:170,ellipsis:true,render:(rule:string)=><Tooltip title={rule || "Xray не сообщил правило для этой записи"}><span>{rule || "—"}</span></Tooltip>},
  {title:"Исходящий",width:300,ellipsis:true,render:(_,e)=><Tooltip title={`${e.OutboundLabel} · ${e.Outbound}`}><span><FlagText text={e.OutboundLabel}/></span></Tooltip>},
  ...(internal ? [{title:"Входящий",dataIndex:"Inbound",width:110}] : []),
 ];
 return <div className="connections-panel">
  <Space wrap style={{width:"100%",justifyContent:"space-between",marginBottom:8}}>
   <Space size={10}><Typography.Text strong>Соединения</Typography.Text><Typography.Text type="secondary">Записей: {filtered.length}</Typography.Text></Space>
   <Space size={6}><Button size="small" aria-pressed={paused} onClick={()=>setPaused(!paused)}>{paused?"Продолжить":"Пауза"}</Button><Button size="small" icon={<ReloadOutlined/>} disabled={paused} loading={loading} onClick={()=>setRefresh((v)=>v+1)}>Обновить</Button></Space>
  </Space>
  <Segmented className="connection-views" size="small" value={view} onChange={v=>setView(String(v))} options={[{value:"active",label:`Активные TCP (${snapshot.TCPAvailable&&!error?counts.active:"—"})`},{value:"closed",label:`Закрытые TCP (${counts.closed})`},{value:"udp",label:`Журнал UDP (${counts.udp})`}]}/>
  <div className="connection-caption">{view==="active"?"Открытые сокеты Xray":view==="closed"?"Закрытия за последние 15 минут":"Обращения UDP за последние 15 минут"} · обновление 0,5 с · {paused ? "пауза" : "автоматически"}<Tooltip title="TCP через прозрачный шлюз проверяется по сокетам Xray. В закрытые попадают только соединения, которые сборщик сначала наблюдал открытыми. Время закрытия — момент обнаружения. Короткие подключения между опросами могут не попасть в эти два списка. UDP остаётся журналом без определения активности. Звёздочка у времени означает, что начало неизвестно. История хранится в памяти сборщика и очищается при его перезапуске."><span className="connection-help" tabIndex={0}>Об учёте соединений</span></Tooltip>{view!=="udp"&&counts.unknown>0&&<Tooltip title="Сборщик не наблюдал эти TCP-сокеты открытыми: это старые записи журнала либо подключения короче интервала опроса. Их закрытие не подтверждено."><span className="connection-help" tabIndex={0}>Без подтверждённого состояния: {counts.unknown}</span></Tooltip>}</div>
  {(error || snapshot.Message) && <Alert showIcon type="warning" title={error || snapshot.Message} style={{marginBottom:12}}/>}
  <Space wrap size={[6,6]} className="connection-filters">
   <Input size="small" prefix={<SearchOutlined/>} placeholder="Поиск адреса или устройства" aria-label="Поиск соединений" value={query} onChange={(e)=>setQuery(e.target.value)} allowClear style={{width:240}}/>
   <Select size="small" aria-label="Устройство" placeholder="Все устройства" value={source} onChange={setSource} options={sources} allowClear showSearch optionFilterProp="label" style={{width:185}}/>
   <Select size="small" aria-label="Исходящий" placeholder="Все исходящие" value={outbound} onChange={setOutbound} options={outbounds} allowClear showSearch optionFilterProp="label" style={{width:245}}/>
   <Space><Switch size="small" aria-label="Показывать DNS и служебные обращения" checked={internal} onChange={setInternal}/><span>DNS и служебные обращения</span></Space>
  </Space>

  <Table<Event> className="connections-table" size="small" rowKey="ID" columns={columns} dataSource={filtered} loading={loading && !events.length} scroll={{x:1140,y:"max(240px, calc(100dvh - 420px))"}} pagination={{current:Math.min(current,Math.max(1,Math.ceil(filtered.length/pageSize))),pageSize,onChange:(page,size)=>{setCurrent(page);setPageSize(size)},showSizeChanger:{showSearch:false,style:{width:120}},pageSizeOptions:[25,50,100],showTotal:(total)=>`Всего: ${total}`}} locale={{emptyText:"Обращений по выбранным условиям пока нет"}}/>
 </div>;
}

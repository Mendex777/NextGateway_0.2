import {Alert,Modal,theme} from "antd";
import type {Metrics} from "./Dashboard";
import Sparkline from "./reference/Sparkline";
import {SizeFormatter} from "./reference/utils";

const percentage=(used:number,total:number)=>total>0?used/total*100:0;
const formatPercent=(value:number)=>`${value.toFixed(1)} %`;
const time=(value:number)=>new Date(value).toLocaleTimeString('ru-RU');

export default function SystemHistory({open,onClose,samples,failure}:{open:boolean;onClose:()=>void;samples:Metrics[];failure:string}){
 const {token}=theme.useToken();
 const latest=samples.at(-1);
 const rows=[
  {id:'cpu',label:'Процессор',values:samples.map(s=>s.cpu),detail:latest?`Ядер: ${latest.cores} · ${(latest.cpuMHz/1000).toFixed(2)} ГГц`:''},
  {id:'memory',label:'Память',values:samples.map(s=>percentage(s.memory,s.memoryTotal)),detail:latest?`${SizeFormatter.sizeFormat(latest.memory)} / ${SizeFormatter.sizeFormat(latest.memoryTotal)}`:''},
  {id:'swap',label:'Подкачка',values:samples.map(s=>percentage(s.swap,s.swapTotal)),detail:latest?`${SizeFormatter.sizeFormat(latest.swap)} / ${SizeFormatter.sizeFormat(latest.swapTotal)}`:''},
  {id:'disk',label:'Диск',values:samples.map(s=>percentage(s.disk,s.diskTotal)),detail:latest?`${SizeFormatter.sizeFormat(latest.disk)} / ${SizeFormatter.sizeFormat(latest.diskTotal)}`:''},
 ];
 return <Modal className="home-dialog system-history" open={open} onCancel={onClose} title="История ресурсов ВМ" width={960} footer={null} destroyOnHidden>
  {failure&&<Alert type="warning" title={failure}/>}
  {samples.length>0?<>
   <p className="system-history-range">{time(samples[0].time)} — {time(latest!.time)} · загрузка ресурсов в процентах</p>
   <div className="system-history-grid">{rows.map(row=><section className="system-history-metric" key={row.id} aria-label={row.label}>
    <header><h3>{row.label}</h3><b>{formatPercent(row.values.at(-1)!)}</b></header>
    <p>{row.detail}</p>
    <Sparkline data={row.values} labels={samples.map(s=>time(s.time))} maxPoints={samples.length} height={170} stroke={token.colorPrimary} strokeWidth={1.7} valueMin={0} valueMax={100} yTickStep={25} tickCountX={3} showAxes showTooltip showLegend={false} showMarker fillOpacity={0} name1={row.label} yFormatter={formatPercent}/>
   </section>)}</div>
  </>:<p className="system-history-range">Показатели ещё не получены.</p>}
 </Modal>;
}

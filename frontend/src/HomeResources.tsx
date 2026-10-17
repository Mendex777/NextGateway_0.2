import {Button,Popover} from "antd";
import {Cpu,MemoryStick,HardDrive,Clock,ChartNoAxesCombined,ScrollText} from "lucide-react";
import type {Metrics} from "./Dashboard";
import {SizeFormatter} from "./reference/utils";
const bytes=SizeFormatter.sizeFormat;
const pct=(used:number,total:number)=>total>0?Math.max(0,Math.min(100,used/total*100)):0;
const duration=(s:number)=>s>=86400?`${Math.floor(s/86400)} д ${Math.floor(s%86400/3600)} ч`:s>=3600?`${Math.floor(s/3600)} ч ${Math.floor(s%3600/60)} мин`:`${Math.floor(s/60)} мин`;
export default function HomeResources({metrics,failure,onHistory,onLogs}:{metrics?:Metrics;failure:string;onHistory:()=>void;onLogs:()=>void}){
 const m=metrics&&!failure&&Date.now()-metrics.time<10000?metrics:undefined;
 const memory=m?pct(m.memory,m.memoryTotal):0;const disk=m?pct(m.disk,m.diskTotal):0;
 const rows=[
  {id:'cpu',name:'Процессор',icon:Cpu,value:m?`${m.cpu.toFixed(1)} %`:'—',usage:m?.cpu,detail:m?`Логических ядер: ${m.cores}; физических: ${m.physicalCores||"—"}; частота: ${(m.cpuMHz/1000).toFixed(2)} ГГц.`:'Нет актуальных данных'},
  {id:'memory',name:'Память',icon:MemoryStick,value:m?`${memory.toFixed(1)} %`:'—',usage:m?memory:undefined,detail:m?`Занято ${bytes(m.memory)} из ${bytes(m.memoryTotal)}. Свободно ${bytes(Math.max(0,m.memoryTotal-m.memory))}. Подкачка: ${bytes(m.swap)} / ${bytes(m.swapTotal)}.`:'Нет актуальных данных'},
  {id:'disk',name:'Диск',icon:HardDrive,value:m?`${disk.toFixed(1)} %`:'—',usage:m?disk:undefined,detail:m?`Занято ${bytes(m.disk)} из ${bytes(m.diskTotal)}. Свободно ${bytes(Math.max(0,m.diskTotal-m.disk))}.`:'Нет актуальных данных'},
  {id:'uptime',name:'Время работы',icon:Clock,value:m?duration(m.uptime):'—',usage:undefined,detail:m?`ВМ: ${duration(m.uptime)}. Xray: ${m.xrayPID>0?duration(m.xrayUptime):"остановлен"}. Панель: ${bytes(m.panelMemory)}, потоков: ${m.threads}.`:'Нет актуальных данных'}
 ];
 return <section className="home-vm" aria-label="Ресурсы ВМ">
  <header><h2>Ресурсы ВМ</h2>{!m&&<span role="status">{failure?'Данные недоступны':'Ожидаем показатели'}</span>}<div className="vm-actions"><Button size="small" type="text" icon={<ScrollText size={13}/>} onClick={onLogs}>Логи</Button><Button size="small" type="text" icon={<ChartNoAxesCombined size={13}/>} onClick={onHistory}>История</Button></div></header>
  <div className="vm-metrics">{rows.map(r=><Popover key={r.id} trigger="click" title={r.name} content={<p className="vm-detail">{r.detail}</p>}><button className={'vm-metric '+(r.usage!==undefined?(r.usage>=95?'bad':r.usage>=85?'warn':''):'')} aria-label={`${r.name}: ${r.value}. Подробнее`}>
   <span className="vm-label"><r.icon size={13} aria-hidden="true"/>{r.name}</span><b>{r.value}</b>
   {r.usage!==undefined?<span className="vm-meter" aria-hidden="true"><i style={{width:Math.max(0,Math.min(100,r.usage))+'%'}}/></span>:<span className="vm-meter-placeholder"/>}
   <small>{!m?'Нет данных':r.id==='memory'?`${bytes(m.memory)} / ${bytes(m.memoryTotal)}`:r.id==='disk'?`${bytes(Math.max(0,m.diskTotal-m.disk))} свободно`:r.id==='cpu'?`Ядер: ${m.cores} · ${(m.cpuMHz/1000).toFixed(2)} ГГц`:(m.xrayPID>0?`Xray ${duration(m.xrayUptime)}`:"Xray остановлен")}</small>
  </button></Popover>)}</div>
 </section>;
}

import {useEffect,useRef,useState} from "react";
import {Router,X,Network,Server,type LucideIcon} from "lucide-react";
import type {TrafficRoute,TrafficValue} from "./types";
import {buildVisibleFlows,retainFlowJourneys} from "./flowModel";
import {particleFrequency} from "./trafficMotion";
import {SizeFormatter} from "./reference/utils";
import {usePanelTheme} from "./PanelTheme";
import TrafficFlow from "./TrafficFlow";
import "./flow-map.css";

interface FlowGroup {id:string;name:string;activeName:string;activeTag:string}
interface FlowNode {group?:string;Icon?:LucideIcon;id:string;kind:"device"|"gateway"|"exit";title:string;subtitle:string;ip?:string;tag?:string;traffic?:TrafficValue;health?:{alive:boolean;delay?:number;checked:number;stale:boolean}}
const rate=(v:number|undefined)=>SizeFormatter.sizeFormat(v||0)+"/с";
export default function FlowMap({nodes,routes,routesReady,deviceReady,exitReady,selected,onSelect,exitGroups=[],connections=[],connectionsReady=false}:{connections?:{Source:string;Outbound:string}[];connectionsReady?:boolean;exitGroups?:FlowGroup[];nodes:FlowNode[];routes:TrafficRoute[];routesReady:boolean;deviceReady:boolean;exitReady:boolean;selected:string;onSelect:(id:string)=>void}){
 const {mapSpeeds}=usePanelTheme();
 const [now,setNow]=useState(Date.now);
 const journeys=useRef(new Map());
 useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);},[]);
 const root=useRef<HTMLDivElement>(null),[width,setWidth]=useState(1000),[inView,setInView]=useState(false),[hovered,setHovered]=useState("");
 useEffect(()=>{
  const el=root.current;if(!el)return;
  const resize=new ResizeObserver(([entry])=>{if(entry.contentRect.width>0)setWidth(entry.contentRect.width);});resize.observe(el);
  let intersecting=false;const update=()=>setInView(intersecting&&!document.hidden);
  const observer=new IntersectionObserver(([entry])=>{intersecting=entry.isIntersecting;update();});observer.observe(el);
  document.addEventListener("visibilitychange",update);
  return()=>{resize.disconnect();observer.disconnect();document.removeEventListener("visibilitychange",update);};
 },[]);
 const devices=nodes.filter(n=>n.kind==="device"),exits=nodes.filter(n=>n.kind==="exit"),gateway=nodes.find(n=>n.kind==="gateway");
 const small=width<560;
 const minStep=small?(mapSpeeds?60:34):20;
 const spacing=(count:number)=>minStep+12*Math.max(0,Math.min(1,(20-count)/14));
 const deviceStep=spacing(devices.length),exitStep=spacing(exits.length);
 const labelWidth=Math.min(mapSpeeds&&!small?293:190,Math.max(86,width*(small?.29:mapSpeeds?.32:.22)));
 const exitLabelWidth=small?labelWidth:Math.min(labelWidth+55,width*.39);
 const blocks:{key:string;group?:FlowGroup;nodes:FlowNode[]}[]=[],byGroup=new Map<string,typeof blocks[number]>();
 for(const n of exits){
  const group=n.group?exitGroups.find(g=>g.id===n.group):undefined;
  if(group){let block=byGroup.get(group.id);if(!block){block={key:group.id,group,nodes:[]};byGroup.set(group.id,block);blocks.push(block);}block.nodes.push(n);}
  else blocks.push({key:n.id,nodes:[n]});
 }
 const blockGap=16,exitHeight=blocks.reduce((sum,b)=>sum+b.nodes.length*exitStep+(b.group?46:0),0)+Math.max(0,blocks.length-1)*blockGap;
 const height=Math.max(420,devices.length*deviceStep+16,exitHeight+16);
 const center={x:width/2,y:height/2},positions=new Map<string,{x:number;y:number}>([["gateway",center]]);
 devices.forEach((n,i)=>positions.set(n.id,{x:labelWidth+16,y:height/2+(i-(devices.length-1)/2)*deviceStep}));
 let cursor=(height-exitHeight)/2;
 const frames:{group:FlowGroup;top:number;height:number}[]=[];
 for(const block of blocks){
  const blockHeight=block.nodes.length*exitStep+(block.group?46:0);
  if(block.group)frames.push({group:block.group,top:cursor,height:blockHeight});
  block.nodes.forEach((n,i)=>positions.set(n.id,{x:width-exitLabelWidth-16,y:cursor+(block.group?34:0)+(i+.5)*exitStep}));
  cursor+=blockHeight+blockGap;
 }
 const measuredFlows=buildVisibleFlows(nodes,routes,routesReady,deviceReady,exitReady,connections,connectionsReady);
 const flows=retainFlowJourneys(journeys.current,measuredFlows,new Set(nodes.map(n=>n.id)),now);
 const focus=hovered||selected,focused=nodes.find(n=>n.id===focus),related=new Set([focus,"gateway"]);
 if(focus==="gateway")nodes.forEach(n=>related.add(n.id));
 else for(const f of flows)if(f.from===focus||f.to===focus){related.add(f.from);related.add(f.to);}
 const densityScale=Math.min(1,36/Math.max(1,flows.reduce((sum,f)=>sum+particleFrequency(f.up,f.down),0)));
 const segment=(a:{x:number;y:number},b:{x:number;y:number})=>{const mid=(a.x+b.x)/2;return `C ${mid} ${a.y}, ${mid} ${b.y}, ${b.x} ${b.y}`;};
 const tipWidth=Math.min(280,width-24),at=focused?positions.get(focused.id):undefined;
 const tipLeft=focused?.kind==="device"?labelWidth+24:focused?.kind==="exit"?width-labelWidth-tipWidth-24:(width-tipWidth)/2;
 const ready=focused?.kind==="exit"?exitReady:deviceReady;
 const select=(id:string)=>onSelect(selected===id?"":id);
 return <div className="particle-flow" ref={root}>

  <div className={"flow-scene "+(hovered?"is-inspecting":"")} style={{height}} aria-label="Потоки трафика между устройствами, шлюзом и выходами">
   <svg className="flow-edges" width={width} height={height} aria-hidden="true">
    {flows.map(f=>{
     const a=positions.get(f.from)!,b=positions.get(f.to)!;
     const path=`M ${a.x} ${a.y} ${f.attributed?segment(a,center)+" ":""}${segment(f.attributed?center:a,b)}`;
     const dim=focus&&focus!=="gateway"&&f.from!==focus&&f.to!==focus;
     return <g key={f.key} className={dim?"flow-is-dim":""} data-flow-from={f.from} data-flow-to={f.to} data-attributed={f.attributed}>
      <TrafficFlow wire connected={f.connected} path={path} up={f.up} down={f.down} densityScale={densityScale} scale={f.attributed?1.8:1} visible={inView}/>
     </g>;
    })}
   </svg>
   {frames.map(({group,top,height})=><section key={group.id} className="flow-group-frame" style={{left:width-exitLabelWidth-26,top,width:exitLabelWidth+20,height}} aria-label={`Группа ${group.name}. ${group.activeName?`Активный узел: ${group.activeName}`:"Активный узел определяется"}`}>
    <div className="flow-group-heading"><b title={group.name}>{group.name}</b></div>
   </section>)}
   {nodes.map(n=>{
    const point=positions.get(n.id);if(!point)return null;
    const Icon=n.Icon||(n.kind==="device"?Network:Server);
    const measured=n.kind==="exit"?exitReady:deviceReady,active=measured&&(n.traffic?.UpBPS||0)+(n.traffic?.DownBPS||0)>0;
    const failed=n.health&&!n.health.stale&&!n.health.alive;
    const groupActive=n.group&&exitGroups.find(g=>g.id===n.group)?.activeTag===n.tag;
    const showDelay=n.kind==="exit"&&n.tag!=="direct"&&n.tag!=="block";
    const delayLabel=!n.health?"—":n.health.stale?"устар.":!n.health.alive?"ошибка":n.health.delay!==undefined?`${Math.round(n.health.delay)} мс`:"—";
    return <button key={n.id} className={`flow-node flow-${n.kind}${focus&&!related.has(n.id)?" flow-is-dim":""}${focus===n.id?" is-focused":""}${active?" is-active":""}${failed?" is-failed":""}${groupActive?" is-group-active":""}`} data-active-group-member={groupActive||undefined} style={{top:point.y,left:n.kind==="gateway"?point.x:n.kind==="device"?16:point.x,width:n.kind==="gateway"?undefined:n.kind==="exit"?exitLabelWidth:labelWidth}} onMouseEnter={()=>setHovered(n.id)} onMouseLeave={()=>setHovered("")} onFocus={()=>setHovered(n.id)} onBlur={()=>setHovered("")} onClick={()=>select(n.id)} onKeyDown={e=>{if(e.key==="Escape"){setHovered("");onSelect("");}}} aria-label={`${n.title}, ${n.subtitle}. ${measured?`Отправка ${rate(n.traffic?.UpBPS)}, приём ${rate(n.traffic?.DownBPS)}`:"Нет свежих данных скорости"}${showDelay?`. Задержка: ${delayLabel}`:""}. Подробнее`} aria-pressed={selected===n.id}>
     {n.kind==="gateway"?<><Router size={20}/><span><b>NGPanel</b><small>{gateway?.subtitle}</small></span></>:<><Icon size={15} strokeWidth={1.6} className="flow-node-icon" aria-hidden="true"/><span className="flow-node-dot"/><span className="flow-node-copy"><span className="flow-node-title">{n.title}</span>{showDelay&&<span className={`flow-node-latency${failed?" is-failed":""}${!n.health||n.health.stale?" is-stale":""}`} title={n.health?`Последняя проверка: ${new Date(n.health.checked).toLocaleString("ru-RU")}`:"Нет результатов проверки"}>{delayLabel}</span>}{mapSpeeds&&<span className="flow-node-rates"><span>↑ {measured?rate(n.traffic?.UpBPS):"—"}</span><span>↓ {measured?rate(n.traffic?.DownBPS):"—"}</span></span>}</span></>}
    </button>;
   })}
   {focused&&at&&<div className="flow-tip" role="tooltip" style={{width:tipWidth,left:Math.max(12,Math.min(width-tipWidth-12,tipLeft)),top:at.y>height-160?undefined:Math.max(30,at.y-35),bottom:at.y>height-160?12:undefined}}>
    <b>{focused.title}</b><small>{focused.ip||focused.subtitle}</small>
    {mapSpeeds&&<span className="flow-tip-rates"><span>↑ {ready?rate(focused.traffic?.UpBPS):"—"}</span><span>↓ {ready?rate(focused.traffic?.DownBPS):"—"}</span></span>}
    {focused.health&&<small>{focused.health.stale?"Проверка устарела":focused.health.alive?`Доступен${focused.health.delay?` · ${focused.health.delay} мс`:""}`:"Недоступен"} · {new Date(focused.health.checked).toLocaleTimeString("ru-RU")}</small>}
   </div>}
   {!flows.length&&<div className="flow-empty">{deviceReady||exitReady?"Сейчас нет активного трафика":"Ожидаем свежие данные трафика"}</div>}
  </div>
  {selected&&<div className="flow-mode-footer"><button onClick={()=>onSelect("")} aria-label="Снять выделение потока"><X size={13}/> Снять выделение</button></div>}
 </div>;
}

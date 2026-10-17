import { useEffect, useRef, useState, type PointerEvent } from "react";
import { Button, Input, Select } from "antd";
import { Globe, Monitor, Network, Router, Server, ShieldCheck, Smartphone, Tablet, Tv, X } from "lucide-react";
import { action, getJSON } from "./api";
import type { Page, GroupStatus, TrafficSnapshot, TrafficValue, TrafficRoute, MapProbe, Probe } from "./types";
import "./network-map.css";
import {usePanelTheme} from "./PanelTheme";
import TrafficFlow from "./TrafficFlow";
import { particleFrequency } from "./trafficMotion";
import { mapSegment } from "./mapPaths";
import { SizeFormatter } from "./reference/utils";

interface Event { Source:string; Device:string; Outbound:string; OutboundLabel:string }
interface Point { x:number; y:number }
type Kind="phone"|"computer"|"tablet"|"tv"|"router"|"unknown";
interface MapNode { id:string; kind:"device"|"gateway"|"exit"; title:string; subtitle:string; count:number; tag?:string; ip?:string; group?:string; type?:Kind; traffic?:TrafficValue }
const typeKey="ngpanel-network-map-types";
function read<T>(key:string,fallback:T):T {try{return JSON.parse(localStorage.getItem(key)||"null")||fallback;}catch{return fallback;}}
function write(key:string,value:unknown){try{localStorage.setItem(key,JSON.stringify(value));}catch{/* Layout remains available in this session. */}}
const types=[{value:"unknown",label:"Не указан"},{value:"phone",label:"Телефон"},{value:"computer",label:"Компьютер"},{value:"tablet",label:"Планшет"},{value:"tv",label:"Телевизор"},{value:"router",label:"Роутер"}];
const infer=(name:string):Kind=>/xiaomi|iphone|redmi|android|pixel|samsung/i.test(name)?"phone":/ipad|tablet|планшет/i.test(name)?"tablet":/desktop|windows|macbook|ноутбук|компьютер/i.test(name)?"computer":"unknown";
const icon=(node:MapNode)=>node.kind==="gateway"?Router:node.kind==="exit"?(node.tag==="direct"?Globe:node.tag==="block"?ShieldCheck:Server):node.type==="phone"?Smartphone:node.type==="tablet"?Tablet:node.type==="computer"?Monitor:node.type==="tv"?Tv:node.type==="router"?Router:Network;

export default function NetworkMap({p,events,fresh,groups,navigate,traffic,trafficError}:{p:Page;events:Event[];fresh:boolean;groups:Record<string,GroupStatus>;navigate:(tab:string)=>void;traffic?:TrafficSnapshot;trafficError:boolean}){
 const {mapAnimation}=usePanelTheme();
 const routeCache=useRef(new Map<string,{route:TrafficRoute;seen:number}>());
 const probeSeen=useRef<Set<string>|undefined>(undefined);
 const [probeTrips,setProbeTrips]=useState<(MapProbe&{until:number})[]>([]);
 useEffect(()=>{
  if(!traffic?.Probes)return;
  if(!probeSeen.current){probeSeen.current=new Set(traffic.Probes.map(p=>p.ID));return;}
  const now=Date.now(),added: (MapProbe&{until:number})[]=[];
  for(const p of traffic.Probes){if(probeSeen.current.has(p.ID))continue;probeSeen.current.add(p.ID);if(now-Date.parse(p.Time)<10000)added.push({...p,until:now+7000});}
  if(added.length)setProbeTrips(old=>[...old.filter(p=>p.until>now),...added]);
  if(probeSeen.current.size>1024)probeSeen.current=new Set(traffic.Probes.map(p=>p.ID));
 },[traffic]);
 useEffect(()=>{const timer=setInterval(()=>setProbeTrips(old=>old.some(p=>p.until<Date.now())?old.filter(p=>p.until>Date.now()):old),1000);return()=>clearInterval(timer);},[]);
 const [known,setKnown]=useState<Page["Devices"]>([]);
 const [positions,setPositions]=useState<Record<string,Point>>({});
 const [deviceTypes,setTypes]=useState<Record<string,Kind>>(()=>read(typeKey,{}));
 const [selected,setSelected]=useState("");
 const [hovered,setHovered]=useState("");
 const [draftName,setDraftName]=useState("");
 const [nameBusy,setNameBusy]=useState(false),[nameError,setNameError]=useState("");
 const [probes,setProbes]=useState<Record<string,Probe>>({});
 useEffect(()=>{const abort=new AbortController();let busy=false;const load=async()=>{if(busy||document.hidden)return;busy=true;try{const result=await getJSON<{Nodes:Record<string,Probe>}>("/probe-status",abort.signal);if(!abort.signal.aborted)setProbes(result.Nodes||{});}catch{/* Unknown health is neutral. */}finally{busy=false;}};void load();const timer=setInterval(load,15000);return()=>{abort.abort();clearInterval(timer);};},[]);
 const openDetail=(node:MapNode)=>{setSelected(node.id);const d=known.find(d=>d.IP===node.ip);setDraftName(d?.Manual?d.Name:"");setNameError("");};
 const saveName=async(ip:string)=>{setNameBusy(true);setNameError("");try{await action("device-name","status",{ip,name:draftName});const updated=await getJSON<Page>("/api/map/devices");setKnown(updated.Devices||[]);}catch(e){setNameError(e instanceof Error?e.message:String(e));}finally{setNameBusy(false);}};

 const drag=useRef<{id:string;start:Point;origin:Point;moved:boolean}|undefined>(undefined);
 const boardWidth=1160;
 const [frameWidth,setFrameWidth]=useState(boardWidth);
 const viewScale=Math.min(1,frameWidth/boardWidth);
 const frame=useRef<HTMLDivElement>(null);
 const board=useRef<HTMLDivElement>(null);
 const positionsRef=useRef(positions);positionsRef.current=positions;
 const returnFrames=useRef<Record<string,number>>({});
 const [motionVisible,setMotionVisible]=useState(false);
 useEffect(()=>{const el=board.current;if(!el)return;let visible=false;const update=()=>setMotionVisible(visible&&!document.hidden);const observer=new IntersectionObserver(([entry])=>{visible=entry.isIntersecting;update();});observer.observe(el);document.addEventListener("visibilitychange",update);return()=>{observer.disconnect();document.removeEventListener("visibilitychange",update);Object.values(returnFrames.current).forEach(cancelAnimationFrame);};},[]);
 useEffect(()=>{const el=frame.current;if(!el)return;const observer=new ResizeObserver(([entry])=>setFrameWidth(Math.max(1,entry.contentRect.width)));observer.observe(el);return()=>observer.disconnect();},[]);
 useEffect(()=>{const abort=new AbortController();let busy=false;const load=async()=>{if(busy||document.hidden)return;busy=true;try{const page=await getJSON<Page>("/api/map/devices",abort.signal);if(!abort.signal.aborted)setKnown(page.Devices||[]);}catch{/* Active connections still identify devices. */}finally{busy=false;}};void load();const timer=setInterval(load,15000);return()=>{abort.abort();clearInterval(timer);};},[]);

 const recent=!!traffic&&!trafficError&&Date.now()-Date.parse(traffic.Updated)<5000;
 const deviceReady=recent&&traffic!.DevicesAvailable,exitReady=recent&&traffic!.OutboundsAvailable;
 const routeReady=recent&&traffic!.RoutesAvailable;
 const now=Date.now();
 for(const entry of routeCache.current.values())entry.route={...entry.route,UpBPS:0,DownBPS:0};
 if(routeReady)for(const route of traffic!.Routes||[]){
  const key=route.Source+"|"+route.Outbound,old=routeCache.current.get(key);
  if(route.UpBPS+route.DownBPS>0||old)routeCache.current.set(key,{route,seen:route.UpBPS+route.DownBPS>0?now:old!.seen});
 }
 for(const [key,entry] of routeCache.current)if(now-entry.seen>7000)routeCache.current.delete(key);
 const journeys=Array.from(routeCache.current,([key,entry])=>({key,...entry.route}));
 const departureRate=journeys.reduce((sum,r)=>sum+particleFrequency(r.UpBPS,r.DownBPS),0);
 const densityScale=departureRate>60?60/departureRate:1;
 const format=SizeFormatter.sizeFormat;
 const speed=(v:number)=>format(v)+"/с";
 const devices=new Map<string,{name:string;count:number}>();
 for(const d of known)devices.set(d.IP,{name:d.Name||d.AutoName||d.IP,count:0});
 for(const [ip,value] of Object.entries(traffic?.Devices||{}))if(value.UpBytes+value.DownBytes>0&&!devices.has(ip))devices.set(ip,{name:ip,count:0});
 const counts=new Map<string,number>();
 for(const event of events){const d=devices.get(event.Source)||{name:event.Device||event.Source,count:0};d.count++;devices.set(event.Source,d);const tag=event.Outbound;if(tag)counts.set(tag,(counts.get(tag)||0)+1);}
 const nodes:MapNode[]=Array.from(devices,([ip,d])=>({id:"device:"+ip,kind:"device",title:d.name,subtitle:ip,count:d.count,ip,type:deviceTypes[ip]||infer(d.name),traffic:traffic?.Devices[ip]}));
 nodes.push({id:"gateway",kind:"gateway",title:"NGPanel",subtitle:p.DetectedNetwork.address||"Наш шлюз",count:events.length,traffic:Object.values(traffic?.Devices||{}).reduce<TrafficValue>((sum,v)=>({UpBytes:sum.UpBytes+v.UpBytes,DownBytes:sum.DownBytes+v.DownBytes,UpBPS:sum.UpBPS+v.UpBPS,DownBPS:sum.DownBPS+v.DownBPS}),{UpBytes:0,DownBytes:0,UpBPS:0,DownBPS:0})});
 const exits:MapNode[]=(p.Home?.Exits||[]).flatMap(e=>(e.Members?.length?e.Members:[{Tag:e.Tag,Name:e.Name}]).map(member=>({id:"exit:"+member.Tag,kind:"exit" as const,title:member.Tag==="direct"?"Провайдер":member.Tag==="block"?"Блокировка":member.Name,subtitle:member.Tag==="direct"?"Direct":member.Tag==="block"?"Запрет трафика":"VPN",tag:member.Tag,count:counts.get(member.Tag)||0,group:e.GroupID,traffic:traffic?.Outbounds[member.Tag]})));
 for(const [tag,count] of counts)if(!exits.some(e=>e.tag===tag))exits.push({id:"exit:"+tag,kind:"exit",title:events.find(e=>e.Outbound===tag)?.OutboundLabel||tag,subtitle:"Исходящее соединение",tag,count,group:"",traffic:traffic?.Outbounds[tag]});
 nodes.push(...exits);
 // All nodes stay visible around the gateway; dense groups spread into staggered bands.
 const height=520;
 const shownDevices=nodes.filter(n=>n.kind==="device"),shownExits=exits,shownNodes=nodes;
 const baseWidth=224,baseHeight=60;
 const defaults=new Map<string,Point>([["gateway",{x:.5,y:.5}]]);
 const arrange=(list:MapNode[],left:boolean)=>{
  if(list.length>6){
   const span=(boardWidth-baseWidth)/2-38;
   const columns=Math.max(1,Math.ceil(Math.sqrt(list.length*span*baseHeight/((height-48)*baseWidth))));
   let offset=0;
   for(let col=0;col<columns;col++){
    const count=Math.ceil((list.length-offset)/(columns-col));
    const distance=baseWidth/2+24+(col+.5)*span/columns;
    for(let row=0;row<count;row++)defaults.set(list[offset+row].id,{
     x:.5+(left?-1:1)*distance/boardWidth,
     y:(24+(row+.5)*(height-48)/count+(col%2?4:-4))/height
    });
    offset+=count;
   }
   return;
  }
  for(let i=0;i<list.length;i++){
   const angle=list.length===1?0:-Math.PI*.35+Math.PI*.70*i/(list.length-1);
   defaults.set(list[i].id,{
    x:.5+(left?-1:1)*Math.cos(angle)*(boardWidth-baseWidth-28)/(2*boardWidth),
    y:.5+Math.sin(angle)*(height-baseHeight-36)/(2*height)
   });
  }
 };
 arrange(shownDevices,true);arrange(shownExits,false);
 // Fit rectangular cards between every pair of centres, including the gateway.
 let density=1;
 const widthFactor=(kind:MapNode["kind"])=>kind==="exit"?1:kind==="gateway"?.72:.82;
 const centres=shownNodes.map(n=>({...defaults.get(n.id)!,width:baseWidth*widthFactor(n.kind)}));
 for(let i=0;i<centres.length;i++)for(let j=0;j<i;j++){
  const dx=Math.abs(centres[i].x-centres[j].x)*boardWidth,dy=Math.abs(centres[i].y-centres[j].y)*height;
  density=Math.min(density,Math.max((dx-8)/((centres[i].width+centres[j].width)/2),(dy-8)/baseHeight));
 }
 density=Math.max(.05,density);
 const nodeWidth=baseWidth*density,nodeHeight=baseHeight*density;
 const cardWidth=(node:MapNode)=>nodeWidth*widthFactor(node.kind);
 const defaultPosition=(node:MapNode):Point=>defaults.get(node.id)||{x:.5,y:.5};
 const focus=nodes.find(n=>n.id===(hovered||selected));
 const related=new Set<string>();
 if(focus){related.add(focus.id);related.add("gateway");
  if(focus.kind==="gateway")nodes.forEach(n=>related.add(n.id));
  else {for(const event of events)if(focus.ip?event.Source===focus.ip:event.Outbound===focus.tag){related.add("device:"+event.Source);related.add("exit:"+event.Outbound);}
   for(const route of journeys)if(focus.ip?route.Source===focus.ip:route.Outbound===focus.tag){related.add("device:"+route.Source);related.add("exit:"+route.Outbound);}}
 }
 const emphasis=(id:string)=>focus?(related.has(id)?" is-related":" is-muted"):"";
 const health=(node:MapNode)=>{
  if(node.kind!=="exit"||node.tag==="direct"||node.tag==="block")return undefined;
  const sample=groups[node.group||""]?.Samples?.find(s=>node.tag===`auto-vpn-${node.group}-${s.NodeID}-`);
  const id=node.tag?.match(/^node-(\d+)$/)?.[1];
  const probe=id?probes[id]:undefined;
  const checked=sample?.Checked?sample.Checked*1000:probe?.Checked?Date.parse(probe.Checked):0;
  if(!checked||!Number.isFinite(checked))return undefined;
  return{alive:sample?sample.Alive:probe?.State==="ok",delay:sample?sample.DelayMS:probe?.HTTPSMS,checked,stale:Date.now()-checked>120000};
 };
 const position=(node:MapNode)=>{const saved=positions[node.id];const point=saved&&Number.isFinite(saved.x)&&Number.isFinite(saved.y)?saved:defaultPosition(node);return {x:Math.max(nodeWidth/2+12,Math.min(boardWidth-nodeWidth/2-12,point.x*boardWidth)),y:Math.max(nodeHeight/2+12,Math.min(height-nodeHeight/2-12,point.y*height))};};
 const gateway=nodes.find(n=>n.id==="gateway")!;
 const center=position(gateway);
 const segmentCache=new Map<string,string>();
 const segment=(a:Point,b:Point)=>{
  const key=`${a.x},${a.y}:${b.x},${b.y}`,cached=segmentCache.get(key);if(cached)return cached;
  const obstacles=shownNodes.map(n=>({p:position(n),width:cardWidth(n)})).filter(({p})=>Math.hypot(p.x-a.x,p.y-a.y)>1&&Math.hypot(p.x-b.x,p.y-b.y)>1).map(({p,width})=>({left:p.x-width/2-7,right:p.x+width/2+7,top:p.y-nodeHeight/2-7,bottom:p.y+nodeHeight/2+7}));
  const result=mapSegment(a,b,obstacles);segmentCache.set(key,result);return result;
 };
 const pathBetween=(a:Point,b:Point)=>`M ${a.x} ${a.y} ${segment(a,b)}`;
 const detail=nodes.find(n=>n.id===selected);
 const startDrag=(e:PointerEvent<HTMLButtonElement>,node:MapNode)=>{if(e.button!==0)return;cancelAnimationFrame(returnFrames.current[node.id]);e.currentTarget.setPointerCapture(e.pointerId);drag.current={id:node.id,start:{x:e.clientX,y:e.clientY},origin:position(node),moved:false};};
 const move=(e:PointerEvent<HTMLButtonElement>)=>{const state=drag.current;if(!state)return;const dx=(e.clientX-state.start.x)/viewScale,dy=(e.clientY-state.start.y)/viewScale;if(Math.abs(dx)+Math.abs(dy)>4)state.moved=true;if(!state.moved)return;setPositions(old=>({...old,[state.id]:{x:Math.max(nodeWidth/2+12,Math.min(boardWidth-nodeWidth/2-12,state.origin.x+dx))/boardWidth,y:Math.max(nodeHeight/2+12,Math.min(height-nodeHeight/2-12,state.origin.y+dy))/height}}));};
 const returnHome=(id:string)=>{
  cancelAnimationFrame(returnFrames.current[id]);
  const node=nodes.find(n=>n.id===id),start=positionsRef.current[id];if(!node||!start)return;
  const end=defaultPosition(node),began=performance.now();
  const reduced=!mapAnimation||matchMedia("(prefers-reduced-motion: reduce)").matches;
  const frame=(now:number)=>{const t=reduced?1:Math.min(1,(now-began)/650),ease=1-Math.pow(1-t,4);setPositions(old=>{const next={...old};if(t===1)delete next[id];else next[id]={x:start.x+(end.x-start.x)*ease,y:start.y+(end.y-start.y)*ease};return next;});if(t<1)returnFrames.current[id]=requestAnimationFrame(frame);};
  returnFrames.current[id]=requestAnimationFrame(frame);
 };
 const finish=()=>{const id=drag.current?.id;drag.current=undefined;if(id)returnHome(id);};
 return <section className={"network-map "+(nodeWidth<75?"map-tiny ":nodeHeight<52?"map-dense ":"")+(motionVisible?"":"motion-paused")} aria-label="Карта устройств и выходов">
  <div className="network-map-scroll" ref={frame} style={{height:height*viewScale}}><div className="network-map-board" ref={board} style={{height,width:boardWidth,marginLeft:-boardWidth*viewScale/2,transform:`scale(${viewScale})`}}>
   <svg className="network-map-lines" width="100%" height={height} aria-hidden="true">
    {shownNodes.filter(n=>n.kind!=="gateway").map(node=>{
     const at=position(node),from=node.kind==="device"?at:center,to=node.kind==="device"?center:at;
     const measured=node.kind==="device"?deviceReady:exitReady,up=measured?(node.traffic?.UpBPS||0):0,down=measured?(node.traffic?.DownBPS||0):0;
     const lit=up+down>0||probeTrips.some(p=>p.Outbound===node.tag);
     const path=pathBetween(from,to);
     return <g key={node.id} className={(lit?"is-active ":"")+(related.has(node.id)?"is-selected":"")+emphasis(node.id)}>
      <path d={path} className={"map-link"+(!lit&&!(fresh&&node.count>0)?" map-link-idle":"")}/>
     </g>;
    })}
    {journeys.map(route=>{
     const device=shownDevices.find(n=>n.ip===route.Source);if(!device)return null;
     const exit=shownExits.find(n=>n.tag===route.Outbound),start=position(device);
     const path=pathBetween(start,center)+(exit?" "+segment(center,position(exit)):"");
     return <g key={route.key} className={focus&&!(related.has("device:"+route.Source)&&(!route.Outbound||related.has("exit:"+route.Outbound)))?"is-muted":""} data-source={route.Source} data-outbound={exit?route.Outbound:""}><TrafficFlow path={path} up={route.UpBPS} down={route.DownBPS} scale={exit?2:1} densityScale={densityScale} visible={motionVisible}/></g>;
    })}
    {probeTrips.map(probe=>{const exit=shownExits.find(n=>n.tag===probe.Outbound);return exit?<g key={probe.ID} className={emphasis("exit:"+probe.Outbound)} data-probe-outbound={probe.Outbound}><TrafficFlow path={pathBetween(center,position(exit))} up={0} down={0} pulse={probe} visible={motionVisible}/></g>:null;})}
   </svg>
   {shownNodes.map(node=>{const at=position(node),Icon=icon(node),status=health(node),expanded=node.kind==="exit"&&node.tag!=="direct"&&node.tag!=="block"&&hovered===node.id,expandedWidth=Math.min(boardWidth-20,Math.max(nodeWidth+40,320));return <button key={node.id} className={`map-node map-node-${node.kind} ${expanded?"has-expanded":""} ${selected===node.id?"is-selected":""}${emphasis(node.id)} ${status&&!status.stale?(status.alive?"vpn-ready":"vpn-failed"):""}`} style={{left:at.x,top:at.y,width:cardWidth(node),height:nodeHeight}} onPointerDown={e=>startDrag(e,node)} onPointerMove={move} onPointerUp={finish} onPointerCancel={finish} onMouseEnter={()=>setHovered(node.id)} onMouseLeave={()=>setHovered("")} onFocus={()=>setHovered(node.id)} onClick={()=>openDetail(node)} onKeyUp={()=>returnHome(node.id)} onBlur={()=>{returnHome(node.id);setHovered("");}} onKeyDown={e=>{const delta=({ArrowLeft:[-12,0],ArrowRight:[12,0],ArrowUp:[0,-12],ArrowDown:[0,12]} as Record<string,number[]>)[e.key];if(!delta)return;e.preventDefault();cancelAnimationFrame(returnFrames.current[node.id]);const next={...positions,[node.id]:{x:Math.max(nodeWidth/2+12,Math.min(boardWidth-nodeWidth/2-12,at.x+delta[0]))/boardWidth,y:Math.max(nodeHeight/2+12,Math.min(height-nodeHeight/2-12,at.y+delta[1]))/height}};setPositions(next);}} aria-label={`${node.title}, ${node.subtitle}. Подробнее; стрелки — переместить`} title={`${node.title}\n${node.subtitle}${status?`\n${status.stale?"Последняя проверка устарела":status.alive?"Доступен":"Недоступен"}${status.alive&&status.delay?` · ${status.delay} мс`:""}\nПроверено: ${new Date(status.checked).toLocaleString("ru-RU")}`:node.kind==="exit"&&node.tag!=="direct"&&node.tag!=="block"?"\nЕщё нет результатов проверки":""}`}><span className="map-node-icon"><Icon size={24} strokeWidth={1.6}/></span><span className="map-node-copy"><b>{node.title}</b><small>{node.subtitle}</small></span><span className={`map-node-state ${(node.kind==="device"?deviceReady:node.kind==="exit"?exitReady:deviceReady)&&((node.traffic?.UpBPS||0)+(node.traffic?.DownBPS||0)>0)?"has-traffic":fresh&&node.count>0?"has-connection":""}`}/>{expanded&&<span className={"map-node-expanded "+(at.y<90?"expand-below":at.y>height-90?"expand-above":"")} style={{width:expandedWidth,left:Math.max(10,Math.min(boardWidth-expandedWidth-10,at.x-expandedWidth/2))-at.x+cardWidth(node)/2}}><Icon size={24} strokeWidth={1.6}/><span><b>{node.title}</b><small>{status?`${status.stale?"Проверка устарела":status.alive?"Доступен":"Недоступен"}${status.alive&&status.delay?` · ${status.delay} мс`:""} · ${new Date(status.checked).toLocaleTimeString("ru-RU")}`:"Нет актуальной проверки"}</small></span></span>}</button>;})}
  </div></div>
  {detail&&<div className="map-node-details"><div><b>{detail.title}</b><span>{detail.kind==="gateway"?`Xray ${p.Service.trim()==="active"?"запущен":"остановлен"} · ${p.DetectedNetwork.address}`:detail.subtitle}</span><span>{fresh?`${detail.count} активных TCP`:"Нет актуального снимка TCP"}</span>{detail.kind!=="gateway"&&<><span>{(detail.kind==="device"?deviceReady:exitReady)?`↑ ${speed(detail.traffic?.UpBPS||0)} · ↓ ${speed(detail.traffic?.DownBPS||0)}`:"Скорость недоступна"}</span><span>Отправлено {detail.traffic?format(detail.traffic.UpBytes):"—"} · получено {detail.traffic?format(detail.traffic.DownBytes):"—"}</span><span>Учёт {detail.kind==="device"?"по IP устройства":"выхода Xray"}{traffic?.Started?` с ${new Date(traffic.Started).toLocaleString("ru-RU")}`:" ещё не начат"}</span></>}{detail.group&&<span>{groups[detail.group]?.Name?`Текущий узел: ${groups[detail.group].Name}`:"Текущий VPN-узел определяется"}</span>}</div>{detail.kind==="device"&&<><label>Имя устройства<Input size="small" aria-label="Имя устройства на карте" maxLength={100} placeholder={known.find(d=>d.IP===detail.ip)?.AutoName||detail.ip} value={draftName} onChange={e=>setDraftName(e.target.value)} onPressEnter={()=>void saveName(detail.ip!)}/><Button size="small" loading={nameBusy} onClick={()=>void saveName(detail.ip!)}>Сохранить имя</Button>{nameError&&<span role="alert">{nameError}</span>}</label><label>Тип устройства<Select size="small" aria-label="Тип устройства на карте" value={detail.type} options={types} onChange={value=>{const next={...deviceTypes,[detail.ip!]:value};setTypes(next);write(typeKey,next);}}/></label></>}<Button size="small" onClick={()=>navigate(detail.kind==="device"?`routing&view=connections&source=${encodeURIComponent(detail.ip!)}`:detail.kind==="gateway"?"gateway":"subscriptions")}>{detail.kind==="device"?"Показать соединения":detail.kind==="gateway"?"Сеть и DNS":"Подключения"}</Button><button className="map-close" aria-label="Закрыть детали узла" onClick={()=>setSelected("")}><X size={16}/></button></div>}

 </section>;
}

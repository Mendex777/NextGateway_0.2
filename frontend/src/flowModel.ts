import type {TrafficRoute,TrafficValue} from "./types";
export interface FlowEndpoint {id:string;kind:"device"|"gateway"|"exit";ip?:string;tag?:string;traffic?:TrafficValue}
export interface VisibleFlow {key:string;from:string;to:string;up:number;down:number;attributed:boolean;connected?:boolean}
const clean=(n:number|undefined)=>Number.isFinite(n)&&n!>0?n!:0;
// Only measured route records or fresh open sockets connect a device to an exit. Unattributed counters
// terminate at the gateway; they must never imply a device/exit relationship.
export function buildVisibleFlows(nodes:FlowEndpoint[],routes:TrafficRoute[],routesReady:boolean,devicesReady:boolean,exitsReady:boolean,connections:{Source:string;Outbound:string}[]=[],connectionsReady=false):VisibleFlow[]{
 const devices=new Map(nodes.filter(n=>n.kind==="device").map(n=>[n.ip,n]));
 const exits=new Map(nodes.filter(n=>n.kind==="exit").map(n=>[n.tag,n]));
 const flows:VisibleFlow[]=[],used=new Map<string,{up:number;down:number}>();
 const unique=new Map(routes.map(r=>[r.Source+"\0"+r.Outbound,r]));
 if(routesReady)for(const [key,r] of unique){
  const from=devices.get(r.Source),to=exits.get(r.Outbound),up=clean(r.UpBPS),down=clean(r.DownBPS);
  if(!from||!to||up+down===0)continue;
  flows.push({key:"route:"+key,from:from.id,to:to.id,up,down,attributed:true});
  for(const id of [from.id,to.id]){const old=used.get(id)||{up:0,down:0};used.set(id,{up:old.up+up,down:old.down+down});}
 }
 // Open sockets keep their measured relationship visible even during idle samples.
 // They never supply rates or infer a relationship from configuration alone.
 if(connectionsReady)for(const c of connections){
  const from=devices.get(c.Source),to=exits.get(c.Outbound);if(!from||!to)continue;
  const key="route:"+c.Source+"\0"+c.Outbound,existing=flows.find(f=>f.key===key);
  if(existing)existing.connected=true;
  else flows.push({key,from:from.id,to:to.id,up:0,down:0,attributed:true,connected:true});
 }
 for(const n of nodes){
  if(n.kind==="gateway"||!(n.kind==="device"?devicesReady:exitsReady))continue;
  const covered=used.get(n.id),up=Math.max(0,clean(n.traffic?.UpBPS)-(covered?.up||0)),down=Math.max(0,clean(n.traffic?.DownBPS)-(covered?.down||0));
  if(up+down===0)continue;
  flows.push({key:"counter:"+n.id,from:n.kind==="device"?n.id:"gateway",to:n.kind==="device"?"gateway":n.id,up,down,attributed:false});
 }
 return flows;
}

// Keep component identity during empty samples so in-flight particles can arrive.
// Retained routes have zero rates: stale samples never generate new departures.
export function retainFlowJourneys(previous:Map<string,{flow:VisibleFlow;seen:number}>, current:VisibleFlow[], ids:Set<string>, now:number):VisibleFlow[]{
 const active=new Set(current.map(f=>f.key));
 for(const f of current)previous.set(f.key,{flow:f,seen:now});
 for(const [key,item] of previous)if(!ids.has(item.flow.from)||!ids.has(item.flow.to)||now-item.seen>8000)previous.delete(key);
 return [...previous.values()].map(({flow})=>active.has(flow.key)?flow:{...flow,up:0,down:0,...(flow.connected!==undefined?{connected:false}:{})});
}

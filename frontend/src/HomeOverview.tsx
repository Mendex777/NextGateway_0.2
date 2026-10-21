import { useEffect, useState } from "react";
import { Alert, Button } from "antd";
import { getJSON } from "./api";
import type {Run} from "./common";
import type { Page, GroupStatus } from "./types";
import type { Metrics } from "./Dashboard";
import HomeTraffic from "./HomeTraffic";
import ServiceHealth from "./ServiceHealth";
import {useGatewayTraffic} from "./useGatewayTraffic";
import HomeResources from "./HomeResources";
import "./home.css";
import NetworkMap from "./NetworkMap";

interface Snapshot { ReceivedAt:number; Updated:string; TCPAvailable:boolean; Events:{TCPState:string;Network:string;Inbound:string;Source:string;Device:string;Outbound:string;OutboundLabel:string}[] }

export default function HomeOverview({p,samples,failure,navigate,onTool,run,working}:{p:Page;run:Run;working:boolean;samples:Metrics[];failure:string;navigate:(tab:string)=>void;onTool:(tool:string)=>void}) {
 const {traffic,error:trafficError}=useGatewayTraffic();
 const [snapshot,setSnapshot]=useState<Snapshot>();
 const [connectionError,setConnectionError]=useState("");
 const [groups,setGroups]=useState<Record<string,GroupStatus>>({});
 const groupIDs=(p.Home?.Exits||[]).map(e=>e.GroupID).filter(Boolean).sort().join(",");
 useEffect(()=>{
  const abort=new AbortController();let busy=false;
  const load=async()=>{if(busy||document.hidden)return;busy=true;try{const s=await getJSON<Snapshot>("/api/connections?active=1",abort.signal);if(!abort.signal.aborted){setSnapshot({...s,ReceivedAt:performance.now()});setConnectionError("");}}catch{if(!abort.signal.aborted)setConnectionError("Данные соединений недоступны");}finally{busy=false;}};
  void load();const timer=setInterval(load,2000);return()=>{abort.abort();clearInterval(timer);};
 },[]);
 useEffect(()=>{
  const abort=new AbortController();let busy=false;
  const load=async()=>{if(busy||document.hidden||!groupIDs)return;busy=true;try{const rows=await Promise.all(groupIDs.split(",").map(async id=>[id,await getJSON<GroupStatus>("/balance-status?id="+encodeURIComponent(id),abort.signal)] as const));if(!abort.signal.aborted)setGroups(Object.fromEntries(rows));}catch{if(!abort.signal.aborted)setGroups({});}finally{busy=false;}};
  setGroups({});void load();const timer=setInterval(load,15000);return()=>{abort.abort();clearInterval(timer);};
 },[groupIDs]);
 const m=samples.at(-1);
 const fresh=!!snapshot?.TCPAvailable&&!connectionError&&performance.now()-snapshot.ReceivedAt<10000;
 const events=fresh?snapshot!.Events.filter(e=>e.TCPState==="active"&&e.Network==="tcp"&&e.Inbound==="tproxy-in"):[];
 const devices=Array.from(events.reduce((map,e)=>{const row=map.get(e.Source)||{ip:e.Source,name:e.Device||e.Source,count:0};row.count++;map.set(e.Source,row);return map;},new Map<string,{ip:string;name:string;count:number}>()).values()).sort((a,b)=>b.count-a.count);
 return <div className="gateway-home">
  <section className="home-flow-scene" aria-label="Карта сети и трафик шлюза">
   <NetworkMap p={p} events={events} fresh={fresh} groups={groups} navigate={navigate} traffic={traffic} trafficError={trafficError}/>
   <HomeTraffic traffic={traffic} error={trafficError} sessions={fresh?{count:events.length,devices:devices.length,updated:snapshot!.Updated}:undefined} onSessionsClick={()=>navigate("routing&view=connections")} actions={<><Button size="small" onClick={()=>navigate("routing")}>Правила</Button></>}/>
  </section>
  {(p.VPNWarning||p.GatewayHealth)&&<Alert showIcon type="warning" title={p.GatewayHealth||p.VPNWarning} action={<Button size="small" onClick={()=>navigate(p.GatewayHealth?"gateway":"subscriptions")}>Открыть</Button>}/>}
  <ServiceHealth p={p} run={run} working={working} metrics={m} onTool={onTool}/>
  <HomeResources metrics={m} failure={failure} onHistory={()=>onTool("history")} onLogs={()=>navigate("diagnostics&view=logs")}/>
 </div>;
}

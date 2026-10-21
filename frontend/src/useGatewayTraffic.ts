import {useEffect,useState} from "react";
import {getJSON} from "./api";
import type {TrafficSnapshot} from "./types";
import {trafficFresh} from "./gatewayTraffic";
export function useGatewayTraffic(){
 const [traffic,setTraffic]=useState<TrafficSnapshot>();
 const [error,setError]=useState(false);
 const [,tick]=useState(0);
 useEffect(()=>{
  let stopped=false,busy=false;let request:AbortController|undefined;
  const load=async()=>{
   tick(v=>v+1);
   if(busy||document.hidden)return;busy=true;
   request=new AbortController();const timeout=setTimeout(()=>request?.abort(),4000);
   try{const data=await getJSON<TrafficSnapshot>("/api/traffic",request.signal);if(!stopped){setTraffic({...data,ReceivedAt:performance.now()});setError(false);}}
   catch{if(!stopped)setError(true);}finally{clearTimeout(timeout);busy=false;}
  };
  void load();const timer=setInterval(load,1000);
  document.addEventListener("visibilitychange",load);
  return()=>{stopped=true;request?.abort();clearInterval(timer);document.removeEventListener("visibilitychange",load);};
 },[]);
 return {traffic,error:error||!!traffic&&!trafficFresh(traffic,false)};
}

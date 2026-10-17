import {useEffect,useState} from "react";
import {getJSON} from "./api";
import type {TrafficSnapshot} from "./types";
export function useGatewayTraffic(){
 const [traffic,setTraffic]=useState<TrafficSnapshot>();
 const [error,setError]=useState(false);
 useEffect(()=>{
  const abort=new AbortController();let busy=false;
  const load=async()=>{
   if(busy||document.hidden)return;busy=true;
   try{const data=await getJSON<TrafficSnapshot>("/api/traffic",abort.signal);if(!abort.signal.aborted){setTraffic(data);setError(false);}}
   catch{if(!abort.signal.aborted)setError(true);}finally{busy=false;}
  };
  void load();const timer=setInterval(load,1000);
  document.addEventListener("visibilitychange",load);
  return()=>{abort.abort();clearInterval(timer);document.removeEventListener("visibilitychange",load);};
 },[]);
 return {traffic,error};
}

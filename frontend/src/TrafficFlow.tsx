import { useEffect, useRef, useState } from "react";
import {usePanelTheme} from "./PanelTheme";
import { particleTimings } from "./trafficMotion";

interface Particle { id:number; reverse:boolean; duration:number; probe?:boolean; reply?:boolean }

// A particle is a single journey. Measurements only control new departures;
// existing particles keep their direction, speed and endpoint until arrival.
export default function TrafficFlow({path,up,down,visible,scale=1,densityScale,pulse,shape="dash",wire=false,connected=false}:{connected?:boolean;wire?:boolean;path:string;up:number;down:number;visible:boolean;scale?:number;densityScale?:number;pulse?:{ID:string;Success:boolean};shape?:"dash"|"dot"}) {
 const {mapAnimation}=usePanelTheme();
 const [particles,setParticles]=useState<Particle[]>([]);
 const [reduced,setReduced]=useState(()=>matchMedia("(prefers-reduced-motion: reduce)").matches);
 const rates=useRef({up,down,densityScale});rates.current={up,down,densityScale};
 const serial=useRef(0),last=useRef({up:0,down:0}),pulseSent=useRef("");
 useEffect(()=>{const media=matchMedia("(prefers-reduced-motion: reduce)");const update=()=>{setReduced(media.matches);if(media.matches)setParticles([]);};media.addEventListener("change",update);return()=>media.removeEventListener("change",update);},[]);
 useEffect(()=>{
  if(!visible||reduced||!mapAnimation||pulse)return;
  const emit=()=>{
   const now=performance.now(),departures:Particle[]=[],timings=particleTimings(rates.current.up,rates.current.down,rates.current.densityScale);
   for(const direction of ["up","down"] as const){
    const timing=timings[direction];
    if(!timing){last.current[direction]=0;continue;}
    if(last.current[direction]&&now-last.current[direction]<timing.interval)continue;
    last.current[direction]=now;
    departures.push({id:++serial.current,reverse:direction==="down",duration:timing.duration*scale});
   }
   if(departures.length)setParticles(old=>old.length+departures.length<=48?[...old,...departures]:old);
  };
  emit();const timer=setInterval(emit,25);return()=>clearInterval(timer);
 },[visible,reduced,scale,mapAnimation]);
 useEffect(()=>{
  if(!pulse||reduced||!mapAnimation||!visible||pulseSent.current===pulse.ID)return;
  pulseSent.current=pulse.ID;
  const particle:Particle={id:++serial.current,reverse:false,duration:1600,probe:true,reply:pulse.Success};
  setParticles(old=>[...old,particle]);
 },[pulse?.ID,reduced,visible,mapAnimation]);
 useEffect(()=>{if(!mapAnimation){setParticles([]);last.current={up:0,down:0};}},[mapAnimation]);
 const finish=(particle:Particle)=>{
  const reply:Particle|undefined=particle.probe&&particle.reply?{id:++serial.current,reverse:true,duration:1600,probe:true}:undefined;
  setParticles(old=>{const kept=old.filter(p=>p.id!==particle.id);return reply?[...kept,reply]:kept;});
 };
 const line=wire?<path d={path} className="flow-wire" data-connected={connected||undefined} data-in-flight={mapAnimation&&!reduced&&particles.length>0||undefined} style={{opacity:connected||up+down>0||(mapAnimation&&!reduced&&particles.length>0)?undefined:0}}/>:null;
 if(!mapAnimation||reduced)return line;
 const ParticleShape=shape==="dot"?"circle":"rect";
 return <>
  {line}
  <g className={"map-particles "+(visible?"":"is-paused")}>
   {particles.map(particle=><ParticleShape r={2.6} key={particle.id} data-particle-id={particle.id} x={-6} y={-1.8} width={12} height={3.6} rx={1.8} data-route-path={path} data-probe={particle.probe||undefined} className={"map-particle "+(particle.reverse?"is-return ":"")+(particle.probe?"is-probe":"")} style={{offsetPath:`path('${path}')`,animationDuration:`${particle.duration}ms`}} onAnimationEnd={()=>finish(particle)}/>)}
  </g>
 </>;
}

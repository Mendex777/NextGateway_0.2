import {useEffect,useRef,useState,useId,type PointerEvent,type ReactNode} from "react";
import type {TrafficSnapshot} from "./types";
import {appendGatewayPoint,gatewayTrafficPoint,trafficCurve,nearestTrafficPoint,restoreGatewayHistory,mergeGatewayHistory,gatewayHistoryKey,type GatewayTrafficPoint} from "./gatewayTraffic";
import {SizeFormatter} from "./reference/utils";
const bytes=SizeFormatter.sizeFormat;
export default function HomeTraffic({traffic,error,actions,sessions,onSessionsClick}:{traffic?:TrafficSnapshot;error:boolean;actions:ReactNode;sessions?:{count:number;devices:number;updated:string};onSessionsClick:()=>void}){
 const tooltipID=useId();
 const [hover,setHover]=useState<{point:GatewayTrafficPoint;x:number}|undefined>();
 const hoverRef=useRef(hover);hoverRef.current=hover;
 const axis=useRef({end:Date.now(),width:1});
 const [points,setPoints]=useState<GatewayTrafficPoint[]>(()=>{try{return restoreGatewayHistory(sessionStorage.getItem(gatewayHistoryKey));}catch{return [];}});
 const canvas=useRef<HTMLCanvasElement>(null),pointsRef=useRef(points),arrived=useRef(0),refresh=useRef(()=>{});
 pointsRef.current=points;
 const current=gatewayTrafficPoint(traffic,error);
 useEffect(()=>{const next=gatewayTrafficPoint(traffic,error);setPoints(old=>{const merged=mergeGatewayHistory(old,traffic?.History);return next?appendGatewayPoint(merged,next):merged;});},[traffic,error]);
 useEffect(()=>{arrived.current=performance.now();refresh.current();try{sessionStorage.setItem(gatewayHistoryKey,JSON.stringify({version:1,points}));}catch{/* Live chart works when storage is unavailable. */}},[points]);
 useEffect(()=>{refresh.current();},[hover]);
 useEffect(()=>{
  const el=canvas.current;if(!el)return;
  const ctx=el.getContext("2d");if(!ctx)return;
  const reduced=matchMedia("(prefers-reduced-motion: reduce)");
  let width=0,height=164,frame=0,visible=false,lastDraw=0,ceiling=1;
  const draw=(now:number)=>{
   if(!width)return;
   const data=pointsRef.current,wall=Date.now(),left=48,right=8,top=12,bottom=26,w=width-left-right,h=height-top-bottom;
   const style=getComputedStyle(el),up=style.getPropertyValue("--map-upload").trim()||"#c4b5fd",down=style.getPropertyValue("--map-download").trim()||"#8b5cf6";
   const max=Math.max(1024,...data.filter(p=>p.time>wall-90000).flatMap(p=>[p.up,p.down]))*1.15;
   ceiling=reduced.matches?max:ceiling+(max-ceiling)*Math.min(1,(now-lastDraw)/250);
   if(!lastDraw)ceiling=max;lastDraw=now;
   ctx.clearRect(0,0,width,height);ctx.font='10px "Geist Variable",sans-serif';ctx.fillStyle=style.getPropertyValue("--ng-muted").trim()||"#888";
   ctx.textAlign="right";ctx.setLineDash([2,5]);ctx.strokeStyle=style.getPropertyValue("--ng-border").trim()||"#333";ctx.lineWidth=1;
   for(let i=0;i<=3;i++){const y=top+h*i/3;ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(width-right,y);ctx.stroke();ctx.fillText(bytes(ceiling*(1-i/3)),left-7,y+3);}
   ctx.setLineDash([]);ctx.textAlign="center";
   const end=reduced.matches?(data.at(-1)?.time||wall):wall;axis.current={end,width};
   for(let t=Math.ceil((end-90000)/30000)*30000;t<=end;t+=30000){const x=left+(t-end+90000)/90000*w;ctx.fillText(new Date(t).toLocaleTimeString("ru-RU",{hour:'2-digit',minute:'2-digit',second:'2-digit'}),x,height-5);}
   ctx.save();ctx.beginPath();ctx.rect(left,top,w,h);ctx.clip();
   for(const [key,color] of [['up',up],['down',down]] as const){
    ctx.strokeStyle=color;ctx.lineWidth=1.7;ctx.lineJoin='round';ctx.lineCap='round';ctx.beginPath();
    let group:{x:number;y:number}[]=[],previous:GatewayTrafficPoint|undefined;
    const trace=()=>{if(!group.length)return;ctx.moveTo(group[0].x,group[0].y);for(const c of trafficCurve(group))ctx.bezierCurveTo(c.c1.x,c.c1.y,c.c2.x,c.c2.y,c.b.x,c.b.y);group=[];};
    for(let i=0;i<data.length;i++){
     const p=data[i];if(p.time<end-90000-2000)continue;
     let x=left+(p.time-end+90000)/90000*w,y=top+h*(1-p[key]/ceiling);
     if(i===data.length-1&&previous&&!reduced.matches){const progress=Math.min(1,(now-arrived.current)/300);const px=left+(previous.time-end+90000)/90000*w,py=top+h*(1-previous[key]/ceiling);x=px+(x-px)*progress;y=py+(y-py)*progress;}
     if(previous&&p.time-previous.time>2500)trace();
     group.push({x,y});previous=p;
    }
    trace();
    ctx.stroke();
   }
   const selected=hoverRef.current?.point;
   if(selected){
    const x=left+(selected.time-end+90000)/90000*w;
    ctx.strokeStyle=style.getPropertyValue('--ng-muted').trim()||'#888';ctx.lineWidth=1;ctx.setLineDash([3,4]);ctx.beginPath();ctx.moveTo(x,top);ctx.lineTo(x,top+h);ctx.stroke();ctx.setLineDash([]);
    for(const [key,color] of [['up',up],['down',down]] as const){ctx.fillStyle=color;ctx.beginPath();ctx.arc(x,top+h*(1-selected[key]/ceiling),3,0,Math.PI*2);ctx.fill();}
   }
   ctx.restore();
  };
  const loop=(now:number)=>{if(now-lastDraw>=32)draw(now);if(visible&&!document.hidden&&!reduced.matches)frame=requestAnimationFrame(loop);};
  const restart=()=>{cancelAnimationFrame(frame);if(!visible||document.hidden)return;draw(performance.now());if(!reduced.matches)frame=requestAnimationFrame(loop);};
  refresh.current=restart;
  const resize=new ResizeObserver(([entry])=>{width=entry.contentRect.width;const dpr=devicePixelRatio||1;el.width=Math.round(width*dpr);el.height=height*dpr;ctx.setTransform(dpr,0,0,dpr,0,0);restart();});resize.observe(el);
  const observer=new IntersectionObserver(([entry])=>{visible=entry.isIntersecting;restart();});observer.observe(el);
  document.addEventListener('visibilitychange',restart);reduced.addEventListener('change',restart);
  return()=>{cancelAnimationFrame(frame);resize.disconnect();observer.disconnect();document.removeEventListener('visibilitychange',restart);reduced.removeEventListener('change',restart);refresh.current=()=>{};};
 },[]);
 const selectAt=(e:PointerEvent<HTMLCanvasElement>)=>{
  const x=e.clientX-e.currentTarget.getBoundingClientRect().left,{end,width}=axis.current;
  if(x<48||x>width-8){setHover(undefined);return;}
  const point=nearestTrafficPoint(pointsRef.current,end-90000+(x-48)/(width-56)*90000);
  setHover(point?{point,x:Math.max(0,Math.min(width-196,x+12))}:undefined);
 };
 return <section className="home-flow-traffic" aria-label="Трафик устройств через шлюз">
  <div className="home-flow-toolbar"><div className="home-flow-rates"><h2 title="Трафик устройств по тем же счётчикам Linux, что и карта. Без собственного трафика ВМ.">Трафик шлюза</h2><span className="flow-upload">↑ Отправка <b>{current?bytes(current.up)+"/с":"—"}</b></span><span className="flow-download">↓ Приём <b>{current?bytes(current.down)+"/с":"—"}</b></span></div><div className="home-flow-actions"><button className="home-flow-sessions" onClick={onSessionsClick} title={sessions?`${sessions.devices} устройств с активными TCP. Снимок ${new Date(sessions.updated).toLocaleTimeString("ru-RU")}. UDP — отдельный журнал; сокеты ВМ не входят.`:"Нет актуального снимка активных TCP"}><b>{sessions?sessions.count:"—"}</b> активных TCP</button>{actions}</div></div>
  <div className="home-flow-chart-wrap">
  <canvas ref={canvas} className="home-flow-chart" role="img" tabIndex={0} aria-describedby={hover?tooltipID:undefined} onPointerMove={selectAt} onPointerDown={selectAt} onPointerLeave={()=>setHover(undefined)} onBlur={()=>setHover(undefined)} onFocus={()=>{const point=pointsRef.current.at(-1);if(point)setHover({point,x:Math.max(0,axis.current.width-196)});}} onKeyDown={e=>{if(e.key==='Escape'){setHover(undefined);return;}if(e.key!=='ArrowLeft'&&e.key!=='ArrowRight')return;e.preventDefault();const data=pointsRef.current,index=hover?data.findIndex(p=>p.time===hover.point.time):data.length-1,next=Math.max(0,Math.min(data.length-1,index+(e.key==='ArrowLeft'?-1:1))),point=data[next];if(point)setHover({point,x:Math.max(0,Math.min(axis.current.width-196,48+(point.time-axis.current.end+90000)/90000*(axis.current.width-56)))});}} aria-label={`Трафик устройств за 90 секунд. Отправка ${current?bytes(current.up):'недоступна'}, приём ${current?bytes(current.down):'недоступен'}`} data-sample-time={current?.time} data-points={points.length}/>
  {hover&&<div id={tooltipID} role="tooltip" className="home-flow-tooltip" style={{left:hover.x}}><time>{new Date(hover.point.time).toLocaleTimeString("ru-RU")}</time><span className="flow-upload">↑ Отправка <b>{bytes(hover.point.up)}/с</b></span><span className="flow-download">↓ Приём <b>{bytes(hover.point.down)}/с</b></span></div>}
  </div>
  <div className="home-flow-totals"><span>Отправлено <b>{current?bytes(current.sent):'—'}</b></span><span>Получено <b>{current?bytes(current.received):'—'}</b></span><span role="status">{!current?(error?'Данные трафика недоступны':traffic?.Message||'Получаем данные трафика…'):''}</span></div>
 </section>;
}

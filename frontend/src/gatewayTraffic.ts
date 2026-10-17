import type {TrafficSnapshot} from "./types";
export interface GatewayTrafficPoint {time:number;up:number;down:number;sent:number;received:number}
export function gatewayTrafficPoint(traffic:TrafficSnapshot|undefined,error:boolean,now=Date.now()):GatewayTrafficPoint|null{
 if(!traffic||error||!traffic.DevicesAvailable)return null;
 const time=Date.parse(traffic.Updated);
 if(!Number.isFinite(time)||Math.abs(now-time)>5000)return null;
 const clean=(v:number)=>Number.isFinite(v)&&v>0?v:0;
 return Object.values(traffic.Devices||{}).reduce((sum,v)=>({time,up:sum.up+clean(v.UpBPS),down:sum.down+clean(v.DownBPS),sent:sum.sent+clean(v.UpBytes),received:sum.received+clean(v.DownBytes)}),{time,up:0,down:0,sent:0,received:0});
}
export function appendGatewayPoint(points:GatewayTrafficPoint[],next:GatewayTrafficPoint){
 if(points.length&&next.time<=points[points.length-1].time)return points;
 return [...points.filter(p=>p.time>=next.time-90000),next];
}

export interface CurvePoint {x:number;y:number}
// Monotone cubic interpolation rounds corners without inventing peaks or negatives.
export function trafficCurve(points:CurvePoint[]){
 const p=points.filter((v,i)=>i===0||v.x>points[i-1].x);
 if(p.length<2)return [];
 const delta=p.slice(1).map((v,i)=>(v.y-p[i].y)/(v.x-p[i].x));
 const slopes=p.map((_,i)=>i===0?delta[0]:i===p.length-1?delta.at(-1)!:delta[i-1]*delta[i]<=0?0:2/(1/delta[i-1]+1/delta[i]));
 return p.slice(1).map((b,i)=>{const a=p[i],h=(b.x-a.x)/3;return {a,b,c1:{x:a.x+h,y:a.y+slopes[i]*h},c2:{x:b.x-h,y:b.y-slopes[i+1]*h}};});
}
export function nearestTrafficPoint(points:GatewayTrafficPoint[],time:number){
 const nearest=points.reduce<GatewayTrafficPoint|undefined>((best,p)=>!best||Math.abs(p.time-time)<Math.abs(best.time-time)?p:best,undefined);
 return nearest&&Math.abs(nearest.time-time)<=2500?nearest:undefined;
}

export const gatewayHistoryKey="ngpanel-gateway-traffic-v1";
// Only restore recent measured values; storage is optional and untrusted.
export function restoreGatewayHistory(raw:string|null,now=Date.now()):GatewayTrafficPoint[]{
 try{
  if(!raw||raw.length>128000)return [];
  const saved=JSON.parse(raw);
  if(saved?.version!==1||!Array.isArray(saved.points))return [];
  const rows=saved.points.filter((p:GatewayTrafficPoint)=>p&&[p.time,p.up,p.down,p.sent,p.received].every(v=>typeof v==='number'&&Number.isFinite(v)&&v>=0)&&p.time>=now-90000&&p.time<=now+5000).sort((a:GatewayTrafficPoint,b:GatewayTrafficPoint)=>a.time-b.time);
  return rows.filter((p:GatewayTrafficPoint,i:number)=>!i||p.time!==rows[i-1].time).slice(-96).map(({time,up,down,sent,received}:GatewayTrafficPoint)=>({time,up,down,sent,received}));
 }catch{return [];}
}

// Prefer collector measurements over the browser fallback at matching timestamps.
export function mergeGatewayHistory(local:GatewayTrafficPoint[],server:GatewayTrafficPoint[]|undefined,now=Date.now()){
 const merged=restoreGatewayHistory(JSON.stringify({version:1,points:[...(server||[]),...local]}),now);
 return merged.length===local.length&&merged.every((p,i)=>Object.keys(p).every(k=>p[k as keyof GatewayTrafficPoint]===local[i][k as keyof GatewayTrafficPoint]))?local:merged;
}

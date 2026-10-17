export interface MapPoint {x:number;y:number}
export interface MapObstacle {left:number;right:number;top:number;bottom:number}
// A boundary is safe: obstacles already include clearance around the visible card.
export function crossesCard(a:MapPoint,b:MapPoint,r:MapObstacle):boolean {
 let low=0,high=1;
 for(const [origin,delta,min,max] of [[a.x,b.x-a.x,r.left+.01,r.right-.01],[a.y,b.y-a.y,r.top+.01,r.bottom-.01]]){
  if(Math.abs(delta)<.00001){if(origin<=min||origin>=max)return false;continue;}
  let enter=(min-origin)/delta,leave=(max-origin)/delta;
  if(enter>leave)[enter,leave]=[leave,enter];
  low=Math.max(low,enter);high=Math.min(high,leave);if(low>=high)return false;
 }
 return high>0&&low<1;
}
const inside=(p:MapPoint,r:MapObstacle)=>p.x>r.left+.01&&p.x<r.right-.01&&p.y>r.top+.01&&p.y<r.bottom-.01;
export function routeMapPoints(a:MapPoint,b:MapPoint,obstacles:MapObstacle[]):MapPoint[]{
 const clear=(p:MapPoint,q:MapPoint)=>!obstacles.some(r=>crossesCard(p,q,r));
 if(clear(a,b))return[a,b];
 const points=[a,b,...obstacles.flatMap(r=>[{x:r.left,y:r.top},{x:r.right,y:r.top},{x:r.left,y:r.bottom},{x:r.right,y:r.bottom}]).filter(p=>!obstacles.some(r=>inside(p,r)))];
 const distances=points.map(()=>Infinity),previous=points.map(()=>-1),done=new Set<number>();distances[0]=0;
 for(let step=0;step<points.length;step++){
  let current=-1;for(let i=0;i<points.length;i++)if(!done.has(i)&&(current<0||distances[i]<distances[current]))current=i;
  if(current<0||!Number.isFinite(distances[current]))break;
  if(current===1){const result:MapPoint[]=[];for(let i=1;i>=0;i=previous[i])result.unshift(points[i]);return result;}
  done.add(current);
  for(let i=0;i<points.length;i++)if(!done.has(i)){
   const distance=distances[current]+Math.hypot(points[i].x-points[current].x,points[i].y-points[current].y);
   if(distance<distances[i]&&clear(points[current],points[i])){distances[i]=distance;previous[i]=current;}
  }
 }
 return[a,b];
}
// Round each detour corner with a quadratic curve tangent to both adjoining legs.
// Keep the rounding inside the free corridor; crowded corners use a smaller radius.
export function roundedMapRoute(points:MapPoint[],obstacles:MapObstacle[]):string {
 let result="";
 for(let i=1;i<points.length-1;i++){
  const before=points[i-1],corner=points[i],after=points[i+1];
  const incoming=Math.hypot(corner.x-before.x,corner.y-before.y),outgoing=Math.hypot(after.x-corner.x,after.y-corner.y);
  if(incoming<.001||outgoing<.001)continue;
  let radius=Math.min(22,incoming*.45,outgoing*.45);
  const endpoints=(r:number)=>({entry:{x:corner.x+(before.x-corner.x)*r/incoming,y:corner.y+(before.y-corner.y)*r/incoming},exit:{x:corner.x+(after.x-corner.x)*r/outgoing,y:corner.y+(after.y-corner.y)*r/outgoing}});
  let {entry,exit}=endpoints(radius);
  for(let attempt=0;attempt<8;attempt++){
   let previous=entry,blocked=false;
   for(let step=1;step<=32;step++){
    const t=step/32,u=1-t,next={x:u*u*entry.x+2*u*t*corner.x+t*t*exit.x,y:u*u*entry.y+2*u*t*corner.y+t*t*exit.y};
    if(obstacles.some(r=>crossesCard(previous,next,r)))blocked=true;previous=next;
   }
   if(!blocked)break;
   radius/=2;({entry,exit}=endpoints(radius));
  }
  result+=` L ${entry.x} ${entry.y} Q ${corner.x} ${corner.y}, ${exit.x} ${exit.y}`;
 }
 const end=points.at(-1)!;
 return (result+` L ${end.x} ${end.y}`).trim();
}
export function mapSegment(a:MapPoint,b:MapPoint,obstacles:MapObstacle[]):string {
 const middle=(a.x+b.x)/2;
 const curve=`C ${middle} ${a.y}, ${middle} ${b.y}, ${b.x} ${b.y}`;
 let previous=a,blocked=false;
 for(let i=1;i<=32;i++){
  const t=i/32,u=1-t,next={x:u*u*u*a.x+3*u*u*t*middle+3*u*t*t*middle+t*t*t*b.x,y:u*u*u*a.y+3*u*u*t*a.y+3*u*t*t*b.y+t*t*t*b.y};
  if(obstacles.some(r=>crossesCard(previous,next,r)))blocked=true;previous=next;
 }
 if(!blocked)return curve;
 const room=obstacles.map(r=>({left:r.left-10,right:r.right+10,top:r.top-10,bottom:r.bottom+10}));
 let points=routeMapPoints(a,b,room);
 // Prefer room for gentle turns, but do not circle an entire column when a
 // shorter safe corridor exists between cards.
 const narrow=routeMapPoints(a,b,obstacles);
 const length=(route:MapPoint[])=>route.slice(1).reduce((sum,p,i)=>sum+Math.hypot(p.x-route[i].x,p.y-route[i].y),0);
 if((points.length===2&&obstacles.some(r=>crossesCard(a,b,r)))||length(points)>length(narrow)*1.15)points=narrow;
 return roundedMapRoute(points,obstacles);
}

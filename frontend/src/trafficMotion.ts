const clean=(v:number)=>Number.isFinite(v)&&v>0?v:0;

// Each route has its own density; a shared cap may reduce departures only.
export function particleFrequency(up:number,down:number) {
 const total=clean(up)+clean(down);
 const intensity=Math.min(1,Math.max(0,Math.log2(Math.max(1,total/(1024*1024)))/5));
 return Math.min(9,Math.sqrt(total/1024)/4)*(1+2*intensity);
}

export function particleTimings(up:number,down:number,densityScale=1) {
 up=clean(up);down=clean(down);
 const total=up+down;
 const frequency=particleFrequency(up,down)*Math.min(1,clean(densityScale));
 const timing=(rate:number,download=false)=>{
  if(!rate||!frequency)return null;
  // Flight speed depends only on this direction, never on another device or ACKs.
  const intensity=Math.min(1,Math.max(0,Math.log2(Math.max(1,rate/(1024*1024)))/5));
  const duration=Math.max(1300,2300-Math.log2(1+rate/1024)*65)*(1-.45*intensity);
  // Leave gaps between incoming capsules at high download rates.
  const spacing=download?1+.4*intensity:1;
  return {interval:spacing*1000/(frequency*(rate/total)),duration};
 };
 return {up:timing(up),down:timing(down,true)};
}

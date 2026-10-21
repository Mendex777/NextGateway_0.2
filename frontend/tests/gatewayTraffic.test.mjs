import {test} from 'node:test';
import assert from 'node:assert/strict';
import {gatewayTrafficPoint,appendGatewayPoint,trafficCurve,nearestTrafficPoint,restoreGatewayHistory,mergeGatewayHistory,trafficFresh,trafficServerNow} from '../src/gatewayTraffic.ts';
const now=Date.parse('2026-10-17T15:00:00Z');
const snapshot={ReceivedAt:now,ServerNow:new Date(now).toISOString(),Updated:new Date(now).toISOString(),DevicesAvailable:true,Devices:{pc:{UpBPS:100,DownBPS:25000000,UpBytes:1000,DownBytes:5000},phone:{UpBPS:20,DownBPS:300,UpBytes:500,DownBytes:700}},Outbounds:{vpn:{UpBPS:100000,DownBPS:100000}}};
test('map and graph use device counters once, with matching directions',()=>{
 assert.deepEqual(gatewayTrafficPoint(snapshot,false,now),{time:now,up:120,down:25000300,sent:1500,received:5700});
});
test('missing, stale and failed measurements are gaps, not invented zero traffic',()=>{
 for(const value of [undefined,{...snapshot,DevicesAvailable:false},{...snapshot,Updated:'invalid'},{...snapshot,ReceivedAt:now-6000}])assert.equal(gatewayTrafficPoint(value,false,now),null);
 assert.equal(gatewayTrafficPoint(snapshot,true,now),null);
});
test('polling the same snapshot does not create extra graph points',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),history=[p];
 assert.equal(appendGatewayPoint(history,p),history);
 assert.equal(appendGatewayPoint(history,{...p,time:now-1000}),history);
 assert.equal(appendGatewayPoint([{...p,time:now-100000},p],{...p,time:now+1000}).length,2);
});

test('smooth curves stay within the measured range of every segment',()=>{
 const points=[{x:0,y:0},{x:1,y:100},{x:2.2,y:20},{x:3,y:20},{x:4,y:0},{x:5,y:50}];
 const curves=trafficCurve(points);
 assert.equal(curves.length,points.length-1);
 for(const c of curves)for(let i=0;i<=100;i++){
  const t=i/100,u=1-t,y=u*u*u*c.a.y+3*u*u*t*c.c1.y+3*u*t*t*c.c2.y+t*t*t*c.b.y;
  assert.ok(y>=Math.min(c.a.y,c.b.y)-1e-9&&y<=Math.max(c.a.y,c.b.y)+1e-9);
 }
});
test('curves have matching tangents at joins, including flat extrema',()=>{
 const c=trafficCurve([{x:0,y:0},{x:1,y:20},{x:2,y:40},{x:3,y:0}]);
 for(let i=1;i<c.length;i++){
  const left=(c[i-1].b.y-c[i-1].c2.y)/(c[i-1].b.x-c[i-1].c2.x);
  const right=(c[i].c1.y-c[i].a.y)/(c[i].c1.x-c[i].a.x);
  assert.ok(Math.abs(left-right)<1e-9);
 }
});
test('hover selects a real measurement and avoids missing periods',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),history=[p,{...p,time:now+1000,down:500}];
 assert.equal(nearestTrafficPoint(history,now+900),history[1]);
 assert.equal(nearestTrafficPoint(history,now+10000),undefined);
 assert.equal(nearestTrafficPoint([],now),undefined);
});

test('recent graph history survives serialization and expired samples disappear',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),points=[{...p,time:now-91000},{...p,time:now-30000},p];
 assert.deepEqual(restoreGatewayHistory(JSON.stringify({version:1,points}),now),points.slice(1));
 assert.deepEqual(restoreGatewayHistory(JSON.stringify({version:1,points}),now+100000),[]);
});
test('malformed or invalid stored data cannot break the graph',()=>{
 for(const raw of [null,'broken','null','{}','{"version":99,"points":[]}',JSON.stringify({version:1,points:[null,{}, {time:now,up:-1}, {time:now,up:"1",down:0,sent:0,received:0}]})])assert.deepEqual(restoreGatewayHistory(raw,now),[]);
});
test('restored samples are ordered, deduplicated and bounded',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),points=Array.from({length:500},(_,i)=>({...p,time:now-i*100}));
 const restored=restoreGatewayHistory(JSON.stringify({version:1,points:[...points,p]}),now);
 assert.equal(restored.length,96);
 for(let i=1;i<restored.length;i++)assert.ok(restored[i].time>restored[i-1].time);
 assert.deepEqual(restored.at(-1),p);
});

test('returning to the home page restores samples collected while it was unmounted',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),local=[{...p,time:now-120000}];
 const server=Array.from({length:90},(_,i)=>({...p,time:now-(89-i)*1000,down:i+1}));
 const merged=mergeGatewayHistory(local,server,now);
 assert.equal(merged.length,90);assert.equal(merged.at(-1).down,90);
 assert.deepEqual(mergeGatewayHistory([],server,now),merged);
 assert.equal(mergeGatewayHistory(merged,server,now),merged);
});
test('collector history takes precedence without bridging missing samples',()=>{
 const p=gatewayTrafficPoint(snapshot,false,now),local=[p],server=[{...p,down:500},{...p,time:now-10000,down:100}];
 const merged=mergeGatewayHistory(local,server,now);
 assert.equal(merged.length,2);assert.equal(merged.at(-1).down,500);
 assert.equal(nearestTrafficPoint(merged,now-5000),undefined);
 assert.equal(mergeGatewayHistory(local,undefined,now),local);
});


test('traffic freshness ignores VM clock offsets in either direction',()=>{
 for(const shift of [-86400000,-6000,6000,86400000]){
  const server=now+shift,s={...snapshot,Updated:new Date(server).toISOString(),ServerNow:new Date(server+1000).toISOString()};
  assert.equal(trafficFresh(s,false,now+100),true);
  assert.equal(gatewayTrafficPoint(s,false,now+100).time,server);
  assert.equal(trafficServerNow(s,now+500),server+1500);
  assert.equal(trafficFresh(s,false,now+5001),false);
 }
});

test('server availability and receipt expiry still stop animation on stale snapshots',()=>{
 assert.equal(gatewayTrafficPoint({...snapshot,DevicesAvailable:false},false,now),null);
 assert.equal(gatewayTrafficPoint({...snapshot,ReceivedAt:undefined},false,now),null);
 assert.equal(gatewayTrafficPoint(snapshot,false,now+6000),null);
 assert.equal(gatewayTrafficPoint(snapshot,false,now-1),null);
 assert.equal(trafficFresh(snapshot,true,now),false);
});

test('graph history uses the server timeline even with a day of clock drift',()=>{
 const server=now-86400000,s={...snapshot,ServerNow:new Date(server).toISOString()};
 const point={time:server-1000,up:100,down:1000,sent:10,received:100};
 assert.deepEqual(mergeGatewayHistory([], [point], trafficServerNow(s,now)),[point]);
 const history=[point];assert.equal(appendGatewayPoint(history,{...point}),history);
});

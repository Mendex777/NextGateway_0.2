import {test} from 'node:test';
import assert from 'node:assert/strict';
import {particleTimings,particleFrequency} from '../src/trafficMotion.ts';
test('idle and invalid measurements never emit',()=>{
 for(const v of [0,-1,NaN,Infinity])assert.deepEqual(particleTimings(v,v),{up:null,down:null});
});
test('fast download remains dominant with more space between incoming particles',()=>{
 const {up,down}=particleTimings(1504308,111188323);
 assert.ok(up.interval/down.interval>50);
 assert.ok(up.duration>down.duration);
 assert.ok(1000/up.interval+1000/down.interval<=27.00001);
 assert.ok(down.duration>=700);
});
test('reversing transfer swaps direction, equal transfers share the budget',()=>{
 const a=particleTimings(250000,1900),b=particleTimings(1900,250000);
 assert.deepEqual(a.up,b.down);assert.deepEqual(a.down,b.up);
 const same=particleTimings(100000000,100000000);
 assert.equal(same.up.duration,same.down.duration);
 assert.ok(Math.abs(same.down.interval/same.up.interval-1.4)<.0001);
 assert.ok(1000/same.up.interval<=13.50001);
 assert.equal(particleTimings(1000,0).down,null);
});

test('PC download and phone retain independent speeds in both directions',()=>{
 const phone={up:4096,down:32768};
 const pcIdle={up:1024,down:2048},pcDownload={up:1504308,down:111188323};
 const scale=pc=>Math.min(1,60/(particleFrequency(phone.up,phone.down)+particleFrequency(pc.up,pc.down)));
 assert.deepEqual(particleTimings(phone.up,phone.down,scale(pcIdle)),particleTimings(phone.up,phone.down,scale(pcDownload)));
 const before=particleTimings(pcIdle.up,pcIdle.down),after=particleTimings(pcIdle.up,pcDownload.down);
 assert.equal(before.up.duration,after.up.duration);
 assert.ok(after.down.duration<before.down.duration);
});

test('global departure cap never changes flight speed',()=>{
 const normal=particleTimings(4096,32768),capped=particleTimings(4096,32768,.1);
 for(const d of ['up','down']){
  assert.equal(normal[d].duration,capped[d].duration);
  assert.ok(Math.abs(capped[d].interval/normal[d].interval-10)<.0001);
 }
 const rates=Array.from({length:20},()=>[1000000,100000000]);
 const scale=60/rates.reduce((sum,[up,down])=>sum+particleFrequency(up,down),0);
 const departures=rates.reduce((sum,[up,down])=>{const t=particleTimings(up,down,scale);return sum+1000/t.up.interval+1000/t.down.interval;},0);
 assert.ok(departures<=60.00001);
});

test('large transfers increase density and speed while light traffic stays calm',()=>{
 const light=particleTimings(0,1024).down;
 assert.equal(light.interval,4000);
 const medium=particleTimings(0,2*1024*1024).down;
 const fast=particleTimings(0,64*1024*1024).down;
 assert.ok(fast.interval<medium.interval);assert.ok(fast.duration<medium.duration);
 assert.ok(Math.abs(1000/fast.interval-27/1.4)<.0001);
 assert.ok(fast.duration*2*(1000/fast.interval)/1000<48);
});

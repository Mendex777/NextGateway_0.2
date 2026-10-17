import {test} from 'node:test';
import assert from 'node:assert/strict';
import {crossesCard,routeMapPoints,mapSegment} from '../src/mapPaths.ts';
const a={x:0,y:50},b={x:300,y:50};
test('routes avoid intervening cards and preserve endpoints',()=>{
 const boxes=[{left:80,right:140,top:20,bottom:80},{left:170,right:230,top:0,bottom:65}];
 const path=routeMapPoints(a,b,boxes);
 assert.deepEqual(path[0],a);assert.deepEqual(path.at(-1),b);assert.ok(path.length>2);
 for(let i=1;i<path.length;i++)for(const box of boxes)assert.equal(crossesCard(path[i-1],path[i],box),false);
 assert.match(mapSegment(a,b,boxes),/^L /);
});
test('free paths retain their curve, including reverse direction',()=>{
 assert.match(mapSegment(a,b,[]),/^C /);
 assert.deepEqual(routeMapPoints(b,a,[]),[b,a]);
});
test('a shared corner or boundary does not trap a path',()=>{
 const box={left:80,right:140,top:20,bottom:80};
 assert.equal(crossesCard({x:80,y:20},{x:140,y:20},box),false);
 assert.equal(crossesCard(a,b,box),true);
});

test('rounded detours preserve endpoints and stay outside cards in both directions',()=>{
 const boxes=[{left:80,right:140,top:20,bottom:80},{left:170,right:230,top:0,bottom:65}];
 for(const [start,end] of [[a,b],[b,a]]){
  const path=mapSegment(start,end,boxes);assert.match(path,/ Q /);
  let at=start;
  for(const part of path.matchAll(/([LQ])([^LQ]*)/g)){
   const v=part[2].trim().split(/[ ,]+/).map(Number),from=at;
   at=part[1]==='Q'?{x:v[2],y:v[3]}:{x:v[0],y:v[1]};
   let previous=from;
   for(let i=1;i<=100;i++){
    const t=i/100,u=1-t;
    const next=part[1]==='Q'?{x:u*u*from.x+2*u*t*v[0]+t*t*at.x,y:u*u*from.y+2*u*t*v[1]+t*t*at.y}:{x:from.x+(at.x-from.x)*t,y:from.y+(at.y-from.y)*t};
    for(const box of boxes)assert.equal(crossesCard(previous,next,box),false);
    previous=next;
   }
  }
  assert.deepEqual(at,end);
 }
});

test('a narrow free corridor avoids a detour around the whole column',()=>{
 const path=mapSegment({x:0,y:0},{x:300,y:100},[{left:80,right:140,top:-100,bottom:40},{left:80,right:140,top:60,bottom:200}]);
 for(const part of path.matchAll(/([LQ])([^LQ]*)/g)){
  const values=part[2].trim().split(/[ ,]+/).map(Number);
  for(let i=1;i<values.length;i+=2)assert.ok(values[i]>=0&&values[i]<=100);
 }
});

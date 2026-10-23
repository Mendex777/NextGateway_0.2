import {test} from 'node:test';
import assert from 'node:assert/strict';
import {buildVisibleFlows} from '../src/flowModel.ts';
const nodes=[{id:'device:a',kind:'device',ip:'a',traffic:{UpBPS:100,DownBPS:200}},{id:'exit:b',kind:'exit',tag:'b',traffic:{UpBPS:120,DownBPS:250}},{id:'gateway',kind:'gateway'}];
test('aggregate counters never invent an end-to-end route',()=>{
 const flows=buildVisibleFlows(nodes,[],true,true,true);
 assert.equal(flows.length,2);assert.ok(flows.every(f=>!f.attributed&&(f.from==='gateway'||f.to==='gateway')));
});
test('measured routes are not counted again in aggregate legs',()=>{
 const routes=[{Source:'a',Outbound:'b',UpBPS:100,DownBPS:200}];
 const flows=buildVisibleFlows(nodes,routes,true,true,true);
 assert.equal(flows.filter(f=>f.attributed).length,1);
 assert.deepEqual(flows.find(f=>!f.attributed),{key:'counter:exit:b',from:'gateway',to:'exit:b',up:20,down:50,attributed:false});
 assert.deepEqual(buildVisibleFlows(nodes,[...routes,...routes],true,true,true),flows);
});
test('stale data, unknown peers and invalid rates do not create phantom flows',()=>{
 assert.deepEqual(buildVisibleFlows(nodes,[{Source:'a',Outbound:'b',UpBPS:1,DownBPS:1}],false,false,false),[]);
 assert.deepEqual(buildVisibleFlows(nodes,[{Source:'unknown',Outbound:'b',UpBPS:100,DownBPS:200}],true,false,false),[]);
 assert.deepEqual(buildVisibleFlows([{...nodes[0],traffic:{UpBPS:NaN,DownBPS:-1}}],[],true,true,true),[]);
});

import {retainFlowJourneys} from '../src/flowModel.ts';
test('empty samples drain the same journey without stale departures; expired endpoints are removed',()=>{
 const cache=new Map(),ids=new Set(['a','b']),flow={key:'r',from:'a',to:'b',up:10,down:20,attributed:true};
 assert.deepEqual(retainFlowJourneys(cache,[flow],ids,0),[flow]);
 assert.deepEqual(retainFlowJourneys(cache,[],ids,1000),[{...flow,up:0,down:0}]);
 assert.deepEqual(retainFlowJourneys(cache,[flow],ids,2000),[flow]);
 assert.deepEqual(retainFlowJourneys(cache,[],ids,10001),[]);
 retainFlowJourneys(cache,[flow],ids,11000);
 assert.deepEqual(retainFlowJourneys(cache,[],new Set(['a']),11001),[]);
});
test('fresh idle sockets keep a single known route with zero rates; stale sockets do not',()=>{
 const connections=[{Source:'a',Outbound:'b'},{Source:'a',Outbound:'b'},{Source:'missing',Outbound:'b'}];
 const idle=buildVisibleFlows(nodes,[],false,false,false,connections,true);
 assert.deepEqual(idle,[{key:'route:a\0b',from:'device:a',to:'exit:b',up:0,down:0,attributed:true,connected:true}]);
 assert.deepEqual(buildVisibleFlows(nodes,[],false,false,false,connections,false),[]);
 const measured=buildVisibleFlows(nodes,[{Source:'a',Outbound:'b',UpBPS:100,DownBPS:200}],true,false,false,connections,true);
 assert.equal(measured.length,1);assert.equal(measured[0].up,100);assert.equal(measured[0].connected,true);
 const cache=new Map(),ids=new Set(['device:a','exit:b']);retainFlowJourneys(cache,idle,ids,0);
 assert.equal(retainFlowJourneys(cache,[],ids,1000)[0].connected,false);
});

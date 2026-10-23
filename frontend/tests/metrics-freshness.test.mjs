import test from 'node:test';
import assert from 'node:assert/strict';
import {receiveMetrics,metricsFresh} from '../src/metrics-freshness.ts';
test('VM timestamps stay fresh independently of the computer wall clock',()=>{
 const serverNow=Date.parse('2026-10-23T09:00:00Z');
 const [sample]=receiveMetrics([{time:serverNow-1500}],new Date(serverNow).toUTCString(),100);
 assert.equal(metricsFresh(sample,2000),true);
 assert.equal(metricsFresh(sample,8600),false);
});
test('a successful HTTP response cannot revive a stale sample',()=>{
 const serverNow=Date.parse('2026-10-23T09:00:00Z');
 const [sample]=receiveMetrics([{time:serverNow-12000}],new Date(serverNow).toUTCString(),100);
 assert.equal(metricsFresh(sample,100),false);
});
test('missing timestamps and invalid clocks are unavailable',()=>{
 assert.equal(metricsFresh(undefined),false);
 assert.equal(metricsFresh({time:123}),false);
 const [sample]=receiveMetrics([{time:123}],null,100);
 assert.equal(metricsFresh(sample,200),false);
 assert.equal(metricsFresh({time:123,receivedAt:200,ageAtReceipt:0},100),false);
});

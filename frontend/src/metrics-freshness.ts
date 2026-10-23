export interface MetricTimestamp {
  time: number;
  receivedAt?: number;
  ageAtReceipt?: number;
}

// The HTTP Date and sample timestamp use the VM clock. Elapsed time uses the browser monotonic clock.
export function receiveMetrics<T extends {time:number}>(history:T[], serverDate:string|null, receivedAt=performance.now()) {
  const serverNow=Date.parse(serverDate||"");
  return history.map(sample=>({...sample,receivedAt,ageAtReceipt:Number.isFinite(serverNow)&&Number.isFinite(sample.time)?Math.max(0,serverNow-sample.time):Infinity}));
}
export function metricsFresh(sample:MetricTimestamp|undefined,now=performance.now()):boolean {
  if(!sample||sample.receivedAt===undefined||sample.ageAtReceipt===undefined)return false;
  const elapsed=now-sample.receivedAt;
  return elapsed>=0&&sample.ageAtReceipt+elapsed<10000;
}

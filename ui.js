
const edit = document.getElementById('rule-edit');
if (edit) { edit.showModal(); const closeEdit = () => { edit.close(); const u=new URL(location.href);u.searchParams.delete('edit');history.replaceState(null,'',u); };document.getElementById('close-rule-edit').onclick=closeEdit;edit.addEventListener('cancel',e=>{e.preventDefault();closeEdit();});edit.addEventListener('click',e=>{if(e.target===edit){const r=edit.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)closeEdit();}}); }
const list=document.getElementById('rule-list');let moving=null;
const add=document.getElementById('rule-add');
if(add){document.getElementById('open-rule-add').onclick=()=>add.showModal();document.getElementById('close-rule-add').onclick=()=>add.close();add.addEventListener('click',e=>{if(e.target===add){const r=add.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)add.close();}});}
if(list){list.addEventListener('dragstart',e=>{const handle=e.target.closest('.drag-handle');if(!handle)return;moving=handle.closest('[data-rule-id]');moving.classList.add('dragging');e.dataTransfer.effectAllowed='move';e.dataTransfer.setData('text/plain',moving.dataset.ruleId);});list.addEventListener('dragover',e=>{const row=e.target.closest('[data-rule-id]');if(!moving||!row||row===moving)return;e.preventDefault();e.dataTransfer.dropEffect='move';list.querySelectorAll('.drop-target').forEach(r=>r.classList.remove('drop-target'));row.classList.add('drop-target');});list.addEventListener('drop',e=>{const row=e.target.closest('[data-rule-id]');if(!moving||!row||row===moving)return;e.preventDefault();const r=row.getBoundingClientRect();row.parentNode.insertBefore(moving,e.clientY<r.top+r.height/2?row:row.nextSibling);const form=document.getElementById('rule-order-form');form.elements.order.value=Array.from(list.querySelectorAll('[data-rule-id]'),r=>r.dataset.ruleId).join(',');if(form.elements.order.value!==form.elements.before.value)form.requestSubmit();});list.addEventListener('dragend',()=>{list.querySelectorAll('.dragging,.drop-target').forEach(r=>r.classList.remove('dragging','drop-target'));moving=null;});}

const sourceEdit=document.getElementById('source-edit');if(sourceEdit){sourceEdit.showModal();const close=()=>{sourceEdit.close();const u=new URL(location.href);u.searchParams.delete('edit');history.replaceState(null,'',u);};document.getElementById('close-source-edit').onclick=close;sourceEdit.addEventListener('cancel',e=>{e.preventDefault();close();});}
const groups = document.getElementById('source-groups');
document.querySelectorAll('[data-open]').forEach(b=>b.onclick=()=>document.getElementById(b.dataset.open).showModal());
document.querySelectorAll('[data-close]').forEach(b=>b.onclick=()=>document.getElementById(b.dataset.close).close());
document.querySelectorAll('[data-confirm]').forEach(b=>b.onclick=e=>{if(!confirm(b.dataset.confirm))e.preventDefault();});
if(groups){
 const search=document.getElementById('node-search'), filter=document.getElementById('node-filter');
 const allGroups=[...groups.querySelectorAll('.source-group')];
 const storage={get(k){try{return localStorage.getItem(k)}catch{return null}},set(k,v){try{localStorage.setItem(k,v)}catch{}}};
 let filtering=false;
 for(const group of allGroups){const saved=storage.get('source-open:'+group.dataset.sourceId);if(saved!==null)group.open=saved==='1';group.querySelector('summary').addEventListener('click',()=>{if(!filtering)storage.set('source-open:'+group.dataset.sourceId,group.open?'0':'1');});}
 const applyFilters=(reveal=true)=>{
  const q=search.value.trim().toLocaleLowerCase(),state=filter.value;const wasFiltering=filtering;filtering=!!q||state!=='all';let total=0;
  for(const group of allGroups){let count=0;const name=group.querySelector('.source-name').textContent.toLocaleLowerCase();const rows=group.querySelectorAll('[data-node-id]');for(const row of rows){const match=(!q||name.includes(q)||(row.querySelector('.node-name').textContent+' '+row.cells[1].textContent).toLocaleLowerCase().includes(q))&&(state==='all'||(state==='unchecked'?!row.dataset.state:row.dataset.state===state));row.hidden=!match;if(match)count++;}group.hidden=filtering&&count===0;if(filtering&&count&&reveal)group.open=true;else if(wasFiltering&&!filtering){const saved=storage.get('source-open:'+group.dataset.sourceId);group.open=saved===null?group.dataset.selected==='true':saved==='1';}group.querySelector('.group-count').textContent=filtering?count+' из '+rows.length+' подключений':rows.length+' подключений';total+=count;}
  document.getElementById('node-count').textContent='Показано: '+total;document.getElementById('no-nodes').hidden=!filtering||total>0;
 };
 search.value=storage.get('node-search')||'';filter.value=storage.get('node-filter')||'all';
 search.oninput=()=>{storage.set('node-search',search.value);applyFilters();};filter.onchange=()=>{storage.set('node-filter',filter.value);applyFilters();};
 document.getElementById('collapse-sources').onclick=()=>{for(const g of allGroups){g.open=false;if(!filtering)storage.set('source-open:'+g.dataset.sourceId,'0');}};
 document.querySelectorAll('.node-details').forEach(b=>b.onclick=()=>{for(const k of ['name','host','port','transport','security','encryption','sni','flow','path','compatibility'])document.getElementById('detail-'+k).textContent=b.dataset[k]||(k==='compatibility'?'Параметры поддерживаются':'—');document.getElementById('node-detail').showModal();});
 applyFilters();
 let timer;
 const poll=async()=>{
  try{const response=await fetch('/probe-status');if(!response.ok)throw Error();const data=await response.json();let running=data.Batch.State==='running';
   for(const row of groups.querySelectorAll('[data-node-id]')){const p=data.Nodes[row.dataset.nodeId]||{};row.dataset.state=p.State||'';if(p.State==='running')running=true;const cell=row.querySelector('.probe-result');cell.replaceChildren(document.createTextNode(p.Message||'Не проверен'));if(p.Checked){cell.append(document.createElement('br'));const small=document.createElement('small');small.textContent=p.Checked+(p.UDP?'':' · TCP: '+p.TCPMS+' мс')+(p.State==='ok'?' · HTTPS: '+p.HTTPSMS+' мс':'');cell.append(small);}}
   const batch=data.Batch,panel=document.getElementById('batch-panel');panel.hidden=!batch.State;if(batch.State){const group=allGroups.find(g=>g.dataset.sourceId===batch.SourceID);document.getElementById('batch-progress').textContent=(group?group.querySelector('.source-name').textContent+': ':'')+(batch.State==='running'?'Проверка':batch.State==='cancelled'?'Проверка отменена':'Проверка завершена')+' — '+batch.Done+' / '+batch.Total+', работают: '+batch.OK;document.getElementById('cancel-batch').hidden=batch.State!=='running';}applyFilters(false);timer=setTimeout(poll,running?2000:15000);
  }catch{timer=setTimeout(poll,15000);}
 };
 poll();window.addEventListener('pagehide',()=>clearTimeout(timer));
}

const deviceEdit=document.getElementById('device-edit');if(deviceEdit){deviceEdit.showModal();const clean=()=>{const u=new URL(location.href);u.searchParams.delete('edit');history.replaceState(null,'',u);};deviceEdit.addEventListener('close',clean);}
document.querySelectorAll('.device-picker').forEach(select=>select.onchange=()=>{if(select.value){const field=select.closest('form').querySelector('[name="source"]');field.value=select.value;}});

{const panel=document.getElementById('panel-update');if(panel){let wasRunning=panel.dataset.state==='running';setInterval(async()=>{try{const r=await fetch('/panel-update-status');if(!r.ok)return;const s=await r.json();document.getElementById('panel-update-message').textContent=s.Message;panel.querySelector('[value="panel-update-install"]').disabled=!s.Available||s.State==='running';panel.querySelector('[value="panel-update-rollback"]').disabled=!s.CanRollback||s.State==='running';if(wasRunning&&s.State!=='running'){location.reload();return;}wasRunning=s.State==='running';}catch{}},2000);}}

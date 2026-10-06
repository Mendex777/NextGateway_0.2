const localDate=value=>{const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('ru-RU',{day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'});};
document.querySelectorAll('time[datetime]').forEach(time=>{time.title=new Date(time.dateTime).toLocaleString('ru-RU');time.textContent=localDate(time.dateTime);});
const menuToggle=document.getElementById('menu-toggle');
const closeNav=()=>{document.body.classList.remove('nav-open');menuToggle?.setAttribute('aria-expanded','false');};
if(menuToggle){menuToggle.onclick=()=>{const open=document.body.classList.toggle('nav-open');menuToggle.setAttribute('aria-expanded',String(open));};document.getElementById('menu-overlay').onclick=closeNav;}
document.addEventListener('keydown',e=>{if(e.key==='Escape'){closeNav();document.querySelectorAll('.action-menu[open]').forEach(menu=>menu.open=false);}});
document.addEventListener('click',e=>{document.querySelectorAll('.action-menu[open]').forEach(menu=>{if(!menu.contains(e.target)||e.target.closest('button,a'))menu.open=false;});});
document.querySelectorAll('.action-menu').forEach(menu=>menu.addEventListener('toggle',()=>{if(!menu.open)return;document.querySelectorAll('.action-menu[open]').forEach(other=>{if(other!==menu)other.open=false;});const box=menu.querySelector('div'),anchor=menu.querySelector('summary').getBoundingClientRect();box.style.position='fixed';box.style.width='190px';box.style.right='auto';box.style.bottom='auto';box.style.left=Math.max(8,Math.min(innerWidth-198,anchor.right-190))+'px';box.style.top=(anchor.bottom+box.offsetHeight+8>innerHeight?Math.max(8,anchor.top-box.offsetHeight-5):anchor.bottom+5)+'px';}));
window.addEventListener('resize',()=>document.querySelectorAll('.action-menu[open]').forEach(menu=>menu.open=false));
document.addEventListener('scroll',e=>{if(!e.target.closest?.('.action-menu'))document.querySelectorAll('.action-menu[open]').forEach(menu=>menu.open=false);},true);
document.querySelectorAll('[data-persist-group]').forEach(group=>{try{group.open=localStorage.getItem('vpn-group:'+group.dataset.persistGroup)==='1';}catch{}group.addEventListener('toggle',()=>{try{localStorage.setItem('vpn-group:'+group.dataset.persistGroup,group.open?'1':'0');}catch{}});});
// Returning from a subscription action keeps the user's place in a long list.
if(document.body.dataset.tab==='subscriptions'){
 try{const restore=JSON.parse(sessionStorage.getItem('subscription-scroll'));sessionStorage.removeItem('subscription-scroll');if(restore&&Date.now()-restore.time<60000)requestAnimationFrame(()=>window.scrollTo(0,restore.y));}catch{}
 document.addEventListener('submit',e=>{if(e.defaultPrevented||e.target.closest('dialog'))return;try{sessionStorage.setItem('subscription-scroll',JSON.stringify({y:window.scrollY,time:Date.now()}));}catch{}});
}
const renderProbe=(cell,p)=>{
 cell.title=p.Message||'';cell.replaceChildren();const status=document.createElement(p.State==='ok'?'strong':'span');
 status.className=p.State==='ok'?'health good':p.State==='error'?'badge bad':p.State==='running'?'badge':'muted';
 status.textContent=p.State==='ok'?p.HTTPSMS+' мс':p.State==='error'?'Ошибка':p.State==='running'?'Проверка…':'Не проверен';cell.append(status);
 if(p.State==='error'&&p.Message){const error=document.createElement('small');error.textContent=p.Message;cell.append(error);}
 if(p.Checked){const stamp=document.createElement('small');stamp.textContent=localDate(p.Checked);stamp.title=new Date(p.Checked).toLocaleString('ru-RU');cell.append(stamp);}
};
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
   for(const row of groups.querySelectorAll('[data-node-id]')){const p=data.Nodes[row.dataset.nodeId]||{};row.dataset.state=p.State||'';if(p.State==='running')running=true;const cell=row.querySelector('.probe-result');renderProbe(cell,p);}
   const batch=data.Batch,panel=document.getElementById('batch-panel');panel.hidden=!batch.State;if(batch.State){const group=allGroups.find(g=>g.dataset.sourceId===batch.SourceID);document.getElementById('batch-progress').textContent=(group?group.querySelector('.source-name').textContent+': ':'')+(batch.State==='running'?'Проверка':batch.State==='cancelled'?'Проверка отменена':'Проверка завершена')+' — '+batch.Done+' / '+batch.Total+', работают: '+batch.OK;document.getElementById('cancel-batch').hidden=batch.State!=='running';}applyFilters(false);timer=setTimeout(poll,running?2000:15000);
  }catch{timer=setTimeout(poll,15000);}
 };
 poll();window.addEventListener('pagehide',()=>clearTimeout(timer));
}

const deviceEdit=document.getElementById('device-edit');if(deviceEdit){deviceEdit.showModal();const clean=()=>{const u=new URL(location.href);u.searchParams.delete('edit');history.replaceState(null,'',u);};deviceEdit.addEventListener('close',clean);}
document.querySelectorAll('.device-picker').forEach(select=>select.onchange=()=>{if(select.value){const field=select.closest('form').querySelector('[name="source"]');field.value=select.value;}});

{const panel=document.getElementById('panel-update');if(panel){let wasRunning=panel.dataset.state==='running';setInterval(async()=>{try{const r=await fetch('/panel-update-status');if(!r.ok)return;const s=await r.json();document.getElementById('panel-update-message').textContent=s.Message;panel.querySelector('[value="panel-update-install"]').disabled=!s.Available||s.State==='running';panel.querySelector('[value="panel-update-rollback"]').disabled=!s.CanRollback||s.State==='running';if(wasRunning&&s.State!=='running'&&!document.body.dataset.operation){location.reload();return;}wasRunning=s.State==='running';}catch{}},2000);}}

(()=>{const box=document.getElementById('action-notice'),text=document.getElementById('action-notice-text');if(!box)return;const url=new URL(location.href);const clean=()=>{for(const key of ['message','operation','since'])url.searchParams.delete(key);history.replaceState(null,'',url);};const show=(message,state)=>{text.textContent=message;box.className='notice '+(state==='error'?'error':state==='running'?'running':'success');box.hidden=false;};document.getElementById('dismiss-notice').onclick=()=>{box.hidden=true;};let flash;try{flash=JSON.parse(sessionStorage.getItem('ngpanel-result'));sessionStorage.removeItem('ngpanel-result');}catch{}if(flash)show(flash.Message,flash.State);const action=document.body.dataset.operation,since=document.body.dataset.since;if(action&&since){show('Выполняется операция…','running');let finished=false;const poll=async()=>{if(finished)return;try{const r=await fetch('/operation-status?'+new URLSearchParams({action,since}));if(!r.ok)return;const s=await r.json();show(s.Message,s.State);if(s.Done){finished=true;clean();if(s.State!=='error'){sessionStorage.setItem('ngpanel-result',JSON.stringify(s));location.replace(url.href);}}}catch{}};poll();setInterval(poll,1500);}else{clean();if(!box.hidden){if(!flash)show(text.textContent,text.textContent.startsWith('Ошибка')?'error':'ok');if(!box.classList.contains('error'))setTimeout(()=>box.hidden=true,12000);}}})();

const balanceSearch = document.getElementById('balance-search');
const balanceOnly = document.getElementById('balance-selected-only');
const balanceSource = document.getElementById('balance-source');
const balanceFilter = () => {
 if(!balanceSearch)return;
 const rows=Array.from(document.querySelectorAll('.balance-entry'));
 const selected=rows.filter(row=>row.querySelector('input').checked);
 const query=balanceSearch.value.trim().toLocaleLowerCase();
 let visible=0;
 for(const row of rows){const input=row.querySelector('input');row.hidden=!row.textContent.toLocaleLowerCase().includes(query)||(balanceOnly.checked&&!input.checked)||(balanceSource.value&&row.dataset.source!==balanceSource.value);if(!row.hidden)visible++;row.classList.toggle('picked',input.checked);input.disabled=input.dataset.incompatible==='1'||(!input.checked&&selected.length>=8);}
 const count=document.getElementById('balance-selection-count');count.textContent=selected.length+' / 8';
 const chips=document.getElementById('balance-chips');chips.replaceChildren();
 if(!selected.length){const hint=document.createElement('small');hint.textContent='Пока ничего не выбрано';chips.append(hint);}
 for(const row of selected){const input=row.querySelector('input'),name=row.querySelector('.member-name').textContent;const chip=document.createElement('button');chip.type='button';chip.className='member-chip';chip.textContent=name+' ×';chip.setAttribute('aria-label','Убрать '+name);chip.onclick=()=>{input.checked=false;balanceFilter();};chips.append(chip);}
 document.getElementById('balance-empty').hidden=visible>0;
 document.getElementById('balance-clear').disabled=!selected.length;
};
if(balanceSearch){
 document.querySelectorAll('.balance-entry input').forEach(input=>{input.dataset.incompatible=input.disabled?'1':'0';input.addEventListener('change',balanceFilter);});
 balanceSearch.addEventListener('input',balanceFilter);balanceOnly.addEventListener('change',balanceFilter);balanceSource.addEventListener('change',balanceFilter);
 document.getElementById('balance-clear').onclick=()=>{document.querySelectorAll('.balance-entry input').forEach(input=>input.checked=false);balanceFilter();};
 balanceSearch.closest('form').addEventListener('submit',event=>{const count=document.querySelectorAll('.balance-entry input:checked').length;if(count<2||count>8){event.preventDefault();document.getElementById('balance-selection-count').textContent='Нужно от 2 до 8 подключений';balanceSearch.focus();}});
 balanceFilter();
}

document.querySelectorAll('.group-status').forEach(el=>{
 const poll=async()=>{try{
  const r=await fetch('/balance-status?id='+encodeURIComponent(el.dataset.groupId),{cache:'no-store'});if(!r.ok)return;
  const s=await r.json();el.textContent=[s.Policy,s.Controller||s.Message].filter(Boolean).join(' · ');el.title=s.Message||'';
  const group=el.closest('details');group.querySelector('.group-current').textContent=s.Name?'Сейчас: '+s.Name:s.Message;
  const samples=new Map((s.Samples||[]).map(n=>[n.NodeID,n]));
  group.querySelectorAll('[data-group-node]').forEach(row=>{
   const n=samples.get(row.dataset.groupNode),active=Boolean(n?.Active);row.classList.toggle('selected',active);
   const delay=row.querySelector('.group-delay');delay.textContent=n?.Checked&&n.Alive?n.DelayMS+' мс':'—';delay.classList.toggle('health',Boolean(n?.Alive));delay.classList.toggle('good',Boolean(n?.Alive));
   const stale=n?.Checked&&(Date.now()/1000-n.Checked>Number(el.dataset.interval)*3+15);
   const state=row.querySelector('.group-node-status');state.textContent=!n?.Checked?'Не проверен':stale?'Данные устарели':n.Alive?'Доступен':'Недоступен';state.className='group-node-status health '+(stale?'warn':n?.Alive?'good':n?.Checked?'bad':'neutral');state.title=n?.Checked?'Проверено: '+new Date(n.Checked*1000).toLocaleString('ru-RU'):'';
   const button=row.querySelector('[value="group-select"]');button.textContent=active?'Активен':'Выбрать';button.setAttribute('aria-pressed',String(active));button.classList.toggle('quiet',active);
  });
 }catch{el.textContent='Не удалось получить состояние группы';}};
 el.closest('details').addEventListener('group-updated',poll);poll();setInterval(poll,5000);
});
const policySelector=document.querySelector('select[name=policy]');if(policySelector){const update=()=>document.querySelector('.threshold-fields').hidden=policySelector.value!=='threshold';policySelector.addEventListener('change',update);update();}
const groupEdit=document.getElementById('group-edit');if(groupEdit){const members=groupEdit.dataset.members.trim().split(/\s+/);groupEdit.querySelectorAll("input[name=balance_node]").forEach(el=>el.checked=members.includes(el.value));balanceFilter();groupEdit.showModal();}

document.querySelectorAll('.group-check-result').forEach(el=>{
 const poll=async()=>{try{const r=await fetch('/group-check-status?id='+encodeURIComponent(el.dataset.groupId),{cache:'no-store'});if(!r.ok)return;const result=await r.json();el.replaceChildren();if(!result.State)return;const text=document.createElement('span');text.textContent=result.Message;el.append(text);if(result.Samples?.length){const list=document.createElement('ul');for(const sample of result.Samples){const row=el.closest('details').querySelector('[data-group-node="'+sample.NodeID+'"]');const item=document.createElement('li');item.textContent=(row?.cells[0].childNodes[0].textContent||sample.NodeID)+': '+(sample.Alive?sample.DelayMS+' мс':'Недоступен');list.append(item);}el.append(list);}}catch{el.textContent='Не удалось получить результат проверки';}};
 poll();setInterval(poll,3000);
});

// Keep the expanded group and scroll position while applying group actions.
document.querySelectorAll('.group-status').forEach(status=>{
 const group=status.closest('details');
 group.querySelectorAll('form').forEach(form=>form.addEventListener('submit',async event=>{
  const button=event.submitter;
  if(!button||!['group-select','group-check'].includes(button.value))return;
  event.preventDefault();
  const buttons=Array.from(group.querySelectorAll('button[name="action"]'));
  if(buttons.some(b=>b.disabled))return;
  buttons.forEach(b=>b.disabled=true);
  let notice=group.querySelector('.group-action-result');
  if(!notice){notice=document.createElement('p');notice.className='group-action-result';notice.setAttribute('role','status');status.after(notice);}
  notice.textContent=button.value==='group-select'?'Переключение…':'Запуск проверки…';
  try{
   const body=new URLSearchParams(new FormData(form));body.set('action',button.value);
   const response=await fetch('/action',{method:'POST',headers:{Accept:'application/json'},body});
   const result=await response.json();notice.textContent=result.message;
   if(result.ok)group.dispatchEvent(new Event('group-updated'));
  }catch{notice.textContent='Не удалось получить результат. Проверьте состояние группы перед повторной попыткой.';}
  finally{buttons.forEach(b=>b.disabled=false);}
 }));
});

const setupButton=document.getElementById('setup-components');
if(setupButton)setupButton.addEventListener('click',async()=>{
 const progress=document.getElementById('setup-progress');setupButton.disabled=true;
 const installed=action=>document.querySelector('[data-component-action="'+action+'"]')?.dataset.state==='good';
 const steps=[...(!installed('install')?[['install','Установка Xray']]:[]),...(!installed('dependencies')?[['dependencies','Установка компонентов шлюза']]:[]),...(!installed('geodata')?[['geodata','Загрузка geo-баз']]:[])];
 try{
  for(const [action,label] of steps){
   progress.textContent=label+'…';
   const request=await fetch('/action',{method:'POST',headers:{Accept:'application/json'},body:new URLSearchParams({action,tab:'status'})});
   const queued=await request.json();if(!queued.ok)throw new Error(queued.message);
   const deadline=Date.now()+600000;
   while(true){
    await new Promise(resolve=>setTimeout(resolve,2000));
    const response=await fetch('/operation-status?action='+action+'&since='+encodeURIComponent(queued.since),{cache:'no-store'});
    if(!response.ok)throw new Error('Не удалось получить состояние операции');
    const result=await response.json();progress.textContent=label+' · '+result.Message;
    if(result.Done){if(result.State!=='ok')throw new Error(result.Message);break;}
    if(Date.now()>deadline)throw new Error('Операция ещё не завершена. Проверьте состояние на главной странице.');
   }
  }
  progress.textContent='Компоненты готовы. Перейдите к восстановлению бекапа или настройке подписок.';
  const refresh=document.createElement('a');refresh.href='/?tab=status&setup=1';refresh.textContent=' Обновить статусы';progress.append(refresh);
 }catch(error){progress.textContent=error.message;}
 finally{setupButton.disabled=false;}
});

document.querySelectorAll('dialog').forEach(dialog=>{
 const field=dialog.querySelector('input:not([type=hidden]):not([type=checkbox]),textarea,select');if(field)field.setAttribute('autofocus','');
 const button=document.createElement('button');button.type='button';button.className='dialog-close';button.setAttribute('aria-label','Закрыть окно');button.textContent='×';
 button.onclick=()=>{const event=new Event('cancel',{cancelable:true});if(dialog.dispatchEvent(event))dialog.close();};dialog.prepend(button);
});
if(groupEdit)groupEdit.addEventListener('close',()=>{const url=new URL(location.href);url.searchParams.delete('group');history.replaceState(null,'',url);});

document.querySelectorAll('.rule-switch-form').forEach(form=>form.addEventListener('submit',async event=>{
 event.preventDefault();const button=form.querySelector('[role="switch"]');if(button.disabled)return;button.disabled=true;
 try{const body=new URLSearchParams(new FormData(form));body.set('action','rule-toggle');const response=await fetch('/action',{method:'POST',headers:{Accept:'application/json'},body});const result=await response.json();if(!result.ok)throw new Error(result.message);const checked=result.enabled;button.setAttribute('aria-checked',String(checked));button.title=(checked?'Отключить':'Включить')+' правило';
 const notice=document.getElementById('action-notice');notice.className='notice success';notice.hidden=false;document.getElementById('action-notice-text').textContent=result.message;
 if(!document.querySelector('.pending-bar')){const bar=document.createElement('section');bar.className='pending-bar';bar.setAttribute('aria-label','Неприменённые изменения');bar.innerHTML='<div><strong>Есть неприменённые изменения</strong><small>Применение перезапустит Xray. Текущие соединения могут переподключиться.</small></div><form method="post" action="/action"><input type="hidden" name="tab" value="routing"><button name="action" value="apply" class="primary">Применить конфигурацию</button></form>';document.getElementById('main-content').prepend(bar);}
 }catch(error){const notice=document.getElementById('action-notice');notice.className='notice error';notice.hidden=false;document.getElementById('action-notice-text').textContent=error.message||'Не удалось сохранить. Обновите страницу перед повторной попыткой.';}finally{button.disabled=false;}
}));

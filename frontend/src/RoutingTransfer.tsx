import { useState } from "react";
import { Alert, App, Button, Checkbox, Form, Input, Modal, Select, Space, Table, Upload, Typography } from "antd";
import { DownloadOutlined, UploadOutlined, GithubOutlined } from "@ant-design/icons";
import type { Page } from "./types";
import { Target } from "./common";
type Template = {format:string;version:number;default_route:string;outgoings:{key:string;label:string;kind:string}[];rules:{name:string;kind:string;value:string;target:string;source?:string;disabled?:boolean}[]};
export default function RoutingTransfer({p,onDone}:{p:Page;onDone:()=>Promise<unknown>}) {
 const { message }=App.useApp();
 const [open,setOpen]=useState(false),[url,setURL]=useState(""),[template,setTemplate]=useState<Template>(),[mapping,setMapping]=useState<Record<string,string>>({}),[sources,setSources]=useState<Record<string,string>>({}),[replace,setReplace]=useState(false),[useDefault,setUseDefault]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const request=async(body:unknown)=>{const response=await fetch("/routes-template",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});if(!response.ok)throw Error(await response.text());return response.json();};
 const preview=async(body:unknown)=>{setBusy(true);setError("");try{const t:Template=await request({operation:"preview",...body as object});setTemplate(t);setMapping({});setSources(Object.fromEntries(t.rules.filter(r=>r.source).map(r=>[r.source!,r.source!])));setReplace(false);setUseDefault(false);}catch(e){setError(String(e instanceof Error?e.message:e));}finally{setBusy(false);}};
 const submit=async()=>{if(!template)return;setBusy(true);setError("");try{const result=await request({operation:"import",template,mapping,sources,replace,default:useDefault,before:p.RuleOrder});await onDone();void message.success(`Импортировано правил: ${result.count}. Нажмите «Применить».`);setOpen(false);}catch(e){setError(String(e instanceof Error?e.message:e));}finally{setBusy(false);}};
 const names=Object.fromEntries((template?.outgoings||[]).map(e=>[e.key,e.label]));
 const label=(key:string)=>({direct:"Direct — провайдер",proxy:"Выбранный VPN",block:"Блокировать"}[key]||names[key]||key);
 return <>
 <Button icon={<DownloadOutlined />} href="/routes-template">Экспорт правил</Button>
 <Button icon={<UploadOutlined />} onClick={()=>{setOpen(true);setTemplate(undefined);setError("");}}>Импорт правил</Button>
 <Modal title="Импорт маршрутов" open={open} onCancel={()=>setOpen(false)} width={900} style={{top:24}} footer={template?<Space><Button onClick={()=>setTemplate(undefined)}>Другой шаблон</Button><Button onClick={()=>setOpen(false)}>Отмена</Button><Button type="primary" loading={busy} disabled={template.outgoings.some(e=>!mapping[e.key])} onClick={()=>void submit()}>{replace?"Заменить правила":"Добавить правила"}</Button></Space>:null}>
 {error&&<Alert showIcon type="error" title={error} style={{marginBottom:16}} />}
 {!template?<Space orientation="vertical" style={{width:"100%"}} size={16}>
 <Typography.Text>Загрузите JSON, созданный кнопкой «Экспорт правил», или укажите ссылку GitHub на такой файл.</Typography.Text>
 <Upload accept=".json,application/json" showUploadList={false} beforeUpload={async(file)=>{if(file.size>16*1024*1024){setError("Лимит файла — 16 МБ");return false;}try{await preview({template:JSON.parse(await file.text())});}catch{setError("Некорректный JSON");}return false;}}><Button loading={busy} icon={<UploadOutlined />}>Выбрать файл</Button></Upload>
 <Space.Compact style={{width:"100%"}}><Input aria-label="Ссылка на шаблон GitHub" placeholder="https://github.com/…/blob/main/routes.json" value={url} onChange={e=>setURL(e.target.value)} /><Button icon={<GithubOutlined />} loading={busy} disabled={!url} onClick={()=>void preview({url})}>Загрузить</Button></Space.Compact>
 </Space>:<>
 <Alert showIcon type="info" title={`Правил в шаблоне: ${template.rules.length}. Подписки, подключения и группы не импортируются.`} description="Direct, выбранный VPN и блокировка сохраняют своё значение. Остальные исходящие нужно назначить на ваши узлы или группы; можно пропустить связанные правила. Импорт не применяет конфигурацию Xray." style={{marginBottom:16}} />
 <Form layout="vertical">
 {template.outgoings.map(exit=><Form.Item key={exit.key} label={`Исходящий шаблона: ${exit.label}`} required><Space.Compact style={{width:"100%"}}><div style={{flex:1}}><Target p={p} value={mapping[exit.key]==="skip"?undefined:mapping[exit.key]} onChange={value=>setMapping(old=>({...old,[exit.key]:value}))} /></div><Button type={mapping[exit.key]==="skip"?"primary":"default"} onClick={()=>setMapping(old=>({...old,[exit.key]:"skip"}))}>Пропустить</Button></Space.Compact></Form.Item>)}
 {Object.entries(sources).map(([original,value])=><Form.Item key={original} label={`IP устройств в шаблоне: ${original}`} extra="Проверьте адреса для вашей сети. Пусто — все устройства; для правила устройства адрес обязателен."><Input.TextArea aria-label={`IP устройств: ${original}`} value={value} onChange={e=>setSources(old=>({...old,[original]:e.target.value}))} rows={2} /></Form.Item>)}
 <Space orientation="vertical" style={{marginBottom:16}}><Checkbox checked={replace} onChange={e=>setReplace(e.target.checked)}>Заменить все существующие правила</Checkbox><Checkbox checked={useDefault} onChange={e=>setUseDefault(e.target.checked)}>Импортировать маршрут по умолчанию: {label(template.default_route)}</Checkbox></Space>
 {replace&&<Alert type="warning" showIcon title="Существующие правила будут удалены. Для возврата заранее экспортируйте их." style={{marginBottom:16}} />}
 <Table size="small" rowKey={(_,i)=>String(i)} pagination={{pageSize:10}} dataSource={template.rules} columns={[{title:"Название",dataIndex:"name"},{title:"Тип",dataIndex:"kind"},{title:"Устройства",dataIndex:"source",render:v=>v||"Все"},{title:"Исходящий шаблона",dataIndex:"target",render:label},{title:"Включено",dataIndex:"disabled",render:v=>v?"Нет":"Да"}]} />
 </Form>
 </>}
 </Modal>
 </>;
}
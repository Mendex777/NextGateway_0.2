import {useEffect,useState} from "react";
import { Button, ColorPicker, Space, Typography, Input, Segmented, Switch } from "antd";
import { CheckOutlined } from "./icons";
import { baseThemes, defaultUpload, defaultDownload, defaultAccent, palettes, usePanelTheme } from "./PanelTheme";
function TrafficColorSetting({label,value,onChange}:{label:string;value:string;onChange:(color:string)=>void}){
 const [draft,setDraft]=useState(value);useEffect(()=>setDraft(value),[value]);
 return <div style={{display:"flex",alignItems:"center",gap:12,marginTop:10}}><label style={{flex:1}}>{label}</label><ColorPicker aria-label={`Цвет: ${label}`} value={value} disabledAlpha onChange={color=>onChange(color.toHexString())}/><Input aria-label={`Цвет: ${label}, HEX`} style={{width:104}} value={draft} maxLength={7} onChange={e=>{setDraft(e.target.value);if(/^#[0-9a-f]{6}$/i.test(e.target.value))onChange(e.target.value);}} onBlur={()=>setDraft(value)}/></div>;
}
export default function PanelSettings() {
  const {trafficUpload,trafficDownload,setTrafficUpload,setTrafficDownload,networkView,setNetworkView,accent, setAccent, baseTheme, setBaseTheme,density,setDensity,mapAnimation,setMapAnimation,mapSpeeds,setMapSpeeds,resetAppearance } = usePanelTheme();
  return <div className="appearance-panel">
    <Typography.Title level={5} style={{ marginTop: 0 }}>Основная тема</Typography.Title>
    <Typography.Paragraph type="secondary">Оформление всей панели. Изменения сохраняются в этом браузере.</Typography.Paragraph>
    <div className="base-theme-options" role="group" aria-label="Основная тема">
      {baseThemes.map((item) => <Button key={item.id} className="base-theme-choice" aria-pressed={baseTheme === item.id} onClick={() => setBaseTheme(item.id)}>
        <span className="base-theme-preview" aria-hidden="true" style={{ background: item.background, borderColor: item.border }}><span style={{ background: item.sidebar }} /><span style={{ background: item.surface, borderColor: item.border }} /></span>
        <span>{item.label}</span>{baseTheme === item.id && <CheckOutlined />}
      </Button>)}
    </div>
    <Typography.Title level={5} style={{ marginTop: 28 }}>Акцентный цвет</Typography.Title>
    <Typography.Paragraph type="secondary">Акцентный цвет кнопок, меню и графиков. Настройка сохраняется в этом браузере.</Typography.Paragraph>
    <Space wrap size={[12, 12]}>
      {palettes.map(({color,label}) => <Button key={color} aria-pressed={accent === color}
        className="accent-choice" onClick={() => setAccent(color)}
        icon={<span className="accent-swatch" style={{background:color}}>{accent === color && <CheckOutlined />}</span>}>{label}</Button>)}
    </Space>
    <div className="accent-custom"><Space wrap>
      <Typography.Text>Свой цвет</Typography.Text>
      <ColorPicker value={accent} disabledAlpha showText onChange={(color) => setAccent(color.toHexString())} />
      <Button type="text" disabled={accent === defaultAccent} onClick={() => setAccent(defaultAccent)}>По умолчанию</Button>
    </Space></div>
    <section className="appearance-section"><h3>Плотность таблиц</h3><Segmented block aria-label="Плотность таблиц" value={density} onChange={value=>setDensity(value as "normal"|"compact")} options={[{label:"Обычная",value:"normal"},{label:"Компактная",value:"compact"}]}/></section>
    <section className="appearance-section"><h3>Вид сети на главной</h3><Segmented block aria-label="Вид сети на главной" value={networkView} onChange={value=>setNetworkView(value as "map"|"flows")} options={[{label:"Карта",value:"map"},{label:"Потоки",value:"flows"}]}/></section>
    <section className="appearance-section"><h3>Цвета трафика</h3><Typography.Paragraph type="secondary">Отправка и приём в карте, потоках и графике трафика. Сохраняются в этом браузере.</Typography.Paragraph>
      <TrafficColorSetting label="Отправка" value={trafficUpload} onChange={setTrafficUpload}/><TrafficColorSetting label="Приём" value={trafficDownload} onChange={setTrafficDownload}/>
      <Button type="text" style={{marginTop:8}} disabled={trafficUpload===defaultUpload&&trafficDownload===defaultDownload} onClick={()=>{setTrafficUpload(defaultUpload);setTrafficDownload(defaultDownload);}}>Стандартные цвета трафика</Button>
    </section>
    <section className="appearance-section appearance-animation"><div><h3>Скорость на карточках</h3><p>Отправка и приём на карточках устройств, шлюза и VPN.</p></div><Switch aria-label="Скорость на карточках" checked={mapSpeeds} onChange={setMapSpeeds}/></section>
    <section className="appearance-section appearance-animation"><div><h3>Анимация карты</h3><p>Движение трафика и возврат устройств на место. Сбор данных продолжается.</p></div><Switch aria-label="Анимация карты" checked={mapAnimation} onChange={setMapAnimation}/></section>
    <Button className="appearance-reset" block onClick={resetAppearance}>Сбросить оформление</Button>
  </div>;
}

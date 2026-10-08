import { Button, Card, ColorPicker, Space, Typography } from "antd";
import { CheckOutlined } from "@ant-design/icons";
import { defaultAccent, palettes, usePanelTheme } from "./PanelTheme";
export default function PanelSettings() {
  const { accent, setAccent } = usePanelTheme();
  return <Card title="Настройки панели" className="panel-settings">
    <Typography.Title level={5}>Цветовая схема</Typography.Title>
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
  </Card>;
}

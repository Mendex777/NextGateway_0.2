import { useState } from "react";
import {
  App,
  Alert,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Typography,
  Upload,
} from "antd";
import {
  DownloadOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  UploadOutlined,
} from "./icons";
import {
  date,
  Editor,
  JsonDetails,
  required,
  Target,
  type Run,
} from "./common";
import type { Device, Page } from "./types";
import "./gateway.css";
export function Devices({ p, run }: { p: Page; run: Run }) {
  const [editing, setEditing] = useState<Device | null | undefined>();
  return (
    <>
      <Card
        title="Устройства"
        extra={
          <Space>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => void run("device-discover").catch(() => {})}
            >
              Обнаружить
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setEditing(null)}
            >
              Добавить
            </Button>
          </Space>
        }
      >
        {p.DeviceDiscovery && (
          <Typography.Paragraph type="secondary">
            {p.DeviceDiscovery}
          </Typography.Paragraph>
        )}
        <Table
          rowKey="IP"
          size="small"
          dataSource={p.Devices || []}
          pagination={false}
          scroll={{ x: 700 }}
          columns={[
            { title: "Имя", dataIndex: "Name" },
            { title: "IPv4", dataIndex: "IP" },
            { title: "MAC", dataIndex: "MAC" },
            { title: "Производитель", dataIndex: "Vendor" },
            { title: "Маршрут", dataIndex: "TargetLabel" },
            { title: "Обнаружено", dataIndex: "Seen", render: date },
            {
              title: "",
              render: (_, d) => (
                <Button
                  size="small"
                  icon={<EditOutlined />}
                  aria-label={"Изменить устройство " + d.Name}
                  onClick={() => setEditing(d)}
                />
              ),
            },
          ]}
        />
      </Card>
      <Editor
        key={editing?.IP || "device-new"}
        title={editing ? "Изменить устройство" : "Добавить устройство"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        initial={
          editing
            ? {
                ip: editing.IP,
                name: editing.Manual ? editing.Name : "",
                target: editing.Target,
              }
            : { target: "" }
        }
        onSave={(v) => run("device-save", v)}
      >
        <Form.Item name="ip" label="IPv4" rules={required}>
          <Input disabled={!!editing} />
        </Form.Item>
        <Form.Item
          name="name"
          label="Имя"
          extra="Пусто — определить автоматически"
        >
          <Input maxLength={200} placeholder={editing?.AutoName} />
        </Form.Item>
        <Form.Item name="target" label="Маршрут" extra="Пусто — общие правила">
          <Target p={p} allowClear />
        </Form.Item>
      </Editor>
    </>
  );
}
export function Gateway({ p, run }: { p: Page; run: Run }) {
  const doRun = (name: string) => void run(name).catch(() => {});
  const [networkForm] = Form.useForm();
  const networkMode = Form.useWatch("mode", networkForm);
  return <div className="gateway-settings-page">
    <h1>Сеть и DNS</h1>
    <Card title="Настройки сети ВМ">
      <Descriptions items={[
        {key:"address",label:"Текущий IP",children:p.DetectedNetwork.address},
        {key:"interface",label:"Интерфейс",children:p.DetectedNetwork.interface},
        {key:"router",label:"Текущий шлюз",children:p.DetectedNetwork.router},
        {key:"dns",label:"Текущий системный DNS",children:p.DetectedNetwork.system_dns || "—"},
      ]} />
      <Typography.Paragraph type="secondary">По умолчанию настройки получает DHCP. Системный DNS используется самой ВМ для загрузки подписок и обновлений.</Typography.Paragraph>
                {p.Runtime.Network === "pending" && (
                  <Alert
                    type="warning"
                    title="Подтвердите доступность панели в течение 120 секунд, иначе изменение сети будет отменено."
                  />
                )}
                {p.NetworkError && (
                  <Alert type="error" title={p.NetworkError} />
                )}
                <Form
                  form={networkForm}
                  key={JSON.stringify(p.Network)}
                  layout="vertical"
                  initialValues={{ ...p.Network, mode: p.Network.mode === "router" ? "static" : p.Network.mode || "dhcp", system_dns: p.Network.mode === "router" ? p.Network.system_dns || p.DetectedNetwork.system_dns : p.Network.system_dns }}
                  onFinish={(v) => void run("network-save-apply", v).catch(() => {})}
                >
                  <Form.Item name="mode" label="Получение сетевых настроек"><Select onChange={(mode) => {
                    if (mode === "dhcp") networkForm.setFieldsValue({address:p.DetectedNetwork.address,cidr:p.DetectedNetwork.cidr,router:p.DetectedNetwork.router,system_dns:""});
                    if (mode === "static" && !networkForm.getFieldValue("system_dns")) networkForm.setFieldValue("system_dns",p.DetectedNetwork.system_dns || "");
                  }} options={[{value:"dhcp",label:"Автоматически по DHCP"},{value:"static",label:"Статический IP"}]} /></Form.Item>
                  <Form.Item noStyle shouldUpdate={(a,b)=>a.mode!==b.mode}>{({getFieldValue}) => <div className="form-columns">
                    {[
                      { name: "interface", label: "Интерфейс LAN" },
                      { name: "address", label: "IPv4 ВМ" },
                      { name: "cidr", label: "Подсеть LAN (CIDR)" },
                      { name: "router", label: "Шлюз ВМ" },
                    ].map((f) => (
                      <Form.Item
                        key={f.name}
                        name={f.name}
                        label={f.label}
                        rules={required}
                      >
                        <Input disabled={getFieldValue("mode")==="dhcp" && f.name!=="interface"} />
                      </Form.Item>
                    ))}
                  </div>}</Form.Item>
                  <Form.Item name="system_dns" label="Системный DNS ВМ" extra="IPv4, каждый с новой строки. В режиме DHCP пустое поле означает DNS от роутера."><Input.TextArea rows={2} disabled={networkMode === "dhcp"} placeholder={networkMode === "dhcp" ? "Автоматически от DHCP" : ""} /></Form.Item>{networkMode === "dhcp" && <Alert type="info" title="IP, шлюз и системный DNS будут получены от DHCP после применения сети ВМ. Выше показаны текущие параметры." className="section-gap" />}<Button type="primary" htmlType="submit" disabled={p.Runtime.Network === "pending"}>Сохранить и применить сеть</Button>
                </Form>
                {p.Runtime.Network === "pending" && <Button type="primary" className="section-gap" onClick={() => doRun("network-confirm")}>Подтвердить доступность</Button>}

    </Card>
    <Card title="DNS Xray" className="section-gap">
      <Typography.Paragraph type="secondary">DNS для трафика через Xray. Эти настройки не меняют системный DNS ВМ.</Typography.Paragraph>
        <Form
          layout="vertical"
          initialValues={{
            dns: p.DNS,
            dns_mode: p.DNSMode,
            dns_direct_servers: p.DNSDirectServers,
            dns_vpn_servers: p.DNSVPNServers,
          }}
          onFinish={(v) => void run("gateway-settings", v).catch(() => {})}
        >
          <Form.Item name="dns_mode" label="Режим DNS">
            <Select
              options={[
                { value: "direct", label: "Напрямую, указанный DNS" },
                { value: "proxy", label: "DNS через выбранный VPN" },
                { value: "rules", label: "По правилам маршрутизации" },
              ]}
            />
          </Form.Item>
          <div className="form-columns">
            <Form.Item
              name="dns_direct_servers"
              label="Прямые DNS-серверы"
              extra="IPv4 или DoH, каждый с новой строки"
            >
              <Input.TextArea rows={4} />
            </Form.Item>
            <Form.Item
              name="dns_vpn_servers"
              label="DNS через VPN"
              extra="IPv4 или DoH, каждый с новой строки"
            >
              <Input.TextArea rows={4} />
            </Form.Item>
          </div>
          <Form.Item
            name="dns"
            label="DNS для запуска VPN и DoH"
            extra="По умолчанию используется системный DNS ВМ. Для переопределения укажите IPv4; очистите поле, чтобы вернуть автоматический выбор."
          >
            <Input placeholder="Автоматически: системный DNS ВМ" />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              Сохранить
            </Button>
          </Space>
        </Form>
    </Card>
  </div>;
}
export function Backup({ reload }: { reload: () => Promise<Page> }) {
  const { message, modal } = App.useApp();
  const [busy, setBusy] = useState(false);
  const restore = (file: File) => {
    modal.confirm({
      title: "Восстановить настройки из бекапа?",
      content:
        "Текущие подписки, подключения, группы и правила будут заменены. Работающий Xray изменится после применения конфигурации.",
      okText: "Восстановить",
      cancelText: "Отмена",
      onOk: async () => {
        setBusy(true);
        try {
          const body = new FormData();
          body.append("backup", file);
          const r = await fetch("/backup", {
            method: "POST",
            body,
            headers: { Accept: "application/json" },
          });
          if (!r.ok) throw new Error(await r.text());
          await reload();
          message.success("Бекап восстановлен. Примените конфигурацию.");
        } catch (e) {
          message.error(
            e instanceof Error ? e.message : "Ошибка восстановления",
          );
          throw e;
        } finally {
          setBusy(false);
        }
      },
    });
    return false;
  };
  return (
    <div className="management-cards">
      <Card title="Создать бекап">
        <Typography.Paragraph>
          Подписки, подключения, группы, маршруты и настройки шлюза.
        </Typography.Paragraph>
        <Button type="primary" icon={<DownloadOutlined />} href="/backup">
          Скачать бекап
        </Button>
        <Typography.Paragraph type="secondary" className="section-gap">
          Файл содержит ссылки и параметры доступа VPN. Храните его в надёжном
          месте.
        </Typography.Paragraph>
      </Card>
      <Card title="Восстановить бекап">
        <Typography.Paragraph>
          Поддерживаются бекапы предыдущих версий NGPanel.
        </Typography.Paragraph>
        <Upload
          accept=".json,application/json"
          showUploadList={false}
          beforeUpload={restore}
        >
          <Button loading={busy} icon={<UploadOutlined />}>
            Выбрать файл бекапа
          </Button>
        </Upload>
      </Card>
    </div>
  );
}

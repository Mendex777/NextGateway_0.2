import { useRef, useState } from "react";
import {
  App,
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Statistic,
  Steps,
  Switch,
  Table,
  Tag,
  Typography,
  Upload,
} from "antd";
import {
  DownloadOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { action, getJSON } from "./api";
import {
  date,
  Editor,
  JsonDetails,
  required,
  Target,
  type Run,
} from "./common";
import type { Device, Page } from "./types";
export function Overview({
  p,
  run,
  navigate,
}: {
  p: Page;
  run: Run;
  navigate: (tab: string) => void;
}) {
  const [wizard, setWizard] = useState(p.Wizard),
    [stage, setStage] = useState(-1),
    [progress, setProgress] = useState(""),
    [setupError, setSetupError] = useState("");
  const cancelled = useRef(false);
  const installComponents = async () => {
    cancelled.current = false;
    setSetupError("");
    try {
      for (const [i, name] of [
        "install",
        "dependencies",
        "geodata",
      ].entries()) {
        if (cancelled.current) break;
        setStage(i);
        const result = await action(name, "status");
        let done = false;
        const until = Date.now() + 15 * 60 * 1000;
        while (!done && Date.now() < until) {
          const status = await getJSON<{
            Done: boolean;
            Message: string;
            State: string;
          }>(
            "/operation-status?" +
              new URLSearchParams({ action: name, since: result.since }),
          );
          setProgress(status.Message);
          if (status.State === "error") throw new Error(status.Message);
          done = status.Done;
          if (!done) await new Promise((resolve) => setTimeout(resolve, 1500));
        }
        if (!done)
          throw new Error(
            "Операция выполняется дольше ожидаемого. Проверьте диагностику.",
          );
      }
      setStage(3);
    } catch (e) {
      setSetupError(e instanceof Error ? e.message : "Ошибка установки");
      setStage(-1);
    }
  };
  const doRun = (name: string) => void run(name).catch(() => {});
  return (
    <>
      <Card
        title={p.GatewayReady ? "Шлюз работает" : "Шлюз требует настройки"}
        extra={
          <Button onClick={() => setWizard(!wizard)}>Мастер настройки</Button>
        }
      >
        <Descriptions
          column={{ xs: 1, sm: 3 }}
          items={[
            {
              key: "gateway",
              label: "Шлюз и DNS устройств",
              children: p.DetectedNetwork.address,
            },
            {
              key: "router",
              label: "Роутер",
              children: p.DetectedNetwork.router,
            },
            {
              key: "default",
              label: "Маршрут по умолчанию",
              children:
                p.Mode === "direct"
                  ? "Провайдер"
                  : p.Mode === "proxy"
                    ? "Выбранный VPN"
                    : "Группа VPN",
            },
          ]}
        />
      </Card>
      {wizard && (
        <Card
          className="section-gap"
          title="Первичная настройка"
          extra={
            <Button
              onClick={() => {
                cancelled.current = true;
                setWizard(false);
                doRun("setup-skip");
              }}
            >
              Пропустить мастер
            </Button>
          }
        >
          <Steps
            current={stage < 0 ? 0 : stage}
            items={[
              { title: "Xray" },
              { title: "Компоненты шлюза" },
              { title: "Geo-базы" },
              { title: "Данные и запуск" },
            ]}
          />
          <Space className="wizard-actions" wrap>
            <Button
              type="primary"
              loading={stage >= 0 && stage < 3}
              onClick={() => void installComponents()}
            >
              Установить компоненты
            </Button>
            <Button onClick={() => navigate("backup")}>
              Восстановить бекап
            </Button>
            <Button onClick={() => navigate("subscriptions")}>
              Добавить подписку
            </Button>
          </Space>
          {progress && (
            <Alert
              className="section-gap"
              type={setupError ? "error" : "info"}
              title={setupError || progress}
            />
          )}
          <Typography.Paragraph className="section-gap">
            После загрузки подписок или бекапа выберите VPN и примените
            конфигурацию в верхнем блоке.
          </Typography.Paragraph>
        </Card>
      )}
      <div className="dashboard-cards">
        {[
          { title: "Подписки", value: p.SourceCount, tab: "subscriptions" },
          { title: "Подключения", value: p.NodeCount, tab: "subscriptions" },
          { title: "Правила", value: p.RuleCount, tab: "routing" },
          { title: "Устройства", value: p.DeviceCount, tab: "devices" },
        ].map((item) => (
          <Card key={item.title} hoverable onClick={() => navigate(item.tab)}>
            <Statistic title={item.title} value={item.value} />
          </Card>
        ))}
      </div>
      <Card title="Компоненты">
        <Table
          rowKey="Name"
          size="small"
          pagination={false}
          dataSource={p.Components || []}
          scroll={{ x: 650 }}
          columns={[
            { title: "Компонент", dataIndex: "Name" },
            {
              title: "Состояние",
              render: (_, c) => (
                <Space>
                  <Tag
                    color={
                      c.Class === "good"
                        ? "green"
                        : c.Class === "bad"
                          ? "red"
                          : c.Class === "warn"
                            ? "orange"
                            : undefined
                    }
                  >
                    {c.Class === "good"
                      ? "Готово"
                      : c.Class === "bad"
                        ? "Ошибка"
                        : c.Class === "warn"
                          ? "Требует настройки"
                          : "Сведения"}
                  </Tag>
                  {c.State}
                  {c.Update && (
                    <Button
                      size="small"
                      type="primary"
                      title={c.Hint}
                      onClick={() => doRun(c.Action)}
                    >
                      {c.Update}
                    </Button>
                  )}
                  {c.Name === "Панель" && p.PanelUpdate.Available && (
                    <Tag color="var(--ng-accent)">Доступно обновление</Tag>
                  )}
                </Space>
              ),
            },
            {
              title: "Действие",
              width: 200,
              render: (_, c) =>
                c.Link ? (
                  <Button
                    onClick={() =>
                      navigate(
                        new URLSearchParams(c.Link.split("?")[1]).get("tab") ||
                          "gateway",
                      )
                    }
                  >
                    {c.Button}
                  </Button>
                ) : c.Action === "apply" ||
                  (c.Class === "good" &&
                    ["start", "dependencies"].includes(c.Action)) ? null : (
                  <Button
                    title={c.Hint}
                    onClick={() =>
                      doRun(
                        c.Action === "install" && c.Class === "good"
                          ? "xray-update-check"
                          : c.Action,
                      )
                    }
                  >
                    {c.Action === "install" && c.Class === "good"
                      ? "Проверить обновления"
                      : c.Button}
                  </Button>
                ),
            },
          ]}
        />
      </Card>
      <Collapse
        className="section-gap"
        items={[
          {
            key: "manage",
            label: "Управление и обновления",
            children: (
              <Space direction="vertical" style={{ width: "100%" }}>
                <div className="status-actions">
                  <Button onClick={() => doRun("install")}>
                    Установить / обновить Xray
                  </Button>
                  <Button onClick={() => doRun("check")}>
                    Проверить конфигурацию
                  </Button>
                  <Button onClick={() => doRun("start")}>Запустить Xray</Button>
                  <Button onClick={() => doRun("stop")}>Остановить Xray</Button>
                  <Button onClick={() => doRun("rollback")}>
                    Откатить конфигурацию
                  </Button>
                </div>
                <Card title={"Панель " + p.PanelVersion}>
                  <Typography.Paragraph>
                    {p.PanelUpdate.Message}
                  </Typography.Paragraph>
                  <Space wrap>
                    <Button onClick={() => doRun("panel-update-check")}>
                      Проверить обновления
                    </Button>
                    <Button
                      disabled={!p.PanelUpdate.Available}
                      onClick={() => doRun("panel-update-install")}
                    >
                      Установить обновление
                    </Button>
                    <Button
                      disabled={!p.PanelUpdate.CanRollback}
                      onClick={() => doRun("panel-update-rollback")}
                    >
                      Откатить панель
                    </Button>
                  </Space>
                </Card>
              </Space>
            ),
          },
          {
            key: "vm",
            label: "Сведения о ВМ",
            children: (
              <>
                <Descriptions
                  items={[
                    {
                      key: "uptime",
                      label: "Время работы",
                      children: p.Uptime,
                    },
                  ]}
                />
                <JsonDetails value={p.Routes} />
                <JsonDetails value={p.Memory} />
                <JsonDetails value={p.Version} />
              </>
            ),
          },
        ]}
      />
    </>
  );
}
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
  return <>
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
  </>;
}
export function Diagnostics({ p, run }: { p: Page; run: Run }) {
  return (
    <Card
      title="Диагностика"
      extra={
        <Space wrap>
          <Button onClick={() => void run("logs").catch(() => {})}>
            Обновить журнал Xray
          </Button>
          <Button onClick={() => void run("check").catch(() => {})}>
            Проверить конфигурацию
          </Button>
        </Space>
      }
    >
      <Typography.Title level={5}>Последняя операция</Typography.Title>
      <Alert
        type={p.Runtime.State === "error" ? "error" : "info"}
        title={p.Runtime.Message || "Операций пока нет"}
      />
      <Typography.Title level={5}>Журнал Xray</Typography.Title>
      <JsonDetails value={p.Service || "Журнал ещё не загружен"} />
      <Typography.Title level={5}>Политики маршрутизации ВМ</Typography.Title>
      <JsonDetails value={p.Routes} />
    </Card>
  );
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

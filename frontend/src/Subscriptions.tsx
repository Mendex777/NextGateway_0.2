import { FlagText } from "./FlagText";
import { useEffect, useRef, useState } from "react";
import {
  App,
  Alert,
  Button,
  Card,
  Descriptions,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Radio,
  Tooltip,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Transfer,
  Typography,
} from "antd";
import {
  CloudDownloadOutlined,
  CheckOutlined,
  EditOutlined,
  MoreOutlined,
  PlusOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RightOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import "./subscriptions.css";
import { getJSON } from "./api";
import {
  date,
  Editor,
  JsonDetails,
  NodeLatency,
  required,
  type Run,
} from "./common";
import type {
  Group,
  GroupStatus,
  Node,
  Page,
  Probe,
  Source,
  Values,
} from "./types";
export default function Subscriptions({
  p,
  run,
}: {
  p: Page;
  run: Run;
  reload: (q?: Values) => Promise<Page>;
}) {
  const { modal } = App.useApp();
  const [source, setSource] = useState<Source | null | undefined>(),
    [group, setGroup] = useState<Group | null | undefined>(),
    [manual, setManual] = useState(false),
    [section, setSection] = useState("connections"),
    [pagination, setPagination] = useState<Record<string, { current: number; pageSize: number }>>({}),
    [detail, setDetail] = useState<Node | null>(null),
    [testMode, setTestMode] = useState("real"),
    [search, setSearch] = useState(""),
    [status, setStatus] = useState<Record<string, GroupStatus>>({}),
    [checks, setChecks] = useState<
      Record<
        string,
        { State: string; Message: string; Samples: GroupStatus["Samples"] }
      >
    >({}),
    [probes, setProbes] = useState<Record<string, Probe>>({}),
    [batch, setBatch] = useState<{
      State: string;
      Done: number;
      Total: number;
      OK: number;
    } | null>(null),
    [members, setMembers] = useState<string[]>([]),
    [opened, setOpened] = useState<string[]>(() => {
      try {
        return JSON.parse(localStorage.getItem("ngpanel-sources-open") || "[]");
      } catch {
        return [];
      }
    });
  const probeRequests = useRef<Record<string, string>>({});
  const batchRequest = useRef("");
  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      try {
        const [probe, ...groups] = await Promise.all([
          getJSON<{
            Nodes: Record<string, Probe>;
            Batch: { RunID?: string; State: string; Done: number; Total: number; OK: number };
          }>("/probe-status"),
          ...(p.Groups || []).map((g) =>
            Promise.all([
              getJSON<GroupStatus>(
                "/balance-status?id=" + encodeURIComponent(g.id),
              ),
              getJSON<{
                State: string;
                Message: string;
                Samples: GroupStatus["Samples"];
              }>("/group-check-status?id=" + encodeURIComponent(g.id)),
            ]),
          ),
        ]);
        if (cancelled) return;
        setProbes((previous) => {
          const next = { ...previous };
          for (const [id, request] of Object.entries(probeRequests.current)) {
            const result = probe.Nodes?.[id];
            if (result?.RunID === request) next[id] = result;
            else if (probe.Batch.RunID === request && probe.Batch.State !== "running" && next[id]?.State === "running") delete next[id];
          }
          return next;
        });
        if (batchRequest.current && probe.Batch.RunID === batchRequest.current) setBatch(probe.Batch);
        setStatus(
          Object.fromEntries(
            (p.Groups || []).map((g, i) => [g.id, groups[i][0]]),
          ),
        );
        setChecks(
          Object.fromEntries(
            (p.Groups || []).map((g, i) => [g.id, groups[i][1]]),
          ),
        );
      } catch {
        /* status retried on next tick */
      }
    };
    void poll();
    const timer = setInterval(poll, 800);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [p.Groups]);
  const doRun = (name: string, values: Values = {}) => {
    if (["node-probe", "source-probe", "probe-all", "group-check"].includes(name)) {
      const request = Date.now().toString(36) + Math.random().toString(36).slice(2);
      const nodes = (p.Nodes || []).filter(n => !n.Disabled && (name === "node-probe" ? String(n.ID) === String(values.id) : name === "source-probe" ? String(n.SourceID) === String(values.id) : name === "group-check" ? (p.Groups || []).find(g => g.id === String(values.group_id))?.nodes.includes(String(n.ID)) : true));
      for (const n of nodes) probeRequests.current[n.ID] = request;
      setProbes(previous => ({ ...previous, ...Object.fromEntries(nodes.map(n => [n.ID, { State: "running", Mode: testMode, RunID: request, Message: "Проверяется…", Checked: "", HTTPSMS: 0 }])) }));
      if (name === "source-probe" || name === "probe-all") {
        batchRequest.current = request;
        setBatch({ State: "running", Done: 0, Total: nodes.length, OK: 0 });
      }
      void run(name, { ...values, request_id: request }).catch(() => {
        for (const n of nodes) if (probeRequests.current[n.ID] === request) delete probeRequests.current[n.ID];
        setProbes(previous => Object.fromEntries(Object.entries(previous).filter(([, result]) => result.RunID !== request)));
        if (batchRequest.current === request) { batchRequest.current = ""; setBatch(null); }
      });
      return;
    }
    void run(name, values).catch(() => {});
  };
  const confirm = (title: string, name: string, values: Values) =>
    modal.confirm({
      title,
      content: (() => {
        const ids = (p.Nodes || []).filter(n => name === "delete" ? String(n.SourceID) === String(values.id) : String(n.ID) === String(values.id)).map(n => String(n.ID));
        const rules = (p.Rules || []).filter(r => ids.includes(r.Target.replace(/^node:/, "")) || (r.Target === "proxy" && ids.includes(p.Selected)));
        const groups = (p.Groups || []).filter(g => g.nodes.some(id => ids.includes(id)));
        return <Space direction="vertical"><span>Правила и группы сохранятся. Для недоступных выходов действует настройка «Если VPN недоступен».</span>{ids.includes(p.Selected) && <span>Выбор VPN будет снят.</span>}{rules.length > 0 && <span>Правила: {rules.map(r => r.Name).join(", ")}</span>}{groups.length > 0 && <span>Группы: {groups.map(g => g.name).join(", ")}</span>}</Space>;
      })(),
      okText: "Удалить",
      cancelText: "Отмена",
      okButtonProps: { danger: true },
      onOk: () => run(name, values),
    });
  const filtered = (nodes: Node[], scope = "") =>
    nodes
      .filter((n) =>
        (scope + " " + n.Name + " " + n.Host + " " + n.Protocol)
          .toLocaleLowerCase()
          .includes(search.toLocaleLowerCase()),
      )
      .map((n) => ({ ...n, Probe: probes[n.ID] }));
  const table = (nodes: Node[], g?: Group, scope = "") => (
    <Table
      className="node-table outbound-nodes"
      rowKey="ID"
      size="small"
      pagination={{
        current: pagination[scope]?.current || 1,
        pageSize: pagination[scope]?.pageSize || 30,
        pageSizeOptions: [10, 30, 50, 100],
        onChange: (current, pageSize) => setPagination(previous => ({ ...previous, [scope]: { current, pageSize } })),
        showSizeChanger: true,
        hideOnSinglePage: true,
      }}
      scroll={{ x: 1150 }}
      dataSource={filtered(nodes, scope)}
      columns={[
        {
          title: "#",
          width: 100,
          render: (_, n) => (
            <div className="outbound-actions">
              <span className="outbound-index">{nodes.findIndex((v) => v.ID === n.ID) + 1}</span>
              <TooltipButton title={"Параметры «" + n.Name + "»"} icon={<EditOutlined />} onClick={() => setDetail(n)} />
              <Dropdown trigger={["click"]} menu={{
                items: [
                  { key: "select", label: g ? "Выбрать в группе" : "Выбрать VPN", disabled: n.Disabled || !!n.Compatibility },
                  ...(!g ? [{ key: "delete", label: "Удалить", danger: true }] : []),
                ],
                onClick: ({ key }) => key === "select"
                  ? doRun(g ? "group-select" : "select-node", g ? { group_id: g.id, node_id: n.ID } : { id: n.ID })
                  : confirm("Удалить подключение «" + n.Name + "»?", "node-delete", { id: n.ID }),
              }}>
                <Button size="small" shape="circle" aria-label={"Действия " + n.Name} icon={<MoreOutlined />} />
              </Dropdown>
            </div>
          ),
        },
        {
          title: "Название",
          width: 240,
          render: (_, n) => (
            <div className="outbound-title">
              <Typography.Text ellipsis={{ tooltip: n.Name }} className="outbound-name"><FlagText text={n.Name} /></Typography.Text>
              {((!g && p.Selected === String(n.ID)) || (g && status[g.id]?.Tag?.startsWith("auto-vpn-" + g.id + "-" + n.ID + "-"))) && <Tag color="var(--ng-accent)">Выбран</Tag>}
            </div>
          ),
        },
        {
          title: "Подключение",
          width: 180,
          render: (_, n) => (
            <div className="outbound-tags">
              <Tag color="green">{n.Protocol}</Tag>
              {n.Transport && <Tag>{n.Transport}</Tag>}
              {n.Security && n.Security !== "none" && <Tag color="purple">{n.Security}</Tag>}
              {n.Compatibility && <Tooltip title={n.Compatibility}><Tag color="orange">Не поддерживается</Tag></Tooltip>}
            </div>
          ),
        },
        {
          title: "Адрес",
          width: 200,
          render: (_, n) => <Typography.Text className="outbound-address" copyable={{ text: n.Host + ":" + n.Port }}>{n.Host}:{n.Port}</Typography.Text>,
        },
        {
          title: "Выход",
          width: 155,
          render: (_, n) => n.Probe?.ExitIP ? <Typography.Text copyable={{ text: n.Probe.ExitIP }}>{n.Probe.ExitIP}</Typography.Text> : "—",
        },
        {
          title: "Страна",
          width: 130,
          render: (_, n) => n.Probe?.Country ? <Tag><FlagText text={Array.from(n.Probe.Country).map(c => String.fromCodePoint(127397 + c.charCodeAt(0))).join("") + " " + new Intl.DisplayNames(["ru"], { type: "region" }).of(n.Probe.Country)} /></Tag> : "—",
        },
        {
          title: "Задержка",
          width: 110,
          render: (_, n) => <NodeLatency node={n} />,
        },
        {
          title: "Проверить",
          width: 100,
          render: (_, n) => (
            <Space size={4}>
              <TooltipButton
                title="Выбрать"
                icon={<CheckOutlined />}
                disabled={n.Disabled || !!n.Compatibility}
                onClick={() =>
                  doRun(
                    g ? "group-select" : "select-node",
                    g ? { group_id: g.id, node_id: n.ID } : { id: n.ID },
                  )
                }
              />
              <TooltipButton
                title="Проверить"
                icon={<ThunderboltOutlined />}
                loading={n.Probe?.State === "running"}
                disabled={n.Disabled}
                onClick={() => doRun("node-probe", { id: n.ID, mode: testMode })}
              />

            </Space>
          ),
        },
      ]}
    />
  );
  const editGroup = (g: Group | null) => {
    setMembers((g?.nodes || []).filter(id => (p.Nodes || []).some(n => String(n.ID) === id)));
    setGroup(g);
  };
  const groupItems = (p.Groups || []).map((g) => ({
    key: "group:" + g.id,
    name: g.name, kind: "Группа", count: (p.Nodes || []).filter(n => !n.Disabled && g.nodes.includes(String(n.ID))).length,
    disabled: false, toggle: undefined,
    state: status[g.id]?.Name || "Ожидание Xray",
    mode: g.mode === "fastest" ? "Самый быстрый" : g.mode === "failover" ? "Только при отказе" : "Порог: " + g.threshold_ms + " мс",
    latency: status[g.id]?.Samples?.find(sample => status[g.id]?.Tag?.startsWith("auto-vpn-" + g.id + "-" + sample.NodeID + "-")),
    nodes: (p.Nodes || []).filter((n) => !n.Disabled && g.nodes.includes(String(n.ID))),
    edit: () => editGroup(g),
    label: (
      <Space>
        <Typography.Text strong><FlagText text={g.name} /></Typography.Text>
        <Tag>Группа</Tag>
        <Typography.Text type="secondary">
          {g.nodes.length} подключений
        </Typography.Text>
      </Space>
    ),
    extra: (
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "edit", label: "Изменить" },
            { key: "check", label: "Проверить группу" },
            { key: "delete", label: "Удалить", danger: true },
          ],
          onClick: ({ key, domEvent }) => {
            domEvent.stopPropagation();
            if (key === "edit") editGroup(g);
            else if (key === "check") doRun("group-check", { group_id: g.id });
            else
              confirm("Удалить группу «" + g.name + "»?", "balance-delete", {
                group_id: g.id,
              });
          },
        }}
      >
        <Button
          type="text"
          icon={<MoreOutlined />}
          onClick={(e) => e.stopPropagation()}
        />
      </Dropdown>
    ),
    content: (
      <>
        <Alert
          className="group-current"
          showIcon
          type="info"
          title={<FlagText text={"Активный узел: " + (status[g.id]?.Name || "Ожидание Xray")} />}
          description={<FlagText text={status[g.id]?.Message || status[g.id]?.Policy || "После применения конфигурации появятся результаты проверок."} />}
        />
        {checks[g.id]?.State && (
          <Alert
            className="section-gap"
            type={checks[g.id].State === "error" ? "error" : "info"}
            title={checks[g.id].Message}
          />
        )}{" "}
        {status[g.id]?.Policy && (
          <Typography.Paragraph type="secondary">
            {status[g.id].Policy}
          </Typography.Paragraph>
        )}
        {table(
          (p.Nodes || []).filter((n) => !n.Disabled && g.nodes.includes(String(n.ID))),
          g, g.name,
        )}
      </>
    ),
  }));
  const sourceItems = (p.Sources || []).map((s) => ({
    key: "source:" + s.ID,
    name: s.Name, kind: s.URL === "manual:" ? "Вручную" : "Подписка", count: s.Count,
    disabled: s.Disabled, toggle: s.URL === "manual:" ? undefined : (enabled: boolean) => doRun("source-toggle", { id: s.ID, enabled: enabled ? "1" : "0" }),
    state: s.Error || (s.Updated ? date(s.Updated) : "Не обновлена"),
    mode: "", latency: undefined,
    nodes: s.Nodes || [],
    edit: s.URL === "manual:" ? undefined : () => setSource(s),
    label: (
      <Space>
        <Typography.Text strong><FlagText text={s.Name} /></Typography.Text>
        <Tag>{s.Count} подключений</Tag>
      </Space>
    ),
    extra: (
      <Space size={0}>
        {s.URL !== "manual:" && (
          <Button
            type="text"
            aria-label={"Обновить " + s.Name}
            icon={<ReloadOutlined />}
            onClick={(e) => {
              e.stopPropagation();
              doRun("refresh", { id: s.ID });
            }}
          />
        )}
        <Dropdown
          trigger={["click"]}
          menu={{
            items: [
              ...(s.URL === "manual:"
                ? []
                : [{ key: "edit", label: "Изменить" }]),
              { key: "probe", label: "Проверить подключения", disabled: s.Disabled },
              { key: "delete", label: "Удалить", danger: true },
            ],
            onClick: ({ key, domEvent }) => {
              domEvent.stopPropagation();
              if (key === "edit") setSource(s);
              else if (key === "probe") doRun("source-probe", { id: s.ID, mode: testMode });
              else
                confirm("Удалить подписку «" + s.Name + "»?", "delete", {
                  id: s.ID,
                });
            },
          }}
        >
          <Button
            type="text"
            aria-label={"Действия подписки " + s.Name}
            icon={<MoreOutlined />}
            onClick={(e) => e.stopPropagation()}
          />
        </Dropdown>
      </Space>
    ),
    content: (
      <>
        {s.Disabled && <Alert type="warning" className="section-gap" title="Подписка отключена. Её подключения исключены из VPN, маршрутов, групп и проверок после применения конфигурации." />}
        <div className="source-meta">
          <Typography.Text type="secondary">
            Обновлена: {date(s.Updated)}
          </Typography.Text>
          {s.Usage && (
            <Tag>
              {s.Usage} / {s.Limit || "∞"}
            </Tag>
          )}
          {s.Expires && <Tag>Истекает: {s.Expires}</Tag>}
          {s.Interval > 0 && <Tag>Автообновление: {s.Interval} мин</Tag>}
        </div>
        {s.Error && (
          <Alert type="error" title={s.Error} className="section-gap" />
        )}
        {s.ProviderMessage && (
          <Typography.Paragraph>{s.ProviderMessage}</Typography.Paragraph>
        )}
        {table(s.Nodes || [], undefined, s.Name)}
      </>
    ),
  }));
  return (
    <>
      <Card className="subscriptions-card">
        <Tabs activeKey={section} onChange={(key) => { setSection(key); setSearch(""); }} items={[
          { key: "connections", label: "Подключения" },
          { key: "groups", label: "Группы" },
        ]} />
        <div className="source-toolbar">
          <Space wrap>
            {section === "connections" ? <>
            <Button
              type="primary"
              icon={<CloudDownloadOutlined />}
              onClick={() => setSource(null)}
            >
              Подписка
            </Button>
            <Button onClick={() => setManual(true)}>Добавить ссылку</Button>
            </> : <Button type="primary" icon={<PlusOutlined />} onClick={() => editGroup(null)}>Добавить группу</Button>}
          </Space>


        {section === "connections" && <div className="connection-test-toolbar">
          <Radio.Group size="small" optionType="button" buttonStyle="solid" value={testMode} onChange={(e) => setTestMode(e.target.value)}>
            <Tooltip title="TCP-соединение с сервером. Для UDP-подключений используется HTTP-проверка через VPN"><Radio.Button value="tcp">TCP</Radio.Button></Tooltip>
            <Tooltip title="HTTP-запрос через VPN по уже установленному соединению"><Radio.Button value="http">HTTP</Radio.Button></Tooltip>
            <Tooltip title="HTTPS через VPN, включая установление соединения и TLS"><Radio.Button value="real">Реальная задержка</Radio.Button></Tooltip>
          </Radio.Group>
          <Button type="primary" size="small" icon={<PlayCircleOutlined />} loading={batch?.State === "running"} disabled={batch?.State === "running" || !(p.Nodes || []).some(n => !n.Disabled)} onClick={() => doRun("probe-all", { mode: testMode })}>Тестировать все</Button>
        </div>}
        </div>
        <div className="source-search">
          <Input.Search
            allowClear
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={section === "groups" ? "Поиск групп и участников" : "Поиск подключений"}
          />
        </div>
        {section === "connections" && p.SelectedNode && (
          <Alert
            showIcon
            type="success"
            title={<FlagText text={"Выбранный VPN: " + p.SelectedNode.Name} />}
          />
        )}{" "}
        {section === "connections" && <Space wrap className="section-gap">
          <Typography.Text>Если VPN недоступен</Typography.Text>
          <Select aria-label="Если VPN недоступен" value={p.VPNUnavailable || "block"} style={{minWidth:240}} options={[{value:"block",label:"Блокировать трафик"},{value:"direct",label:"Напрямую через провайдера"}]} onChange={value => value === "direct" ? modal.confirm({title:"Разрешить трафик напрямую при недоступности VPN?",content:"Это относится к выбранному VPN, отдельным узлам и группам без доступных участников. Такой трафик пойдёт через провайдера без VPN после применения конфигурации.",okText:"Разрешить",cancelText:"Отмена",onOk:()=>run("vpn-unavailable",{value})}) : doRun("vpn-unavailable",{value})} />
          {p.Selected && <Button onClick={() => modal.confirm({title:"Снять выбор VPN?",content:"Правила сохранятся. Для них будет действовать выбранное поведение при недоступности VPN после применения конфигурации.",okText:"Снять выбор",cancelText:"Отмена",onOk:()=>run("clear-vpn")})}>Снять выбор VPN</Button>}
        </Space>}
        {p.VPNWarning && <Alert showIcon type="warning" className="section-gap" title={p.VPNWarning} />}
        {batch?.State && (
          <Alert
            className="section-gap"
            type={batch.State === "running" ? "info" : batch.State === "cancelled" ? "warning" : "success"}
            title={
              (batch.State === "running" ? "Проверка" : batch.State === "cancelled" ? "Проверка отменена" : "Проверка завершена") +
              ": " +
              batch.Done +
              " / " +
              batch.Total +
              ", доступны " +
              batch.OK
            }
            action={
              batch.State === "running" ? (
                <Button size="small" onClick={() => doRun("probe-cancel")}>
                  Отменить
                </Button>
              ) : undefined
            }
          />
        )}
        <Table
          className="subscriptions-table section-gap"
          size="small"
          rowKey="key"
          rowClassName={item => item.disabled ? "source-disabled" : ""}
          pagination={false}
          scroll={{ x: 820 }}
          dataSource={(section === "groups" ? groupItems : sourceItems).filter((item) => item.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()) || filtered(item.nodes).length > 0)}
          locale={{ emptyText: section === "groups" ? "Групп пока нет. Добавьте группу и выберите её участников." : "Подключений нет. Добавьте подписку или ссылку подключения." }}
          expandable={{
            expandIcon: ({ expanded, onExpand, record }) => <Button type="text" size="small" className="source-expand" aria-label={expanded ? "Свернуть строку" : "Развернуть строку"} aria-expanded={expanded} icon={<RightOutlined rotate={expanded ? 90 : 0} />} onClick={e => onExpand(record, e)} />,
            expandedRowKeys: opened,
            expandedRowRender: (item) => <div className="subscription-expanded">{item.content}</div>,
            onExpandedRowsChange: (keys) => {
              const prefix = section === "groups" ? "group:" : "source:";
              const v = [...new Set([...opened.filter(key => !key.startsWith(prefix)), ...keys.map(String)])];
              setOpened(v);
              localStorage.setItem("ngpanel-sources-open", JSON.stringify(v));
            },
          }}
          columns={[
            { title: "#", width: 90, render: (_, item, index) => <div className="outbound-actions"><span className="outbound-index">{index + 1}</span>{item.edit && <TooltipButton title={"Изменить «" + item.name + "»"} icon={<EditOutlined />} onClick={item.edit} />}</div> },
            ...(section === "connections" ? [{ title: "Включено", width: 85, render: (_: unknown, item: (typeof groupItems)[number] | (typeof sourceItems)[number]) => item.toggle ? <Switch size="small" checked={!item.disabled} aria-label={"Подписка " + item.name} onChange={item.toggle} /> : "—" }] : []),
            { title: "Название", render: (_, item) => <Typography.Text strong ellipsis={{ tooltip: item.name }}><FlagText text={item.name} /></Typography.Text> },
            ...(section === "connections" ? [{ title: "Тип", width: 110, render: (_: unknown, item: (typeof groupItems)[number] | (typeof sourceItems)[number]) => <Tag>{item.kind}</Tag> }] : []),
            { title: "Подключения", width: 115, dataIndex: "count" },
            { title: section === "groups" ? "Активный узел" : "Обновление", width: 245, render: (_, item) => <Typography.Text type="secondary" ellipsis={{ tooltip: item.state }}><FlagText text={item.state} /></Typography.Text> },
            ...(section === "groups" ? [
              { title: "Задержка", width: 100, render: (_: unknown, item: (typeof groupItems)[number] | (typeof sourceItems)[number]) => item.latency ? <Tag color={item.latency.Alive ? "green" : "red"}>{item.latency.Alive ? item.latency.DelayMS + " мс" : "Недоступен"}</Tag> : "—" },
              { title: "Переключение", width: 170, dataIndex: "mode" },
            ] : []),
            { title: "Действия", width: 95, render: (_, item) => item.extra },
          ]}
        />
      </Card>
      <Editor
        key={source?.ID ?? "source-new"}
        title={source ? "Изменить подписку" : "Добавить подписку"}
        open={source !== undefined}
        onClose={() => setSource(undefined)}
        initial={
          source
            ? {
                id: source.ID,
                name: source.Name,
                url: source.URL,
                headers: source.Headers,
                interval: source.Interval,
              }
            : { headers: "{}", interval: 0 }
        }
        onSave={(v) => run(source ? "source-update" : "source", v)}
      >
        <Form.Item name="name" label="Название" rules={required}>
          <Input />
        </Form.Item>
        <Form.Item
          name="url"
          label="Ссылка подписки"
          rules={[
            ...required,
            { type: "url", message: "Укажите HTTP(S)-ссылку" },
          ]}
        >
          <Input />
        </Form.Item>
        <Form.Item
          name="headers"
          label="HTTP-заголовки (JSON)"
          rules={[
            {
              validator: (_, v) => {
                try {
                  const j = JSON.parse(v);
                  if (!j || Array.isArray(j) || typeof j !== "object")
                    throw Error();
                  return Promise.resolve();
                } catch {
                  return Promise.reject(new Error("Нужен JSON-объект"));
                }
              },
            },
          ]}
        >
          <Input.TextArea rows={4} />
        </Form.Item>
        <Form.Item name="interval" label="Автообновление, минут (0 — вручную)">
          <InputNumber min={0} max={10080} />
        </Form.Item>
      </Editor>
      <Editor
        key={group?.id ?? "group-new"}
        title={group ? "Изменить группу" : "Создать группу"}
        open={group !== undefined}
        onClose={() => setGroup(undefined)}
        initial={
          group
            ? {
                group_id: group.id,
                name: group.name,
                interval: group.interval,
                policy: group.mode,
                threshold_ms: group.threshold_ms,
                failures: group.failures,
                cooldown: group.cooldown,
              }
            : {
                interval: 30,
                policy: "threshold",
                threshold_ms: 1000,
                failures: 2,
                cooldown: 60,
              }
        }
        onSave={(v) => {
          if (members.length < 2)
            throw new Error("Выберите минимум два подключения");
          return run("balance-settings", { ...v, balance_node: members });
        }}
      >
        <Form.Item name="name" label="Название" rules={required}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item name="policy" label="Условия переключения">
          <Select
            options={[
              { value: "fastest", label: "Самый быстрый" },
              { value: "threshold", label: "При превышении порога" },
              { value: "failover", label: "Только при отказе" },
            ]}
          />
        </Form.Item>
        <div className="form-columns">
          <Form.Item name="interval" label="Проверять каждые, секунд">
            <InputNumber min={10} max={600} />
          </Form.Item>
          <Form.Item name="threshold_ms" label="Порог задержки, мс">
            <InputNumber min={50} max={60000} />
          </Form.Item>
          <Form.Item name="failures" label="Превышений подряд">
            <InputNumber min={1} max={10} />
          </Form.Item>
          <Form.Item name="cooldown" label="Пауза между переключениями, с">
            <InputNumber min={0} max={3600} />
          </Form.Item>
        </div>
        <Form.Item
          label={"Подключения группы: " + members.length}
          extra="От 2 до 8 подключений. Поиск сохраняет выбор."
        >
          <Transfer
            className="group-picker"
            showSearch
            titles={["Доступные", "В группе"]}
            dataSource={(p.Nodes || []).filter(n => !n.Disabled).map((n) => ({
              key: String(n.ID),
              title: n.Name,
              description: n.Host,
              disabled: !!n.Compatibility,
            }))}
            targetKeys={members}
            onChange={(keys) => setMembers(keys.map(String))}
            render={(item) => item.title}
            filterOption={(q, item) =>
              (item.title + " " + item.description)
                .toLocaleLowerCase()
                .includes(q.toLocaleLowerCase())
            }
          />
        </Form.Item>
      </Editor>
      <Editor
        title="Добавить подключение"
        open={manual}
        onClose={() => setManual(false)}
        initial={{}}
        onSave={(v) => run("manual-node", v)}
      >
        <Form.Item name="uri" label="Ссылка подключения" rules={required}>
          <Input.TextArea rows={5} />
        </Form.Item>
      </Editor>
      <CardDetail node={detail} close={() => setDetail(null)} />
    </>
  );
}
import { Modal } from "antd";
function TooltipButton(props: {
  title: string;
  icon: React.ReactNode;
  disabled?: boolean;
  loading?: boolean;
  onClick: () => void;
}) {
  return (
    <Tooltip title={props.title}>
      <Button
        size="small"
        shape="circle"
        aria-label={props.title}
        icon={props.icon}
        disabled={props.disabled}
        loading={props.loading}
        onClick={props.onClick}
      />
    </Tooltip>
  );
}
function CardDetail({ node, close }: { node: Node | null; close: () => void }) {
  return (
    <Modal
      title={<FlagText text={node?.Name} />}
      open={!!node}
      onCancel={close}
      footer={<Button onClick={close}>Закрыть</Button>}
    >
      <Descriptions
        bordered
        size="small"
        column={1}
        items={
          node
            ? [
                "Host",
                "Port",
                "Protocol",
                "Transport",
                "Security",
                "Encryption",
                "SNI",
                "Flow",
                "Path",
                "Compatibility",
              ].map((key) => ({
                key,
                label: key,
                children: String(node[key as keyof Node] || "—"),
              }))
            : []
        }
      />
    </Modal>
  );
}

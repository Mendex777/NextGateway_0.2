import { useEffect, useState } from "react";
import {
  App,
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Transfer,
  Typography,
} from "antd";
import {
  CheckOutlined,
  EditOutlined,
  MoreOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
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
    [detail, setDetail] = useState<Node | null>(null),
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
  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      try {
        const [probe, ...groups] = await Promise.all([
          getJSON<{
            Nodes: Record<string, Probe>;
            Batch: { State: string; Done: number; Total: number; OK: number };
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
        setProbes(probe.Nodes || {});
        setBatch(probe.Batch);
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
    const timer = setInterval(poll, 3000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [p.Groups]);
  const doRun = (name: string, values: Values = {}) =>
    void run(name, values).catch(() => {});
  const confirm = (title: string, name: string, values: Values) =>
    modal.confirm({
      title,
      okText: "Удалить",
      cancelText: "Отмена",
      okButtonProps: { danger: true },
      onOk: () => run(name, values),
    });
  const filtered = (nodes: Node[]) =>
    nodes
      .filter((n) =>
        (n.Name + " " + n.Host + " " + n.Protocol)
          .toLocaleLowerCase()
          .includes(search.toLocaleLowerCase()),
      )
      .map((n) => ({ ...n, Probe: probes[n.ID] || n.Probe }));
  const table = (nodes: Node[], g?: Group) => (
    <Table
      className="node-table"
      rowKey="ID"
      size="small"
      pagination={{
        pageSize: 30,
        showSizeChanger: true,
        hideOnSinglePage: true,
      }}
      scroll={{ x: 650 }}
      dataSource={filtered(nodes).map((n) => {
        const sample = g
          ? (checks[g.id]?.Samples?.length
              ? checks[g.id].Samples
              : status[g.id]?.Samples
            )?.find((v) => v.NodeID === String(n.ID))
          : undefined;
        return sample
          ? {
              ...n,
              Probe: {
                State: sample.Alive ? "ok" : "error",
                HTTPSMS: sample.DelayMS,
                Message: "Проверка группы Xray",
                Checked: String(sample.Checked),
              },
            }
          : n;
      })}
      columns={[
        {
          title: "Подключение",
          render: (_, n) => (
            <>
              <Typography.Text>{n.Name}</Typography.Text>
              {(!g && p.Selected === String(n.ID)) ||
              (g &&
                status[g.id]?.Tag?.startsWith(
                  "auto-vpn-" + g.id + "-" + n.ID + "-",
                )) ? (
                <Tag color="var(--ng-accent)" style={{ marginLeft: 8 }}>
                  Выбран
                </Tag>
              ) : null}
              <div className="node-info">
                {n.Protocol} / {n.Transport} / {n.Security} · {n.Host}:{n.Port}
              </div>
              {n.Compatibility && (
                <Typography.Text type="warning">
                  {n.Compatibility}
                </Typography.Text>
              )}
            </>
          ),
        },
        {
          title: "Задержка",
          width: 110,
          render: (_, n) => <NodeLatency node={n} />,
        },
        {
          title: "Действия",
          width: 150,
          render: (_, n) => (
            <Space size={4}>
              <TooltipButton
                title="Выбрать"
                icon={<CheckOutlined />}
                disabled={!!n.Compatibility}
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
                onClick={() => doRun("node-probe", { id: n.ID })}
              />
              <Dropdown
                trigger={["click"]}
                menu={{
                  items: [
                    { key: "details", label: "Параметры подключения" },
                    ...(!g
                      ? [{ key: "delete", label: "Удалить", danger: true }]
                      : []),
                  ],
                  onClick: ({ key }) =>
                    key === "details"
                      ? setDetail(n)
                      : confirm(
                          "Удалить подключение «" + n.Name + "»?",
                          "node-delete",
                          { id: n.ID },
                        ),
                }}
              >
                <Button
                  size="small"
                  aria-label={"Действия " + n.Name}
                  icon={<MoreOutlined />}
                />
              </Dropdown>
            </Space>
          ),
        },
      ]}
    />
  );
  const editGroup = (g: Group | null) => {
    setMembers(g?.nodes || []);
    setGroup(g);
  };
  const groupItems = (p.Groups || []).map((g) => ({
    key: "group:" + g.id,
    label: (
      <Space>
        <Typography.Text strong>{g.name}</Typography.Text>
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
    children: (
      <>
        <Alert
          className="group-current"
          showIcon
          type="info"
          title={"Активный узел: " + (status[g.id]?.Name || "Ожидание Xray")}
          description={
            status[g.id]?.Message ||
            status[g.id]?.Policy ||
            "После применения конфигурации появятся результаты проверок."
          }
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
          (p.Nodes || []).filter((n) => g.nodes.includes(String(n.ID))),
          g,
        )}
      </>
    ),
  }));
  const sourceItems = (p.Sources || []).map((s) => ({
    key: "source:" + s.ID,
    label: (
      <Space>
        <Typography.Text strong>{s.Name}</Typography.Text>
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
              { key: "probe", label: "Проверить подключения" },
              { key: "delete", label: "Удалить", danger: true },
            ],
            onClick: ({ key, domEvent }) => {
              domEvent.stopPropagation();
              if (key === "edit") setSource(s);
              else if (key === "probe") doRun("source-probe", { id: s.ID });
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
    children: (
      <>
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
        {table(s.Nodes || [])}
      </>
    ),
  }));
  return (
    <>
      <Card>
        <div className="source-toolbar">
          <Input.Search
            allowClear
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Поиск подключений"
          />
          <Space wrap>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setSource(null)}
            >
              Подписка
            </Button>
            <Button icon={<PlusOutlined />} onClick={() => editGroup(null)}>
              Группа
            </Button>
            <Button onClick={() => setManual(true)}>Добавить ссылку</Button>
          </Space>
        </div>
        {p.SelectedNode && (
          <Alert
            showIcon
            type="success"
            title={"Выбранный VPN: " + p.SelectedNode.Name}
          />
        )}{" "}
        {batch?.State && (
          <Alert
            className="section-gap"
            type={batch.State === "running" ? "info" : "success"}
            title={
              (batch.State === "running" ? "Проверка" : "Проверка завершена") +
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
        <Collapse
          className="section-gap"
          activeKey={opened}
          onChange={(keys) => {
            const v = Array.isArray(keys) ? keys : [keys];
            setOpened(v);
            localStorage.setItem("ngpanel-sources-open", JSON.stringify(v));
          }}
          items={[...groupItems, ...sourceItems]}
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
            dataSource={(p.Nodes || []).map((n) => ({
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
import { Modal, Tooltip } from "antd";
function TooltipButton(props: {
  title: string;
  icon: React.ReactNode;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Tooltip title={props.title}>
      <Button
        size="small"
        aria-label={props.title}
        icon={props.icon}
        disabled={props.disabled}
        onClick={props.onClick}
      />
    </Tooltip>
  );
}
function CardDetail({ node, close }: { node: Node | null; close: () => void }) {
  return (
    <Modal
      title={node?.Name}
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

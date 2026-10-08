import { FlagText } from "./FlagText";
import { createContext, useContext, useEffect, useState } from "react";
import type { HTMLAttributes } from "react";
import {
  App,
  Alert,
  Button,
  Card,
  Descriptions,
  Dropdown,
  Form,
  Input,
  Modal,
  Popover,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Tooltip,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  AimOutlined,
  ControlOutlined,
  EditOutlined,
  HolderOutlined,
  MoreOutlined,
  PlusOutlined,
  SwapOutlined,
} from "@ant-design/icons";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { Page, RouteResult, Rule, Values } from "./types";
import { getPage } from "./api";
import { Editor, JsonDetails, required, Target, type Run } from "./common";
const DragContext = createContext<ReturnType<typeof useSortable> | null>(null);
function DragRow(
  props: HTMLAttributes<HTMLTableRowElement> & { "data-row-key": string },
) {
  const sortable = useSortable({ id: Number(props["data-row-key"]) });
  return (
    <DragContext.Provider value={sortable}>
      <tr
        {...props}
        ref={sortable.setNodeRef}
        style={{
          ...props.style,
          transform: CSS.Transform.toString(sortable.transform),
          transition: sortable.transition,
          opacity: sortable.isDragging ? 0.5 : 1,
          position: sortable.isDragging ? "relative" : undefined,
          zIndex: sortable.isDragging ? 2 : undefined,
        }}
      />
    </DragContext.Provider>
  );
}
function Grip() {
  const drag = useContext(DragContext);
  return (
    <button
      className="drag-grip"
      aria-label="Перетащить правило"
      ref={drag?.setActivatorNodeRef}
      {...drag?.attributes}
      {...drag?.listeners}
    >
      <HolderOutlined />
    </button>
  );
}
function Destination({ rule }: { rule: Rule }) {
  const values = rule.Value.split("\n").filter(Boolean);
  return (
    <div className="rule-destination">
      <span className="field-prefix">
        {rule.Kind === "device" ? "" : rule.Kind}
      </span>
      {values[0] || "—"}
      {values.length > 1 && (
        <Popover
          trigger="click"
          title={values.length + " записей"}
          content={
            <div className="destination-values">
              <JsonDetails value={rule.Value} />
            </div>
          }
        >
          <Tag bordered={false} style={{ marginLeft: 5, cursor: "pointer" }}>
            +{values.length - 1}
          </Tag>
        </Popover>
      )}
    </div>
  );
}
export default function Routing({
  p,
  run,
  reload,
}: {
  p: Page;
  run: Run;
  reload: (query?: Values) => Promise<Page>;
}) {
  const { modal, message } = App.useApp();
  const [geoTarget, setGeoTarget] = useState("proxy"),
    [geoFilter, setGeoFilter] = useState("");
  const [tab, setTab] = useState("rules"),
    [rules, setRules] = useState(p.Rules || []),
    [editing, setEditing] = useState<Rule | null | undefined>(),
    [result, setResult] = useState<RouteResult | null>(null),
    [geo, setGeo] = useState<Page | null>(null),
    [query, setQuery] = useState(""),
    [searching, setSearching] = useState(false);
  useEffect(() => setRules(p.Rules || []), [p.Rules]);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
  const remove = (r: Rule) =>
    modal.confirm({
      title: "Удалить правило «" + r.Name + "»?",
      okText: "Удалить",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: () => run("rule-delete", { id: r.ID }),
    });
  const columns: ColumnsType<Rule> = [
    {
      title: "#",
      width: 48,
      render: (_, r, i) => (
        <span className="row-index">
          <Grip />
          {i + 1}
        </span>
      ),
    },
    {
      title: "Действия",
      width: 80,
      render: (_, r) => (
        <Space size={4}>
          <Tooltip title="Изменить">
            <Button
              aria-label={"Изменить правило " + r.Name}
              shape="circle"
              size="small"
              icon={<EditOutlined />}
              onClick={() => setEditing(r)}
            />
          </Tooltip>
          <Dropdown
            trigger={["click"]}
            menu={{
              items: [
                { key: "edit", label: "Изменить" },
                { key: "up", label: "Переместить вверх" },
                { key: "down", label: "Переместить вниз" },
                { key: "delete", label: "Удалить", danger: true },
              ],
              onClick: ({ key }) => {
                if (key === "edit") setEditing(r);
                else if (key === "delete") remove(r);
                else
                  void run("rule-" + key, {
                    id: r.ID,
                    before: p.RuleOrder,
                  }).catch(() => {});
              },
            }}
          >
            <Button
              shape="circle"
              size="small"
              aria-label={"Действия с правилом " + r.Name}
              icon={<MoreOutlined />}
            />
          </Dropdown>
        </Space>
      ),
    },
    {
      title: "Включить",
      width: 75,
      render: (_, r) => (
        <Switch
          size="small"
          checked={!r.Disabled}
          aria-label={"Правило " + r.Name}
          onChange={() => void run("rule-toggle", { id: r.ID }).catch(() => {})}
        />
      ),
    },
    { title: "Комментарий", dataIndex: "Name", width: 170, ellipsis: true },
    { title: "Сеть", width: 65, render: () => "—" },
    {
      title: "Пункт назначения",
      width: 270,
      render: (_, r) => <Destination rule={r} />,
    },
    {
      title: "Устройства",
      dataIndex: "Source",
      width: 155,
      render: (v) =>
        v ? (
          <>
            <span className="field-prefix">IP</span>
            {v}
          </>
        ) : (
          "—"
        ),
    },
    {
      title: "Исходящие",
      dataIndex: "TargetLabel",
      width: 220,
      render: (v) => (
        <Tag color={String(v).includes("недоступен") ? "orange" : "green"} bordered={false}>
          <FlagText text={v} />
        </Tag>
      ),
    },
  ];
  const rulesPanel = (
    <>
      <Space className="routing-toolbar">
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setEditing(null)}
        >
          Маршрутизация
        </Button>
        <Dropdown
          trigger={["click"]}
          menu={{
            items: [
              { key: "basic", label: "Маршрут по умолчанию" },
              { key: "geo", label: "Поиск geosite / geoip" },
            ],
            onClick: ({ key }) => setTab(key),
          }}
        >
          <Button icon={<MoreOutlined />}>ещё</Button>
        </Dropdown>
      </Space>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={async ({ active, over }) => {
          if (!over || active.id === over.id) return;
          const before = rules;
          const ordered = arrayMove(
            rules,
            rules.findIndex((r) => r.ID === active.id),
            rules.findIndex((r) => r.ID === over.id),
          );
          setRules(ordered);
          try {
            await run("rule-order", {
              before: p.RuleOrder,
              order: ordered.map((r) => r.ID).join(","),
            });
          } catch {
            setRules(before);
          }
        }}
      >
        <SortableContext
          items={rules.map((r) => r.ID)}
          strategy={verticalListSortingStrategy}
        >
          <Table
            className="rule-table"
            rowKey="ID"
            columns={columns}
            dataSource={rules}
            components={{ body: { row: DragRow } }}
            size="small"
            pagination={false}
            scroll={{ x: 1100 }}
          />
        </SortableContext>
      </DndContext>
    </>
  );
  const basic = (
    <Form layout="vertical">
      <Form.Item
        label="Маршрут по умолчанию"
        extra="Для трафика, который не совпал с правилами."
      >
        <Target
          p={p}
          value={p.Mode}
          onChange={(v) => void run("settings", { mode: v }).catch(() => {})}
        />
      </Form.Item>
    </Form>
  );
  const test = (
    <>
      <Alert
        showIcon
        type="info"
        title="Проверьте, какой маршрут выбран для домена или IP. Проверка использует сохранённые правила и не отправляет трафик."
      />
      <Form
        className="route-test section-gap"
        layout="vertical"
        onFinish={async (v) => {
          setSearching(true);
          try {
            setResult(
              (
                await getPage("routing", {
                  ...v,
                  source: v.source || v.device || "",
                  test: 1,
                })
              ).RouteCheck,
            );
          } catch (e) {
            message.error(e instanceof Error ? e.message : "Ошибка проверки");
          } finally {
            setSearching(false);
          }
        }}
      >
        <Form.Item name="domain" label="Домен">
          <Input placeholder="Домен" />
        </Form.Item>
        <Form.Item name="address" label="IPv4 назначения">
          <Input placeholder="Необязательно" />
        </Form.Item>
        <Form.Item name="device" label="Устройство">
          <Select
            allowClear
            options={(p.Devices || []).map((d) => ({
              value: d.IP,
              label: d.Name + " (" + d.IP + ")",
            }))}
          />
        </Form.Item>
        <Form.Item name="source" label="IP устройства">
          <Input id="source-ip" placeholder="Необязательно" />
        </Form.Item>
        <Button icon={<AimOutlined />} loading={searching} htmlType="submit">
          Тест маршрута
        </Button>
      </Form>
      {result && (
        <Descriptions
          className="section-gap"
          bordered
          column={1}
          size="small"
          items={[
            { key: "result", label: "Результат", children: result.Result },
            ...(result.Target
              ? [
                  {
                    key: "target",
                    label: "Исходящий",
                    children: <FlagText text={result.TargetLabel} />,
                  },
                  { key: "reason", label: "Почему", children: result.Reason },
                ]
              : []),
            ...(result.DNSReason
              ? [
                  {
                    key: "dns",
                    label: "DNS",
                    children: result.DNSReason + " " + result.DNSLabel,
                  },
                ]
              : []),
            ...(result.Warning
              ? [
                  {
                    key: "warning",
                    label: "Обратите внимание",
                    children: result.Warning,
                  },
                ]
              : []),
          ]}
        />
      )}
    </>
  );
  const geoPanel = (
    <>
      <Space wrap>
        <Input.Search
          placeholder="intel, telegram, youtube, discord"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          enterButton="Найти"
          loading={searching}
          onSearch={async (q) => {
            setSearching(true);
            try {
              setGeo(await getPage("routing", { q }));
            } catch (e) {
              message.error(e instanceof Error ? e.message : "Ошибка проверки");
            } finally {
              setSearching(false);
            }
          }}
        />
        <Button onClick={() => void run("geodata").catch(() => {})}>
          Обновить geo-базы
        </Button>
        <div style={{ minWidth: 240 }}>
          <Target p={p} value={geoTarget} onChange={setGeoTarget} />
        </div>
      </Space>
      {geo?.GeoError && <Alert type="error" title={geo.GeoError} />}
      <Table
        className="section-gap"
        rowKey={(r) => r.Kind + ":" + r.Code}
        dataSource={geo?.GeoResults || []}
        size="small"
        columns={[
          {
            title: "Категория",
            render: (_, r) => (
              <Button
                type="link"
                onClick={async () =>
                  setGeo(
                    await getPage("routing", {
                      q: query,
                      category: r.Kind + ":" + r.Code,
                    }),
                  )
                }
              >
                {r.Kind}:{r.Code}
              </Button>
            ),
          },
          { title: "Записей", dataIndex: "Count" },
          { title: "Совпадения", dataIndex: "Example" },
          {
            title: "",
            render: (_, r) => (
              <Button
                onClick={() =>
                  void run("rule-add", {
                    name: r.Code,
                    kind: r.Kind === "geosite" ? "domain" : "ip",
                    value: r.Kind + ":" + r.Code,
                    target: geoTarget,
                  }).catch(() => {})
                }
              >
                Добавить
              </Button>
            ),
          },
        ]}
      />
      {geo?.GeoDetail && (
        <Card title={geo.GeoDetail.Kind + ":" + geo.GeoDetail.Code}>
          <Input.Search
            className="section-gap"
            placeholder="Фильтр записей"
            value={geoFilter}
            onChange={(e) => setGeoFilter(e.target.value)}
            onSearch={async (filter) => {
              try {
                setGeo(
                  await getPage("routing", {
                    q: query,
                    category: geo.GeoDetail!.Kind + ":" + geo.GeoDetail!.Code,
                    filter,
                  }),
                );
              } catch (e) {
                message.error(e instanceof Error ? e.message : "Ошибка поиска");
              }
            }}
          />
          <JsonDetails value={geo.GeoDetail.Entries} />
          {geo.GeoNext > 0 && (
            <Button
              onClick={async () =>
                setGeo(
                  await getPage("routing", {
                    q: query,
                    category: geo.GeoDetail!.Kind + ":" + geo.GeoDetail!.Code,
                    offset: geo.GeoNext,
                    filter: geoFilter,
                  }),
                )
              }
            >
              Следующие 200
            </Button>
          )}
        </Card>
      )}
    </>
  );
  return (
    <>
      <Card>
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            {
              key: "basic",
              label: "Базовые соединения",
              icon: <ControlOutlined />,
              children: basic,
            },
            {
              key: "rules",
              label: "Маршрутизация",
              icon: <SwapOutlined />,
              children: rulesPanel,
            },
            {
              key: "test",
              label: "Тест маршрута",
              icon: <AimOutlined />,
              children: test,
            },
            ...(tab === "geo"
              ? [
                  {
                    key: "geo",
                    label: "Поиск geosite / geoip",
                    children: geoPanel,
                  },
                ]
              : []),
          ]}
        />
      </Card>
      <Editor
        key={editing?.ID ?? "new"}
        title={editing ? "Изменить правило" : "Добавить правило"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        initial={
          editing
            ? {
                id: editing.ID,
                name: editing.Name,
                kind: editing.Kind,
                value: editing.Value,
                source: editing.Source,
                target: editing.Target,
              }
            : { kind: "domain", target: "proxy", source: "" }
        }
        onSave={(v) => run(editing ? "rule-update" : "rule-add", v)}
      >
        <Form.Item name="name" label="Комментарий" rules={required}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item name="kind" label="Тип" rules={required}>
          <Select
            options={[
              { value: "domain", label: "Домены" },
              { value: "ip", label: "IPv4 / CIDR" },
              { value: "device", label: "Весь трафик устройства" },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="source"
          label="IP устройств"
          extra="Пусто — все устройства. Можно указать IPv4 или подсеть."
        >
          <Input.TextArea rows={2} />
        </Form.Item>
        <Form.Item
          name="value"
          label="Пункт назначения"
          extra="Каждое значение с новой строки. Для правила всего устройства не требуется."
        >
          <Input.TextArea rows={5} />
        </Form.Item>
        <Form.Item name="target" label="Исходящий" rules={required}>
          <Target p={p} />
        </Form.Item>
      </Editor>
    </>
  );
}

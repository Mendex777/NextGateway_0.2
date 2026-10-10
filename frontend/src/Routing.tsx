import { Devices } from "./Pages";
import RoutingTransfer from "./RoutingTransfer";
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
  TeamOutlined,
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
  const [tab, setTab] = useState(p.Tab === "devices" ? "devices" : "rules"),
    [rules, setRules] = useState(p.Rules || []),
    [editing, setEditing] = useState<Rule | null | undefined>(),
    [result, setResult] = useState<RouteResult | null>(null),
    [geo, setGeo] = useState<Page | null>(null),
    [query, setQuery] = useState(""),
    [searching, setSearching] = useState(false);
  const [selectedRules, setSelectedRules] = useState<number[]>([]);
  const [bulkTarget, setBulkTarget] = useState("proxy");
  const [bulkSaving, setBulkSaving] = useState(false);
  useEffect(() => setSelectedRules(ids => ids.filter(id => (p.Rules || []).some(rule => rule.ID === id))), [p.Rules]);
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
      <Space wrap className="routing-toolbar">
 <RoutingTransfer p={p} onDone={()=>reload()} />
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setEditing(null)}
        >
          Маршрутизация
        </Button>
      </Space>
      {selectedRules.length > 0 && <Space wrap className="section-gap" style={{marginBottom: 12}}>
        <span>Выбрано правил: {selectedRules.length}</span>
        <Target p={p} value={bulkTarget} onChange={setBulkTarget} />
        <Button type="primary" loading={bulkSaving} onClick={async () => {
          setBulkSaving(true);
          try { await run("rule-bulk-target", {ids: selectedRules.join(","), target: bulkTarget}); setSelectedRules([]); }
          catch {} finally { setBulkSaving(false); }
        }}>Изменить исходящее</Button>
        <Button danger disabled={bulkSaving} onClick={() => {
          const ids = [...selectedRules];
          modal.confirm({title: `Удалить выбранные правила (${ids.length})?`, content: "После удаления нажмите «Применить», чтобы обновить конфигурацию Xray.", okText: "Удалить", cancelText: "Отмена", okButtonProps: {danger: true}, onOk: async () => {
            await run("rule-bulk-delete", {ids: ids.join(",")}); setSelectedRules([]);
          }});
        }}>Удалить выбранные</Button>
        <Button onClick={() => setSelectedRules([])}>Снять выделение</Button>
        <Tooltip title="Shift + флажок выделяет диапазон; обычный клик добавляет или убирает отдельное правило."><span>Shift: диапазон</span></Tooltip>
      </Space>}
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
            rowSelection={{selectedRowKeys: selectedRules, onChange: keys => setSelectedRules(keys.map(Number))}}
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
              key: "devices",
              label: "Устройства",
              icon: <TeamOutlined />,
              children: <Devices p={p} run={run} />,
            },
            {
              key: "test",
              label: "Тест маршрута",
              icon: <AimOutlined />,
              children: test,
            },
            {
              key: "geo",
              label: "Поиск geosite / geoip",
              children: geoPanel,
            },
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
          extra={<>
            <div>Каждое значение с новой строки. Для правила всего устройства не требуется.</div>
            <details style={{marginTop: 8}}>
              <summary style={{cursor: "pointer"}}>Форматы доменов и IP</summary>
              <div style={{marginTop: 8, lineHeight: 1.7}}>
                <div><strong>Тип «Домены»</strong></div>
                <div><code>full:api.example.com</code> — только это имя, без поддоменов.</div>
                <div><code>domain:example.com</code> — сам домен и все его поддомены.</div>
                <div><code>example.com</code> — то же, что <code>domain:example.com</code>.</div>
                <div><code>geosite:youtube</code> — список доменов из установленной geo-базы.</div>
                <div>Не указывайте https://, путь страницы или *. Для поддоменов используйте domain:.</div>
                <div style={{marginTop: 8}}><strong>Тип «IP»</strong></div>
                <div><code>203.0.113.10</code> — один IPv4-адрес.</div>
                <div><code>203.0.113.0/24</code> — подсеть, от 203.0.113.0 до 203.0.113.255.</div>
                <div><code>geoip:telegram</code> — список IP-сетей из установленной geo-базы.</div>
                <div>IPv6 и диапазоны через дефис пока не поддерживаются.</div>
              </div>
            </details>
          </>}
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

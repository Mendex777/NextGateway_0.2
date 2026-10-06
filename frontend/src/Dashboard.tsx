// Dashboard layout adapted from 3x-ui v3.9.0 (GPL-3.0); NGPanel metrics and actions.
import { useEffect, useState, type ReactNode } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
  Upload,
} from "antd";
import {
  ArrowUpOutlined,
  AreaChartOutlined,
  BarsOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
  ControlOutlined,
  CopyOutlined,
  DatabaseOutlined,
  DownloadOutlined,
  EyeInvisibleOutlined,
  EyeOutlined,
  GlobalOutlined,
  HddOutlined,
  PauseOutlined,
  ReloadOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { action, getJSON } from "./api";
import { Overview } from "./Pages";
import type { Page } from "./types";
import type { Run } from "./common";
import "./dashboard.css";

interface Metrics {
  time: number;
  cpu: number;
  cores: number;
  memory: number;
  memoryTotal: number;
  swap: number;
  swapTotal: number;
  disk: number;
  diskTotal: number;
  upload: number;
  download: number;
  sent: number;
  received: number;
  tcp: number;
  udp: number;
  uptime: number;
  xrayUptime: number;
  panelMemory: number;
  threads: number;
  xrayPID: number;
  xrayMemory: number;
  xrayThreads: number;
  interface: string;
}
const empty: Metrics = {
  time: 0,
  cpu: 0,
  cores: 0,
  memory: 0,
  memoryTotal: 0,
  swap: 0,
  swapTotal: 0,
  disk: 0,
  diskTotal: 0,
  upload: 0,
  download: 0,
  sent: 0,
  received: 0,
  tcp: 0,
  udp: 0,
  uptime: 0,
  xrayUptime: 0,
  panelMemory: 0,
  threads: 0,
  xrayPID: 0,
  xrayMemory: 0,
  xrayThreads: 0,
  interface: "",
};
const bytes = (n: number) => {
  let i = 0;
  while (n >= 1024 && i < 4) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i ? 2 : 0)} ${["Б", "КБ", "МБ", "ГБ", "ТБ"][i]}`;
};
const percent = (n: number, total: number) => (total ? (100 * n) / total : 0);
const duration = (n: number) =>
  n >= 86400
    ? `${Math.floor(n / 86400)} д ${Math.floor((n % 86400) / 3600)} ч`
    : n >= 3600
      ? `${Math.floor(n / 3600)} ч ${Math.floor((n % 3600) / 60)} мин`
      : `${Math.floor(n / 60)} мин`;
async function copyText(text: string) {
  if (navigator.clipboard) return navigator.clipboard.writeText(text);
  const input = document.createElement("textarea");
  input.value = text;
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.append(input);
  input.select();
  const ok = document.execCommand("copy");
  input.remove();
  if (!ok) throw new Error("Copy unavailable");
}
function download(text: string, name: string, type = "text/plain") {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function Chart({
  data,
  second = [],
  height = 62,
  label,
}: {
  data: number[];
  second?: number[];
  height?: number;
  label: string;
}) {
  const max = Math.max(...data, ...second, 1);
  const path = (values: number[]) =>
    values
      .map(
        (v, i) =>
          `${i ? "L" : "M"} ${(i * 600) / Math.max(values.length - 1, 1)} ${height - 8 - (v / max) * (height - 16)}`,
      )
      .join(" ");
  return (
    <svg
      role="img"
      aria-label={label}
      className="ov-chart"
      viewBox={`0 0 600 ${height}`}
      preserveAspectRatio="none"
      style={{ height }}
    >
      <title>{label}</title>
      {height > 100 &&
        [0.25, 0.5, 0.75].map((y) => (
          <line
            key={y}
            x1="0"
            x2="600"
            y1={height * y}
            y2={height * y}
            stroke="currentColor"
            opacity=".08"
          />
        ))}
      {data.length > 1 && (
        <>
          <path
            d={`${path(data)} L 600 ${height} L 0 ${height} Z`}
            fill="#1677ff"
            opacity=".12"
          />
          <path
            d={path(data)}
            fill="none"
            stroke="#1677ff"
            strokeWidth="1.6"
            vectorEffect="non-scaling-stroke"
          />
          {second.length > 1 && (
            <path
              d={path(second)}
              fill="none"
              stroke="#8c8c8c"
              strokeWidth="1.6"
              vectorEffect="non-scaling-stroke"
            />
          )}
        </>
      )}
    </svg>
  );
}
function Vital({
  label,
  icon,
  value,
  detail,
  series,
}: {
  label: string;
  icon: ReactNode;
  value: number;
  detail: string;
  series: number[];
}) {
  const avg = series.length
    ? series.reduce((a, b) => a + b, 0) / series.length
    : 0;
  return (
    <Card hoverable className="ov-tile" styles={{ body: { padding: 0 } }}>
      <div className="ov-tile-head">
        <span className="ov-tile-icon">{icon}</span>
        <span className="ov-kicker">{label}</span>
      </div>
      <div className="ov-tile-value">
        <span className="ov-tile-number">{value.toFixed(1)}</span>
        <span className="ov-tile-unit">%</span>
      </div>
      <div className="ov-tile-detail">{detail}</div>
      <div className="ov-tile-foot">
        <span>Среднее {avg.toFixed(1)}%</span>
        <span>Пик {Math.max(...series, 0).toFixed(1)}%</span>
      </div>
      <div className="ov-tile-chart">
        <Chart data={series} label={`${label}, история нагрузки`} />
      </div>
    </Card>
  );
}

export default function Dashboard({
  p,
  run,
  navigate,
  reload,
}: {
  p: Page;
  run: Run;
  navigate: (tab: string) => void;
  reload: () => Promise<Page>;
}) {
  const { message, modal } = App.useApp();
  const [samples, setSamples] = useState<Metrics[]>([]),
    [failure, setFailure] = useState(""),
    [window, setWindow] = useState(""),
    [showIP, setShowIP] = useState(false),
    [config, setConfig] = useState(""),
    [configError, setConfigError] = useState(""),
    [keepMachine, setKeepMachine] = useState(true),
    [restoring, setRestoring] = useState(false),
    [log, setLog] = useState(""),
    [logError, setLogError] = useState(""),
    [logDate, setLogDate] = useState(""),
    [rows, setRows] = useState(20),
    [level, setLevel] = useState("all"),
    [panelLog, setPanelLog] = useState(false),
    [auto, setAuto] = useState(false),
    [logBusy, setLogBusy] = useState(false);
  useEffect(() => {
    let stopped = false;
    const load = async () => {
      try {
        const next = await getJSON<Metrics>("/api/dashboard");
        if (!stopped) {
          setSamples((old) =>
            old.at(-1)?.time === next.time ? old : [...old.slice(-119), next],
          );
          setFailure("");
        }
      } catch {
        if (!stopped) setFailure("Не удалось получить показатели ВМ");
      }
    };
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 3000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, []);
  const m = samples.at(-1) || empty;
  const series = (key: keyof Metrics) => samples.map((s) => Number(s[key]));
  const mem = samples.map((s) => percent(s.memory, s.memoryTotal)),
    swap = samples.map((s) => percent(s.swap, s.swapTotal)),
    disk = samples.map((s) => percent(s.disk, s.diskTotal));
  const loadLog = async () => {
    setLogBusy(true);
    try {
      const s = await getJSON<{ text: string; updated: string }>(
        "/api/dashboard/logs?service=" + (panelLog ? "panel" : "xray"),
      );
      setLog(s.text);
      setLogDate(s.updated);
      setLogError("");
    } catch {
      setLogError("Журнал недоступен");
    } finally {
      setLogBusy(false);
    }
  };
  useEffect(() => {
    if (window !== "logs") return;
    void loadLog();
    const timer = setInterval(
      () => {
        if (!document.hidden) void loadLog();
      },
      auto ? 5000 : 1500,
    );
    return () => clearInterval(timer);
  }, [window, panelLog, auto]);
  useEffect(() => {
    if (window !== "logs" || !auto) return;
    const timer = setInterval(() => {
      if (!document.hidden) void action("logs", "status").catch(() => {});
    }, 10000);
    return () => clearInterval(timer);
  }, [window, auto]);
  const openConfig = async () => {
    setWindow("config");
    setConfig("");
    setConfigError("");
    try {
      setConfig(
        JSON.stringify(await getJSON("/api/dashboard/config"), null, 2),
      );
    } catch {
      setConfigError("Применённая конфигурация ещё не создана или недоступна");
    }
  };
  const doRun = (name: string) => void run(name).catch(() => {});
  const control = (name: string) =>
    modal.confirm({
      title: name === "stop" ? "Остановить Xray?" : "Перезапустить Xray?",
      content: "Текущие соединения будут прерваны.",
      okText: name === "stop" ? "Остановить" : "Перезапустить",
      cancelText: "Отмена",
      onOk: () => run(name),
    });
  const restore = (file: File) => {
    modal.confirm({
      title: "Восстановить настройки из бекапа?",
      content:
        "Подписки, подключения, группы и правила будут заменены. Работающий шлюз изменится после применения конфигурации.",
      okText: "Восстановить",
      cancelText: "Отмена",
      onOk: async () => {
        setRestoring(true);
        try {
          const body = new FormData();
          body.append("backup", file);
          body.append("keep_machine", keepMachine ? "1" : "0");
          const r = await fetch("/backup", {
            method: "POST",
            body,
            headers: { Accept: "application/json" },
          });
          if (!r.ok) throw new Error(await r.text());
          await reload();
          message.success("Бекап восстановлен. Примените конфигурацию.");
          setWindow("");
        } catch (e) {
          message.error(
            e instanceof Error ? e.message : "Ошибка восстановления",
          );
          throw e;
        } finally {
          setRestoring(false);
        }
      },
    });
    return false;
  };
  const filtered = log
    .split("\n")
    .filter(
      (line) =>
        line.trim() && (level === "all" || line.toLowerCase().includes(level)),
    )
    .slice(-rows);
  const active = p.Service.trim() === "active";
  const version = p.Version.match(/\d+\.\d+\.\d+/)?.[0] || "не установлен";
  return (
    <div className="index-page">
      <div className="ov-page">
        <div className="ov-bar">
          <div className={"ov-state " + (active ? "ov-state-running" : "")}>
            <span className="ov-state-dot" />
            Xray · {active ? "Запущен" : "Остановлен"}
            <Button type="text" size="small" onClick={() => setWindow("setup")}>
              v{version}
            </Button>
          </div>
          <Button type="text" size="small" onClick={() => setWindow("update")}>
            {p.PanelVersion}
          </Button>
          {p.PanelUpdate.Available && <Tag color="blue">Обновление</Tag>}
          <div className="ov-bar-actions">
            <Button
              icon={<ReloadOutlined />}
              onClick={() => (active ? control("restart") : doRun("start"))}
            >
              {active ? "Перезапустить" : "Запустить"}
            </Button>
            <Button
              type="text"
              icon={<PauseOutlined />}
              onClick={() => control("stop")}
              disabled={!active}
            >
              Остановить
            </Button>
            <span className="ov-bar-sep" />
            <Button
              type="text"
              icon={<BarsOutlined />}
              onClick={() => {
                setWindow("logs");
                doRun("logs");
              }}
            >
              Логи
            </Button>
            <Button
              type="text"
              icon={<ControlOutlined />}
              onClick={() => void openConfig()}
            >
              Конфигурация
            </Button>
            <Button
              type="text"
              icon={<CloudServerOutlined />}
              onClick={() => setWindow("backup")}
            >
              Бэкап
            </Button>
            <span className="ov-bar-sep" />
            <Button
              type="text"
              icon={<AreaChartOutlined />}
              onClick={() => setWindow("history")}
            >
              История
            </Button>
            <Button
              type="text"
              icon={<ArrowUpOutlined />}
              onClick={() => setWindow("metrics")}
            >
              Метрики Xray
            </Button>
            <Tooltip title="Компоненты и мастер настройки шлюза">
              <Button
                type="text"
                icon={<SettingOutlined />}
                aria-label="Настройка шлюза"
                onClick={() => setWindow("setup")}
              />
            </Tooltip>
          </div>
        </div>
        <hr className="ov-rule" />
        {failure && <Alert type="warning" title={failure} />}
        {!samples.length && !failure ? (
          <Spin />
        ) : (
          <>
            <div className="ov-vitals">
              <Vital
                label="CPU"
                icon={<ThunderboltOutlined />}
                value={m.cpu}
                detail={`${m.cores} ${m.cores === 1 ? "ядро" : "ядер"}`}
                series={series("cpu")}
              />
              <Vital
                label="Память"
                icon={<DatabaseOutlined />}
                value={percent(m.memory, m.memoryTotal)}
                detail={`${bytes(m.memory)} / ${bytes(m.memoryTotal)}`}
                series={mem}
              />
              <Vital
                label="Swap"
                icon={<SwapIcon />}
                value={percent(m.swap, m.swapTotal)}
                detail={`${bytes(m.swap)} / ${bytes(m.swapTotal)}`}
                series={swap}
              />
              <Vital
                label="Диск"
                icon={<HddOutlined />}
                value={percent(m.disk, m.diskTotal)}
                detail={`${bytes(m.disk)} / ${bytes(m.diskTotal)}`}
                series={disk}
              />
            </div>
            <div className="ov-mid">
              <Card hoverable styles={{ body: { padding: 0 } }}>
                <div className="ov-wide-head">
                  <div>
                    <div className="ov-kicker">Пропускная способность</div>
                    <div className="ov-sub">
                      Всего по интерфейсу {m.interface}
                    </div>
                  </div>
                  <div className="ov-wide-legend">
                    {[
                      ["Отправка", m.upload, "#1677ff"],
                      ["Приём", m.download, "#8c8c8c"],
                    ].map(([label, value, color]) => (
                      <div key={label}>
                        <div className="ov-legend-label">
                          <span
                            className="ov-swatch"
                            style={{ background: String(color) }}
                          />
                          {label}
                        </div>
                        <div className="ov-legend-num">
                          {bytes(Number(value))}/с
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                <div className="ov-wide-chart">
                  <Chart
                    data={series("upload")}
                    second={series("download")}
                    height={186}
                    label="Отправка и приём по интерфейсу ВМ"
                  />
                </div>
                <div className="ov-wide-foot">
                  {[
                    ["Отправлено", bytes(m.sent)],
                    ["Получено", bytes(m.received)],
                    [
                      "Среднее за окно",
                      bytes(
                        samples.reduce((n, s) => n + s.upload + s.download, 0) /
                          Math.max(samples.length, 1),
                      ) + "/с",
                    ],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <div className="ov-kicker">{label}</div>
                      <div className="ov-foot-value">{value}</div>
                    </div>
                  ))}
                </div>
              </Card>
              <Card hoverable styles={{ body: { padding: 0 } }}>
                <div className="ov-wide-head ov-wide-head-stack">
                  <div className="ov-kicker">Соединения</div>
                  <div className="ov-conn-total">
                    <span className="ov-tile-number">{m.tcp + m.udp}</span>
                    <span className="ov-sub">открытых сокетов</span>
                  </div>
                </div>
                <div className="ov-conn-legend">
                  <div>
                    <span className="ov-legend-label">TCP</span>
                    <strong>{m.tcp}</strong>
                  </div>
                  <div>
                    <span className="ov-legend-label">UDP</span>
                    <strong>{m.udp}</strong>
                  </div>
                </div>
                <div className="ov-wide-chart">
                  <Chart
                    data={series("tcp")}
                    second={series("udp")}
                    height={186}
                    label="TCP и UDP сокеты ВМ"
                  />
                </div>
              </Card>
            </div>
            <Card hoverable styles={{ body: { padding: 0 } }}>
              <div className="ov-strip-grid">
                <div className="ov-strip-cell">
                  <div className="ov-kicker ov-kicker-icon">
                    <ClockCircleOutlined />
                    Время работы
                  </div>
                  <div className="ov-strip-split">
                    <div>
                      <div className="ov-strip-sub">Xray</div>
                      <div className="ov-strip-value">
                        {active ? duration(m.xrayUptime) : "—"}
                      </div>
                    </div>
                    <span className="ov-strip-split-sep" />
                    <div>
                      <div className="ov-strip-sub">OS</div>
                      <div className="ov-strip-value">{duration(m.uptime)}</div>
                    </div>
                  </div>
                </div>
                <div className="ov-strip-cell">
                  <div className="ov-kicker ov-kicker-icon">
                    <DatabaseOutlined />
                    Панель
                  </div>
                  <div className="ov-strip-split">
                    <div>
                      <div className="ov-strip-sub">Память</div>
                      <div className="ov-strip-value">
                        {bytes(m.panelMemory)}
                      </div>
                    </div>
                    <span className="ov-strip-split-sep" />
                    <div>
                      <div className="ov-strip-sub">Потоки</div>
                      <div className="ov-strip-value">{m.threads}</div>
                    </div>
                  </div>
                </div>
                <div className="ov-strip-cell">
                  <div className="ov-kicker ov-kicker-icon">
                    <GlobalOutlined />
                    Адрес шлюза
                    <Button
                      size="small"
                      type="text"
                      aria-label="Показать или скрыть адрес"
                      icon={showIP ? <EyeOutlined /> : <EyeInvisibleOutlined />}
                      onClick={() => setShowIP(!showIP)}
                    />
                  </div>
                  <div className={"ov-ip " + (showIP ? "" : "ip-hidden")}>
                    <div className="ov-mono">{p.DetectedNetwork.address}</div>
                    <div className="ov-mono ov-ip-v6">{m.interface}</div>
                  </div>
                </div>
              </div>
            </Card>
          </>
        )}
      </div>
      <Modal
        open={window === "backup"}
        onCancel={() => setWindow("")}
        footer={null}
        title="Бэкап и восстановление"
        width={520}
      >
        <div className="dash-backup-row">
          <div>
            <Typography.Text strong>Экспорт базы данных</Typography.Text>
            <div className="dash-secondary">
              Подписки, подключения, группы и правила
            </div>
          </div>
          <Tooltip title="Скачать бекап">
            <Button
              type="primary"
              shape="circle"
              icon={<DownloadOutlined />}
              aria-label="Скачать бекап"
              href="/backup"
            />
          </Tooltip>
        </div>
        <div className="dash-backup-row">
          <div>
            <Typography.Text strong>Импорт базы данных</Typography.Text>
            <div className="dash-secondary">
              Восстановление из файла NGPanel (.json)
            </div>
          </div>
          <Upload
            accept=".json,application/json"
            showUploadList={false}
            beforeUpload={restore}
          >
            <Button
              type="primary"
              shape="circle"
              loading={restoring}
              icon={<UploadOutlined />}
              aria-label="Загрузить бекап"
            />
          </Upload>
        </div>
        <Checkbox
          checked={keepMachine}
          onChange={(e) => setKeepMachine(e.target.checked)}
        >
          Сохранить настройки этой машины
        </Checkbox>
        <div className="dash-secondary dash-backup-hint">
          Оставляет адрес ВМ, интерфейс, подсеть и роутер вместо значений из
          бекапа.
        </div>
      </Modal>
      <Modal
        open={window === "config"}
        onCancel={() => setWindow("")}
        title="Конфигурация Xray"
        width={900}
        style={{ top: 20 }}
        footer={
          <Space>
            <Button
              icon={<DownloadOutlined />}
              disabled={!config}
              onClick={() =>
                download(config, "config.json", "application/json")
              }
            >
              config.json
            </Button>
            <Button
              type="primary"
              icon={<CopyOutlined />}
              disabled={!config}
              onClick={() =>
                void copyText(config)
                  .then(() => message.success("Скопировано"))
                  .catch(() =>
                    message.error("Копирование недоступно. Скачайте файл."),
                  )
              }
            >
              Копировать
            </Button>
          </Space>
        }
      >
        {configError ? (
          <Alert type="warning" title={configError} />
        ) : config ? (
          <pre className="dash-config">{config}</pre>
        ) : (
          <Spin />
        )}
      </Modal>
      <Modal
        open={window === "logs"}
        onCancel={() => setWindow("")}
        footer={null}
        title={
          <Space>
            Логи
            <Button
              type="text"
              size="small"
              aria-label="Обновить журналы"
              icon={<ReloadOutlined spin={logBusy} />}
              onClick={() => doRun("logs")}
            />
          </Space>
        }
        width={800}
      >
        <Space wrap className="dash-log-toolbar">
          <Select
            value={rows}
            onChange={setRows}
            options={[20, 50, 100, 500, 1000].map((value) => ({
              value,
              label: String(value),
            }))}
          />
          <Select
            value={level}
            onChange={setLevel}
            options={[
              { value: "all", label: "Все уровни" },
              ...["debug", "info", "warning", "error"].map((value) => ({
                value,
                label: value,
              })),
            ]}
          />
          <Checkbox
            checked={panelLog}
            onChange={(e) => setPanelLog(e.target.checked)}
          >
            Панель
          </Checkbox>
          <Checkbox checked={auto} onChange={(e) => setAuto(e.target.checked)}>
            Автообновление
          </Checkbox>
          <Button
            type="primary"
            icon={<DownloadOutlined />}
            aria-label="Скачать журнал"
            onClick={() =>
              download(
                filtered.join("\n"),
                panelLog ? "ngpanel.log" : "xray.log",
              )
            }
          />
        </Space>
        {logError && <Alert type="error" title={logError} />}
        <div className="dash-log-container">
          {filtered.length ? (
            filtered.map((line, i) => (
              <div className="dash-log-line" key={i}>
                {line}
              </div>
            ))
          ) : (
            <div className="dash-secondary">
              Записей пока нет. Нажмите обновить журналы.
            </div>
          )}
        </div>
        {logDate && (
          <div className="dash-secondary">
            Обновлено: {new Date(logDate).toLocaleString("ru-RU")}
          </div>
        )}
      </Modal>
      <Modal
        open={window === "history"}
        onCancel={() => setWindow("")}
        title="История системы"
        width={900}
        footer={null}
      >
        <Typography.Paragraph type="secondary">
          Последние {samples.length} измерений с момента открытия дашборда.
        </Typography.Paragraph>
        {[
          ["CPU", series("cpu")],
          ["Память", mem],
          ["Swap", swap],
          ["Диск", disk],
        ].map(([label, data]) => (
          <Card
            key={String(label)}
            size="small"
            title={String(label)}
            className="section-gap"
          >
            <Chart data={data as number[]} height={140} label={String(label)} />
          </Card>
        ))}
      </Modal>
      <Modal
        open={window === "setup" || (p.Wizard && window === "")}
        onCancel={() => {
          setWindow("");
          if (p.Wizard) doRun("setup-skip");
        }}
        title="Настройка шлюза"
        width={1000}
        footer={null}
      >
        <Overview
          p={p}
          run={run}
          navigate={(tab) => {
            setWindow("");
            if (tab === "backup") setWindow("backup");
            else navigate(tab);
          }}
        />
      </Modal>
      <Modal
        open={window === "metrics"}
        onCancel={() => setWindow("")}
        title="Метрики Xray"
        width={800}
        footer={null}
      >
        <div className="dash-process-grid">
          {[
            ["PID", m.xrayPID || "—"],
            ["Память процесса", bytes(m.xrayMemory)],
            ["Потоки", m.xrayThreads],
            ["Время работы", active ? duration(m.xrayUptime) : "—"],
          ].map(([label, value]) => (
            <Card size="small" key={label}>
              <div className="ov-kicker">{label}</div>
              <div className="ov-strip-value">{value}</div>
            </Card>
          ))}
        </div>
        <Card className="section-gap" size="small" title="Память процесса Xray">
          <Chart
            data={series("xrayMemory")}
            height={186}
            label="История памяти процесса Xray"
          />
        </Card>
      </Modal>
      <Modal
        open={window === "update"}
        onCancel={() => setWindow("")}
        title="Обновление NGPanel"
        footer={null}
      >
        <Typography.Paragraph>
          Установлена {p.PanelVersion}
        </Typography.Paragraph>
        <Typography.Paragraph>{p.PanelUpdate.Message}</Typography.Paragraph>
        <Space wrap>
          <Button onClick={() => doRun("panel-update-check")}>
            Проверить обновления
          </Button>
          <Button
            type="primary"
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
      </Modal>
    </div>
  );
}
function SwapIcon() {
  return <ControlOutlined />;
}

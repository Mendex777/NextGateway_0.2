// Dashboard layout adapted from 3x-ui v3.9.0 (GPL-3.0); NGPanel metrics and actions.
import { useEffect, useState } from "react";
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
  Grid,
  theme,
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
  PoweroffOutlined,
  DashboardOutlined,
  SwapOutlined,
  ReloadOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { action, getJSON } from "./api";
import { Overview } from "./Pages";
import type { Page } from "./types";
import type { Run } from "./common";
import "./dashboard.css";
import Sparkline from "./reference/Sparkline";
import VitalTile from "./reference/VitalTile";
import ThroughputCard from "./reference/ThroughputCard";
import ConnectionsCard from "./reference/ConnectionsCard";
import { SizeFormatter, CPUFormatter, mean, peak } from "./reference/utils";

interface Metrics {
  time: number;
  cpu: number;
  cores: number;
  physicalCores: number;
  cpuMHz: number;
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
  physicalCores: 0,
  cpuMHz: 0,
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
const bytes = SizeFormatter.sizeFormat;
const percent = (n: number, total: number) => (total ? (100 * n) / total : 0);
const duration = (n: number) =>
  n < 60
    ? n.toFixed(0) + "s"
    : n < 3600
      ? (n / 60).toFixed(0) + "m"
      : n < 86400
        ? (n / 3600).toFixed(0) + "h"
        : (n / 86400).toFixed(0) + "d";
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
  height = 140,
  label,
}: {
  data: number[];
  second?: number[];
  height?: number;
  label: string;
}) {
  const { token } = theme.useToken();
  return (
    <Sparkline
      data={data}
      data2={second}
      height={height}
      stroke={token.colorPrimary}
      stroke2={token.colorTextTertiary}
      valueMax={null}
      showTooltip
      showLegend={false}
      fillOpacity={0.24}
      name1={label}
      yFormatter={bytes}
    />
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
  const { token } = theme.useToken();
  const isMobile = !Grid.useBreakpoint().md;
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
        const history = await getJSON<Metrics[]>("/api/dashboard/history");
        if (!stopped) {
          setSamples(history);
          setFailure("");
        }
      } catch {
        if (!stopped) setFailure("Не удалось получить показатели ВМ");
      }
    };
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 2000);
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
    <div className="index-page is-dark">
      <div className="ov-page">
        <div className="ov-bar">
          <div className={"ov-state " + (active ? "ov-state-running" : "")}>
            <span className="ov-state-dot" />
            Xray · {active ? "Запущен" : "Остановлен"}
            <Button type="text" size="small" onClick={() => setWindow("setup")}>
              v{version}
            </Button>
          </div>
          <button
            type="button"
            className="ov-panel-version ov-mono"
            onClick={() => setWindow("update")}
          >
            {p.PanelVersion}
          </button>
          {p.PanelUpdate.Available && <Tag color="blue">Обновление</Tag>}
          <div className="ov-bar-actions">
            <Button
              size={isMobile ? "small" : "middle"}
              color="primary"
              variant="outlined"
              aria-label={active ? "Перезапуск" : "Запустить"}
              icon={<ReloadOutlined />}
              onClick={() => (active ? control("restart") : doRun("start"))}
            >
              {isMobile ? undefined : active ? "Перезапуск" : "Запустить"}
            </Button>
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="Стоп"
              icon={<PoweroffOutlined />}
              onClick={() => control("stop")}
              disabled={!active}
            >
              {isMobile ? undefined : "Стоп"}
            </Button>
            <span className="ov-bar-sep" />
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="Логи"
              icon={<BarsOutlined />}
              onClick={() => {
                setWindow("logs");
                doRun("logs");
              }}
            >
              {isMobile ? undefined : "Логи"}
            </Button>
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="Конфигурация"
              icon={<ControlOutlined />}
              onClick={() => void openConfig()}
            >
              {isMobile ? undefined : "Конфигурация"}
            </Button>
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="Бэкап и восстановление"
              icon={<CloudServerOutlined />}
              onClick={() => setWindow("backup")}
            >
              {isMobile ? undefined : "Бэкап и восстановление"}
            </Button>
            <span className="ov-bar-sep" />
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="История системы"
              icon={<AreaChartOutlined />}
              onClick={() => setWindow("history")}
            >
              {isMobile ? undefined : "История системы"}
            </Button>
            <Button
              size={isMobile ? "small" : "middle"}
              type="text"
              aria-label="Метрики Xray"
              icon={<ArrowUpOutlined />}
              onClick={() => setWindow("metrics")}
            >
              {isMobile ? undefined : "Метрики Xray"}
            </Button>
          </div>
        </div>
        <hr className="ov-rule" />
        {failure && <Alert type="warning" title={failure} />}
        {!samples.length && !failure ? (
          <Spin />
        ) : (
          <>
            <div className="ov-vitals">
              <VitalTile
                icon={<DashboardOutlined />}
                label="ЦП"
                percent={m.cpu}
                statusColor={token.colorPrimary}
                detail={`${CPUFormatter.cpuCoreFormat(m.physicalCores || m.cores)} / ${m.cores}T · ${CPUFormatter.cpuSpeedFormat(m.cpuMHz)}`}
                footLeft={`Среднее ${mean(series("cpu")).toFixed(0)}%`}
                footRight={`Пик ${peak(series("cpu")).toFixed(0)}%`}
                data={series("cpu")}
                isMobile={isMobile}
              />
              <VitalTile
                icon={<DatabaseOutlined />}
                label="Память"
                percent={percent(m.memory, m.memoryTotal)}
                statusColor={token.colorPrimary}
                detail={`${bytes(m.memory)} / ${bytes(m.memoryTotal)}`}
                footLeft={`Среднее ${mean(mem).toFixed(0)}%`}
                footRight={`Пик ${peak(mem).toFixed(0)}%`}
                data={mem}
                isMobile={isMobile}
              />
              <VitalTile
                icon={<SwapOutlined />}
                label="Подкачка"
                percent={percent(m.swap, m.swapTotal)}
                statusColor={token.colorPrimary}
                detail={`${bytes(m.swap)} / ${bytes(m.swapTotal)}`}
                footLeft={`Среднее ${mean(swap).toFixed(1)}%`}
                footRight={`Пик ${peak(swap).toFixed(0)}%`}
                data={swap}
                isMobile={isMobile}
              />
              <VitalTile
                icon={<HddOutlined />}
                label="Диск"
                percent={percent(m.disk, m.diskTotal)}
                statusColor={token.colorPrimary}
                detail={`${bytes(m.disk)} / ${bytes(m.diskTotal)}`}
                footLeft={`Свободно ${bytes(m.diskTotal - m.disk)}`}
                footRight={`Среднее ${mean(disk).toFixed(1)}%`}
                data={disk}
                isMobile={isMobile}
              />
            </div>
            <div className="ov-mid">
              <ThroughputCard
                status={{
                  netIO: { up: m.upload, down: m.download },
                  netTraffic: { sent: m.sent, recv: m.received },
                  tcpCount: m.tcp,
                  udpCount: m.udp,
                }}
                up={series("upload")}
                down={series("download")}
                labels={samples.map((s) =>
                  new Date(s.time).toLocaleTimeString("ru-RU"),
                )}
                isMobile={isMobile}
              />
              <ConnectionsCard
                status={{
                  netIO: { up: m.upload, down: m.download },
                  netTraffic: { sent: m.sent, recv: m.received },
                  tcpCount: m.tcp,
                  udpCount: m.udp,
                }}
                tcp={series("tcp")}
                udp={series("udp")}
                labels={samples.map((s) =>
                  new Date(s.time).toLocaleTimeString("ru-RU"),
                )}
                isMobile={isMobile}
              />
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
                    IP-адреса сервера
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
          Последние {samples.length} измерений системы.
        </Typography.Paragraph>
        {[
          ["ЦП", series("cpu")],
          ["Память", mem],
          ["Подкачка", swap],
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

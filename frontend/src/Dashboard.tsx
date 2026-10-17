// Gateway overview and maintenance dialogs; chart utilities adapted from 3x-ui (GPL-3.0).
import { useEffect, useState } from "react";
import {
  Alert,
  App,
  Button,
  Checkbox,
  Modal,
  Space,
  Spin,
  Tooltip,
  Typography,
  Upload,
} from "antd";
import {
  CopyOutlined,
  DownloadOutlined,
  UploadOutlined,
} from "./icons";
import { getJSON } from "./api";
import type { Page } from "./types";
import type { Run } from "./common";
import "./dashboard.css";
import HomeOverview from "./HomeOverview";
import SystemHistory from "./SystemHistory";

export interface Metrics {
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
export default function Dashboard({
  p,
  run,
  navigate,
  reload,
  working,
}: {
  p: Page;
  working: boolean;
  run: Run;
  navigate: (tab: string) => void;
  reload: () => Promise<Page>;
}) {
  const { message, modal } = App.useApp();
  const [samples, setSamples] = useState<Metrics[]>([]),
    [failure, setFailure] = useState(""),
    [window, setWindow] = useState(""),
    [config, setConfig] = useState(""),
    [configError, setConfigError] = useState(""),
    [keepMachine, setKeepMachine] = useState(true),
    [restoring, setRestoring] = useState(false);
  useEffect(() => {
    let stopped = false;
    let loading = false;
    const load = async () => {
      if (loading) return;
      loading = true;
      try {
        const history = await getJSON<Metrics[]>("/api/dashboard/history");
        if (!stopped) {
          setSamples(history);
          setFailure("");
        }
      } catch {
        if (!stopped) setFailure("Не удалось получить показатели ВМ");
      } finally { loading = false; }
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
        "Сохранённые настройки панели будут заменены: подписки, подключения, группы, правила, устройства и DNS. Параметры шлюза зависят от опции «Сохранить настройки этой машины». Работающий шлюз изменится после применения конфигурации.",
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
  return (
    <div className="index-page is-dark">
      <HomeOverview run={run} working={working} p={p} samples={samples} failure={failure} navigate={navigate} onTool={(tool) => { if (tool === "config") void openConfig(); else if (tool === "start") doRun("start"); else if (tool === "stop" || tool === "restart") control(tool); else setWindow(tool); }} />
      <Modal className="home-dialog"
        open={window === "backup"}
        onCancel={() => setWindow("")}
        footer={null}
        title="Бэкап и восстановление"
        width={520}
      >
        <div className="dash-backup-row">
          <div>
            <Typography.Text strong>Полный бекап настроек</Typography.Text>
            <div className="dash-secondary">
              Все настройки панели, устройства, DNS, шлюз и снимок конфигурации Xray
            </div>
          </div>
          <Tooltip title="Скачать бекап">
            <Button
              type="primary"
              shape="default"
              icon={<DownloadOutlined />}
              aria-label="Скачать бекап"
              href="/backup"
            />
          </Tooltip>
        </div>
        <div className="dash-backup-row">
          <div>
            <Typography.Text strong>Восстановление настроек</Typography.Text>
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
              shape="default"
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
          Восстанавливает сохранённые настройки; для запуска нажмите «Применить». Снимок Xray сохранён для справки. Эта опция оставляет адрес ВМ, интерфейс, подсеть и роутер вместо значений из
          бекапа.
        </div>
      </Modal>
      <Modal className="home-dialog"
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
      <SystemHistory open={window === "history"} onClose={() => setWindow("")} samples={samples} failure={failure}/>
    </div>
  );
}

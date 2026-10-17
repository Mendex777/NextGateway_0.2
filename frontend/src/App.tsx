import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Drawer,
  Layout,
  Menu,
  Space,
  Spin,
  Tooltip,
  Typography,
} from "antd";
import {
  DashboardOutlined,
  SafetyOutlined,
  TeamOutlined,
  SwapOutlined,
  ApartmentOutlined,
  ToolOutlined,
  SettingOutlined,
  PushpinFilled,
  PushpinOutlined,
  MenuOutlined,
} from "./icons";
import { action, getJSON, getPage } from "./api";
import type { Page, Values } from "./types";
import PanelSettings from "./PanelSettings";
import { usePanelTheme } from "./PanelTheme";
import Routing from "./Routing";
import Subscriptions from "./Subscriptions";
import Dashboard from "./Dashboard";
import { Gateway, Backup } from "./Pages";
import Diagnostics from "./Diagnostics";
const navigation = [
  { key: "status", label: "Главная", icon: <DashboardOutlined /> },
  {
    key: "subscriptions",
    label: "Подписки и подключения",
    icon: <SafetyOutlined />,
  },
  { key: "routing", label: "Маршрутизация", icon: <SwapOutlined /> },
  { key: "gateway", label: "Сеть и DNS", icon: <ApartmentOutlined /> },
  { key: "diagnostics", label: "Диагностика", icon: <ToolOutlined /> },
];
function stored(key: string, fallback: boolean) {
  try {
    return localStorage.getItem(key) === null
      ? fallback
      : localStorage.getItem(key) === "true";
  } catch {
    return fallback;
  }
}
export default function App() {
  const { message } = AntApp.useApp();
  const { baseTheme } = usePanelTheme();
  const [tab, setTab] = useState(
    new URLSearchParams(location.search).get("tab") === "settings" ? "status" : new URLSearchParams(location.search).get("tab") || "status",
  );
  const developerImportPending=useRef(false);
  const versionClicks=useRef({count:0,last:0});
  const [developerImport,setDeveloperImport]=useState(false);
  const developerImportOpened=useCallback(()=>setDeveloperImport(false),[]);
  const [appearanceOpen,setAppearanceOpen]=useState(()=>new URLSearchParams(location.search).get("tab")==="settings");
  useEffect(()=>{if(new URLSearchParams(location.search).get("tab")==="settings")history.replaceState(null,"","/?tab=status");},[]);
  const [diagnosticsView,setDiagnosticsView]=useState(()=>new URLSearchParams(location.search).get("view")||"logs");
  const [page, setPage] = useState<Page | null>(null),
    [error, setError] = useState(""),
    [working, setWorking] = useState(false),
    [operation, setOperation] = useState<{
      action: string;
      since: string;
    } | null>(null);
  const [notice, setNotice] = useState<{
      type: "success" | "error" | "info";
      text: string;
    } | null>(null),
    [pinned, setPinned] = useState(() =>
      stored("ngpanel-sidebar-pinned", true),
    ),
    [hover, setHover] = useState(false),
    [drawer, setDrawer] = useState(false);
  const currentTab = useRef(tab);
  currentTab.current = tab;
  const reload = useCallback(async (query: Values = {}) => {
    const requested = currentTab.current;
    const result = await getPage(requested, query);
    if (requested === currentTab.current) {
      setPage(result);
      setError("");
    }
    return result;
  }, []);
  useEffect(() => {
    const abort = new AbortController();
    setPage(null);
    getPage(tab, {}, abort.signal)
      .then(result=>{setPage(result);if(tab==="routing"&&developerImportPending.current){developerImportPending.current=false;setDeveloperImport(true);}})
      .catch((e) => {
        if (e.name !== "AbortError") setError(e.message);
      });
    return () => abort.abort();
  }, [tab]);
  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden && !working && !operation)
        void reload().catch(() => {});
    }, 15000);
    return () => clearInterval(timer);
  }, [reload, working, operation]);
  useEffect(() => {
    const listener = () => {
      const requested=new URLSearchParams(location.search).get("tab")||"status";
      setTab(requested==="settings"?"status":requested);
      if(requested!=="routing"){developerImportPending.current=false;setDeveloperImport(false);}
      if(requested==="settings")setAppearanceOpen(true);
      setDiagnosticsView(new URLSearchParams(location.search).get("view") || "logs");
    };
    window.addEventListener("popstate", listener);
    return () => window.removeEventListener("popstate", listener);
  }, []);
  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDrawer(false);
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, []);
  useEffect(() => {
    if (!operation) return;
    let stopped = false;
    const poll = async () => {
      try {
        const s = await getJSON<{
          State: string;
          Message: string;
          Done: boolean;
        }>("/operation-status?" + new URLSearchParams(operation));
        if (stopped) return;
        setNotice({
          type: s.State === "error" ? "error" : s.Done ? "success" : "info",
          text: s.Message,
        });
        if (s.Done) {
          setOperation(null);
          setWorking(false);
          await reload();
        }
      } catch {
        /* panel updates briefly restart the server */
      }
    };
    void poll();
    const timer = setInterval(poll, 1500);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [operation, reload]);
  const navigate = (next: string) => {
    if(new URLSearchParams("tab="+next).get("tab")!=="routing"){developerImportPending.current=false;setDeveloperImport(false);}
    history.pushState(null, "", "/?tab=" + next);
    setTab(new URLSearchParams("tab=" + next).get("tab") || "status");
    setDiagnosticsView(new URLSearchParams("tab="+next).get("view")||"logs");
    setDrawer(false);
  };
  const onVersionClick=()=>{
    const now=performance.now(),sequence=versionClicks.current;
    sequence.count=now-sequence.last>3000?1:sequence.count+1;
    sequence.last=now;
    if(sequence.count<8)return;
    sequence.count=0;sequence.last=0;
    setAppearanceOpen(false);
    if(tab==="routing"&&page?.Tab==="routing"){setDeveloperImport(true);setDrawer(false);}
    else{developerImportPending.current=true;navigate("routing");}
  };
  const run = async (name: string, values: Values = {}) => {
    setWorking(true);
    setNotice(null);
    try {
      const r = await action(name, tab, values);
      if (r.operation) {
        setNotice({ type: "info", text: "Выполняется операция…" });
        setOperation({ action: name, since: r.since });
      } else {
        void message.success(r.message);
        await reload();
        setWorking(false);
      }
    } catch (e) {
      setWorking(false);
      setNotice({
        type: "error",
        text: e instanceof Error ? e.message : "Не удалось выполнить действие",
      });
      throw e;
    }
  };
  const safeRun = (name: string, values: Values = {}) => {
    void run(name, values).catch(() => {});
  };
  const sidebar = (collapsed: boolean) => (
    <>
      <div className="sider-brand">
        {!collapsed && <Typography.Text strong>NGPanel</Typography.Text>}
        <Tooltip title={pinned ? "Открепить меню" : "Закрепить меню"}>
          <Button
            type="text"
            size="small"
            aria-label={pinned ? "Открепить меню" : "Закрепить меню"}
            icon={pinned ? <PushpinFilled /> : <PushpinOutlined />}
            onClick={() => {
              setPinned(!pinned);
              localStorage.setItem("ngpanel-sidebar-pinned", String(!pinned));
            }}
          />
        </Tooltip>
      </div>
      <Menu
        mode="inline"
        theme={baseTheme === "light" ? "light" : "dark"}
        inlineCollapsed={collapsed}
        selectedKeys={[tab === "devices" ? "routing" : tab]}
        items={navigation}
        onClick={({ key }) => navigate(key)}
      />
      <div className="sider-footer">
        <div className="sider-footer-row">
          {!collapsed&&<button className="sider-version" aria-label={`Версия панели ${page?.PanelVersion||""}`} onClick={onVersionClick}><span>{page?.PanelVersion||"NGPanel"}</span></button>}
          <Tooltip title="Оформление"><Button type="text" size="small" aria-label="Оформление" icon={<SettingOutlined/>} onClick={()=>{setDrawer(false);setAppearanceOpen(true);}}/></Tooltip>
        </div>
        {!collapsed&&<a href="/LICENSE" target="_blank" rel="noreferrer">GPL-3.0</a>}
      </div>
    </>
  );
  return (
    <Layout className="ng-layout">
      <div
        className={"sidebar-rail " + (pinned ? "pinned" : "")}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
      >
        <Layout.Sider
          width={220}
          collapsedWidth={72}
          collapsed={!pinned && !hover}
          className={!pinned && hover ? "floating-sidebar" : ""}
        >
          {sidebar(!pinned && !hover)}
        </Layout.Sider>
      </div>
      <Drawer title="Оформление" placement="right" width="min(420px, 100vw)" open={appearanceOpen} onClose={()=>setAppearanceOpen(false)} className="appearance-drawer"><PanelSettings/></Drawer>
      <Layout>
        <Layout.Header className="mobile-header">
          <Button
            aria-label="Открыть меню"
            icon={<MenuOutlined />}
            onClick={() => setDrawer(true)}
          />
          <Typography.Text>
            {navigation.find((n) => n.key === tab)?.label}
          </Typography.Text>
        </Layout.Header>
        <Drawer
          title="NGPanel"
          placement="left"
          width={220}
          open={drawer}
          onClose={() => setDrawer(false)}
          className="nav-drawer"
        >
          {sidebar(false)}
        </Drawer>
        <Layout.Content id="main-content" className={tab === "gateway" || tab === "diagnostics" ? "bounded-content" : undefined}>
          {page && tab !== "settings" && (tab !== "status" || page.Pending || page.ConfigError) && (
            <Card className="config-card">
              <div className="config-row">
                <Button
                  type="primary"
                  loading={working}
                  disabled={!page.Pending || !!page.ConfigError}
                  onClick={() => safeRun("apply")}
                >
                  Применить
                </Button>
                <Alert
                  showIcon
                  type={
                    page.ConfigError
                      ? "error"
                      : page.Pending
                        ? "warning"
                        : "success"
                  }
                  title={
                    page.ConfigError ||
                    (page.Pending
                      ? "Примените сохранённые изменения. Xray будет перезапущен."
                      : "Конфигурация актуальна. Сохранённые настройки применены к Xray.")
                  }
                />
              </div>
            </Card>
          )}
          {notice && (
            <Alert
              className="operation-alert"
              showIcon
              closable
              type={notice.type}
              title={notice.text}
              onClose={() => setNotice(null)}
            />
          )}
          <Spin spinning={working && !operation}>
            {error ? (
              <Alert
                type="error"
                title={error}
                action={
                  <Button
                    onClick={() =>
                      void reload().catch((e) => setError(e.message))
                    }
                  >
                    Повторить
                  </Button>
                }
              />
            ) : !page ? (
              <div className="loading">
                <Spin />
              </div>
            ) : (tab === "routing" || tab === "devices") ? (
              <Routing p={page} run={run} reload={reload} developerImport={developerImport} onDeveloperImportOpened={developerImportOpened} />
            ) : tab === "subscriptions" ? (
              <Subscriptions p={page} run={run} reload={reload} />
            ) : tab === "status" ? (
              <Dashboard
                working={working}
                p={page}
                run={run}
                navigate={navigate}
                reload={reload}
              />

            ) : tab === "gateway" ? (
              <Gateway p={page} run={run} />
            ) : tab === "diagnostics" ? (
              <Diagnostics p={page} run={run} working={working} view={diagnosticsView} navigate={navigate} />
            ) : (
              <Backup reload={reload} />
            )}
          </Spin>
        </Layout.Content>
      </Layout>
    </Layout>
  );
}

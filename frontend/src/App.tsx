import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Drawer,
  Input,
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
  SearchOutlined,
  MenuOutlined,
} from "@ant-design/icons";
import { action, getJSON, getPage } from "./api";
import type { Page, Values } from "./types";
import PanelSettings from "./PanelSettings";
import Routing from "./Routing";
import Subscriptions from "./Subscriptions";
import Dashboard from "./Dashboard";
import { Devices, Gateway, Diagnostics, Backup } from "./Pages";
const navigation = [
  { key: "status", label: "Главная", icon: <DashboardOutlined /> },
  {
    key: "subscriptions",
    label: "Подписки и подключения",
    icon: <SafetyOutlined />,
  },
  { key: "devices", label: "Устройства", icon: <TeamOutlined /> },
  { key: "routing", label: "Маршрутизация", icon: <SwapOutlined /> },
  { key: "gateway", label: "Сеть и DNS", icon: <ApartmentOutlined /> },
  { key: "diagnostics", label: "Диагностика", icon: <ToolOutlined /> },
  { key: "settings", label: "Настройки панели", icon: <SettingOutlined /> },
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
  const [tab, setTab] = useState(
    new URLSearchParams(location.search).get("tab") || "status",
  );
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
    [drawer, setDrawer] = useState(false),
    [search, setSearch] = useState("");
  const searchRef = useRef<React.ComponentRef<typeof Input>>(null),
    currentTab = useRef(tab);
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
      .then(setPage)
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
    const listener = () =>
      setTab(new URLSearchParams(location.search).get("tab") || "status");
    window.addEventListener("popstate", listener);
    return () => window.removeEventListener("popstate", listener);
  }, []);
  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPinned(true);
        setDrawer(window.innerWidth < 768);
        setTimeout(() => searchRef.current?.focus(), 0);
      }
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
    history.pushState(null, "", "/?tab=" + next);
    setTab(next);
    setDrawer(false);
    setSearch("");
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
  const navItems = navigation.filter((n) =>
    n.label.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
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
      {!collapsed && (
        <div className="sider-search">
          <Input
            ref={searchRef}
            size="small"
            prefix={<SearchOutlined />}
            placeholder="Поиск…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            suffix={<kbd>Ctrl K</kbd>}
          />
        </div>
      )}
      <Menu
        mode="inline"
        theme="dark"
        inlineCollapsed={collapsed}
        selectedKeys={[tab]}
        items={navItems}
        onClick={({ key }) => navigate(key)}
      />
      <div className="sider-footer">
        {collapsed ? (
          "NG"
        ) : (
          <>
            <span>{page?.PanelVersion || "NGPanel"}</span>
            <div>
              <a href="/LICENSE" target="_blank" rel="noreferrer">
                GPL-3.0
              </a>
            </div>
          </>
        )}
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
        <Layout.Content id="main-content">
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
            ) : tab === "settings" ? (
              <PanelSettings />
            ) : tab === "routing" ? (
              <Routing p={page} run={run} reload={reload} />
            ) : tab === "subscriptions" ? (
              <Subscriptions p={page} run={run} reload={reload} />
            ) : tab === "status" ? (
              <Dashboard
                p={page}
                run={run}
                navigate={navigate}
                reload={reload}
              />
            ) : tab === "devices" ? (
              <Devices p={page} run={run} />
            ) : tab === "gateway" ? (
              <Gateway p={page} run={run} />
            ) : tab === "diagnostics" ? (
              <Diagnostics p={page} run={run} />
            ) : (
              <Backup reload={reload} />
            )}
          </Spin>
        </Layout.Content>
      </Layout>
    </Layout>
  );
}

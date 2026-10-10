import { createContext, useContext, useState, useEffect, type ReactNode } from "react";
import { App as AntApp, ConfigProvider, theme } from "antd";
import ruRU from "antd/locale/ru_RU";

export const defaultAccent = "#8b5cf6";
export const palettes = [
  { color: "#c65bd6", label: "Фиолетово-розовый" },
  { color: "#8b5cf6", label: "Фиолетовый" },
  { color: "#1677ff", label: "Синий" },
  { color: "#22b89a", label: "Бирюзовый" },
  { color: "#e7a339", label: "Янтарный" },
];
const key = "ngpanel-accent";
const valid = (value: string) => /^#[0-9a-f]{6}$/i.test(value);
const ThemeContext = createContext({ accent: defaultAccent, setAccent: (_: string) => {}, scheme: "standard", setScheme: (_: string) => {} });
export const usePanelTheme = () => useContext(ThemeContext);
function ThemeVariables({ children }: { children: ReactNode }) {
  const { token } = theme.useToken();
  useEffect(() => {
    document.documentElement.style.setProperty("--ng-accent", token.colorPrimary);
    document.documentElement.style.setProperty("--ng-accent-text", token.colorPrimaryText);
  }, [token.colorPrimary, token.colorPrimaryText]);
  return children;
}
export default function PanelTheme({ children }: { children: ReactNode }) {
  const [accent, updateAccent] = useState(() => {
    try { const saved = localStorage.getItem(key); return saved && valid(saved) ? saved : defaultAccent; }
    catch { return defaultAccent; }
  });
  const [scheme, updateScheme] = useState(() => { try { return localStorage.getItem("ngpanel-scheme") === "zashboard" ? "zashboard" : "standard"; } catch { return "standard"; } });
  const setScheme = (value: string) => { updateScheme(value); try { localStorage.setItem("ngpanel-scheme",value); } catch {} };
  useEffect(() => { document.documentElement.dataset.scheme = scheme; }, [scheme]);
  const z = scheme === "zashboard";
  const primary = z ? "#605dff" : accent;
  const background = z ? "#1d232a" : "#1a1b1f";
  const container = z ? "#1d232a" : "#23252b";
  const sidebar = z ? "#191e24" : "#15161a";
  const setAccent = (color: string) => {
    if (!valid(color)) return;
    updateAccent(color);
    try { localStorage.setItem(key, color); } catch { /* Theme remains usable when storage is unavailable. */ }
  };
  return <ThemeContext.Provider value={{ accent, setAccent, scheme, setScheme }}>
    <ConfigProvider locale={ruRU} theme={{
      algorithm: theme.darkAlgorithm,
      token: { fontFamily: '"NGFlagEmoji", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif', colorPrimary: primary, colorInfo: primary, colorLink: primary, ...(z ? {colorPrimaryHover: "#7774ff", colorPrimaryActive: "#4d49df", colorText: "#ecf9ff", colorTextSecondary: "#a6adbb", colorSuccess: "#00d390", colorBorder: "#3c4653"} : {}),
        colorBgBase: background, colorBgLayout: background, colorBgContainer: container, colorBgElevated: z ? "#232b35" : "#2d2f37" },
      components: {
        Layout: { bodyBg: background, siderBg: sidebar, headerBg: sidebar },
        Menu: { darkItemBg: sidebar, darkSubMenuItemBg: background, darkPopupBg: container, darkItemSelectedBg: z ? "#090b0d" : accent + "24", darkItemSelectedColor: z ? "#ecf9ff" : accent },
      },
    }}><AntApp><ThemeVariables>{children}</ThemeVariables></AntApp></ConfigProvider>
  </ThemeContext.Provider>;
}

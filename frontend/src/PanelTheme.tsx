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
const ThemeContext = createContext({ accent: defaultAccent, setAccent: (_: string) => {} });
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
  const setAccent = (color: string) => {
    if (!valid(color)) return;
    updateAccent(color);
    try { localStorage.setItem(key, color); } catch { /* Theme remains usable when storage is unavailable. */ }
  };
  return <ThemeContext.Provider value={{ accent, setAccent }}>
    <ConfigProvider locale={ruRU} theme={{
      algorithm: theme.darkAlgorithm,
      token: { fontFamily: '"NGFlagEmoji", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif', colorPrimary: accent, colorInfo: accent, colorLink: accent,
        colorBgBase: "#1a1b1f", colorBgLayout: "#1a1b1f", colorBgContainer: "#23252b", colorBgElevated: "#2d2f37" },
      components: {
        Layout: { bodyBg: "#1a1b1f", siderBg: "#15161a", headerBg: "#15161a" },
        Menu: { darkItemBg: "#15161a", darkSubMenuItemBg: "#1a1b1f", darkPopupBg: "#23252b", darkItemSelectedBg: accent + "24", darkItemSelectedColor: accent },
      },
    }}><AntApp><ThemeVariables>{children}</ThemeVariables></AntApp></ConfigProvider>
  </ThemeContext.Provider>;
}

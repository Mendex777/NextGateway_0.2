import { createContext, useContext, useState, useEffect, type ReactNode } from "react";
import { App as AntApp, ConfigProvider, theme } from "antd";
import ruRU from "antd/locale/ru_RU";

export const defaultAccent = "#615fff";
export const palettes = [
  { color: "#c65bd6", label: "Фиолетово-розовый" },
  { color: "#615fff", label: "Фиолетовый" },
  { color: "#1677ff", label: "Синий" },
  { color: "#22b89a", label: "Бирюзовый" },
  { color: "#e7a339", label: "Янтарный" },
];
export const baseThemes = [
  { id: "dark", label: "Тёмная", background: "#0a0a0a", surface: "#171717", elevated: "#202020", header: "#262626", border: "#383838", line: "#292929", text: "#fafafa", muted: "#a3a3a3", faint: "#858585", sidebar: "#171717" },
  { id: "graphite", label: "Графитовая", background: "#1a1b1f", surface: "#23252b", elevated: "#2d2f37", header: "#2b2d35", border: "#525762", line: "#363944", text: "#f0f0f2", muted: "#a1a4ad", faint: "#8b909a", sidebar: "#15161a" },
  { id: "navy", label: "Тёмно-синяя", background: "#0e1522", surface: "#172234", elevated: "#203049", header: "#223149", border: "#41536b", line: "#2d3d53", text: "#edf3fa", muted: "#a5b5c9", faint: "#899db7", sidebar: "#111c2c" },
  { id: "light", label: "Светлая", background: "#f4f5f7", surface: "#ffffff", elevated: "#ffffff", header: "#f0f1f4", border: "#c6cad1", line: "#e0e3e8", text: "#20242c", muted: "#606875", faint: "#747d8b", sidebar: "#ffffff" },
] as const;
export type BaseTheme = typeof baseThemes[number]["id"];
const themeKey = "ngpanel-base-theme";
const key = "ngpanel-accent";
const valid = (value: string) => /^#[0-9a-f]{6}$/i.test(value);
export type Density = "normal" | "compact";
const densityKey="ngpanel-density",animationKey="ngpanel-map-animation",speedKey="ngpanel-map-speeds";
const ThemeContext = createContext({ density:"normal" as Density,setDensity:(_:Density)=>{},mapAnimation:true,setMapAnimation:(_:boolean)=>{},mapSpeeds:true,setMapSpeeds:(_:boolean)=>{},resetAppearance:()=>{},accent: defaultAccent, setAccent: (_: string) => {}, baseTheme: "dark" as BaseTheme, setBaseTheme: (_: BaseTheme) => {} });
export const usePanelTheme = () => useContext(ThemeContext);
function ThemeVariables({ children, palette }: { children: ReactNode; palette: typeof baseThemes[number] }) {
  const { token } = theme.useToken();
  useEffect(() => {
    document.documentElement.style.setProperty("--ng-accent", token.colorPrimary);
    document.documentElement.style.setProperty("--ng-accent-text", token.colorPrimaryText);
    const root = document.documentElement;
    for (const [name, value] of Object.entries({ text: palette.text, success: palette.id === "light" ? "#16804a" : "#34d399", background: palette.background, surface: palette.surface, sidebar: palette.sidebar, border: palette.line, muted: palette.muted, faint: palette.faint, elevated: palette.elevated })) {
      root.style.setProperty("--ng-" + name, value);
    }
    root.style.colorScheme = palette.id === "light" ? "light" : "dark";
  }, [token.colorPrimary, token.colorPrimaryText, palette]);
  return children;
}
export default function PanelTheme({ children }: { children: ReactNode }) {
  const [baseTheme, updateBaseTheme] = useState<BaseTheme>(() => {
    try { const saved = localStorage.getItem(themeKey); return baseThemes.find((t) => t.id === saved)?.id || "dark"; }
    catch { return "dark"; }
  });
  const setBaseTheme = (value: BaseTheme) => {
    if (!baseThemes.some((t) => t.id === value)) return;
    updateBaseTheme(value);
    try { localStorage.setItem(themeKey, value); } catch { /* Keep selection for this session. */ }
  };
  const palette = baseThemes.find((t) => t.id === baseTheme)!;
  const light = baseTheme === "light";
  const [accent, updateAccent] = useState(() => {
    try { const saved = localStorage.getItem(key); return saved && valid(saved) && saved.toLowerCase() !== "#8b5cf6" ? saved : defaultAccent; }
    catch { return defaultAccent; }
  });
  const setAccent = (color: string) => {
    if (!valid(color)) return;
    updateAccent(color);
    try { localStorage.setItem(key, color); } catch { /* Theme remains usable when storage is unavailable. */ }
  };
  const [density,updateDensity]=useState<Density>(()=>{try{return localStorage.getItem(densityKey)==="compact"?"compact":"normal";}catch{return "normal";}});
  const [mapAnimation,updateMapAnimation]=useState(()=>{try{return localStorage.getItem(animationKey)!=="off";}catch{return true;}});
  const [mapSpeeds,updateMapSpeeds]=useState(()=>{try{return localStorage.getItem(speedKey)!=="off";}catch{return true;}});
  const setMapSpeeds=(value:boolean)=>{updateMapSpeeds(value);try{localStorage.setItem(speedKey,value?"on":"off");}catch{}};
  const setDensity=(value:Density)=>{if(value!=="normal"&&value!=="compact")return;updateDensity(value);try{localStorage.setItem(densityKey,value);}catch{}};
  const setMapAnimation=(value:boolean)=>{updateMapAnimation(value);try{localStorage.setItem(animationKey,value?"on":"off");}catch{}};
  const resetAppearance=()=>{setBaseTheme("dark");setAccent(defaultAccent);setDensity("normal");setMapAnimation(true);setMapSpeeds(true);};
  useEffect(()=>{document.documentElement.dataset.density=density;document.documentElement.dataset.mapAnimation=mapAnimation?"on":"off";},[density,mapAnimation]);
  return <ThemeContext.Provider value={{ accent, setAccent, baseTheme, setBaseTheme,density,setDensity,mapAnimation,setMapAnimation,mapSpeeds,setMapSpeeds,resetAppearance }}>
    <ConfigProvider locale={ruRU} theme={{
      algorithm: light ? theme.defaultAlgorithm : theme.darkAlgorithm,
      token: { fontFamily: '"NGFlagEmoji", "Geist Variable", sans-serif', colorPrimary: accent, colorInfo: accent, colorLink: accent,
        colorBgBase: palette.background, colorBgLayout: palette.background, colorBgContainer: palette.surface, colorBgElevated: palette.elevated, colorBorder: palette.border, colorBorderSecondary: palette.line, colorText: palette.text, colorTextSecondary: palette.muted, colorTextTertiary: palette.faint, borderRadius: 6 },
      components: {
        Table: { headerBg: palette.header, rowHoverBg: palette.header, borderColor: palette.line },
        Alert: { colorSuccessBg: palette.surface, colorSuccessBorder: light ? "#a7d9be" : "#164e3d", colorSuccess: light ? "#16804a" : "#34d399" },
        Layout: { bodyBg: palette.background, siderBg: palette.sidebar, headerBg: palette.sidebar },
        Menu: { darkItemBg: palette.sidebar, darkSubMenuItemBg: palette.background, darkPopupBg: palette.surface, darkItemColor: palette.text, darkItemHoverColor: palette.text, darkItemHoverBg: palette.header, darkItemSelectedBg: palette.header, darkItemSelectedColor: palette.text },
      },
    }}><AntApp><ThemeVariables palette={palette}>{children}</ThemeVariables></AntApp></ConfigProvider>
  </ThemeContext.Provider>;
}

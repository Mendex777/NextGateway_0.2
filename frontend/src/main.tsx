import React from "react";
import { createRoot } from "react-dom/client";
import { App as AntApp, ConfigProvider, theme } from "antd";
import ruRU from "antd/locale/ru_RU";
import App from "./App";
import "./style.css";
// Palette and layout calibrated against 3x-ui v3.9.0's useTheme and AppSidebar.
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={ruRU}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: {
          colorBgBase: "#1a1b1f",
          colorBgLayout: "#1a1b1f",
          colorBgContainer: "#23252b",
          colorBgElevated: "#2d2f37",
        },
        components: {
          Layout: {
            bodyBg: "#1a1b1f",
            siderBg: "#15161a",
            headerBg: "#15161a",
          },
          Menu: {
            darkItemBg: "#15161a",
            darkSubMenuItemBg: "#1a1b1f",
            darkPopupBg: "#23252b",
            darkItemSelectedBg: "#152943",
            darkItemSelectedColor: "#1677ff",
          },
        },
      }}
    >
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>,
);

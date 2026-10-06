import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    proxy: Object.fromEntries(
      [
        "/api",
        "/action",
        "/backup",
        "/balance-status",
        "/group-check-status",
        "/probe-status",
        "/operation-status",
        "/config-state",
        "/LICENSE",
        "/THIRD_PARTY_NOTICES.txt",
      ].map((path) => [path, "http://127.0.0.1:8083"]),
    ),
  },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import {readdirSync,readFileSync,writeFileSync} from "node:fs";
import {createHash} from "node:crypto";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
const frontend=fileURLToPath(new URL(".",import.meta.url));
function artifactManifest(){return {name:"ngpanel-artifact-manifest",apply:"build" as const,closeBundle(){
 const files:Record<string,string>={};
 const walk=(relative:string)=>{for(const entry of readdirSync(join(frontend,"dist",relative),{withFileTypes:true})){
  const name=relative?relative+"/"+entry.name:entry.name;
  if(entry.isSymbolicLink())throw new Error("Symlink in frontend output");
  if(entry.isDirectory())walk(name);else files[name]=createHash("sha256").update(readFileSync(join(frontend,"dist",name))).digest("hex");
 }};walk("");writeFileSync(join(frontend,"dist-manifest.json"),JSON.stringify({schema:1,files},null,2)+"\n");
}};}
export default defineConfig({
  plugins: [react(),artifactManifest()],
  build: { assetsInlineLimit: 0, outDir: "dist", emptyOutDir: true },
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

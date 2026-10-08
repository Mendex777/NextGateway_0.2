import type { ActionResult, Page, Values } from "./types";
export async function getJSON<T>(
  url: string,
  signal?: AbortSignal,
): Promise<T> {
  const r = await fetch(url, { signal, cache: "no-store" });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
export const getPage = (
  tab: string,
  query: Values = {},
  signal?: AbortSignal,
) =>
  getJSON<Page>(
    "/api/page?" +
      new URLSearchParams({
        tab: tab === "settings" ? "status" : tab,
        ...Object.fromEntries(
          Object.entries(query).map(([k, v]) => [k, String(v ?? "")]),
        ),
      }),
    signal,
  );
export async function action(
  name: string,
  tab: string,
  values: Values = {},
): Promise<ActionResult> {
  const body = new URLSearchParams({ action: name, tab });
  for (const [k, v] of Object.entries(values)) {
    if (Array.isArray(v)) v.forEach((item) => body.append(k, String(item)));
    else
      body.set(k, typeof v === "boolean" ? (v ? "1" : "0") : String(v ?? ""));
  }
  const r = await fetch("/api/action", { method: "POST", body });
  const text = await r.text();
  let result: ActionResult;
  try {
    result = JSON.parse(text);
  } catch {
    throw new Error(text || "Сервер недоступен");
  }
  if (!r.ok || !result.ok)
    throw new Error(result.message || "Не удалось выполнить действие");
  return result;
}

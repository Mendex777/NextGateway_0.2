import type { ReactNode } from "react";
import "./flags.css";

// Local assets avoid depending on the browser's emoji font.
const files = import.meta.glob("../node_modules/flag-icons/flags/4x3/*.svg", {
  eager: true, query: "?url", import: "default",
}) as Record<string, string>;
const flags = Object.fromEntries(Object.entries(files).map(([path, url]) => [path.split("/").pop()!.slice(0, -4), url]));

export function FlagText({ text = "" }: { text?: string }) {
  const parts: ReactNode[] = [];
  const pattern = /[\u{1F1E6}-\u{1F1FF}]{2}/gu;
  let start = 0;
  for (const match of text.matchAll(pattern)) {
    parts.push(text.slice(start, match.index));
    const code = Array.from(match[0], (letter) => String.fromCharCode(letter.codePointAt(0)! - 0x1f1e6 + 97)).join("");
    parts.push(flags[code] ? <img key={match.index} className="country-flag" src={flags[code]} alt={match[0]} draggable={false} /> : match[0]);
    start = match.index! + match[0].length;
  }
  parts.push(text.slice(start));
  return <>{parts}</>;
}

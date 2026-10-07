// Formatting and strings from 3x-ui v3.9.0 (GPL-3.0).
export class SizeFormatter {
  static readonly ONE_KB = 1024;
  static readonly ONE_MB = SizeFormatter.ONE_KB * 1024;
  static readonly ONE_GB = SizeFormatter.ONE_MB * 1024;
  static readonly ONE_TB = SizeFormatter.ONE_GB * 1024;
  static readonly ONE_PB = SizeFormatter.ONE_TB * 1024;

  static sizeFormat(size: number | null | undefined): string {
    if (size == null || !Number.isFinite(size) || size <= 0) return "0 B";
    if (size < SizeFormatter.ONE_KB) return size.toFixed(0) + " B";
    if (size < SizeFormatter.ONE_MB)
      return (size / SizeFormatter.ONE_KB).toFixed(2) + " KB";
    if (size < SizeFormatter.ONE_GB)
      return (size / SizeFormatter.ONE_MB).toFixed(2) + " MB";
    if (size < SizeFormatter.ONE_TB)
      return (size / SizeFormatter.ONE_GB).toFixed(2) + " GB";
    if (size < SizeFormatter.ONE_PB)
      return (size / SizeFormatter.ONE_TB).toFixed(2) + " TB";
    return (size / SizeFormatter.ONE_PB).toFixed(2) + " PB";
  }

  // Same unit ladder as sizeFormat, expressed per-second.
  static speedFormat(bps: number | null | undefined): string {
    return SizeFormatter.sizeFormat(bps) + "/s";
  }
}

export class CPUFormatter {
  static cpuSpeedFormat(speed: number): string {
    return speed > 1000
      ? (speed / 1000).toFixed(2) + " GHz"
      : speed.toFixed(2) + " MHz";
  }

  static cpuCoreFormat(cores: number): string {
    return cores === 1 ? "1 Core" : cores + " Cores";
  }
}

export const mean = (values: number[]) =>
  values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0;
export const peak = (values: number[]) => Math.max(...values, 0);
const labels: Record<string, string> = {
  "pages.index.overallSpeed": "Общая скорость передачи трафика",
  "pages.index.throughputSub": "Всего по интерфейсу",
  "pages.index.peak": "пик",
  "pages.index.upload": "Загрузка",
  "pages.index.download": "Скачать",
  "pages.index.sent": "Отправлено",
  "pages.index.received": "Получено",
  "pages.index.avgWindow": "Среднее за период",
  "pages.index.connectionCount": "Количество соединений",
  "pages.index.openSockets": "открытых сокетов",
};
export function useTranslation() {
  return { t: (key: string) => labels[key] || key };
}

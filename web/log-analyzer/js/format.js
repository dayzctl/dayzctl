/** Small formatting helpers shared by the views. */

export function fmtInt(n) {
  if (!Number.isFinite(n)) return '0';
  return Math.round(n).toLocaleString('en-US');
}

export function fmtPercent(a, b) {
  if (!b) return '—';
  return `${Math.round((a / b) * 100)}%`;
}

/** 3725 -> "1h 2m", 95 -> "1m 35s", 30 -> "30s" */
export function fmtDuration(seconds) {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) {
    const rs = s % 60;
    return rs ? `${m}m ${rs}s` : `${m}m`;
  }
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm ? `${h}h ${rm}m` : `${h}h`;
}

/** Seconds-of-day -> "18:22:31" */
export function fmtTime(tod) {
  if (!Number.isFinite(tod)) return '—';
  const s = Math.floor(tod) % 86400;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(h)}:${pad(m)}:${pad(sec)}`;
}

/** Seconds-of-day -> "18:22" */
export function fmtClock(tod) {
  return fmtTime(tod).slice(0, 5);
}

export function fmtBytes(n) {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let value = n;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  const digits = value >= 100 || i === 0 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[i]}`;
}

export function fmtDistance(meters) {
  if (!Number.isFinite(meters)) return '—';
  return `${meters.toFixed(meters < 10 ? 1 : 0)} m`;
}

export function truncate(text, max = 120) {
  const s = String(text ?? '');
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1)}…`;
}

/** Deterministic colour from a string, for per-player accents. */
export function colorFor(name) {
  let h = 0;
  for (let i = 0; i < name.length; i++) {
    h = (h * 31 + name.codePointAt(i)) % 360;
  }
  return `hsl(${h} 62% 58%)`;
}

/** Case-insensitive, diacritic-insensitive key used to merge player names. */
export function nameKey(name) {
  return String(name ?? '')
    .trim()
    .replace(/^["']|["']$/g, '')
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase();
}

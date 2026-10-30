// طبقة الاتصال بوكيل NetGuard (REST + SSE)

export type State = "ok" | "degraded" | "down";

export interface Sample {
  ts: number;
  ping_ms: number;
  ok: boolean;
}

export interface Status {
  state: State;
  uptime_sec: number;
  ping_last: number;
  ping_avg_1m: number;
  loss_1m_pct: number;
  restarts_today: number;
  actions_last_hour: number;
  router: { host: string; mode: string };
  ai: { risk: number; recommendation: string };
}

export interface AgentEvent {
  type: string;
  data: unknown;
  ts: number;
}

export interface AIEvent {
  risk: number;
  recommendation: string;
}

export interface ActionEvent {
  type: string;
  reason: string;
  status: string;
}

export interface LogEntry {
  ts: number;
  text: string;
  kind: "info" | "warn" | "danger";
}

// دعم اختياري للتوكن عبر ?token=... في عنوان الصفحة
const TOKEN = new URLSearchParams(location.search).get("token") ?? "";
const authHeaders = (): Record<string, string> =>
  TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {};

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { ...(init?.headers ?? {}), ...authHeaders() },
  });
  if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
  return (await res.json()) as T;
}

export const api = {
  status: () => req<Status>("/api/status"),
  history: (seconds: number) => req<Sample[]>(`/api/history?seconds=${seconds}`),
  action: (type: "reconnect" | "reboot") =>
    req<{ ok: boolean }>("/api/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ type }),
    }),
  eventsUrl: () =>
    `/api/events${TOKEN ? `?token=${encodeURIComponent(TOKEN)}` : ""}`,
};

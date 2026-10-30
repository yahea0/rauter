// تحديث واجهة المستخدم: البطاقات، حالة الاتصال، السجل، التنبيهات
import type { LogEntry, Status } from "./api";

const $ = <T extends HTMLElement>(id: string): T => {
  const el = document.getElementById(id);
  if (!el) throw new Error(`عنصر مفقود: #${id}`);
  return el as T;
};

export interface UIHandlers {
  onAction: (type: "reconnect" | "reboot") => void;
}

export function initUI(h: UIHandlers): void {
  $("btn-reconnect").addEventListener("click", () => {
    if (window.confirm("هل تريد إعادة تشغيل الشبكة الآن؟")) h.onAction("reconnect");
  });
  $("btn-reboot").addEventListener("click", () => {
    if (
      window.confirm(
        "إعادة تشغيل الراوتر بالكامل تستغرق نحو دقيقة. هل تريد المتابعة؟"
      )
    )
      h.onAction("reboot");
  });
}

const stateLabels: Record<string, string> = {
  ok: "متصل",
  degraded: "متقطع",
  down: "منقطع",
};

export function renderStatus(s: Status): void {
  const pill = $("status-pill");
  pill.textContent = stateLabels[s.state] ?? s.state;
  pill.className = `pill ${s.state}`;

  $("v-ping").textContent =
    s.ping_last > 0 ? `${Math.round(s.ping_last)} ms` : "—";
  $("v-avg").textContent =
    s.ping_avg_1m > 0 ? `${Math.round(s.ping_avg_1m)} ms` : "—";
  $("v-loss").textContent = `${Math.round(s.loss_1m_pct)}%`;
  $("v-uptime").textContent = fmtUptime(s.uptime_sec);
  $("v-restarts").textContent = String(s.restarts_today);
  $("v-router").textContent = `${s.router.host} — وضع ${s.router.mode}`;

  const aiEl = $("v-ai");
  if (s.ai && s.ai.risk > 0) {
    aiEl.textContent = `خطر الانقطاع: ${(s.ai.risk * 100).toFixed(0)}%`;
    aiEl.className = s.ai.risk > 0.7 ? "risk high" : "risk mid";
  } else {
    aiEl.textContent = "لا مؤشرات خطر";
    aiEl.className = "risk";
  }
}

export function fmtUptime(sec: number): string {
  if (sec <= 0) return "—";
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  if (h > 0) return `${h} ساعة ${m} دقيقة`;
  if (m > 0) return `${m} دقيقة ${s} ثانية`;
  return `${s} ثانية`;
}

export function addLog(e: LogEntry): void {
  const tb = $<HTMLTableSectionElement>("log-body");
  const tr = document.createElement("tr");
  if (e.kind === "danger") tr.className = "danger";
  else if (e.kind === "warn") tr.className = "warn";

  const tdT = document.createElement("td");
  tdT.textContent = new Date(e.ts * 1000).toLocaleTimeString("ar-EG");
  const tdM = document.createElement("td");
  tdM.textContent = e.text;

  tr.appendChild(tdT);
  tr.appendChild(tdM);
  tb.prepend(tr);
  while (tb.rows.length > 80) tb.deleteRow(-1);
}

export function toast(msg: string): void {
  const t = $("toast");
  t.textContent = msg;
  t.classList.add("show");
  window.setTimeout(() => t.classList.remove("show"), 3500);
}

// NetGuard — نقطة الدخول: تحميل الحالة، البث المباشر SSE، ربط الأزرار
import { api } from "./api";
import type { ActionEvent, AgentEvent, AIEvent, Sample, Status } from "./api";
import { addLog, initUI, renderStatus, toast } from "./ui";
import { drawPingChart } from "./chart";

const samples: Sample[] = [];
const MAX_SAMPLES = 1800; // 90 دقيقة بفاصل 3 ثوانٍ

function push(s: Sample): void {
  samples.push(s);
  if (samples.length > MAX_SAMPLES) {
    samples.splice(0, samples.length - MAX_SAMPLES);
  }
}

function stateLabel(s: string): string {
  return s === "ok" ? "متصل" : s === "degraded" ? "متقطع" : "منقطع";
}

function actionText(type: string, reason: string): string {
  const name =
    type === "reboot" ? "إعادة تشغيل الراوتر" : "إعادة تشغيل الشبكة";
  return `${name} (${reason})`;
}

async function refresh(): Promise<void> {
  try {
    renderStatus(await api.status());
  } catch {
    /* الخادم غير متاح حالياً */
  }
}

async function boot(): Promise<void> {
  const canvas = document.getElementById("ping-chart") as HTMLCanvasElement | null;
  if (!canvas) return;

  initUI({
    onAction: async (type) => {
      try {
        await api.action(type);
        toast(
          type === "reboot"
            ? "جاري إعادة تشغيل الراوتر…"
            : "جاري إعادة تشغيل الشبكة…"
        );
      } catch (err) {
        toast(`فشل الإجراء: ${(err as Error).message}`);
      }
    },
  });

  // الحالة الابتدائية + السجل التاريخي
  try {
    renderStatus(await api.status());
    (await api.history(900)).forEach(push);
    drawPingChart(canvas, samples);
  } catch (err) {
    console.error("تعذر تحميل الحالة الأولى:", err);
  }

  // البث المباشر
  const es = new EventSource(api.eventsUrl());
  es.onmessage = (ev) => {
    let msg: AgentEvent;
    try {
      msg = JSON.parse(ev.data) as AgentEvent;
    } catch {
      return;
    }
    switch (msg.type) {
      case "hello":
        renderStatus(msg.data as Status);
        break;
      case "sample": {
        const s = msg.data as Sample;
        push(s);
        drawPingChart(canvas, samples);
        break;
      }
      case "status": {
        const d = msg.data as { state: string; prev: string };
        addLog({
          ts: msg.ts,
          text: `تغيرت الحالة: ${stateLabel(d.prev)} ← ${stateLabel(d.state)}`,
          kind:
            d.state === "ok" ? "info" : d.state === "degraded" ? "warn" : "danger",
        });
        void refresh();
        break;
      }
      case "action": {
        const a = msg.data as ActionEvent;
        const failed = a.status.startsWith("فشل");
        addLog({
          ts: msg.ts,
          text: `${actionText(a.type, a.reason)} — ${a.status}`,
          kind: failed ? "danger" : "warn",
        });
        toast(`${actionText(a.type, a.reason)} — ${a.status}`);
        void refresh();
        break;
      }
      case "ai": {
        const a = msg.data as AIEvent;
        const el = document.getElementById("v-ai");
        if (el && a.risk > 0) {
          el.textContent = `خطر الانقطاع: ${(a.risk * 100).toFixed(0)}%`;
          el.className = a.risk > 0.7 ? "risk high" : "risk mid";
        }
        break;
      }
    }
  };

  // تحديث دوري احتياطي في حال توقف البث
  window.setInterval(() => void refresh(), 15000);
}

void boot();

// رسم منحنى زمن الاستجابة على Canvas — بلا أي مكتبات خارجية
import type { Sample } from "./api";

export function drawPingChart(
  canvas: HTMLCanvasElement,
  samples: Sample[],
  minutes = 15
): void {
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return;

  canvas.width = Math.round(rect.width * dpr);
  canvas.height = Math.round(rect.height * dpr);
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  ctx.scale(dpr, dpr);

  const W = rect.width;
  const H = rect.height;
  const padL = 40;
  const padR = 10;
  const padT = 12;
  const padB = 22;
  const iw = W - padL - padR;
  const ih = H - padT - padB;

  ctx.clearRect(0, 0, W, H);

  const cutoff = Date.now() / 1000 - minutes * 60;
  const pts = samples.filter((s) => s.ts >= cutoff);
  const maxV = Math.max(80, ...pts.map((p) => p.ping_ms || 0)) * 1.2;

  // خطوط الشبكة ووسوم المحور
  ctx.font = "10px system-ui, sans-serif";
  ctx.textAlign = "left";
  for (let i = 0; i <= 4; i++) {
    const v = (maxV / 4) * i;
    const y = padT + ih - (v / maxV) * ih;
    ctx.strokeStyle = "rgba(148,163,184,0.15)";
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(W - padR, y);
    ctx.stroke();
    ctx.fillStyle = "rgba(148,163,184,0.7)";
    ctx.fillText(v.toFixed(0), 6, y + 3);
  }

  if (pts.length < 2) {
    ctx.fillStyle = "rgba(148,163,184,0.6)";
    ctx.textAlign = "center";
    ctx.fillText("بانتظار البيانات…", W / 2, H / 2);
    return;
  }

  const t0 = pts[0].ts;
  const t1 = pts[pts.length - 1].ts;
  const X = (ts: number) =>
    padL + ((ts - t0) / Math.max(1, t1 - t0)) * iw;
  const Y = (v: number) => padT + ih - (v / maxV) * ih;

  // منحنى البينغ
  ctx.beginPath();
  let started = false;
  for (const p of pts) {
    if (!p.ok || p.ping_ms <= 0) continue;
    if (!started) {
      ctx.moveTo(X(p.ts), Y(p.ping_ms));
      started = true;
    } else {
      ctx.lineTo(X(p.ts), Y(p.ping_ms));
    }
  }
  ctx.strokeStyle = "#22c55e";
  ctx.lineWidth = 1.8;
  ctx.stroke();

  // تعبئة خفيفة تحت المنحنى
  if (started) {
    ctx.lineTo(X(t1), Y(0));
    ctx.lineTo(X(t0), Y(0));
    ctx.closePath();
    const grad = ctx.createLinearGradient(0, padT, 0, padT + ih);
    grad.addColorStop(0, "rgba(34,197,94,0.25)");
    grad.addColorStop(1, "rgba(34,197,94,0)");
    ctx.fillStyle = grad;
    ctx.fill();
  }

  // نقاط الانقطاع على المحور
  ctx.fillStyle = "#ef4444";
  for (const p of pts) {
    if (!p.ok) {
      ctx.fillRect(X(p.ts) - 1.5, padT + ih - 3, 3, 3);
    }
  }
}

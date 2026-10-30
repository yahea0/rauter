"""محرك التنبؤ في NetGuard — يتنبأ بانقطاع الإنترنت قبل حدوثه.

يعتمد على إحصاءات بسيطة وقابلة للتفسير بدون أي مكتبات ثقيلة:
  1. معدل فقدان الحزم في آخر دقيقة (أقوى مؤشر على شبهة الانقطاع).
  2. ميل زمن البينغ خلال آخر دقائق — ارتفاع سريع يعني تدهور الشبكة (3G ضعيف).
  3. تقلب البينغ عبر متوسط متحرك أُسّي (EWMA) — قفزات مفاجئة تعني خللاً في الخلية.

كل مؤشر يتحول إلى درجة بين 0 و1، ثم تُدمج بالأوزان: 0.55 فقدان، 0.27 اتجاه، 0.18 تقلب.
عند بلوغ الخطر عتبة معينة لعدة قراءات متتالية نوصي بإعادة اتصال استباقية
قبل أن تنقطع الشبكة تماماً — فالإعادة أثناء وجود اتصال أسرع وأكثر نجاحاً.
"""
from __future__ import annotations

import math
import time
from collections import deque
from typing import Optional


def _slope_per_minute(points: list[tuple[float, float]]) -> float:
    """ميل خط الانحدار الأصغر مربعات بوحدة (ميلي ثانية / دقيقة)."""
    n = len(points)
    if n < 5:
        return 0.0
    t0 = points[0][0]
    xs = [(t - t0) / 60.0 for t, _ in points]
    ys = [v for _, v in points]
    mean_x = sum(xs) / n
    mean_y = sum(ys) / n
    denom = sum((x - mean_x) ** 2 for x in xs)
    if denom <= 1e-9:
        return 0.0
    return sum((x - mean_x) * (y - mean_y) for x, y in zip(xs, ys)) / denom


class Predictor:
    """يحتفظ بنافذة عينات متحركة ويحسب درجة خطر الانقطاع بعد كل قياس."""

    def __init__(
        self,
        window: int = 300,
        alpha: float = 0.25,
        high_risk: float = 0.72,
        streak_needed: int = 3,
    ) -> None:
        self.samples: deque[tuple[float, Optional[float], bool, Optional[float]]] = deque(maxlen=window)
        self.alpha = alpha
        self.ewma: Optional[float] = None
        self.ewm_var = 0.0
        self.high_risk = high_risk
        self.streak_needed = streak_needed
        self.high_streak = 0
        self.risk = 0.0

    def reset(self) -> None:
        self.samples.clear()
        self.ewma = None
        self.ewm_var = 0.0
        self.high_streak = 0
        self.risk = 0.0

    def update(
        self,
        ok: bool,
        ping_ms: Optional[float] = None,
        rssi: Optional[float] = None,
        ts: Optional[float] = None,
    ) -> dict:
        now = ts if ts else time.time()
        self.samples.append((now, ping_ms if ok else None, bool(ok), rssi))
        reasons: list[str] = []

        # 1) فقدان الحزم في آخر 60 ثانية
        recent = [s for s in self.samples if now - s[0] <= 60]
        loss = (sum(1 for s in recent if not s[2]) / len(recent)) if recent else 0.0
        if loss >= 0.3:
            reasons.append(f"فقدان حزم مرتفع في آخر دقيقة: {loss:.0%}")

        # 2) اتجاه زمن البينغ (ميل الانحدار)
        pings = [(s[0], s[1]) for s in self.samples if s[1] is not None]
        slope = _slope_per_minute(pings[-120:])
        if slope >= 25:
            reasons.append(f"زمن البينغ يرتفع بسرعة: +{slope:.0f} ms/دقيقة")

        # 3) التقلب عبر EWMA
        z = 0.0
        if ping_ms is not None and ok:
            if self.ewma is None:
                self.ewma = ping_ms
            else:
                diff = ping_ms - self.ewma
                self.ewm_var = (1 - self.alpha) * (self.ewm_var + self.alpha * diff * diff)
                self.ewma += self.alpha * diff
                if self.ewm_var > 1e-9:
                    z = abs(ping_ms - self.ewma) / math.sqrt(self.ewm_var)
        if z >= 4:
            reasons.append(f"قفزة مفاجئة في زمن الاستجابة (z={z:.1f})")

        # دمج الدرجات الثلاث
        loss_r = min(loss / 0.6, 1.0)
        trend_r = min(max(slope, 0.0) / 80.0, 1.0)
        vol_r = min(z / 8.0, 1.0)
        base = 0.55 * loss_r + 0.27 * trend_r + 0.18 * vol_r
        if not ok:
            base = max(base, 0.95)
            reasons.append("الاتصال منقطع حالياً")
        self.risk = max(0.0, min(base, 1.0))

        if self.risk >= self.high_risk:
            self.high_streak += 1
        else:
            self.high_streak = 0

        recommendation = "none"
        if not ok or (self.risk >= self.high_risk and self.high_streak >= self.streak_needed):
            recommendation = "preemptive_reconnect"
            if self.high_streak >= self.streak_needed:
                reasons.append(f"خطر مرتفع متواصل لمدة {self.high_streak} قراءات متتالية")

        return {
            "risk": round(self.risk, 3),
            "recommendation": recommendation,
            "reasons": reasons,
            "stats": {
                "samples": len(self.samples),
                "loss_1m": round(loss, 3),
                "slope_ms_per_min": round(slope, 1),
                "ewma_ping": None if self.ewma is None else round(self.ewma, 1),
            },
        }

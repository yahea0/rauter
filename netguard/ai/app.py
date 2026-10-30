"""خدمة NetGuard AI — تستقبل قياسات الشبكة وتعيد درجة خطر الانقطاع.

التشغيل:
    pip install -r requirements.txt
    uvicorn app:app --host 0.0.0.0 --port 8090
"""
from typing import Optional

from fastapi import FastAPI
from pydantic import BaseModel

from predictor import Predictor

app = FastAPI(
    title="NetGuard AI",
    version="1.0.0",
    description="تنبؤ بانقطاع الإنترنت قبل حدوثه بناء على إحصاءات البينغ والفقدان",
)

predictor = Predictor()


class Metrics(BaseModel):
    ok: bool
    ping_ms: Optional[float] = None
    rssi: Optional[float] = None
    ts: Optional[float] = None


@app.get("/v1/health")
def health() -> dict:
    return {"status": "ok", "service": "netguard-ai", "samples": len(predictor.samples)}


@app.post("/v1/metrics")
def metrics(m: Metrics) -> dict:
    return predictor.update(ok=m.ok, ping_ms=m.ping_ms, rssi=m.rssi, ts=m.ts)


@app.post("/v1/reset")
def reset() -> dict:
    predictor.reset()
    return {"status": "reset"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8090)

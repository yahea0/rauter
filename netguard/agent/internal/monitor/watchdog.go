// الكلب الحارس: يفحص الاتصال كل بضع ثوانٍ، وإذا انقطع ينفذ خطة إنقاذ متدرجة:
// أولاً إعادة اتصال سريعة، ثم إعادة تشغيل كاملة للراوتر، مع فترات تهدئة وسقف
// آمن للإجراءات في الساعة، وإنقاذ استباقي اختياري عبر تنبؤ الذكاء الاصطناعي.
package monitor

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/yahea0/netguard/agent/internal/config"
    "github.com/yahea0/netguard/agent/internal/store"
)

// ActionDoer هو كل ما يلزم لتنفيذ الإجراءات على الراوتر (يحققه router.Client)
type ActionDoer interface {
    Perform(ctx context.Context, action string) error
    Mode() string
    Host() string
}

// Emitter يبث الأحداث الحية للواجهة (يحققه api.Hub)
type Emitter interface {
    Broadcast(eventType string, data any)
}

// AIClient واجهة خدمة التنبؤ (يحققه ai.Client)
type AIClient interface {
    Evaluate(ok bool, pingMS float64) (risk float64, recommendation string, err error)
}

type Watchdog struct {
    cfg     *config.Config
    doer    ActionDoer
    hist    *store.History
    emitter Emitter
    ai      AIClient

    mu            sync.Mutex
    state         string // ok | degraded | down
    upStreak      int
    downStreak    int
    escalation    int // 0 = لم نحاول بعد، 1 = جربنا level1، 2+ = level2
    lastAction    time.Time
    actionsHour   []time.Time
    since         time.Time
    restartsToday int
    day           int
    preemptStreak int
    lastPing      float64
    lastRisk      float64
    lastRec       string
}

func NewWatchdog(cfg *config.Config, doer ActionDoer, hist *store.History, emitter Emitter, ai AIClient) (*Watchdog, error) {
    p := cfg.Probes
    if len(p.ICMPTargets) == 0 && p.HTTPTarget == "" && p.DNSTarget == "" {
        return nil, fmt.Errorf("يجب ضبط فحص واحد على الأقل (icmp_targets / http_target / dns_target)")
    }
    return &Watchdog{
        cfg:     cfg,
        doer:    doer,
        hist:    hist,
        emitter: emitter,
        ai:      ai,
        state:   "ok",
        since:   time.Now(),
        day:     time.Now().Day(),
    }, nil
}

func (w *Watchdog) Run(ctx context.Context) {
    interval := time.Duration(w.cfg.IntervalSeconds) * time.Second
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    w.tick(ctx)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.tick(ctx)
        }
    }
}

func (w *Watchdog) tick(ctx context.Context) {
    p := w.cfg.Probes
    timeout := time.Duration(p.TimeoutSeconds) * time.Second

    type probeRes struct {
        ok bool
        ms float64
    }

    var wg sync.WaitGroup
    var mu sync.Mutex
    results := make([]probeRes, 0, len(p.ICMPTargets)+2)

    for _, target := range p.ICMPTargets {
        wg.Add(1)
        go func(target string) {
            defer wg.Done()
            cctx, cancel := context.WithTimeout(ctx, timeout)
            defer cancel()
            ms, err := PingThroughSystem(cctx, target, timeout)
            mu.Lock()
            defer mu.Unlock()
            results = append(results, probeRes{err == nil, ms})
        }(target)
    }

    if p.HTTPTarget != "" {
        wg.Add(1)
        go func() {
            defer wg.Done()
            cctx, cancel := context.WithTimeout(ctx, timeout)
            defer cancel()
            err := HTTPProbe(cctx, p.HTTPTarget)
            mu.Lock()
            defer mu.Unlock()
            results = append(results, probeRes{err == nil, 0})
        }()
    }

    if p.DNSTarget != "" {
        wg.Add(1)
        go func() {
            defer wg.Done()
            cctx, cancel := context.WithTimeout(ctx, timeout)
            defer cancel()
            err := DNSProbe(cctx, p.DNSTarget)
            mu.Lock()
            defer mu.Unlock()
            results = append(results, probeRes{err == nil, 0})
        }()
    }
    wg.Wait()

    okCount, ping := 0, 0.0
    for _, r := range results {
        if r.ok {
            okCount++
            if r.ms > 0 && ping == 0 {
                ping = r.ms
            }
        }
    }
    ok := okCount > 0

    sample := store.Sample{TS: time.Now().Unix(), PingMS: ping, OK: ok}
    w.hist.Add(sample)
    w.emitter.Broadcast("sample", sample)

    w.mu.Lock()
    w.lastPing = ping
    w.mu.Unlock()

    // خدمة الذكاء: تقييم مستمر + إنقاذ استباقي اختياري قبل الانقطاع
    if w.cfg.AI.Enabled && w.ai != nil {
        if risk, rec, err := w.ai.Evaluate(ok, ping); err == nil {
            w.emitter.Broadcast("ai", map[string]any{"risk": risk, "recommendation": rec})
            w.mu.Lock()
            w.lastRisk, w.lastRec = risk, rec
            fire := false
            if w.cfg.Action.PreemptiveEnabled && rec == "preemptive_reconnect" &&
                risk >= w.cfg.AI.RiskThreshold && w.state != "down" {
                w.preemptStreak++
                if w.preemptStreak >= 2 && w.reserve(time.Now()) {
                    w.preemptStreak = 0
                    fire = true
                }
            } else {
                w.preemptStreak = 0
            }
            w.mu.Unlock()
            if fire {
                w.fire("reconnect", fmt.Sprintf("تنبؤ استباقي — خطر %.0f%%", risk*100))
            }
        }
    }

    w.evaluate(ok, okCount, len(results))
}

// reserve يحجز موعد إجراء جديد ضمن فترة التهدئة والسقف الساعي.
// يجب استدعاؤه والقفل w.mu ممسوك.
func (w *Watchdog) reserve(now time.Time) bool {
    if now.Sub(w.lastAction) < time.Duration(w.cfg.Action.CooldownSeconds)*time.Second {
        return false
    }
    fresh := w.actionsHour[:0]
    for _, t := range w.actionsHour {
        if now.Sub(t) < time.Hour {
            fresh = append(fresh, t)
        }
    }
    w.actionsHour = fresh
    if len(w.actionsHour) >= w.cfg.Action.MaxPerHour {
        log.Printf("تم بلوغ السقف (%d إجراء/ساعة) — تجاهل هذه المحاولة للحماية", w.cfg.Action.MaxPerHour)
        return false
    }
    w.lastAction = now
    w.actionsHour = append(w.actionsHour, now)
    return true
}

func (w *Watchdog) evaluate(ok bool, okCount, total int) {
    w.mu.Lock()

    prev := w.state
    switch {
    case ok && okCount == total:
        w.state = "ok"
        w.downStreak = 0
        w.upStreak++
    case ok:
        w.state = "degraded"
        w.downStreak = 0
        w.upStreak++
    default:
        w.state = "down"
        w.upStreak = 0
        w.downStreak++
    }
    if w.state == "ok" {
        w.escalation = 0
        if w.upStreak == 1 {
            w.since = time.Now()
        }
    }

    now := time.Now()
    if now.Day() != w.day {
        w.day = now.Day()
        w.restartsToday = 0
    }

    action, reason := "", ""
    if w.state == "down" && w.downStreak >= w.cfg.Action.DownThreshold && w.reserve(now) {
        if w.escalation == 0 && w.cfg.Action.Level1 != "" && w.cfg.Action.Level1 != "none" {
            action, reason = w.cfg.Action.Level1, "انقطاع كامل — إعادة اتصال سريعة"
        } else {
            action, reason = w.cfg.Action.Level2, "انقطاع كامل — إعادة تشغيل الراوتر"
        }
        w.escalation++
        w.downStreak = 0 // نبدأ عدّاً جديداً بعد المحاولة
    }
    w.mu.Unlock()

    if prev != w.state {
        log.Printf("تغيرت الحالة: %s -> %s (نجاح %d/%d)", prev, w.state, okCount, total)
        w.emitter.Broadcast("status", map[string]any{"state": w.state, "prev": prev})
    }
    if action != "" {
        w.fire(action, reason)
    }
}

func (w *Watchdog) fire(action, reason string) {
    switch action {
    case "reconnect":
        if !w.cfg.Action.ReconnectEnabled {
            log.Printf("إجراء reconnect معطّل في الإعدادات — تجاهل")
            return
        }
    case "reboot":
        if !w.cfg.Action.RebootEnabled {
            log.Printf("إجراء reboot معطّل في الإعدادات — تجاهل")
            return
        }
    default:
        log.Printf("إجراء غير معروف: %s", action)
        return
    }

    w.mu.Lock()
    w.restartsToday++
    w.mu.Unlock()

    log.Printf("تنفيذ الإجراء: %s — السبب: %s", action, reason)
    w.emitter.Broadcast("action", map[string]any{"type": action, "reason": reason, "status": "جاري التنفيذ"})

    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
        defer cancel()
        if err := w.doer.Perform(ctx, action); err != nil {
            log.Printf("فشل الإجراء %s: %v", action, err)
            w.emitter.Broadcast("action", map[string]any{"type": action, "reason": reason, "status": "فشل: " + err.Error()})
        } else {
            log.Printf("اكتمل الإجراء %s بنجاح", action)
            w.emitter.Broadcast("action", map[string]any{"type": action, "reason": reason, "status": "اكتمل بنجاح"})
        }
    }()
}

// Manual ينفذ إجراءً بطلب مباشر من المستخدم — يتجاوز فترة التهدئة لكن ليس السقف الآمن
func (w *Watchdog) Manual(action string) error {
    switch action {
    case "reconnect", "reboot":
    default:
        return fmt.Errorf("إجراء غير معروف: %s (المسموح: reconnect أو reboot)", action)
    }
    if action == "reconnect" && !w.cfg.Action.ReconnectEnabled {
        return fmt.Errorf("إجراء reconnect معطّل في الإعدادات")
    }
    if action == "reboot" && !w.cfg.Action.RebootEnabled {
        return fmt.Errorf("إجراء reboot معطّل في الإعدادات")
    }

    w.mu.Lock()
    now := time.Now()
    fresh := w.actionsHour[:0]
    for _, t := range w.actionsHour {
        if now.Sub(t) < time.Hour {
            fresh = append(fresh, t)
        }
    }
    w.actionsHour = fresh
    if len(w.actionsHour) >= w.cfg.Action.MaxPerHour*2 {
        w.mu.Unlock()
        return fmt.Errorf("عدد كبير من الإجراءات خلال الساعة — انتظر قليلاً لسلامة الراوتر")
    }
    w.lastAction = now
    w.actionsHour = append(w.actionsHour, now)
    w.mu.Unlock()

    w.fire(action, "طلب يدوي من لوحة التحكم")
    return nil
}

// Snapshot حالة مختصرة لواجهة البرمجة ولوحة التحكم
func (w *Watchdog) Snapshot() map[string]any {
    w.mu.Lock()
    defer w.mu.Unlock()

    avg, loss := avgLoss(w.hist.Last(60))
    up := 0.0
    if w.state == "ok" {
        up = time.Since(w.since).Seconds()
    }
    return map[string]any{
        "state":             w.state,
        "uptime_sec":        up,
        "ping_last":         w.lastPing,
        "ping_avg_1m":       avg,
        "loss_1m_pct":       loss * 100,
        "restarts_today":    w.restartsToday,
        "actions_last_hour": len(w.actionsHour),
        "router":            map[string]string{"host": w.doer.Host(), "mode": w.doer.Mode()},
        "ai":                map[string]any{"risk": w.lastRisk, "recommendation": w.lastRec},
    }
}

func avgLoss(samples []store.Sample) (avg float64, loss float64) {
    if len(samples) == 0 {
        return 0, 0
    }
    var sum float64
    var okCount int
    for _, s := range samples {
        if s.OK {
            sum += s.PingMS
            okCount++
        }
    }
    if okCount > 0 {
        avg = sum / float64(okCount)
    }
    loss = float64(len(samples)-okCount) / float64(len(samples))
    return avg, loss
}

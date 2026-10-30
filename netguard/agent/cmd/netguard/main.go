// NetGuard — حارس الشبكة: وكيل مراقبة وإصلاح تلقائي لاتصال الراوتر.
//
// التشغيل:
//
//	./netguard -config config.json
package main

import (
    "context"
    "flag"
    "log"
    "net/http"
    "os/signal"
    "syscall"
    "time"

    "github.com/yahea0/netguard/agent/internal/ai"
    "github.com/yahea0/netguard/agent/internal/api"
    "github.com/yahea0/netguard/agent/internal/config"
    "github.com/yahea0/netguard/agent/internal/monitor"
    "github.com/yahea0/netguard/agent/internal/router"
    "github.com/yahea0/netguard/agent/internal/store"
)

func main() {
    cfgPath := flag.String("config", "config.json", "مسار ملف الإعدادات")
    flag.Parse()

    cfg, err := config.Load(*cfgPath)
    if err != nil {
        log.Fatalf("فشل تحميل الإعدادات: %v", err)
    }

    hist := store.NewHistory(cfg.History.Samples, cfg.History.File)
    hub := api.NewHub()
    rc := router.New(cfg.Router)

    var aiClient monitor.AIClient
    if cfg.AI.Enabled {
        aiClient = ai.New(cfg.AI.URL)
    }

    wd, err := monitor.NewWatchdog(cfg, rc, hist, hub, aiClient)
    if err != nil {
        log.Fatalf("خطأ في الإعدادات: %v", err)
    }

    srv := api.New(cfg.API, hub, wd, hist, func(_ context.Context, action string) error {
        return wd.Manual(action)
    })

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go wd.Run(ctx)

    httpSrv := &http.Server{Addr: cfg.API.Listen, Handler: srv.Handler()}
    go func() {
        log.Printf("NetGuard v1.0 — الواجهة: http://localhost%s | الراوتر: %s (وضع %s) | الذكاء: %v",
            cfg.API.Listen, cfg.Router.Host, cfg.Router.Mode, cfg.AI.Enabled)
        if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("خطأ في الخادم: %v", err)
        }
    }()

    <-ctx.Done()
    log.Println("إيقاف NetGuard…")
    shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = httpSrv.Shutdown(shCtx)
}

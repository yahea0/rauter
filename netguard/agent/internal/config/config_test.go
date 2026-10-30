package config

import "testing"

func TestDefaults(t *testing.T) {
    cfg := defaults()
    if cfg.IntervalSeconds != 3 {
        t.Fatalf("interval_seconds الافتراضي = %d، نريد 3", cfg.IntervalSeconds)
    }
    if len(cfg.Probes.ICMPTargets) == 0 || cfg.Probes.HTTPTarget == "" || cfg.Probes.DNSTarget == "" {
        t.Fatal("يجب أن تحتوي الافتراضيات على فحوصات icmp وhttp وdns")
    }
    if cfg.Router.Host != "192.168.0.1" {
        t.Fatalf("عنوان الراوتر الافتراضي = %s، نريد 192.168.0.1", cfg.Router.Host)
    }
    if cfg.Action.Level1 != "reconnect" || cfg.Action.Level2 != "reboot" {
        t.Fatal("خطة الإنقاذ الافتراضية: reconnect ثم reboot")
    }
}

func TestSanitizeClamps(t *testing.T) {
    cfg := defaults()
    cfg.IntervalSeconds = 0
    cfg.Action.CooldownSeconds = 1
    cfg.Action.MaxPerHour = 0
    cfg.History.Samples = 5
    sanitize(cfg)
    if cfg.IntervalSeconds != 1 || cfg.Action.CooldownSeconds != 15 ||
        cfg.Action.MaxPerHour != 1 || cfg.History.Samples != 60 {
        t.Fatalf("sanitize لم يصحح القيم: %+v", cfg)
    }
}

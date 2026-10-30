// Package config يحمّل إعدادات NetGuard من ملف JSON مع قيم افتراضية آمنة.
package config

import (
    "encoding/json"
    "fmt"
    "os"
)

type Config struct {
    IntervalSeconds int     `json:"interval_seconds"`
    Probes          Probes  `json:"probes"`
    Router          Router  `json:"router"`
    Action          Action  `json:"action"`
    AI              AI      `json:"ai"`
    API             API     `json:"api"`
    History         History `json:"history"`
}

// Probes فحوصات الاتصال بالإنترنت (يكفي نجاح واحد لنعبر الاتصال عاملاً)
type Probes struct {
    ICMPTargets    []string `json:"icmp_targets"`
    HTTPTarget     string   `json:"http_target"`
    DNSTarget      string   `json:"dns_target"`
    TimeoutSeconds int      `json:"timeout_seconds"`
}

// Router طريقة الوصول لواجهة إدارة الراوتر
type Router struct {
    Host     string `json:"host"`
    Username string `json:"username"`
    Password string `json:"password"`
    Mode     string `json:"mode"` // hnap | basic | form | custom

    HnapReconnectAction string `json:"hnap_reconnect_action"`

    BasicReconnectPath string `json:"basic_reconnect_path"`
    BasicRebootPath    string `json:"basic_reboot_path"`

    FormLoginPath     string `json:"form_login_path"`
    FormUserField     string `json:"form_user_field"`
    FormPassField     string `json:"form_pass_field"`
    FormReconnectPath string `json:"form_reconnect_path"`
    FormRebootPath    string `json:"form_reboot_path"`

    Custom Custom `json:"custom"`
}

// Custom وضع كامل المرونة: تلتقط الطلب من متصفحك وتلصقه هنا (راجع docs/ROUTER_DISCOVERY_AR.md)
type Custom struct {
    LoginURL     string `json:"login_url"`
    LoginMethod  string `json:"login_method"`
    LoginBody    string `json:"login_body"`
    ActionURL    string `json:"action_url"`
    ActionMethod string `json:"action_method"`
    ActionBody   string `json:"action_body"`
}

// Action سياسة الإنقاذ التلقائي
type Action struct {
    DownThreshold     int    `json:"down_threshold"`
    Level1            string `json:"level1"` // إجراء سريع أولاً (reconnect)
    Level2            string `json:"level2"` // الإجراء الأقوى عند فشل الأول (reboot)
    CooldownSeconds   int    `json:"cooldown_seconds"`
    MaxPerHour        int    `json:"max_per_hour"`
    ReconnectEnabled  bool   `json:"reconnect_enabled"`
    RebootEnabled     bool   `json:"reboot_enabled"`
    PreemptiveEnabled bool   `json:"preemptive_enabled"` // إنقاذ استباقي بناء على تنبؤ الذكاء
}

// AI خدمة التنبؤ بلغة Python
type AI struct {
    Enabled       bool    `json:"enabled"`
    URL           string  `json:"url"`
    RiskThreshold float64 `json:"risk_threshold"`
}

type API struct {
    Listen string `json:"listen"`
    Token  string `json:"token"` // إن تركت فارغة فالواجهة مفتوحة داخل شبكتك المحلية فقط
}

type History struct {
    File    string `json:"file"`
    Samples int    `json:"samples"`
}

// Load يقرأ الملف؛ إن لم يوجد يعمل بالإعدادات الافتراضية
func Load(path string) (*Config, error) {
    cfg := defaults()
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            applyEnv(cfg)
            return cfg, nil
        }
        return nil, err
    }
    if err := json.Unmarshal(data, cfg); err != nil {
        return nil, fmt.Errorf("خطأ في تحليل %s: %w", path, err)
    }
    sanitize(cfg)
    applyEnv(cfg)
    return cfg, nil
}

func defaults() *Config {
    return &Config{
        IntervalSeconds: 3,
        Probes: Probes{
            ICMPTargets:    []string{"8.8.8.8", "1.1.1.1"},
            HTTPTarget:     "https://www.gstatic.com/generate_204",
            DNSTarget:      "www.gstatic.com",
            TimeoutSeconds: 3,
        },
        Router: Router{
            Host:     "192.168.0.1",
            Username: "admin",
            Password: "",
            Mode:     "hnap",
            HnapReconnectAction: "ReconnectWAN",
            BasicReconnectPath:  "/reconnect.cgi",
            BasicRebootPath:     "/reboot.cgi",
            FormLoginPath:       "/login.cgi",
            FormUserField:       "username",
            FormPassField:       "password",
            FormReconnectPath:   "/reconnect.cgi",
            FormRebootPath:      "/reboot.cgi",
        },
        Action: Action{
            DownThreshold:     3,
            Level1:            "reconnect",
            Level2:            "reboot",
            CooldownSeconds:   120,
            MaxPerHour:        6,
            ReconnectEnabled:  true,
            RebootEnabled:     true,
            PreemptiveEnabled: false,
        },
        AI: AI{
            Enabled:       true,
            URL:           "http://127.0.0.1:8090",
            RiskThreshold: 0.72,
        },
        API: API{
            Listen: ":8080",
            Token:  "",
        },
        History: History{
            File:    "netguard.jsonl",
            Samples: 1800,
        },
    }
}

func sanitize(cfg *Config) {
    if cfg.IntervalSeconds < 1 {
        cfg.IntervalSeconds = 1
    }
    if cfg.Probes.TimeoutSeconds < 1 {
        cfg.Probes.TimeoutSeconds = 3
    }
    if cfg.Action.DownThreshold < 1 {
        cfg.Action.DownThreshold = 1
    }
    if cfg.Action.CooldownSeconds < 15 {
        cfg.Action.CooldownSeconds = 15
    }
    if cfg.Action.MaxPerHour < 1 {
        cfg.Action.MaxPerHour = 1
    }
    if cfg.History.Samples < 60 {
        cfg.History.Samples = 60
    }
    if cfg.AI.RiskThreshold <= 0 {
        cfg.AI.RiskThreshold = 0.72
    }
}

// applyEnv يسمح بتجاوز أهم الإعدادات عبر متغيرات البيئة (مفيد لـ Docker)
func applyEnv(cfg *Config) {
    if v := os.Getenv("NETGUARD_ROUTER_HOST"); v != "" {
        cfg.Router.Host = v
    }
    if v := os.Getenv("NETGUARD_ROUTER_PASSWORD"); v != "" {
        cfg.Router.Password = v
    }
    if v := os.Getenv("NETGUARD_AI_URL"); v != "" {
        cfg.AI.URL = v
    }
    if v := os.Getenv("NETGUARD_API_LISTEN"); v != "" {
        cfg.API.Listen = v
    }
}

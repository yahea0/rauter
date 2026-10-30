// Package router يتواصل مع واجهة إدارة الراوتر لتنفيذ إعادة الاتصال وإعادة التشغيل.
//
// يدعم 4 أوضاع تغطي أغلب أجهزة D-Link (ومنها DWR-921) وأي راوتر آخر:
//
//	hnap   — بروتوكول HNAP1 المعتمد في أجهزة D-Link الحديثة (الافتراضي)
//	basic  — HTTP Basic Auth مع مسار إجراء مباشر
//	form   — نموذج تسجيل دخول ثم مسار إجراء
//	custom — قوالب مخصصة بالكامل تلتقطها من المتصفح (docs/ROUTER_DISCOVERY_AR.md)
package router

import (
    "context"
    "crypto/hmac"
    "crypto/md5"
    "encoding/base64"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "strings"
    "time"

    "github.com/yahea0/netguard/agent/internal/config"
)

const (
    hnapPath = "/HNAP1/"
    hnapNS   = "http://purenetworks.com/HNAP1/"
)

type Client struct {
    cfg config.Router
    hc  *http.Client
}

func New(cfg config.Router) *Client {
    jar, _ := cookiejar.New(nil)
    return &Client{
        cfg: cfg,
        hc:  &http.Client{Timeout: 15 * time.Second, Jar: jar},
    }
}

// Mode وضع الاتصال الحالي (تُعرض في لوحة التحكم)
func (c *Client) Mode() string { return c.cfg.Mode }

// Host عنوان الراوتر
func (c *Client) Host() string { return c.cfg.Host }

// Perform ينفذ إجراء: "reconnect" لإعادة الاتصال بالشبكة أو "reboot" لإعادة تشغيل الراوتر
func (c *Client) Perform(ctx context.Context, action string) error {
    switch strings.ToLower(strings.TrimSpace(action)) {
    case "reconnect":
        return c.reconnect(ctx)
    case "reboot":
        return c.reboot(ctx)
    default:
        return fmt.Errorf("إجراء غير معروف: %s", action)
    }
}

func (c *Client) reconnect(ctx context.Context) error {
    switch c.cfg.Mode {
    case "basic":
        return c.basic(ctx, c.cfg.BasicReconnectPath, http.MethodPost)
    case "form":
        return c.form(ctx, c.cfg.FormReconnectPath)
    case "custom":
        return c.custom(ctx)
    case "hnap":
        // نجرب الإجراء المضبوط ثم بدائل شائعة في فيرموار D-Link
        cands := []string{c.cfg.HnapReconnectAction, "ReconnectWAN", "SetWANNotification"}
        seen := map[string]bool{}
        var last error
        for _, a := range cands {
            if a == "" || seen[a] {
                continue
            }
            seen[a] = true
            if err := c.hnapCall(ctx, a, ""); err != nil {
                last = err
                continue
            }
            return nil
        }
        if last == nil {
            last = errors.New("لا توجد إجراءات إعادة اتصال مضبوطة")
        }
        return fmt.Errorf("فشل إعادة الاتصال عبر HNAP (جرّب level1=reboot): %w", last)
    default:
        return fmt.Errorf("وضع الراوتر غير معروف: %s (المسموح: hnap/basic/form/custom)", c.cfg.Mode)
    }
}

func (c *Client) reboot(ctx context.Context) error {
    switch c.cfg.Mode {
    case "basic":
        return c.basic(ctx, c.cfg.BasicRebootPath, http.MethodGet)
    case "form":
        return c.form(ctx, c.cfg.FormRebootPath)
    case "custom":
        return c.custom(ctx)
    case "hnap":
        return c.hnapCall(ctx, "Reboot", "")
    default:
        return fmt.Errorf("وضع الراوتر غير معروف: %s", c.cfg.Mode)
    }
}

// ─── الوضع 1: HNAP ────────────────────────────────────────────────────────────

// hnapCall يسجل دخول HNAP ثم ينفذ الإجراء (مثال: Reboot / ReconnectWAN)
func (c *Client) hnapCall(ctx context.Context, action, inner string) error {
    priv, err := c.hnapLogin(ctx)
    if err != nil {
        return err
    }
    data, err := c.hnapRequest(ctx, action, inner, priv)
    if err != nil {
        return err
    }
    if strings.Contains(data, ">ERROR<") {
        return fmt.Errorf("الراوتر رفض الإجراء %s عبر HNAP (ERROR) — راجع docs/ROUTER_DISCOVERY_AR.md", action)
    }
    return nil
}

// hnapLogin يتبع مصافحة HNAP المعيارية:
//  1. Action=request → Challenge + PublicKey + Cookie
//  2. private_key    = HMAC-MD5(challenge + password, public_key)
//  3. login_password = HMAC-MD5(private_key, challenge + "JSON" + public_key)
//  4. Action=login مع HNAP_AUTH = private_key + " " + timestamp_millis
func (c *Client) hnapLogin(ctx context.Context) (string, error) {
    inner := "<Action>request</Action><Username>" + c.cfg.Username +
        "</Username><PrivateLogin>UserName</PrivateLogin>"
    data, err := c.hnapRequest(ctx, "Login", inner, "")
    if err != nil {
        return "", fmt.Errorf("لا يمكن الوصول إلى /HNAP1/ — تأكد أن وضع hnap يناسب فيرموارك: %w", err)
    }

    challenge := xmlTag(data, "Challenge")
    pubkey := xmlTag(data, "PublicKey")
    if challenge == "" || pubkey == "" {
        return "", errors.New("لم يستجب الراوتر بتحدي HNAP (Challenge/PublicKey) — على الأغلب فيرموارك لا يستخدم HNAP، استخدم وضع custom")
    }

    priv := hmacMD5(challenge+c.cfg.Password, pubkey)
    loginPass := hmacMD5(priv, challenge+"JSON"+pubkey)

    inner2 := "<Action>login</Action><Username>" + c.cfg.Username +
        "</Username><LoginPassword>" + loginPass + "</LoginPassword><PrivateLogin>UserName</PrivateLogin>"
    data2, err := c.hnapRequest(ctx, "Login", inner2, priv)
    if err != nil {
        return "", err
    }
    if !strings.Contains(data2, ">OK<") {
        return "", errors.New("فشل تسجيل دخول HNAP — تحقق من اسم المستخدم وكلمة السر")
    }
    return priv, nil
}

// hnapRequest يرسل طلب SOAP إلى /HNAP1/ ويعيد نص الاستجابة
func (c *Client) hnapRequest(ctx context.Context, action, inner, privateKey string) (string, error) {
    soap := `<?xml version="1.0" encoding="utf-8"?>` +
        `<soap:Envelope xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" ` +
        `xmlns:xsd="http://www.w3.org/2001/XMLSchema" ` +
        `xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">` +
        `<soap:Body><` + action + ` xmlns="` + hnapNS + `">` + inner +
        `</` + action + `></soap:Body></soap:Envelope>`

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.cfg.Host+hnapPath, strings.NewReader(soap))
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", "text/xml; charset=utf-8")
    req.Header.Set("SOAPAction", `"`+hnapNS+action+`"`)
    if privateKey != "" {
        req.Header.Set("HNAP_AUTH", privateKey+" "+fmt.Sprint(time.Now().UnixMilli()))
    }

    resp, err := c.hc.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
    if err != nil {
        return "", err
    }
    if resp.StatusCode >= 400 {
        return "", fmt.Errorf("HNAP %s: HTTP %d", action, resp.StatusCode)
    }
    return string(body), nil
}

// ─── الوضع 2: Basic Auth ─────────────────────────────────────────────────────

func (c *Client) basic(ctx context.Context, path, method string) error {
    if path == "" {
        path = "/reboot.cgi"
    }
    req, err := http.NewRequestWithContext(ctx, method, "http://"+c.cfg.Host+path, nil)
    if err != nil {
        return err
    }
    req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
    resp, err := c.hc.Do(req)
    if err != nil {
        return fmt.Errorf("فشل الوصول إلى %s: %w", path, err)
    }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
    if resp.StatusCode >= 400 {
        return fmt.Errorf("الراوتر رجع HTTP %d من %s", resp.StatusCode, path)
    }
    return nil
}

// ─── الوضع 3: نموذج دخول ─────────────────────────────────────────────────────

func (c *Client) form(ctx context.Context, actionPath string) error {
    base := "http://" + c.cfg.Host
    loginPath := orDefault(c.cfg.FormLoginPath, "/login.cgi")
    userField := orDefault(c.cfg.FormUserField, "username")
    passField := orDefault(c.cfg.FormPassField, "password")

    form := url.Values{}
    form.Set(userField, c.cfg.Username)
    form.Set(passField, c.cfg.Password)

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+loginPath, strings.NewReader(form.Encode()))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    resp, err := c.hc.Do(req)
    if err != nil {
        return fmt.Errorf("فشل تسجيل الدخول: %w", err)
    }
    _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
    resp.Body.Close()
    if resp.StatusCode >= 400 {
        return fmt.Errorf("تسجيل الدخول رجع HTTP %d", resp.StatusCode)
    }

    if actionPath == "" {
        actionPath = "/reboot.cgi"
    }
    req2, err := http.NewRequestWithContext(ctx, http.MethodPost, base+actionPath, nil)
    if err != nil {
        return err
    }
    resp2, err := c.hc.Do(req2)
    if err != nil {
        return fmt.Errorf("فشل تنفيذ الإجراء: %w", err)
    }
    _, _ = io.Copy(io.Discard, io.LimitReader(resp2.Body, 8192))
    resp2.Body.Close()
    if resp2.StatusCode >= 400 {
        return fmt.Errorf("الإجراء رجع HTTP %d", resp2.StatusCode)
    }
    return nil
}

// ─── الوضع 4: قوالب مخصصة ────────────────────────────────────────────────────

// custom ينفذ طلبك الخاص كما التقطته من DevTools، مع استبدال {{username}}
// و{{password}} و{{password_b64}} داخل الجسم.
func (c *Client) custom(ctx context.Context) error {
    k := c.cfg.Custom
    if k.ActionURL == "" {
        return errors.New("لم يتم ضبط router.custom.action_url — راجع docs/ROUTER_DISCOVERY_AR.md")
    }
    base := "http://" + c.cfg.Host

    if k.LoginURL != "" {
        req, err := http.NewRequestWithContext(ctx, c.method(k.LoginMethod), base+k.LoginURL, strings.NewReader(c.render(k.LoginBody)))
        if err != nil {
            return err
        }
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        resp, err := c.hc.Do(req)
        if err != nil {
            return fmt.Errorf("فشل الدخول المخصص: %w", err)
        }
        _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
        resp.Body.Close()
    }

    req, err := http.NewRequestWithContext(ctx, c.method(k.ActionMethod), base+k.ActionURL, strings.NewReader(c.render(k.ActionBody)))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    resp, err := c.hc.Do(req)
    if err != nil {
        return fmt.Errorf("فشل تنفيذ الإجراء المخصص: %w", err)
    }
    _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
    resp.Body.Close()
    if resp.StatusCode >= 400 {
        return fmt.Errorf("الإجراء المخصص رجع HTTP %d", resp.StatusCode)
    }
    return nil
}

func (c *Client) render(s string) string {
    s = strings.ReplaceAll(s, "{{username}}", c.cfg.Username)
    s = strings.ReplaceAll(s, "{{password}}", c.cfg.Password)
    s = strings.ReplaceAll(s, "{{password_b64}}", base64.StdEncoding.EncodeToString([]byte(c.cfg.Password)))
    return s
}

func (c *Client) method(m string) string {
    if strings.EqualFold(strings.TrimSpace(m), "GET") {
        return http.MethodGet
    }
    return http.MethodPost
}

// ─── أدوات مساعدة ────────────────────────────────────────────────────────────

func hmacMD5(key, msg string) string {
    m := hmac.New(md5.New, []byte(key))
    m.Write([]byte(msg))
    return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
}

// xmlTag يستخرج قيمة وسم من استجابة XML دون الاعتماد على مكتبات خارجية
func xmlTag(s, tag string) string {
    open := "<" + tag + ">"
    closing := "</" + tag + ">"
    i := strings.Index(s, open)
    if i < 0 {
        return ""
    }
    i += len(open)
    j := strings.Index(s[i:], closing)
    if j < 0 {
        return ""
    }
    return s[i : i+j]
}

func orDefault(v, def string) string {
    if v == "" {
        return def
    }
    return v
}

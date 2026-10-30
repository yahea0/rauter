// Package monitor يحتوي فحوصات الاتصال ومحرك القرار (الكلب الحارس).
package monitor

import (
    "context"
    "fmt"
    "io"
    "net"
    "net/http"
    "os/exec"
    "regexp"
    "runtime"
    "strconv"
    "time"
)

// timeRe يلتقط "time=23.4 ms" و"time<1 ms" من مخرجات ping (لينكس/ماك/ويندوز)
var timeRe = regexp.MustCompile(`(?i)time[=<]\s*([\d.]+)\s*ms`)

// PingThroughSystem يستخدم أمر ping الموجود في النظام — يعمل بدون صلاحيات
// خاصة على أي نظام، ويعيد زمن الاستجابة بالميلي ثانية.
func PingThroughSystem(ctx context.Context, host string, timeout time.Duration) (float64, error) {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "windows":
        ms := int(timeout.Milliseconds())
        if ms < 1000 {
            ms = 1000
        }
        cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.Itoa(ms), host)
    case "darwin":
        ms := int(timeout.Milliseconds())
        if ms < 1000 {
            ms = 1000
        }
        cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(ms), host)
    default: // لينكس وغيره
        secs := int(timeout.Seconds())
        if secs < 1 {
            secs = 1
        }
        cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(secs), host)
    }

    out, err := cmd.Output()
    if m := timeRe.FindSubmatch(out); m != nil {
        if ms, perr := strconv.ParseFloat(string(m[1]), 64); perr == nil {
            return ms, nil // نجحنا في الاستخراج حتى لو أرجع الأمر رمز خطأ ثانوياً
        }
    }
    if err != nil {
        return 0, fmt.Errorf("ping %s فشل: %v", host, err)
    }
    return 0, fmt.Errorf("لم نستطع تحليل زمن البينغ من مخرجات الأمر")
}

// HTTPProbe يتحقق أن الإنترنت يعمل فعلاً عبر طلب HTTP خفيف (استجابة 204 مثالية)
func HTTPProbe(ctx context.Context, target string) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
    if err != nil {
        return err
    }
    req.Header.Set("User-Agent", "NetGuard/1.0 (+prober)")
    req.Header.Set("Cache-Control", "no-cache")

    hc := &http.Client{Timeout: 5 * time.Second}
    resp, err := hc.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
    if resp.StatusCode >= 500 {
        return fmt.Errorf("HTTP %d", resp.StatusCode)
    }
    return nil
}

// DNSProbe يحرص أن تحليل الأسماء يعمل (انقطاع DNS شائع مع شبكات 3G الضعيفة)
func DNSProbe(ctx context.Context, host string) error {
    r := net.Resolver{}
    ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()
    _, err := r.LookupHost(ctx2, host)
    return err
}

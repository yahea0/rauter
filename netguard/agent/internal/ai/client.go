// Package ai عميل خدمة التنبؤ بلغة Python — يرسل القياسات ويستقبل درجة الخطر
package ai

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

type Client struct {
    url string
    hc  *http.Client
}

func New(url string) *Client {
    return &Client{url: url, hc: &http.Client{Timeout: 2 * time.Second}}
}

type reqBody struct {
    OK     bool     `json:"ok"`
    PingMS *float64 `json:"ping_ms,omitempty"`
    TS     int64    `json:"ts"`
}

type respBody struct {
    Risk           float64 `json:"risk"`
    Recommendation string  `json:"recommendation"`
}

// Evaluate يعيد درجة الخطر 0..1 والتوصية (none | preemptive_reconnect)
func (c *Client) Evaluate(ok bool, pingMS float64) (float64, string, error) {
    b := reqBody{OK: ok, TS: time.Now().Unix()}
    if pingMS > 0 {
        b.PingMS = &pingMS
    }
    buf, _ := json.Marshal(b)

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/metrics", bytes.NewReader(buf))
    if err != nil {
        return 0, "", err
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.hc.Do(req)
    if err != nil {
        return 0, "", err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return 0, "", fmt.Errorf("خدمة الذكاء رجعت HTTP %d", resp.StatusCode)
    }
    var r respBody
    if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
        return 0, "", err
    }
    return r.Risk, r.Recommendation, nil
}

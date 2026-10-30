// Package store سجل عينات دائري في الذاكرة + ملف JSONL للتحليل لاحقاً
package store

import (
    "encoding/json"
    "os"
    "sync"
    "time"
)

type Sample struct {
    TS     int64   `json:"ts"`
    PingMS float64 `json:"ping_ms"`
    OK     bool    `json:"ok"`
}

type History struct {
    mu   sync.Mutex
    buf  []Sample
    max  int
    file string
    fw   *os.File
}

func NewHistory(max int, file string) *History {
    if max < 60 {
        max = 60
    }
    return &History{buf: make([]Sample, 0, max), max: max, file: file}
}

func (h *History) Add(s Sample) {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.buf = append(h.buf, s)
    if len(h.buf) > h.max {
        h.buf = h.buf[len(h.buf)-h.max:]
    }
    if h.file == "" {
        return
    }
    if h.fw == nil {
        f, err := os.OpenFile(h.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        if err != nil {
            return // لا نعطل المراقبة بسبب مشكلة ملف
        }
        h.fw = f
    }
    if b, err := json.Marshal(s); err == nil {
        _, _ = h.fw.Write(append(b, '\n'))
    }
}

// Last يعيد العينات خلال آخر n ثانية (من الأقدم إلى الأحدث)
func (h *History) Last(seconds int64) []Sample {
    h.mu.Lock()
    defer h.mu.Unlock()

    cutoff := time.Now().Unix() - seconds
    out := make([]Sample, 0, len(h.buf))
    for _, s := range h.buf {
        if s.TS >= cutoff {
            out = append(out, s)
        }
    }
    return out
}

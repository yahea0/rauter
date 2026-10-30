// Package api خادم REST + بث SSE مباشر + تقديم لوحة التحكم المدمجة
package api

import (
    "context"
    "encoding/json"
    "net/http"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/yahea0/netguard/agent/internal/config"
    "github.com/yahea0/netguard/agent/internal/store"
    "github.com/yahea0/netguard/agent/internal/web"
)

// Hub مجمّع عملاء البث المباشر (Server-Sent Events)
type Hub struct {
    mu      sync.Mutex
    clients map[chan []byte]struct{}
}

func NewHub() *Hub {
    return &Hub{clients: make(map[chan []byte]struct{})}
}

func (h *Hub) add() chan []byte {
    ch := make(chan []byte, 64)
    h.mu.Lock()
    defer h.mu.Unlock()
    h.clients[ch] = struct{}{}
    return ch
}

func (h *Hub) remove(ch chan []byte) {
    h.mu.Lock()
    defer h.mu.Unlock()
    delete(h.clients, ch)
}

// Broadcast يرسل حدثاً لكل العملاء المتصلين (بدون حظر العملاء البطيئين)
func (h *Hub) Broadcast(eventType string, data any) {
    payload, err := json.Marshal(map[string]any{"type": eventType, "data": data, "ts": time.Now().Unix()})
    if err != nil {
        return
    }
    h.mu.Lock()
    defer h.mu.Unlock()
    for ch := range h.clients {
        select {
        case ch <- payload:
        default:
        }
    }
}

type StatusProvider interface {
    Snapshot() map[string]any
}

type HistoryProvider interface {
    Last(seconds int64) []store.Sample
}

type Server struct {
    cfg    config.API
    hub    *Hub
    status StatusProvider
    hist   HistoryProvider
    manual func(ctx context.Context, action string) error
}

func New(cfg config.API, hub *Hub, status StatusProvider, hist HistoryProvider, manual func(context.Context, string) error) *Server {
    return &Server{cfg: cfg, hub: hub, status: status, hist: hist, manual: manual}
}

func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/status", s.handleStatus)
    mux.HandleFunc("GET /api/history", s.handleHistory)
    mux.HandleFunc("POST /api/action", s.handleAction)
    mux.HandleFunc("GET /api/events", s.handleEvents)
    mux.Handle("/", web.Handler())
    return s.withCORS(mux)
}

func (s *Server) auth(r *http.Request) bool {
    if s.cfg.Token == "" {
        return true
    }
    if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") &&
        strings.TrimPrefix(h, "Bearer ") == s.cfg.Token {
        return true
    }
    return r.URL.Query().Get("token") == s.cfg.Token
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
    if !s.auth(r) {
        http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
        return
    }
    writeJSON(w, http.StatusOK, s.status.Snapshot())
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
    if !s.auth(r) {
        http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
        return
    }
    seconds := int64(900)
    if v := r.URL.Query().Get("seconds"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 86400 {
            seconds = int64(n)
        }
    }
    writeJSON(w, http.StatusOK, s.hist.Last(seconds))
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
    if !s.auth(r) {
        http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
        return
    }
    var req struct {
        Type string `json:"type"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"طلب غير صالح"}`, http.StatusBadRequest)
        return
    }
    if req.Type != "reconnect" && req.Type != "reboot" {
        http.Error(w, `{"error":"النوع يجب أن يكون reconnect أو reboot"}`, http.StatusBadRequest)
        return
    }
    if err := s.manual(r.Context(), req.Type); err != nil {
        writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleEvents بث مباشر SSE: hello ثم sample/status/action/ai لحظة بلحظة
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
    if !s.auth(r) {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    fl, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")

    ch := s.hub.add()
    defer s.hub.remove(ch)

    if b, err := json.Marshal(map[string]any{"type": "hello", "data": s.status.Snapshot(), "ts": time.Now().Unix()}); err == nil {
        _, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
        fl.Flush()
    }

    ctx := r.Context()
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-ch:
            _, _ = w.Write([]byte("data: " + string(msg) + "\n\n"))
            fl.Flush()
        case <-time.After(20 * time.Second):
            _, _ = w.Write([]byte(": keepalive\n\n"))
            fl.Flush()
        }
    }
}

func (s *Server) withCORS(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func writeJSON(w http.ResponseWriter, code int, v any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(v)
}

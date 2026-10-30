// Package web يقدّم لوحة التحكم المبنية من TypeScript (مدمجة داخل الملف التنفيذي)
package web

import (
    "embed"
    "io/fs"
    "net/http"
    "strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler يقدّم ملفات dist مع إرجاع index.html لأي مسار غير معروف (SPA)
func Handler() http.Handler {
    sub, err := fs.Sub(distFS, "dist")
    if err != nil {
        panic(err)
    }
    fileServer := http.FileServer(http.FS(sub))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        path := strings.TrimPrefix(r.URL.Path, "/")
        if path == "" {
            path = "index.html"
        }
        if _, err := fs.Stat(sub, path); err != nil {
            r.URL.Path = "/"
        }
        fileServer.ServeHTTP(w, r)
    })
}

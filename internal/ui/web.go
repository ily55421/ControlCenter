// Package ui — 内嵌首页 SPA。
package ui

import (
	"embed"
	"net/http"
)

//go:embed web/index.html
var webFS embed.FS

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "UI 资源缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

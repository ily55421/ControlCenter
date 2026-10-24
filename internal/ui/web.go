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
	// 内嵌 UI 随二进制升级而变:禁止浏览器缓存,确保窗口总是加载当前版本
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(data)
}

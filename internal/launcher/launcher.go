// Package launcher — 功能面板启动器:确保 Web UI 服务在运行并打开指定页面。
package launcher

import (
	"fmt"

	"net/http"
	"os"
	"strings"
	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/ui"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

// registryPortFile 端口记录文件。
func registryPortFile() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home + `\.controlcenter\ui.port`
	}
	return `.controlcenter\ui.port`
}

// Open 打开功能页(page 为 UI 页 ID);若 UI 服务未运行则以固定端口拉起。
func Open(page, title string) {
	port := readPort()
	if port == 0 || !alive(port) {
		port = startUI()
		if port == 0 {
			fmt.Println("UI 服务启动失败")
			return
		}
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/#%s", port, page)
	_ = win32.ShellExecuteOpen(url)
	time.Sleep(1200 * time.Millisecond)
	_ = win32.FocusWindow(title)
}

func readPort() int {
	b, err := os.ReadFile(registryPortFile())
	if err != nil {
		return 0
	}
	var p int
	fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &p)
	return p
}

func alive(port int) bool {
	c := http.Client{Timeout: 500 * time.Millisecond}
	r, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", port))
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == 200
}

// startUI 以固定端口 8765 启动 UI 服务(与 ccapp 一致)。
func startUI() int {
	w := wmi.Default
	as := appsettings.Default
	kb := keyboard.Default
	fc := fan.Default
	gpuStore := gpu.New(as, w)
	pm := powermode.New(w, as, fc, gpuStore)
	srv := ui.New(w, as, kb, fc, pm)
	port, err := srv.ServeOn(8765)
	if err != nil {
		return 0
	}
	_ = os.MkdirAll(strings.TrimSuffix(registryPortFile(), `\ui.port`), 0o755)
	_ = os.WriteFile(registryPortFile(), []byte(fmt.Sprintf("%d", port)), 0o644)
	return port
}

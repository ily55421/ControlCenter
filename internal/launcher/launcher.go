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

// WritePort 记录当前 UI 服务端口(供托盘/面板启动器复用)。
func WritePort(port int) {
	_ = os.MkdirAll(strings.TrimSuffix(registryPortFile(), `\ui.port`), 0o755)
	_ = os.WriteFile(registryPortFile(), []byte(fmt.Sprintf("%d", port)), 0o644)
}

// resolvePort 解析可用的 UI 端口:端口文件 → 默认 8765 → 新建服务。
func resolvePort() int {
	if p := readPort(); p != 0 && alive(p) {
		return p
	}
	// 端口文件缺失/陈旧:探测 ccapp 的固定端口
	if alive(8765) {
		WritePort(8765)
		return 8765
	}
	return startUI()
}

// Open 打开功能页(page 为 UI 页 ID);UI 服务未运行则以固定端口拉起。
func Open(page, title string) {
	port := resolvePort()
	if port == 0 {
		fmt.Println("UI 服务启动失败")
		return
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/#%s", port, page)
	prev := win32.WindowByTitle(title)
	if !win32.OpenAppWindow(url) {
		return
	}
	waitNewWindow(title, prev)
}

// waitNewWindow 等新出现的那个窗口再前置。
// 原来是无条件 Sleep(1200ms) 后按标题找一次。这里分两种情况提前收工:
// 新句柄出现(FindWindowW 取 Z 序最前,新窗口建好即排在最上)就前置它;
// 句柄始终不变说明 Edge 复用了同名 app 窗口(它自己已经激活该窗口),不必再等。
func waitNewWindow(title string, prev uintptr) {
	const (
		grace   = 400 * time.Millisecond // 判定"复用"的等待下限
		timeout = 1200 * time.Millisecond // 上限:与原来的固定等待同量,不会更差
	)
	start := time.Now()
	for {
		h := win32.WindowByTitle(title)
		elapsed := time.Since(start)
		switch {
		case h != 0 && h != prev:
			win32.FocusWindowHandle(h)
			return
		case h != 0 && h == prev && elapsed >= grace:
			win32.FocusWindowHandle(h)
			return
		case elapsed >= timeout:
			win32.FocusWindowHandle(h)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
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

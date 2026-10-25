// Package launcher — 功能面板启动器:把指定页带到前台。
//
// 原生 GUI 通道(取代旧"HTTP 端口文件 + URL 深链"):
//  1. 运行中的 ccapp 常驻 message-only 导航窗口 → WM_COPYDATA 下发页名,零开销直达;
//  2. 未运行则拉起 `ccapp.exe -page=<页>`,由其单实例守卫完成"新实例 → 导航 + 退出"。
package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/win32"
)

// MainWindowTitle ccapp 原生主窗口标题。
const MainWindowTitle = "Control Center"

// Open 打开功能页(page 为页 ID,兼容原 Web 深链名);UI 未运行则以深链参数拉起。
func Open(page, title string) {
	if title == "" {
		title = MainWindowTitle
	}
	p := NormalizePage(page)
	if win32.NavNotify(p) {
		win32.FocusWindow(title)
		return
	}
	exe := filepath.Join(exeDir(), "ccapp.exe")
	if _, err := os.Stat(exe); err != nil {
		fmt.Println("找不到主程序:", exe)
		return
	}
	if err := win32.StartAsExplorer(fmt.Sprintf(`"%s" -page=%s`, exe, p)); err != nil {
		fmt.Println("启动主程序失败:", err)
		return
	}
	waitWindow(title, 4*time.Second)
}

// NormalizePage 页名归一化(小写、去空白);空值回退 system。
func NormalizePage(p string) string {
	p = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(p, "#")))
	if p == "" {
		return "system"
	}
	return p
}

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// waitWindow 等标题窗口出现并前置(超时静默:窗口可能已被用户手动关掉)。
func waitWindow(title string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h := win32.WindowByTitle(title); h != 0 {
			win32.FocusWindowHandle(h)
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
}

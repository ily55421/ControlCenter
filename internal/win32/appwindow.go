// appwindow.go — 以独立 GUI 应用窗口打开 Web UI(Edge/Chrome app 模式):
// 独立任务栏图标与标题栏、无地址栏/标签页,观感等同原版原生应用窗口;
// 找不到支持 app 模式的浏览器时回退默认浏览器。
package win32

import (
	"os/exec"

	"golang.org/x/sys/windows/registry"
)

// FindAppBrowser 查找支持 app 模式的浏览器可执行文件(msedge 优先,其次 chrome)。
func FindAppBrowser() string {
	for _, name := range []string{`msedge.exe`, `chrome.exe`} {
		for _, base := range []string{
			`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + name,
			`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\` + name,
		} {
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, base, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			v, _, err := k.GetStringValue("")
			k.Close()
			if err == nil && v != "" {
				if _, err := exec.LookPath(v); err == nil || isAbs(v) {
					return v
				}
			}
		}
	}
	return ""
}

func isAbs(p string) bool { return len(p) > 2 && p[1] == ':' }

// OpenAppWindow 以独立应用窗口打开 url;返回是否使用了 app 窗口模式。
func OpenAppWindow(url string) bool {
	if exe := FindAppBrowser(); exe != "" {
		cmd := exec.Command(exe, "--app="+url, "--window-size=1280,820")
		if err := cmd.Start(); err == nil {
			return true
		}
	}
	_ = ShellExecuteOpen(url)
	return false
}

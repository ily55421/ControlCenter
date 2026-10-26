// icon.go — 应用图标(嵌入二进制,运行期解出临时 .ico 再交给 Win32 设置窗口/任务栏图标)。
package gui

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
)

// appIconICO 应用图标(取自原版 CC40 icon_style4.ico,含 256×256 帧)。
//
//go:embed app.ico
var appIconICO []byte

var (
	iconOnce sync.Once
	iconPath string
)

// extractedIconPath 把内嵌图标写到 %LOCALAPPDATA%\ControlCenter\app.ico 并返回路径。
// 用文件而非内存 HICON 的原因:LoadImage 支持 IMAGE_ICON 多帧,
// 可同时给出任务栏(大)与标题栏(小)两个尺寸,避免缩放糊。
// 写失败返回空串,调用方跳过设置图标(不阻断启动)。
func extractedIconPath() string {
	iconOnce.Do(func() {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return
		}
		dir := filepath.Join(base, "ControlCenter")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		p := filepath.Join(dir, "app.ico")
		// 已存在且长度一致则直接复用,避免每次启动都写盘
		if st, err := os.Stat(p); err == nil && st.Size() == int64(len(appIconICO)) {
			iconPath = p
			return
		}
		if err := os.WriteFile(p, appIconICO, 0o644); err != nil {
			return
		}
		iconPath = p
	})
	return iconPath
}

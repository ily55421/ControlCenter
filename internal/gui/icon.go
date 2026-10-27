// icon.go — 应用图标(嵌入二进制,运行期解出临时 .ico 再交给 Win32 设置窗口/任务栏图标)。
package gui

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

//go:embed control-center-v2.ico
var appIconICO []byte

var (
	iconOnce sync.Once
	iconPath string
)

// 内容哈希避免新版图标误用旧缓存。
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
		sum := sha256.Sum256(appIconICO)
		p := filepath.Join(dir, fmt.Sprintf("icon-%x.ico", sum[:8]))
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

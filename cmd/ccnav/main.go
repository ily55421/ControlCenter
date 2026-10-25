// ccnav — 开发辅助:向运行中的 ccapp 发送页面导航(视觉回归测试用)。
// 用法:ccnav <page>   例如 ccnav charger
package main

import (
	"os"

	"controlcenter/internal/launcher"
	"controlcenter/internal/win32"
)

func main() {
	page := "system"
	if len(os.Args) > 1 {
		page = launcher.NormalizePage(os.Args[1])
	}
	if !win32.NavNotify(page) {
		os.Exit(1)
	}
}

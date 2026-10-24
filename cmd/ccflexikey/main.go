// ccflexikey — Flexikey.appxbundle 等价入口:宏键配置(写 C:\ComboKey\profile.xml 后由驱动生效)。
package main

import (
	"fmt"
	"os"

	"controlcenter/internal/flexikey"
)

func main() {
	store, err := flexikey.Load(flexikey.DefaultPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		os.Exit(1)
	}
	p := store.Selected()
	drv := flexikey.NewDriver()
	ctl := flexikey.NewController(drv, nil)
	if err := ctl.Apply(p); err != nil {
		fmt.Fprintln(os.Stderr, "下发驱动失败(请确认 HKFltr 驱动已安装):", err)
		os.Exit(1)
	}
	fmt.Printf("Flexikey 已应用配置 %q(键盘=%v 鼠标=%v,槽位 %d 个)\n", p.Name, p.IsKbEnable, p.IsMouseEnable, len(p.Keys))
}

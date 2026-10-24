// ccpuoc — CPU_OC.appxbundle 等价入口:置 XTU 读取标志后打开 CPU 超频页。
package main

import (
	"controlcenter/internal/appsettings"
	"controlcenter/internal/launcher"
)

func main() {
	_ = appsettings.Default.WriteU8(appsettings.AddrXTUInfoRead, 1)
	launcher.Open("cpu", "CPU OverClocking")
}

// ccbattery — BatteryUtility.appxbundle 等价入口:打开电池页并打印诊断。
package main

import (
	"fmt"

	"controlcenter/internal/battery"
	"controlcenter/internal/launcher"
	"controlcenter/internal/wmi"
)

func main() {
	d := battery.Read(wmi.Default)
	fmt.Printf("循环 %d 次 · 满充 %d/%d mAh(%d%%)· 健康判定:%s\n",
		d.CycleCount, d.FullChargeCapacity, d.DesignCapacity,
		battery.CapacityPct(d), battery.Evaluate(d).String())
	launcher.Open("bat", "Battery Utility")
}

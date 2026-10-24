// wmitest — 硬件在环诊断:读取全部关键 WMI 命令并打印(仅诊断用)。
package main

import (
	"fmt"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/fan"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/smbios"
	"controlcenter/internal/wmi"
)

func main() {
	w := wmi.Default
	fmt.Printf("WMI16 (功能位图) = %#x → 电源模式:%v Flexikey:%v 键盘:%v 风扇:%v 节能:%v 电池:%v\n",
		w.GetInt(16), w.GetInt(16)&1 != 0, w.GetInt(16)&2 != 0, w.GetInt(16)&0x18 != 0,
		w.GetInt(16)&0x80 != 0, w.GetInt(16)&0x100 != 0, w.GetInt(16)&0x200 != 0)
	fmt.Printf("WMI96 = %#x  WMI122 = %#x\n", w.GetInt(96), w.GetInt(122))
	p17 := w.GetPackage(17)
	fmt.Printf("WMI17: CustomerID=%d 皮肤色=(%d,%d,%d) DCmode=%v\n",
		p17[2], p17[3], p17[4], p17[5], p17[13]&0x80 != 0)
	fc := fan.Default
	live := fc.ReadLive()
	fmt.Printf("风扇实时:CPU %d RPM / duty %d%% / %.0f℃   GPU1 %d RPM / %d%% / %.0f℃\n",
		live.Fan.RPM, live.Fan.Duty*100/255, live.Fan.Temp,
		live.GPU1.RPM, live.GPU1.Duty*100/255, live.GPU1.Temp)
	info := fc.ReadInfo()
	fmt.Printf("WMI13: FanCount=%d InitFanMode=%d 存在位=%#x\n", info.FanCount, info.InitFanMode, info.BitsExist)
	kb := keyboard.Default
	fmt.Println("键盘灯类型:", kb.Type())
	d := appsettings.Default
	fmt.Println("ClevoID:", d.GetClevoID(), " 电源模式:", d.GetPowerMode(), " 语言:", d.GetUILanguage())
	fn, win, fnw := w.QueryKeyboard()
	fmt.Printf("键盘设置(FnLock=%d WinKey=%d FnWin=%d)\n", fn, win, fnw)
	tbl := smbios.Get()
	fmt.Println("SMBIOS:", tbl.System.ManufacturerName, tbl.System.ProductName, "BIOS", tbl.BIOS.Version)
}

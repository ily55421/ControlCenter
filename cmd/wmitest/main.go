// wmitest — 硬件在环诊断:读取全部关键 WMI 命令并打印(仅诊断用,全部为读命令)。
//
// -probe=N   按两种条目步长假设转储命令 N 的原始输出,用于真机判定 docs/01 的歧义
// -scan      扫描读命令,列出输出中 count>1 的命令号(多条目响应才会暴露步长差异)
package main

import (
	"encoding/binary"
	"flag"
	"fmt"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/fan"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/smbios"
	"controlcenter/internal/wmi"
)

func main() {
	probe := flag.Int("probe", -1, "转储指定 WMI 命令的条目布局(两种步长假设)")
	scan := flag.Bool("scan", false, "列出输出为多条目(count>1)的读命令")
	flag.Parse()
	switch {
	case *probe >= 0:
		probeCommand(*probe)
		return
	case *scan:
		scanCounts()
		return
	}
	w := wmi.Default
	fmt.Printf("WMI16 (功能位图) = %#x → 电源模式:%v Flexikey:%v 键盘:%v 风扇:%v 节能:%v 电池:%v\n",
		w.GetInt(16), w.GetInt(16)&1 != 0, w.GetInt(16)&2 != 0, w.GetInt(16)&0x18 != 0,
		w.GetInt(16)&0x80 != 0, w.GetInt(16)&0x100 != 0, w.GetInt(16)&0x200 != 0)
	fmt.Printf("WMI96 = %#x  WMI122 = %#x\n", w.GetInt(96), w.GetInt(122))
	p17 := w.GetPackage(17)
	fmt.Printf("WMI17: CustomerID=%d 皮肤色=(%d,%d,%d) DCmode=%v\n",
		p17[2], p17[3], p17[4], p17[5], p17[13]&0x80 != 0)
	fc := fan.Default
	live, err := fc.ReadLive()
	if err != nil {
		fmt.Println("风扇实时读取失败:", err)
	} else {
		fmt.Printf("风扇实时:CPU %d RPM / duty %d%% / %.0f℃   GPU1 %d RPM / %d%% / %.0f℃\n",
			live.Fan.RPM, live.Fan.Duty*100/255, live.Fan.Temp,
			live.GPU1.RPM, live.GPU1.Duty*100/255, live.GPU1.Temp)
	}
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

// readCommands 是文档确认的只读命令白名单(探测用,绝不下发写命令)。
var readCommands = []int{4, 7, 12, 13, 16, 17, 39, 96, 103, 122}

// scanCounts 打印每条只读命令响应头里的条目数:只有 count>1 才能区分两种步长假设。
func scanCounts() {
	b := acpibridge.Default()
	for _, cmd := range readCommands {
		out, err := b.DebugExchange(cmd)
		if err != nil {
			fmt.Printf("WMI%-4d 错误: %v\n", cmd, err)
			continue
		}
		count := binary.LittleEndian.Uint32(out[8:12])
		fmt.Printf("WMI%-4d count=%-3d 输出 %d 字节\n", cmd, count, len(out))
		if count > 1 {
			probeLayout(cmd, out)
		}
	}
}

// probeCommand 转储单条命令的条目布局。
func probeCommand(cmd int) {
	out, err := acpibridge.Default().DebugExchange(cmd)
	if err != nil {
		fmt.Println("错误:", err)
		return
	}
	fmt.Printf("WMI%d: count=%d\n", cmd, binary.LittleEndian.Uint32(out[8:12]))
	probeLayout(cmd, out)
}

// probeLayout 按两种步长假设各走一遍,并标出首个分歧点。
// A(当前实现):size 为纯数据长,step = 4 + max(4,size)
// B(docs/01 字面):size 含 4 字节头,step = max(4,size),数据 = out[off+4 : off+size]
func probeLayout(cmd int, out []byte) {
	type row struct{ off, typ, size, dataLen uint32 }
	walk := func(strict bool) []row {
		var rows []row
		off := uint32(0xC)
		count := binary.LittleEndian.Uint32(out[8:12])
		if max := uint32((len(out) - 0xC) / 8); count > max {
			count = max
		}
		for i := uint32(0); i < count && int(off+4) <= len(out); i++ {
			typ := uint32(binary.LittleEndian.Uint16(out[off:]))
			size := uint32(binary.LittleEndian.Uint16(out[off+2:]))
			var step, dataLen uint32
			if strict {
				dataLen = size
				if dataLen > 4 {
					dataLen -= 4
				} else {
					dataLen = 0
				}
				step = size
				if step < 4 {
					step = 4
				}
			} else {
				dataLen = size
				if dataLen < 4 {
					dataLen = 4
				}
				step = 4 + dataLen
			}
			if int(off+4+dataLen) > len(out) {
				dataLen = uint32(len(out)) - off - 4
				rows = append(rows, row{off, typ, size, dataLen})
				break
			}
			rows = append(rows, row{off, typ, size, dataLen})
			off += step
		}
		return rows
	}
	a, b := walk(false), walk(true)
	fmt.Printf("  假设 A(实现:step=4+max(4,size)) 得到 %d 条\n", len(a))
	for _, r := range a {
		fmt.Printf("    @%#04x type=%d size=%#04x dataLen=%d data=% x\n", r.off, r.typ, r.size, r.dataLen, head(out, r.off+4, r.dataLen))
	}
	fmt.Printf("  假设 B(docs:size 含头,step=max(4,size)) 得到 %d 条\n", len(b))
	for _, r := range b {
		fmt.Printf("    @%#04x type=%d size=%#04x dataLen=%d data=% x\n", r.off, r.typ, r.size, r.dataLen, head(out, r.off+4, r.dataLen))
	}
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			fmt.Printf("  → 首个分歧出现在第 %d 条:A=%v B=%v\n", i, a[i], b[i])
			return
		}
	}
	if len(a) != len(b) {
		fmt.Printf("  → 条目数不同:A=%d B=%d\n", len(a), len(b))
		return
	}
	fmt.Println("  → 两种假设在本响应上完全等价(size 均为 4)")
}

func head(out []byte, off, n uint32) []byte {
	const max = 16
	if int(off) > len(out) {
		return nil
	}
	end := off + n
	if end > uint32(len(out)) {
		end = uint32(len(out))
	}
	if end-off > max {
		end = off + max
	}
	return out[off:end]
}

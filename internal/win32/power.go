// Package win32 封装电源计划 / 亮度 / 挂起等 Windows API(powrprof、user32)。
package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpowrprof          = windows.NewLazySystemDLL("powrprof.dll")
	procPowerEnumerate   = modpowrprof.NewProc("PowerEnumerate")
	procPowerReadName    = modpowrprof.NewProc("PowerReadFriendlyName")
	procPowerGetActive   = modpowrprof.NewProc("PowerGetActiveScheme")
	procPowerSetActive   = modpowrprof.NewProc("PowerSetActiveScheme")
	procPowerWriteACIdx  = modpowrprof.NewProc("PowerWriteACValueIndex")
	procPowerWriteDCIdx  = modpowrprof.NewProc("PowerWriteDCValueIndex")
)

// Windows 内置电源计划 GUID。
var (
	SchemeBalanced      = windows.GUID{Data1: 0x381b4222, Data2: 0xf694, Data3: 0x41f0, Data4: [8]byte{0x96, 0x85, 0xff, 0x5b, 0xb2, 0x60, 0xdf, 0x2e}}
	SchemeHighPerf      = windows.GUID{Data1: 0x8c5e7fda, Data2: 0xe8bf, Data3: 0x4a96, Data4: [8]byte{0x9a, 0x85, 0xa6, 0xe2, 0x3a, 0x8c, 0x63, 0x5c}}
	SchemePowerSaver    = windows.GUID{Data1: 0xa1841308, Data2: 0x3541, Data3: 0x4fab, Data4: [8]byte{0xbc, 0x81, 0xf7, 0x15, 0x56, 0xf2, 0x0b, 0x4a}}
	guidVideoSubgroup   = windows.GUID{Data1: 0x7516b95f, Data2: 0xf776, Data3: 0x4464, Data4: [8]byte{0x8c, 0x53, 0x06, 0x16, 0x7f, 0x40, 0xcc, 0x99}}
	guidVideoBrightness = windows.GUID{Data1: 0xaded5e82, Data2: 0xb909, Data3: 0x4619, Data4: [8]byte{0x99, 0x49, 0xf5, 0xd7, 0x1d, 0xac, 0x0b, 0xcb}}
)

// PowerPlan 一个电源计划。
type PowerPlan struct {
	GUID  windows.GUID
	Name  string
}

// EnumeratePowerSchemes 枚举全部电源计划。
func EnumeratePowerSchemes() []PowerPlan {
	var out []PowerPlan
	var idx uint32
	for {
		var g windows.GUID
		r, _, _ := procPowerEnumerate.Call(
			0, 0, 0,
			0x10, // ACCESS_SCHEME
			uintptr(idx),
			uintptr(unsafe.Pointer(&g)))
		if r != 0 {
			break
		}
		out = append(out, PowerPlan{GUID: g, Name: readPlanName(&g)})
		idx++
	}
	return out
}

func readPlanName(g *windows.GUID) string {
	var size uint32 = 512
	buf := make([]uint16, size)
	r, _, _ := procPowerReadName.Call(
		0, uintptr(unsafe.Pointer(g)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return g.String()
	}
	return windows.UTF16ToString(buf)
}

// GetActivePowerScheme 返回当前活动计划 GUID。
func GetActivePowerScheme() (windows.GUID, error) {
	var g *windows.GUID
	r, _, err := procPowerGetActive.Call(0, uintptr(unsafe.Pointer(&g)))
	if r != 0 {
		return windows.GUID{}, fmt.Errorf("PowerGetActiveScheme: %w", err)
	}
	return *g, nil
}

// SetActivePowerScheme 激活电源计划(等价 powercfg /setactive)。
func SetActivePowerScheme(g windows.GUID) error {
	r, _, err := procPowerSetActive.Call(0, uintptr(unsafe.Pointer(&g)))
	if r != 0 {
		return fmt.Errorf("PowerSetActiveScheme(%s): %w", g.String(), err)
	}
	return nil
}

// SetBrightness 写入屏幕亮度(AC 与 DC 一起,随后激活当前计划生效)。
func SetBrightness(level uint32) error {
	active, err := GetActivePowerScheme()
	if err != nil {
		return err
	}
	for _, proc := range []uintptr{procPowerWriteACIdx.Addr(), procPowerWriteDCIdx.Addr()} {
		r, _, e := syscall.SyscallN(proc,
			0,
			uintptr(unsafe.Pointer(&active)),
			uintptr(unsafe.Pointer(&guidVideoSubgroup)),
			uintptr(unsafe.Pointer(&guidVideoBrightness)),
			uintptr(level))
		if r != 0 {
			return fmt.Errorf("PowerWriteValueIndex(brightness=%d): %w", level, e)
		}
	}
	return SetActivePowerScheme(active)
}

// SuspendState 使系统待机/休眠。
func SuspendState(hibernate bool) error {
	modpowr, err := windows.LoadDLL("powrprof.dll")
	if err != nil {
		return err
	}
	defer modpowr.Release()
	p, err := modpowr.FindProc("SetSuspendState")
	if err != nil {
		return err
	}
	force, disableWake := 0, 0
	h := 0
	if hibernate {
		h = 1
	}
	r, _, e := p.Call(uintptr(force), uintptr(disableWake), uintptr(h))
	if r == 0 {
		return fmt.Errorf("SetSuspendState: %w", e)
	}
	return nil
}

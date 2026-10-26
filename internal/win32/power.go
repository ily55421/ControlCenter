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
	procLocalFree        = modkernel32.NewProc("LocalFree")
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
	if g == nil {
		return windows.GUID{}, fmt.Errorf("PowerGetActiveScheme: 返回空指针")
	}
	// 该 API 用 LocalAlloc 分配 GUID,调用方负责 LocalFree
	defer procLocalFree.Call(uintptr(unsafe.Pointer(g)))
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

// 盖子合上动作设置(GUID_BUTTONS / GUID_LIDCLOSE_ACTION)。
var (
	modPowerReadACIdx  = modpowrprof.NewProc("PowerReadACValueIndex")
	modPowerReadDCIdx  = modpowrprof.NewProc("PowerReadDCValueIndex")
	guidButtonsSubgroup = windows.GUID{Data1: 0x4f971e89, Data2: 0xeebd, Data3: 0x4455, Data4: [8]byte{0xa8, 0xde, 0x9e, 0x59, 0x04, 0x0e, 0x73, 0x47}}
	guidLidCloseAction  = windows.GUID{Data1: 0x5ca83367, Data2: 0x6e45, Data3: 0x459f, Data4: [8]byte{0xa2, 0x7b, 0x47, 0x6b, 0x1d, 0x01, 0xc9, 0x36}}
)

// 盖子动作值(与 Windows 电源选项一致)。
const (
	LidActionNothing    = 0
	LidActionSleep      = 1
	LidActionHibernate  = 2
	LidActionShutdown   = 3
)

// GetLidCloseAction 读当前电源计划的合盖动作(AC 与 DC 分别返回)。
// 对应原版 CallingVariations.GetLIDACTIONAC/DC。
func GetLidCloseAction() (ac, dc int, err error) {
	active, err := GetActivePowerScheme()
	if err != nil {
		return 0, 0, err
	}
	read := func(p uintptr) (int, error) {
		var v uint32
		r, _, e := syscall.SyscallN(p,
			0,
			uintptr(unsafe.Pointer(&active)),
			uintptr(unsafe.Pointer(&guidButtonsSubgroup)),
			uintptr(unsafe.Pointer(&guidLidCloseAction)),
			uintptr(unsafe.Pointer(&v)))
		if r != 0 {
			return LidActionSleep, fmt.Errorf("PowerReadValueIndex(lidclose): %w", e)
		}
		return int(v), nil
	}
	ac, err = read(modPowerReadACIdx.Addr())
	if err != nil {
		return 0, 0, err
	}
	dc, err = read(modPowerReadDCIdx.Addr())
	if err != nil {
		return 0, 0, err
	}
	return ac, dc, nil
}

var (
	modPowrProf      = windows.NewLazySystemDLL("powrprof.dll")
	procCallNtPower  = modPowrProf.NewProc("CallNtPowerInformation")
	procGetSysTimes  = modkernel32.NewProc("GetSystemTimes")
	procSysInfoAlloc = modkernel32.NewProc("GetProcessHeap")
)

type processorPowerInfo struct {
	Number           uint32
	MaxMhz           uint32
	CurrentMhz       uint32
	MhzLimit         uint32
	MaxIdleState     uint32
	CurrentIdleState uint32
}

var (
	cpuTimesLast    [3]uint64 // idle, kernel, user
	cpuTimesStarted bool
	cpuLastPct      = -1 // 最近一次有效利用率;间隔过短时沿用它
)

// init 建立 CPU 利用率采样基线:否则进程内首次调用返回 -1,
// 界面首帧会把利用率显示为 0%(看起来像"没读到数据")。
func init() { CPULoadPercent() }

// CPULoadPercent 采样系统 CPU 利用率(内部保存上次 GetSystemTimes 快照,
// 返回与上次的差分;首次调用返回 -1 表示尚无基线)。
func CPULoadPercent() int {
	var idle, kernel, user uint64
	r, _, _ := procGetSysTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return -1
	}
	if !cpuTimesStarted {
		cpuTimesLast = [3]uint64{idle, kernel, user}
		cpuTimesStarted = true
		return -1
	}
	idleDiff := int64(idle) - int64(cpuTimesLast[0])
	totalDiff := (int64(kernel) - int64(cpuTimesLast[1])) + (int64(user) - int64(cpuTimesLast[2]))
	cpuTimesLast = [3]uint64{idle, kernel, user}
	if totalDiff <= 0 {
		// 采样间隔过短(两次调用之间计数器未推进)时不要返回 -1:
		// 上层会把 -1 当作无效值,界面利用率掉回 0%(实测 0ms 间隔必现)。
		// 此时保留上次结果;尚无结果则报 0%。
		if cpuLastPct >= 0 {
			return cpuLastPct
		}
		return 0
	}
	pct := 100 - int(idleDiff*100/totalDiff)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	cpuLastPct = pct
	return pct
}

// CPUFrequencyMHz 返回各逻辑核当前频率的平均值(CallNtPowerInformation/ProcessorInformation)。
// 失败返回 -1。
func CPUFrequencyMHz() int {
	buf := make([]processorPowerInfo, 64) // 32 线程 × 余量;不足会 STATUS_BUFFER_TOO_SMALL
	r, _, _ := procCallNtPower.Call(
		11, // ProcessorInformation
		0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Sizeof(buf[0]))*uintptr(len(buf)))
	if r != 0 {
		return -1
	}
	sum, cnt := 0, 0
	for _, p := range buf {
		if p.CurrentMhz == 0 {
			break
		}
		sum += int(p.CurrentMhz)
		cnt++
	}
	if cnt == 0 {
		return -1
	}
	return sum / cnt
}

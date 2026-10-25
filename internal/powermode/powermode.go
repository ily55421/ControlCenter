// Package powermode 电源模式引擎(复刻 FnKey ChangePowerMode 全序列)。
package powermode

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

// SupportBits 能力位(从 WMI16/96/122 与 BIOS 特性表解析)。
type SupportBits struct {
	PowerMode        bool
	Quiet            bool
	PowerSaving      bool
	Performance      bool
	Entertainment    bool
	QuietPlusOnly    bool // ByClevoDefaultNotSupportPowerSavingMode
	PowerModeUIID    int
	DTT              bool
	AMDRyzen         bool
	CPUOC            bool
	TurboFan         bool
	SlowFan          bool
	MSHybridSwitch   bool
	SimpleGPUOC      bool
	NVIDIAPowerOff   bool
	FeatureListVer   int
	FanLess          bool
	AntiDust         bool
	NoFanOffset      bool
	RPMFanControl    bool
	HW_PState        bool
	SetPowerPlan     bool
	ASPMControl      bool
	DisablePCIEASPM  bool
	LowRefresh       bool
}

// Detect 能力探测(GetWMI 122/16/96 + BIOS 特性表镜像 page7)。
func Detect(w *wmi.Client, as *appsettings.Store) SupportBits {
	var s SupportBits
	w16 := w.GetInt(16)
	w96 := w.GetInt(96)
	w122 := w.GetInt(122)

	// 优先 BIOS 特性表(WMI4 sub8 → page7 镜像)
	page7 := as.ReadAt(7, 0, 256)
	ver := int(uint16(page7[0])<<8 | uint16(page7[1]))
	s.FeatureListVer = ver
	if ver == 0x100 {
		g16 := int(uint16(page7[13]) | uint16(page7[14])<<8)
		g96 := int(uint16(page7[10]) | uint16(page7[11])<<8)
		g122 := int(uint32(page7[6]) | uint32(page7[7])<<8 | uint32(page7[8])<<16 | uint32(page7[9])<<24)
		w16, w96, w122 = g16, g96, g122
		if page7[16]&(1<<4) != 0 {
			s.TurboFan = true
		}
		if page7[17]&(1<<7) != 0 {
			s.QuietPlusOnly = true
			s.Quiet = false
		}
		s.PowerModeUIID = int(page7[17]>>4) & 0xF
		if page7[18]&1 != 0 {
			s.MSHybridSwitch = true
		}
		if page7[19]&(1<<6) != 0 {
			s.NVIDIAPowerOff = true
		}
		if page7[20]&1 != 0 {
			s.SlowFan = true
		}
		if page7[20]&(1<<3) != 0 {
			s.SimpleGPUOC = true
		}
		if page7[20]&(1<<4) != 0 {
			s.RPMFanControl = true
		}
	}

	s.PowerMode = w16&1 == 1
	s.Quiet = w16&1 == 1
	s.PowerSaving = true
	s.Performance = true
	s.Entertainment = true
	if w122&(1<<23) != 0 {
		s.CPUOC = true
	}
	if w122&(1<<15) != 0 {
		s.FanLess = true
	}
	if w96&(1<<12) != 0 {
		s.DTT = true
	}
	if w96&(1<<13) != 0 {
		s.AMDRyzen = true
	}
	if w96&(1<<7) != 0 {
		s.AntiDust = true
	}
	if w96&(1<<10) != 0 {
		s.NoFanOffset = true
	}
	// page7 v1.0 模式位
	if ver == 0x100 {
		lo := page7[17] & 0xF
		s.Quiet = lo&1 != 0
		s.PowerSaving = lo&(1<<1) != 0
		s.Performance = lo&(1<<2) != 0
		s.Entertainment = lo&(1<<3) != 0
		s.PowerMode = lo != 0
		if s.QuietPlusOnly {
			s.Quiet = false
		}
	}
	return s
}

// ASPM 各模式 PCIe ASPM 表(WMI4 sub15)。
type aspmTable struct{ ac, dc [4]int } // [quiet ps perf ent]

// Engine 电源模式引擎。
type Engine struct {
	w   *wmi.Client
	as  *appsettings.Store
	fan *fan.Controller
	gpuStore *gpu.Store
	Sup  SupportBits

	mu        sync.Mutex
	ongoing   bool
	nextMode  appsettings.ModeType
	onChanged []func(from, to appsettings.ModeType)

	// ControlGPU.exe 路径(GPU 联动);空则跳过 GPU 联动。
	ControlGPUPath string
}

// New 创建引擎。
func New(w *wmi.Client, as *appsettings.Store, f *fan.Controller, g *gpu.Store) *Engine {
	e := &Engine{w: w, as: as, fan: f, gpuStore: g}
	e.Sup = Detect(w, as)
	return e
}

// OnChanged 注册切换完成回调。
func (e *Engine) OnChanged(fn func(from, to appsettings.ModeType)) {
	e.mu.Lock()
	e.onChanged = append(e.onChanged, fn)
	e.mu.Unlock()
}

// NextModeFor Fn 键循环链。
func (e *Engine) NextModeFor(cur appsettings.ModeType) appsettings.ModeType {
	has := func(b bool) bool { return b }
	switch cur {
	case appsettings.Quiet:
		if has(e.Sup.Performance) {
			return appsettings.Performance
		}
		if has(e.Sup.Entertainment) {
			return appsettings.Entertainment
		}
		if has(e.Sup.PowerSaving) {
			return appsettings.PowerSaving
		}
		return appsettings.Performance
	case appsettings.PowerSaving:
		if has(e.Sup.Quiet) {
			return appsettings.Quiet
		}
		if has(e.Sup.Performance) {
			return appsettings.Performance
		}
		if has(e.Sup.Entertainment) {
			return appsettings.Entertainment
		}
		return appsettings.Quiet
	case appsettings.Performance:
		if has(e.Sup.Entertainment) {
			return appsettings.Entertainment
		}
		if has(e.Sup.PowerSaving) {
			return appsettings.PowerSaving
		}
		if has(e.Sup.QuietPlusOnly) {
			return appsettings.QuietPlus
		}
		if has(e.Sup.Quiet) {
			return appsettings.Quiet
		}
		return appsettings.Entertainment
	case appsettings.Entertainment:
		if has(e.Sup.QuietPlusOnly) {
			return appsettings.QuietPlus
		}
		if has(e.Sup.PowerSaving) {
			return appsettings.PowerSaving
		}
		if has(e.Sup.Quiet) {
			return appsettings.Quiet
		}
		if has(e.Sup.Performance) {
			return appsettings.Performance
		}
		return appsettings.PowerSaving
	default: // QuietPlus
		if has(e.Sup.Performance) {
			return appsettings.Performance
		}
		if has(e.Sup.Entertainment) {
			return appsettings.Entertainment
		}
		return appsettings.Performance
	}
}

// setPowerMode 核心下发:写 (1,1) + WMI(121,25,data)。
func (e *Engine) setPowerMode(m appsettings.ModeType) error {
	if err := e.as.SetPowerModeOnly(m); err != nil {
		return fmt.Errorf("写模式字节: %w", err)
	}
	var d uint32
	switch m {
	case appsettings.Performance:
		d = 2
		if e.as.ReadU8(appsettings.AddrDTT) == 1 {
			if isDTTDriverInstalled() {
				d |= 0x80
			} else {
				_ = e.as.WriteU8(appsettings.AddrDTT, 0)
			}
		}
		if e.as.ReadU8(appsettings.AddrTurboFan) == 1 {
			d |= 0x40
		}
	case appsettings.QuietPlus:
		d = 0
	default:
		d = uint32(m)
	}
	if err := e.w.SetWMI(121, 25, d); err != nil {
		return fmt.Errorf("WMI(121,25,%d): %w", d, err)
	}
	return nil
}

func isDTTDriverInstalled() bool {
	for _, p := range win32.ListProcesses() {
		if p.Name == "intel_dptf_service.exe" || p.Name == "esif_utop.exe" {
			return true
		}
	}
	return win32.ServiceExists("esifsvc") || win32.ServiceExists("intel_dptf_service")
}

// Change 完整切换序列(阻塞执行)。
// 逐步记录失败并以 errors.Join 返回;核心下发失败时把模式字节回滚为切换前的值并中止,
// 避免模式字节与 EC 实际状态长期不一致。EC 侧无撤销命令,后续联动步骤失败只能上报不能回退。
func (e *Engine) Change(m appsettings.ModeType) error {
	e.mu.Lock()
	if e.ongoing {
		e.mu.Unlock()
		err := fmt.Errorf("powermode: 上一次切换仍在进行,忽略切到 %d 的请求", int(m))
		log.Print(err.Error())
		return err
	}
	e.ongoing = true
	callbacks := append([]func(appsettings.ModeType, appsettings.ModeType){}, e.onChanged...)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.ongoing = false
		e.mu.Unlock()
	}()

	pre := e.as.GetPowerMode()
	var fails []error
	fail := func(step string, err error) {
		if err == nil {
			return
		}
		log.Printf("powermode: 步骤 %s 失败: %v", step, err)
		fails = append(fails, fmt.Errorf("%s: %w", step, err))
	}

	// 1. NV 独显上电联动
	if e.Sup.NVIDIAPowerOff && (m == appsettings.Performance || m == appsettings.Entertainment) {
		if e.gpuStore != nil {
			if _, _, ready, _ := e.gpuStore.Info(); !ready {
				fail("NV 上电", eventbus.ExecCommand(`C:\Program Files (x86)\ControlCenter\DCHU\cc30wk.exe 0x0013 1`))
				sleep(1000)
			}
		}
	}

	// 2. 核心下发(失败即中止:后续联动都以它为前提)
	if err := e.setPowerMode(m); err != nil {
		log.Printf("powermode: 核心下发失败,回滚模式字节 %d→%d", int(m), int(pre))
		if rbErr := e.as.SetPowerModeOnly(pre); rbErr != nil {
			fail("核心下发(回滚也失败)", fmt.Errorf("%v;回滚: %w", err, rbErr))
		} else {
			fail("核心下发", err)
		}
		return errors.Join(fails...)
	}

	// 3. GPU 超频/限频联动
	if e.gpuStore != nil && e.ControlGPUPath != "" {
		fail("GPU 联动", e.gpuLink(m, pre))
	}

	// 4. EnergyStar
	if !e.Sup.HW_PState {
		es := 1
		switch m {
		case appsettings.Quiet, appsettings.Entertainment:
			es = 2
		case appsettings.Performance:
			es = 0
		}
		fail("EnergyStar", e.w.SetEnergyStar(es))
	}

	// 5. Windows 电源计划(per-mode 表 0 Balanced/1 HighPerf/2 PowerSaver)
	fail("电源计划", e.applyPowerPlan(m))

	// 6. 风扇联动
	if e.fan != nil {
		e.fan.LoadModeForPowerMode(int(m))
	}

	// 7. NV 断电(quiet_plus / pwrsaving 且支持)
	if e.Sup.NVIDIAPowerOff && m == appsettings.QuietPlus {
		fail("NV 断电", eventbus.ExecCommand(`C:\Program Files (x86)\ControlCenter\DCHU\cc30wk.exe 0x0013 0`))
	}

	// 8. 广播完成事件(×2,与原版一致)
	fail("广播完成事件", eventbus.WriteOut("clevofnkey^202"))
	_ = eventbus.WriteOut("clevofnkey^202")

	for _, fn := range callbacks {
		fn(pre, m)
	}
	return errors.Join(fails...)
}

// applyPowerPlan WMI4 sub29 电源计划表 → PowerSetActiveScheme。
func (e *Engine) applyPowerPlan(m appsettings.ModeType) error {
	in := make([]byte, 256)
	in[0] = 29
	out := e.w.SetWMIPackageEx(4, in)
	var plan int
	switch m {
	case appsettings.Quiet, appsettings.QuietPlus:
		plan = int(out[3])
	case appsettings.PowerSaving:
		plan = int(out[4])
	case appsettings.Performance:
		plan = int(out[1])
	case appsettings.Entertainment:
		plan = int(out[2])
	}
	var g = win32.SchemeBalanced
	switch plan {
	case 1:
		g = win32.SchemeHighPerf
	case 2:
		g = win32.SchemePowerSaver
	}
	if err := win32.SetActivePowerScheme(g); err != nil {
		log.Printf("powermode: SetActivePowerScheme(%s): %v", g.String(), err)
		return err
	}
	return nil
}

// gpuLink GPU 限频/超频联动(Load_GPU_forFn_3)。
func (e *Engine) gpuLink(m appsettings.ModeType, pre appsettings.ModeType) error {
	ac := win32.ACOnline()
	var fails []error
	run := func(arg string) {
		if err := eventbus.ExecProcess(e.ControlGPUPath, arg); err != nil {
			fails = append(fails, fmt.Errorf("ControlGPU %s: %w", arg, err))
		}
	}
	switch m {
	case appsettings.Quiet:
		run("set_increment:0:0:0")
	case appsettings.PowerSaving:
		run("set_increment:0:0:0")
	case appsettings.Performance:
		if ac {
			g0, g1 := e.gpuStore.Saved()
			run(fmtSet(0, int(g0.Core), int(g0.Mem)))
			run(fmtSet(1, int(g1.Core), int(g1.Mem)))
		} else {
			run("set_increment:0:0:0")
		}
	case appsettings.Entertainment:
		if e.Sup.SimpleGPUOC || e.Sup.Performance {
			g0, _ := e.gpuStore.Saved()
			run(fmtSet(0, int(g0.Core), int(g0.Mem)))
		}
	}
	return errors.Join(fails...)
}

func fmtSet(idx, core, mem int) string {
	return "set_increment:" + itoa(idx) + ":" + itoa(core) + ":" + itoa(mem)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func sleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

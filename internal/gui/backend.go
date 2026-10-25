// Package gui — ControlCenter 的 Windows 原生 GUI(Direct3D11 硬件渲染,纯 Go)。
// backend 直连 internal 各引擎层,取代原 Web 方案的 HTTP+JSON 轮询:
// 读取是普通函数调用,写入是普通函数调用,不再有序列化和 127.0.0.1 往返。
package gui

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/battery"
	"controlcenter/internal/bluelight"
	"controlcenter/internal/cpuoc"
	"controlcenter/internal/energysave"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/flexikey"
	"controlcenter/internal/gpu"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/smbios"
	"controlcenter/internal/wmi"
)

// Backend 全部功能面板的后端。字段与原 ui.Server 一致,方法语义与原 JSON API 一致。
type Backend struct {
	w  *wmi.Client
	as *appsettings.Store
	kb *keyboard.Controller
	fc *fan.Controller
	pm *powermode.Engine

	GPUOCBase string // ControlGPU.exe 所在目录
	bootStamp string

	platOnce sync.Mutex
	plat     *platform

	mu       sync.Mutex
	sampler  *energysave.Sampler
	batState *batteryState
}

type batteryState struct {
	Ongoing battery.Ongoing
	Started time.Time
}

// platform 运行期不会变的平台信息(开机时由 BIOS 决定),取一次缓存。
// 每项都要付一次 256B WMI 包读(真机实测 ~80ms/次)。
type platform struct {
	tbl        *smbios.Table
	customerID int
	sup        powermode.SupportBits
	kbType     keyboard.KBType
}

func (b *Backend) platformInfo() *platform {
	b.platOnce.Lock()
	defer b.platOnce.Unlock()
	if b.plat == nil {
		b.plat = &platform{
			tbl:        smbios.Get(),
			customerID: int(b.w.GetPackage(17)[2]),
			sup:        powermode.Detect(b.w, b.as),
			kbType:     b.kb.Type(),
		}
	}
	return b.plat
}

// NewBackend 组装后端(依赖与原 ccapp/ui.New 相同)。
func NewBackend(w *wmi.Client, as *appsettings.Store, kb *keyboard.Controller, fc *fan.Controller, pm *powermode.Engine) *Backend {
	return &Backend{
		w: w, as: as, kb: kb, fc: fc, pm: pm,
		bootStamp: strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

// ---------- 状态与支持位 ----------

// SupportBits 主页磁贴支持位(原 /api/status "support")。
type SupportBits struct {
	PowerMode, Flexikey, GPUOC, CPUOC, Fan, Energy, Battery, Keyboard bool
	TurboFan, SlowFan, MSHybrid, AMDRyzen, DTT, QuietPlus             bool
}

// Status 系统总状态(原 /api/status)。
type Status struct {
	WMI16                                    int
	KBType                                   keyboard.KBType
	Mode, Language, CustomerID, ClevoID      int
	Manufacturer, Family, Product, BIOS, CPU string
	Cores                                    int
	Sup                                      SupportBits
}

func (b *Backend) Status() Status {
	w16 := b.w.GetInt(16)
	p := b.platformInfo()
	return Status{
		WMI16:        w16,
		KBType:       p.kbType,
		Mode:         int(b.as.GetPowerMode()),
		Language:     int(b.as.GetUILanguage()),
		CustomerID:   p.customerID,
		ClevoID:      b.as.GetClevoID(),
		Manufacturer: p.tbl.System.ManufacturerName,
		Family:       p.tbl.System.Family,
		Product:      p.tbl.System.ProductName,
		BIOS:         p.tbl.BIOS.Version,
		CPU:          p.tbl.CPU.Version,
		Cores:        p.tbl.CPU.CoreCount,
		Sup: SupportBits{
			PowerMode: p.sup.PowerMode,
			Flexikey:  w16&2 == 2 && p.sup.FeatureListVer >= 0,
			GPUOC:     w16&0x20 != 0 || p.sup.SimpleGPUOC,
			CPUOC:     p.sup.CPUOC,
			Fan:       w16&0x80 != 0,
			Energy:    w16&0x100 != 0,
			Battery:   w16&0x200 != 0,
			Keyboard:  w16&0x18 != 0,
			TurboFan:  p.sup.TurboFan, SlowFan: p.sup.SlowFan,
			MSHybrid: p.sup.MSHybridSwitch, AMDRyzen: p.sup.AMDRyzen, DTT: p.sup.DTT,
			QuietPlus: p.sup.QuietPlusOnly || p.sup.NVIDIAPowerOff,
		},
	}
}

// RequestMode 请求切换电源模式(后台执行,与原 POST /api/mode 的 go pm.Change 一致)。
func (b *Backend) RequestMode(m int) { go b.pm.Change(appsettings.ModeType(m)) }

// ---------- 模式附属开关(原 /api/mode/options) ----------

// ModeOptions 模式附属开关状态。
type ModeOptions struct {
	TurboFan, Noiseless, DTT, LowRefresh          bool
	LowRefreshSupport                             bool
	BlueLight                                     int
}

func (b *Backend) ModeOptions() ModeOptions {
	return ModeOptions{
		TurboFan:           b.as.ReadU8(appsettings.AddrTurboFan) == 1,
		Noiseless:          b.as.ReadU8(appsettings.AddrNoiselessFan) == 1,
		DTT:                b.as.ReadU8(appsettings.AddrDTT) == 1,
		LowRefresh:         b.as.ReadU8(appsettings.AddrLowRefreshOn) == 1,
		LowRefreshSupport:  b.as.ReadU8(appsettings.AddrLowRefreshSup) == 1,
		BlueLight:          int(b.as.ReadU8(appsettings.AddrBlueLight)),
	}
}

// SetModeOption 下发模式附属开关(原 POST key/value)。
func (b *Backend) SetModeOption(key string, value bool) {
	switch key {
	case "turboFan":
		_ = b.fc.SetTurbo(value)
	case "noiseless":
		_ = b.fc.SetNoiseless(value)
	case "dtt":
		v := byte(0)
		if value {
			v = 1
		}
		_ = b.as.WriteU8(appsettings.AddrDTT, v)
		if value {
			_ = b.w.SetWMI(121, 25, 130)
		} else {
			_ = b.w.SetWMI(121, 25, 2)
		}
	case "lowRefresh":
		v := byte(0)
		if value {
			v = 1
		}
		_ = b.as.WriteU8(appsettings.AddrLowRefreshOn, v)
	case "blueLight":
		v := 0
		if value {
			v = 10
		}
		_ = b.as.WriteU8(appsettings.AddrBlueLight, byte(v))
		if v > 0 {
			_ = bluelight.SetBlueLightRamp(float64(v)/100*10 + 1)
		} else {
			bluelight.ResetDeviceGammaRamp()
		}
	}
}

// ---------- 偏好(原 /api/preference) ----------

// Pref 偏好汇总。
type Pref struct {
	FlexiCharger struct {
		Enabled         bool
		Start, Stop     int
		Mode            int
	}
	Keyboard struct {
		FnLock, WinKey, FnWin    bool
		FnLockVisible, WinKeyVisible bool
	}
	GPUSwitch struct{ Current, Support int }
	PgUpDn    bool
	BlueLight int
	Language  int
}

func (b *Backend) Preference() Pref {
	en, st, sp, _, _ := b.w.QueryFlexiCharger()
	mode := battery.ChargerModeOf(en, st, sp)
	if p, ok := battery.LoadChargerPref(); ok {
		mode = p.Mode
	}
	fn, win, fnw := b.w.QueryKeyboard()
	gpuCur, gpuSup := b.w.QueryGPUSwitch()
	var p Pref
	p.FlexiCharger.Enabled, p.FlexiCharger.Start, p.FlexiCharger.Stop = en, int(st), int(sp)
	p.FlexiCharger.Mode = mode
	p.Keyboard.FnLock, p.Keyboard.WinKey, p.Keyboard.FnWin = fn == 1, win == 1, fnw == 1
	p.Keyboard.FnLockVisible, p.Keyboard.WinKeyVisible = fn != 0, win != 0
	p.GPUSwitch.Current, p.GPUSwitch.Support = int(gpuCur), int(gpuSup)
	p.PgUpDn = b.as.ReadU8(appsettings.AddrPgUpDn) == 0
	p.BlueLight = int(b.as.ReadU8(appsettings.AddrBlueLight))
	p.Language = int(b.as.GetUILanguage())
	return p
}

// SetFlexiCharger 下发充电阈值并记住(原 preference.flexiCharger)。
func (b *Backend) SetFlexiCharger(mode, start, stop int) error {
	en := mode != 0
	b.w.SetFlexiCharger(en, byte(start), byte(stop))
	if err := b.as.WriteU8(appsettings.AddrFlexiCharger, byte(mode)); err != nil {
		return err
	}
	if err := battery.SaveChargerPref(battery.ChargerPref{Mode: mode, Start: start, Stop: stop}); err != nil {
		return fmt.Errorf("充电模式已下发但未能记住,重启后会回到原状态: %w", err)
	}
	return nil
}

// SetKeyboardPref 下发 Fn 键行为(原 preference.keyboard)。
func (b *Backend) SetKeyboardPref(fnLock, winKey, fnWin bool) {
	fn, win, fnw := byte(1), byte(1), byte(1)
	if !fnLock {
		fn = 2
	}
	if !winKey {
		win = 2
	}
	if !fnWin {
		fnw = 2
	}
	b.w.SetKeyboardSetting(fn, win, fnw)
	_ = b.as.Write(appsettings.AddrFnLock, 3, []byte{fn, win, fnw})
}

// SetGPUSwitch 切换 GPU 模式(需重启)。
func (b *Backend) SetGPUSwitch(v int) { b.w.SetGPUSwitch(v) }

// SetPgUpDn PgUp/PgDn 启用。
func (b *Backend) SetPgUpDn(enabled bool) {
	v := byte(0)
	if enabled {
		v = 1
	}
	_ = b.as.WriteU8(appsettings.AddrPgUpDn, v)
	b.w.SetPgUpDn(!enabled)
}

// SetBlueLight 蓝光滤镜档位 0..10。
func (b *Backend) SetBlueLight(v float64) {
	_ = b.as.WriteU8(appsettings.AddrBlueLight, byte(int(v)))
	if v > 0 {
		_ = bluelight.SetBlueLightRamp(v/100*10 + 1)
	} else {
		bluelight.ResetDeviceGammaRamp()
	}
}

// SetLanguage 切换 UI 语言并广播。
func (b *Backend) SetLanguage(v int) {
	_ = b.as.WriteU8(appsettings.AddrUILanguage, byte(v))
	_ = eventbus.WriteOut("clevofnkey^102")
}

// ---------- 风扇(原 /api/fan/*) ----------

// FanPulse 单风扇实时读数。
type FanPulse struct{ RPM, Duty, Temp int }

// FanLiveOut 风扇实时数据。
type FanLiveOut struct {
	CPU, GPU1, GPU2, Sys FanPulse
	Mode                 int
}

// FanLive 风扇实时(EC 包读 ~80ms 且独占 ACPI 通道,调用方自行控制节奏)。
func (b *Backend) FanLive() FanLiveOut {
	live, err := b.fc.ReadLive()
	if err != nil {
		return FanLiveOut{Mode: int(b.as.ReadU8(appsettings.AddrFanMode))}
	}
	duty := func(d int) int { return d * 100 / 255 }
	return FanLiveOut{
		CPU:  FanPulse{RPM: live.Fan.RPM, Duty: duty(live.Fan.Duty), Temp: int(live.Fan.Temp)},
		GPU1: FanPulse{RPM: live.GPU1.RPM, Duty: duty(live.GPU1.Duty), Temp: int(live.GPU1.Temp)},
		GPU2: FanPulse{RPM: live.GPU2.RPM, Duty: duty(live.GPU2.Duty), Temp: int(live.GPU2.Temp)},
		Sys:  FanPulse{RPM: live.SysFan.RPM, Duty: duty(live.SysFan.Duty), Temp: int(live.SysFan.Temp)},
		Mode: int(b.as.ReadU8(appsettings.AddrFanMode)),
	}
}

// FanConfigOut 风扇模式与自动偏移。
type FanConfigOut struct{ Mode, Offset int }

func (b *Backend) FanConfig() FanConfigOut {
	return FanConfigOut{
		Mode:   int(b.as.ReadU8(appsettings.AddrFanMode)),
		Offset: int(b.as.ReadU8(appsettings.AddrFanOffset)),
	}
}

func (b *Backend) SetFanMode(m int)   { _ = b.fc.SetMode(fan.FanMode(m)) }
func (b *Backend) SetFanOffset(v int) { _ = b.fc.SetOffsetPercent(v) }

// FanTableOut 三点曲线(温度/转速百分比)。
type FanTableOut struct{ T1, T2, T3, D1, D2, D3 int }

func (b *Backend) FanTables() [3]FanTableOut {
	tables := b.fc.ReadCustomTable()
	var out [3]FanTableOut
	for i, t := range tables {
		out[i] = FanTableOut{T1: t.T1, T2: t.T2, T3: t.T3, D1: t.D1, D2: t.D2, D3: t.D3}
	}
	return out
}

// SaveFanTables 保存自定义曲线并切到自定义模式(原 POST /api/fan/table)。
func (b *Backend) SaveFanTables(in [3]FanTableOut) error {
	var tables [3]fan.FanTable
	for i, t := range in {
		tables[i] = fan.FanTable{
			T1: t.T1, T2: t.T2, T3: t.T3, T4: 100,
			D1: t.D1, D2: t.D2, D3: t.D3, D4: 100,
			T2Def: t.T2, T3Def: t.T3, D2Def: t.D2, D3Def: t.D3,
		}
	}
	if err := b.fc.WriteCustomTable(tables); err != nil {
		return err
	}
	_ = b.fc.SetMode(fan.Custom)
	return nil
}

func (b *Backend) ResetFanTables() { _ = b.fc.LoadDefaultTable() }

// AntiDustOut 除尘状态。
type AntiDustOut struct {
	Enabled      bool
	Days, Hour   int
	Resume       bool
	Running      bool
}

func (b *Backend) AntiDust() AntiDustOut {
	en, days, resume, hour := b.fc.ReadAntiDustScheduler()
	return AntiDustOut{Enabled: en, Days: days, Resume: resume, Hour: hour, Running: b.fc.AntiDustStatus() != 0}
}

func (b *Backend) RunAntiDust()                        { _ = b.fc.RunAntiDust() }
func (b *Backend) SetAntiDustSchedule(days, hour int, resume bool) {
	_ = b.fc.SetAntiDustScheduler(days, resume, hour)
}

// ---------- 键盘灯(原 /api/kb/*) ----------

// KBStateOut 键盘灯状态。
type KBStateOut struct {
	Type         keyboard.KBType
	On           bool
	Brightness   int
	Mode, Speed  int
	AllColor, LeftColor, MidColor, RightColor, TpColor, LogoColor, LightBarColor keyboard.Color
	SleepTrigger bool
	SleepSeconds int
	OverrideBoot int
}

func (b *Backend) KBState() KBStateOut {
	pg := b.as.Page(2)
	at := func(off int) keyboard.Color {
		if off < 0 || off+3 > len(pg) {
			return keyboard.Color{}
		}
		return keyboard.Color{R: int(pg[off]), G: int(pg[off+1]), B: int(pg[off+2])}
	}
	return KBStateOut{
		Type:         b.platformInfo().kbType,
		On:           appsettings.U8(pg, 80) == 1,
		Brightness:   appsettings.U8(pg, 35),
		Mode:         appsettings.U8(pg, 32),
		Speed:        appsettings.U8(pg, 34),
		AllColor:     at(81), LeftColor: at(85), MidColor: at(89), RightColor: at(93),
		TpColor:      at(97), LogoColor: at(101), LightBarColor: at(105),
		SleepTrigger: appsettings.U8(pg, 36) == 1,
		SleepSeconds: appsettings.U16(pg, 37),
		OverrideBoot: appsettings.U8(pg, 7),
	}
}

// KBRequest 键盘灯命令(原 POST /api/kb/cmd)。
type KBRequest struct {
	Cmd                    string // onoff|brightness|color|effect|sleep|override|reset
	Part                   string // all|left|mid|right|tp|logo|lightbar
	R, G, B                int
	Level, Effect          int
	Mode, Seconds          int
	Override               *int
}

func (b *Backend) KBCmd(req KBRequest) error {
	t := b.platformInfo().kbType
	col := keyboard.Color{R: req.R, G: req.G, B: req.B}
	var err error
	switch req.Cmd {
	case "onoff":
		if req.Mode == 1 {
			err = b.kb.SetOnOff(keyboard.OnOffBits{Main: true, Left: true, Mid: true, Right: true})
		} else {
			err = b.kb.SetOnOff(keyboard.OnOffBits{})
		}
	case "brightness":
		if t == keyboard.White {
			err = b.kb.SetWhiteBrightness(req.Level)
		} else {
			err = b.kb.SetBrightnessRGB(req.Level)
		}
	case "color":
		var part byte
		switch req.Part {
		case "left":
			part = 0xF0
		case "mid":
			part = 0xF1
		case "right":
			part = 0xF2
		case "lightbar":
			part = 0xF3
		case "logo":
			part = 0xF6
		default:
			err = b.kb.SetAllColor(t, col)
		}
		if part != 0 {
			err = b.kb.SetColorRGB(t, part, col)
		}
		var base int
		switch req.Part {
		case "all", "":
			base = 593
		case "left":
			base = 597
		case "mid":
			base = 601
		case "right":
			base = 605
		case "tp":
			base = 609
		case "logo":
			base = 613
		case "lightbar":
			base = 617
		}
		if base > 0 {
			_ = b.as.Write(base, 3, []byte{byte(req.R), byte(req.G), byte(req.B)})
		}
	case "effect":
		err = b.kb.SetEffectRGB(keyboard.Effect(req.Effect))
		if req.Effect == int(keyboard.EffBreath) {
			_ = b.kb.SetAllColor(t, col)
		}
	case "sleep":
		on := req.Mode == 1
		err = b.kb.SetSleepTimerTrigger(on)
		if on && req.Seconds > 0 {
			err = b.kb.SetSleepTimerMinutes(req.Seconds / 60)
		}
	case "override":
		if req.Override != nil {
			err = b.kb.SetOverrideBootEffect(*req.Override)
		}
	case "reset":
		err = b.kb.ResetToDefault()
	}
	return err
}

// ---------- GPU 超频(原 /api/gpu/oc) ----------

// GPUOCOut GPU 超频信息。
type GPUOCOut struct {
	Saved0, Saved1, Current gpu.OffsetPair
	Count, BaseClock        int
	Ready                   bool
	Driver                  string
}

func (b *Backend) GPUInfo() GPUOCOut {
	store := gpu.New(b.as, b.w)
	g0, g1 := store.Saved()
	cur := store.Current()
	count, base, ready, ver := store.Info()
	return GPUOCOut{Saved0: g0, Saved1: g1, Current: cur, Count: count, BaseClock: base, Ready: ready, Driver: ver}
}

func (b *Backend) GPUSave(core0, mem0, core1, mem1 int, dual bool) error {
	store := gpu.New(b.as, b.w)
	return store.Save(gpu.ControlGPUPath(b.GPUOCBase),
		gpu.OffsetPair{Core: int16(core0), Mem: int16(mem0)},
		gpu.OffsetPair{Core: int16(core1), Mem: int16(mem1)}, dual)
}

func (b *Backend) GPUReset(dual bool) error {
	store := gpu.New(b.as, b.w)
	return store.Reset(gpu.ControlGPUPath(b.GPUOCBase), dual)
}

// ---------- CPU 超频(原 /api/cpu/*) ----------

func (b *Backend) CPUInfo() (cpuoc.Info, cpuoc.Params, cpuoc.DefaultParams) {
	pg := b.as.Page(6)
	return cpuoc.ReadInfoFrom(pg), cpuoc.ReadParamsFrom(pg), cpuoc.ReadDefaultsFrom(pg)
}

func (b *Backend) CPUApply(p cpuoc.Params) error {
	return cpuoc.Apply(b.as, b.w, p, b.platformInfo().customerID)
}

func (b *Backend) CPURestoreDefaults() error {
	return cpuoc.RestoreDefaults(b.as, b.w, b.platformInfo().customerID)
}

// ---------- 电池(原 /api/battery*) ----------

// BatteryOut 电池信息。
type BatteryOut struct {
	CycleCount, FullCharge, Design, CapacityPct, StopThresh, Status, Date int
	AgeYears                                                             float64
	Health                                                               string
	HealthCode                                                           int
	Ongoing                                                              int
}

func (b *Backend) Battery() BatteryOut {
	d := battery.Read(b.w)
	health := battery.Evaluate(d)
	b.mu.Lock()
	ongoing := battery.Ongoing(0)
	if b.batState != nil {
		ongoing = b.batState.Ongoing
	}
	b.mu.Unlock()
	return BatteryOut{
		CycleCount: d.CycleCount, FullCharge: d.FullChargeCapacity, Design: d.DesignCapacity,
		CapacityPct: battery.CapacityPct(d), StopThresh: d.StopChargeThresh,
		Status: d.BatteryStatus, Date: d.ManufacturerDate, AgeYears: battery.AgeYears(d),
		Health: health.String(), HealthCode: int(health), Ongoing: int(ongoing),
	}
}

// BatteryRefresh 电池校准控制(action: start|stop)。
func (b *Backend) BatteryRefresh(action string) {
	b.mu.Lock()
	if b.batState == nil {
		b.batState = &batteryState{}
	}
	st := b.batState
	switch action {
	case "start":
		battery.RefreshCommand(b.w, 1, 0, 1) // 先恢复
		battery.RefreshCommand(b.w, 0, 1, 0) // 开始放电
		st.Ongoing = battery.Discharge1
		st.Started = time.Now()
		_ = eventbus.WriteOut("clevobattery^207^1")
	case "stop":
		battery.RefreshCommand(b.w, 1, 0, 1)
		st.Ongoing = battery.Stop
		_ = eventbus.WriteOut("clevobattery^207^0")
	}
	b.mu.Unlock()
}

// ---------- 节能计划(原 /api/energy*) ----------

// EnergySampleOut 实时功耗采样。
type EnergySampleOut struct {
	Watt, BatWh, PD                  float64
	ChargeLimit, DischargeLimit      int
}

func (b *Backend) EnergySample() EnergySampleOut {
	b.mu.Lock()
	if b.sampler == nil {
		b.sampler = energysave.NewSampler(b.w)
	}
	sampler := b.sampler
	b.mu.Unlock()
	watt := sampler.SampleAC(false)
	charge, discharge := energysave.AutoChargeLimits(b.w)
	return EnergySampleOut{Watt: watt, BatWh: sampler.BatWh, PD: float64(sampler.PD),
		ChargeLimit: charge, DischargeLimit: discharge}
}

func (b *Backend) EnergyApply(p energysave.Profile) error {
	now := time.Now()
	return energysave.Apply(b.w, p, now.Hour(), now.Minute(), now.Second(), int(now.Weekday()))
}

// ---------- FlexiKey(原 /api/flexikey*) ----------

// FKProfile FlexiKey 配置档。
type FKProfile struct {
	Name            string
	Selected, KB, Mouse bool
	Slots           int
}

// FlexikeyOut FlexiKey 状态。
type FlexikeyOut struct {
	Path                    string
	Exists                  bool
	Profiles                []FKProfile
	KBEnabled, MouseEnabled bool
}

func (b *Backend) FlexikeyState() FlexikeyOut {
	path := flexikey.DefaultPath()
	store, err := flexikey.Load(path)
	if err != nil {
		return FlexikeyOut{Path: path}
	}
	_, statErr := os.Stat(path)
	out := FlexikeyOut{
		Path: path, Exists: statErr == nil,
		KBEnabled:    b.as.ReadU8(appsettings.AddrFlexikeyKB) == 1,
		MouseEnabled: b.as.ReadU8(appsettings.AddrFlexikeyMouse) == 1,
	}
	for _, p := range store.Profiles {
		out.Profiles = append(out.Profiles, FKProfile{
			Name: p.Name, Selected: p.IsSelected, KB: p.IsKbEnable, Mouse: p.IsMouseEnable, Slots: len(p.Keys)})
	}
	return out
}

// FlexikeyApply FlexiKey 操作(action: select|apply|disable)。
func (b *Backend) FlexikeyApply(action, name string) error {
	store, err := flexikey.Load(flexikey.DefaultPath())
	if err != nil {
		return err
	}
	ctl := flexikey.NewController(flexikey.NewDriver(), b.as)
	switch action {
	case "select":
		found := false
		for _, p := range store.Profiles {
			p.IsSelected = p.Name == name
			if p.IsSelected {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("配置 %q 不存在", name)
		}
		return store.Save()
	case "apply":
		if err := ctl.Apply(store.Selected()); err != nil {
			return err
		}
		_ = eventbus.WriteOut("clevofnkey^205")
	case "disable":
		if err := ctl.DisableAll(); err != nil {
			return err
		}
		_ = eventbus.WriteOut("clevofnkey^205")
	default:
		return fmt.Errorf("未知 action %q", action)
	}
	return nil
}

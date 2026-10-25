// Package cpuoc CPU 超频域(复刻 CPUOC2.0.exe:AppSettings page6 全参数表 + Apply/Default 序列)。
package cpuoc

import (
	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/wmi"
)

// Params CPU 超频参数(当前值区)。
type Params struct {
	PL1Watt      int     // (6,33,4)
	PL2Watt      int     // (6,37,4)
	PLTimeSec    float32 // (6,41,4 float)
	VRAuto       bool    // (6,74,1)
	VRLimit      int     // (6,49,4)
	CoreOffset   int     // (6,61,4) Core/Cache 电压偏移 mV
	OverrideVolt float32 // (6,65,4 float) 电压 Override(V),0=Default
	GraphicsOff  int     // (6,69,4) iGPU 偏移
	PCores       [10]int // (6,53..60,75,76)
	ECores       [16]int // (6,77..92)
}

// Info 平台信息(page6 只读区)。
type Info struct {
	CPUMHz          int // (6,9,4) 实时频率
	CPUVolt         float64
	CPUUtil         int // (6,21,1)
	CPUTemp         int // (6,22,1)
	MemMHz          int // (6,23,4)
	RatioMax        int // (6,224,1)
	RatioMin        int // (6,225,1)
	TurboMaxMHz     int // (6,226,2)
	TurboMinMHz     int // (6,228,2)
	MemMaxMHz       int // (6,230,2)
	RefClock        int // (6,232,2)
	XTUBusy         bool // (6,251,1)
	IsSaved         bool // (6,32,1)
}

// DefaultParams 默认值区(6,110..160)。
type DefaultParams struct {
	PL1      int
	PL2      int
	PLTime   float32
	VRAuto   bool
	VR       int
	CoreOff  int
	Override float32
	GfxOff   int
	PCores   [10]int
	ECores   [16]int
}

// ReadInfo 读平台信息。
func ReadInfo(as *appsettings.Store) Info { return ReadInfoFrom(as.Page(6)) }

// ReadInfoFrom 从 page6 快照解码平台信息(偏移量相对页首)。
func ReadInfoFrom(pg []byte) Info {
	return Info{
		CPUMHz:      appsettings.Int(pg, 9, 4),
		CPUVolt:     float64(appsettings.Float(pg, 13)),
		CPUUtil:     appsettings.U8(pg, 21),
		CPUTemp:     appsettings.U8(pg, 22),
		MemMHz:      appsettings.Int(pg, 23, 4),
		RatioMax:    appsettings.U8(pg, 224),
		RatioMin:    appsettings.U8(pg, 225),
		TurboMaxMHz: appsettings.Int(pg, 226, 2),
		TurboMinMHz: appsettings.Int(pg, 228, 2),
		MemMaxMHz:   appsettings.Int(pg, 230, 2),
		RefClock:    appsettings.Int(pg, 232, 2),
		XTUBusy:     appsettings.U8(pg, 251) == 0,
		IsSaved:     appsettings.U8(pg, 32) == 1,
	}
}

// ReadParams 读当前参数。
func ReadParams(as *appsettings.Store) Params { return ReadParamsFrom(as.Page(6)) }

// ReadParamsFrom 从 page6 快照解码当前参数。
func ReadParamsFrom(pg []byte) Params {
	var p Params
	p.PL1Watt = appsettings.Int(pg, 33, 4)
	p.PL2Watt = appsettings.Int(pg, 37, 4)
	p.PLTimeSec = appsettings.Float(pg, 41)
	p.VRAuto = appsettings.U8(pg, 74) == 1
	p.VRLimit = appsettings.Int(pg, 49, 4)
	p.CoreOffset = appsettings.Int(pg, 61, 4)
	p.OverrideVolt = appsettings.Float(pg, 65)
	p.GraphicsOff = appsettings.Int(pg, 69, 4)
	for i := 0; i < 8; i++ {
		p.PCores[i] = appsettings.U8(pg, 53+i)
	}
	p.PCores[8] = appsettings.U8(pg, 75)
	p.PCores[9] = appsettings.U8(pg, 76)
	for i := 0; i < 16; i++ {
		p.ECores[i] = appsettings.U8(pg, 77+i)
	}
	return p
}

// ReadDefaults 读默认值区。
func ReadDefaults(as *appsettings.Store) DefaultParams { return ReadDefaultsFrom(as.Page(6)) }

// ReadDefaultsFrom 从 page6 快照解码默认值。
func ReadDefaultsFrom(pg []byte) DefaultParams {
	var d DefaultParams
	d.PL1 = appsettings.Int(pg, 110, 2)
	d.PL2 = appsettings.Int(pg, 112, 4)
	d.PLTime = appsettings.Float(pg, 114)
	d.VRAuto = appsettings.U8(pg, 118) == 1
	d.VR = appsettings.Int(pg, 119, 4)
	d.CoreOff = appsettings.Int(pg, 123, 4)
	d.Override = appsettings.Float(pg, 127)
	d.GfxOff = appsettings.Int(pg, 131, 4)
	for i := 0; i < 10; i++ {
		d.PCores[i] = appsettings.U8(pg, 135+i)
	}
	for i := 0; i < 16; i++ {
		d.ECores[i] = appsettings.U8(pg, 145+i)
	}
	return d
}

// Apply SetXTUInfoToInsydeBuffer 完整写入序列(逐条,顺序与原版一致)。
// 写入后通知后台 XTU 执行(cpuoc_bg_106 → CC30_BG.exe)。
func Apply(as *appsettings.Store, w *wmi.Client, p Params, customID int) error {
	_ = as.WriteU8(appsettings.AddrXTUSaved, 1)
	_ = as.WriteInt(1536+33, 4, p.PL1Watt)
	_ = as.WriteFloat(1536+41, p.PLTimeSec)
	_ = as.WriteInt(1536+37, 4, p.PL2Watt)
	vrAuto := byte(0)
	if p.VRAuto {
		vrAuto = 1
	}
	_ = as.WriteU8(1536+74, vrAuto)
	_ = as.WriteInt(1536+49, 4, p.VRLimit)
	_ = as.WriteInt(1536+61, 4, p.CoreOffset)
	_ = as.WriteFloat(1536+65, p.OverrideVolt)
	if customID == 1 {
		_ = as.WriteInt(1536+69, 4, p.GraphicsOff)
	}
	for i := 0; i < 8; i++ {
		_ = as.WriteU8(1536+53+i, byte(p.PCores[i]))
	}
	_ = as.WriteU8(1536+75, byte(p.PCores[8]))
	_ = as.WriteU8(1536+76, byte(p.PCores[9]))
	for i := 0; i < 16; i++ {
		_ = as.WriteU8(1536+77+i, byte(p.ECores[i]))
	}
	// 通知后台执行(服务侧 CPUOC\CC30_BG.exe 消费)
	return eventbus.WriteIn("cpuoc_bg_106")
}

// RestoreDefaults Default 序列:读默认值区 → Apply → IsSaved 置 0。
func RestoreDefaults(as *appsettings.Store, w *wmi.Client, customID int) error {
	d := ReadDefaults(as)
	p := Params{
		PL1Watt:      d.PL1,
		PL2Watt:      d.PL2,
		PLTimeSec:    d.PLTime,
		VRAuto:       d.VRAuto,
		VRLimit:      d.VR,
		CoreOffset:   d.CoreOff,
		OverrideVolt: d.Override,
		GraphicsOff:  d.GfxOff,
	}
	p.PCores = d.PCores
	p.ECores = d.ECores
	if err := Apply(as, w, p, customID); err != nil {
		return err
	}
	return as.WriteU8(appsettings.AddrXTUSaved, 0)
}

// WaitXTUIdle 自旋等待服务完成 XTU 执行((6,251)!=0)。
func WaitXTUIdle(as *appsettings.Store) bool {
	for i := 0; i < 300; i++ {
		if as.ReadU8(1536+251) != 0 {
			return true
		}
		sleepMs(1000)
	}
	return false
}

func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

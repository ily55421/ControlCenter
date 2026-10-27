// Package fan 风扇控制域(复刻 FanSpeedSetting.exe / FnKey FAN.cs 的全部命令序列)。
package fan

import (
	"fmt"
	"math"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/wmi"
)

// FanMode 风扇模式(Interface_Fan.FanMode 枚举)。
type FanMode int

const (
	Auto      FanMode = 0
	Max       FanMode = 1
	Silent    FanMode = 2
	X3        FanMode = 3
	X4        FanMode = 4
	MaxQ      FanMode = 5
	Custom    FanMode = 6
	X7        FanMode = 7
	Noiseless FanMode = 8 // 慢速风扇
	IFSC      FanMode = 9
)

// FanPoint 三点曲线(温度℃/duty%)。
type FanPoint struct{ T, D int }

// FanTable 单风扇三点表(D4/T4 恒 100)。
type FanTable struct {
	T1, T2, T3, T4         int
	D1, D2, D3, D4         int
	T2Def, T3Def, D2Def, D3Def int
	R12, R23, R34          int
}

// LiveSample 风扇实时读数(GetWMIPackage(12) 解析)。
type LiveSample struct {
	Fan struct {
		RPM, Duty int
		Temp      float64
	}
	GPU1 struct {
		RPM, Duty int
		Temp      float64
	}
	GPU2 struct {
		RPM, Duty int
		Temp      float64
	}
	SysFan struct { // Fan4
		RPM, Duty int
		Temp      float64
	}
}

// Controller 风扇控制器。
type Controller struct {
	w   *wmi.Client
	as  *appsettings.Store
	Def [3]FanTable // CPU/GPU1/GPU2 默认表(WMI13)
}

// New 创建控制器。
func New(w *wmi.Client, as *appsettings.Store) *Controller { return &Controller{w: w, as: as} }

// Default 默认控制器。
var Default = New(wmi.Default, appsettings.Default)

// ReadLive 读取实时数据(GetWMIPackage(12);RPM 原始值换算 2156160/raw;duty 0-255)。
// 桥接失败返回 error:读不到不能以 RPM=0 冒充真实读数。
func (c *Controller) ReadLive() (LiveSample, error) {
	p, err := c.w.GetPackageErr(12)
	if err != nil {
		return LiveSample{}, fmt.Errorf("fan: 读风扇实时数据: %w", err)
	}
	var s LiveSample
	raw := func(i int) int { return int(uint16(p[i])<<8 | uint16(p[i+1])) }
	rpm := func(i int) int {
		r := raw(i)
		if r == 0 {
			return 0
		}
		return int(math.Round(2156160.0 / float64(r)))
	}
	s.Fan.RPM = rpm(2)
	s.Fan.Duty = int(p[16])
	s.Fan.Temp = float64(p[18])
	s.GPU1.RPM = rpm(4)
	s.GPU1.Duty = int(p[19])
	s.GPU1.Temp = float64(p[21])
	s.GPU2.RPM = rpm(6)
	s.GPU2.Duty = int(p[22])
	s.GPU2.Temp = float64(p[24])
	s.SysFan.RPM = rpm(36)
	s.SysFan.Duty = int(p[38])
	s.SysFan.Temp = float64(p[40])
	return s, nil
}

// Info WMI13 风扇信息。
type Info struct {
	FanCount    int
	InitFanMode int
	KbType      int
	BitsExist   int // bit0..3 = Fan1..4 存在
	DefTables   [3]FanTable
}

// ReadInfo 读取 WMI13(默认表 duty 从 255 制换算为百分比)。
func (c *Controller) ReadInfo() Info {
	p := c.w.GetPackage(13)
	var in Info
	in.FanCount = int(p[12])
	in.InitFanMode = int(p[14])
	in.KbType = int(p[15])
	in.BitsExist = int(p[50])
	conv := func(v int) int { return int(math.Round(float64(v) / 255.0 * 100.0)) }
	for fi := 0; fi < 3; fi++ {
		base := 16 + fi*8
		t := &in.DefTables[fi]
		t.T1, t.T2, t.T3, t.T4 = int(p[base]), int(p[base+2]), int(p[base+4]), 100
		t.D1, t.D2, t.D3, t.D4 = conv(int(p[base+1])), conv(int(p[base+3])), conv(int(p[base+5])), 100
		// 单调性修正(与原版一致)
		if t.D3 >= t.D4 {
			t.D3--
		}
		if t.D2 >= t.D3 {
			t.D2--
		}
		t.T2Def, t.T3Def, t.D2Def, t.D3Def = t.T2, t.T3, t.D2, t.D3
	}
	return in
}

// LoadDefaultTable WMI(121,34,1) 恢复 EC 默认风扇表。
func (c *Controller) LoadDefaultTable() error { return c.w.RestoreFanDefault() }

// SetMode 完整 Set_FanMode 序列:WMI(121,1,mode)+ 持久 (4,5) + 各模式联动。
func (c *Controller) SetMode(m FanMode) error {
	if err := c.w.SetFanMode(int(m)); err != nil {
		return err
	}
	if err := c.as.WriteU8(appsettings.AddrFanMode, byte(m)); err != nil {
		return err
	}
	return c.WriteSituationalData()
}

// SetOffsetPercent SetFanOffset:WMI(121,14,255*off/100) + 持久 (4,7)。
func (c *Controller) SetOffsetPercent(off int) error {
	if err := c.w.SetFanOffsetPct(off); err != nil {
		return err
	}
	if err := c.as.WriteU8(appsettings.AddrFanOffset, byte(off)); err != nil {
		return err
	}
	return c.WriteSituationalData()
}

// SetNoiseless Set_SlowFanMode:on → WMI(121,1,8)+(4,9,1)+(4,5,0);off → WMI(121,1,0)+(4,9,0)。
func (c *Controller) SetNoiseless(on bool) error {
	if on {
		if err := c.w.SetFanMode(int(Noiseless)); err != nil {
			return err
		}
		if err := c.as.WriteU8(appsettings.AddrNoiselessFan, 1); err != nil {
			return err
		}
		if err := c.as.WriteU8(appsettings.AddrFanMode, 0); err != nil {
			return err
		}
	} else {
		if err := c.w.SetFanMode(int(Auto)); err != nil {
			return err
		}
		if err := c.as.WriteU8(appsettings.AddrNoiselessFan, 0); err != nil {
			return err
		}
	}
	return c.WriteSituationalData()
}

// SetTurbo Set_TruboFan 完整序列。
func (c *Controller) SetTurbo(on bool) error {
	v := byte(0)
	if on {
		v = 1
	}
	if err := c.as.WriteU8(appsettings.AddrTurboFan, v); err != nil {
		return err
	}
	if err := c.as.WriteU8(appsettings.AddrDTT, 0); err != nil {
		return err
	}
	if err := c.w.SetPerformanceData(on, false); err != nil {
		return err
	}
	if err := c.as.WriteU8(appsettings.AddrFanMode, 0); err != nil {
		return err
	}
	if err := c.as.Write(appsettings.AddrFanQuietMode+2, 1, []byte{0}); err != nil { // Performance 模式 FanMode 槽(1096)
		return err
	}
	if on {
		return c.w.SetFanMode(int(Auto))
	}
	return nil
}

// WriteSituationalData Write_SituationalMode_of_FanData:按当前电源模式回写 1094..1101。
func (c *Controller) WriteSituationalData() error {
	fm := c.as.ReadU8(appsettings.AddrFanMode)
	if fm == 1 { // Max 模式改写为 Auto(与原版一致)
		fm = 0
	}
	off := c.as.ReadU8(appsettings.AddrFanOffset)
	pm := c.as.ReadU8(appsettings.AddrPowerMode)
	var base int
	switch pm {
	case 0, 4:
		base = appsettings.AddrFanQuietMode
	case 1:
		base = appsettings.AddrFanQuietMode + 1
	case 2:
		base = appsettings.AddrFanQuietMode + 2
	case 3:
		base = appsettings.AddrFanQuietMode + 3
	default:
		return nil
	}
	if err := c.as.WriteU8(base, byte(fm)); err != nil {
		return err
	}
	return c.as.WriteU8(base+4, byte(off))
}

// LoadModeForPowerMode FnKey Load_FanMode_forFn_3:模式切换时恢复对应模式风扇数据。
func (c *Controller) LoadModeForPowerMode(mode int) error {
	keep := c.as.ReadU8(appsettings.AddrFeatureList+20)>>4&1 != 0 ||
		c.as.ReadU8(appsettings.AddrFeatureList+20)&0x10 != 0 // RPM/IFSC 简化判定
	if keep {
		var base int
		switch mode {
		case 0, 4:
			base = appsettings.AddrFanQuietMode
		case 1:
			base = appsettings.AddrFanQuietMode + 1
		case 2:
			base = appsettings.AddrFanQuietMode + 2
		case 3:
			base = appsettings.AddrFanQuietMode + 3
		default:
			return nil
		}
		fm := c.as.ReadU8(base)
		off := c.as.ReadU8(base + 4)
		if err := c.as.WriteU8(appsettings.AddrFanMode, byte(fm)); err != nil {
			return err
		}
		if err := c.as.WriteU8(appsettings.AddrFanOffset, byte(off)); err != nil {
			return err
		}
		if err := c.w.SetFanOffsetPct(off); err != nil {
			return err
		}
		if mode == 3 && c.as.ReadU8(appsettings.AddrNoiselessFan) == 1 {
			return c.w.SetFanMode(int(Noiseless))
		}
		return c.w.SetFanMode(int(fm))
	}
	// 旧方案:0/1/2 → Auto;3 → MaxQ(支持时)
	fm := 0
	if mode == 3 {
		fm = 0
		if c.as.ReadU8(appsettings.AddrFeatureList+20)&1 != 0 { // Support_Slow_fan
			fm = 0
		}
	}
	if err := c.as.WriteU8(appsettings.AddrFanMode, byte(fm)); err != nil {
		return err
	}
	if mode == 3 && c.as.ReadU8(appsettings.AddrNoiselessFan) == 1 {
		return c.w.SetFanMode(int(Noiseless))
	}
	return c.w.SetFanMode(fm)
}

// WriteCustomTable Write_WMI14:三点曲线下发(SetWMIPackage(14,256B))。
// 布局:[2..5]=CPU T2,D2(255制),T3,D3;[6..9]=GPU1;[10..13]=GPU2;[14..31]=斜率(BE)。
func (c *Controller) WriteCustomTable(tables [3]FanTable) error {
	slope := func(t FanTable, seg int) int {
		var d1, d2, t1, t2 int
		switch seg {
		case 0:
			d1, d2, t1, t2 = t.D1, t.D2, t.T1, t.T2
		case 1:
			d1, d2, t1, t2 = t.D2, t.D3, t.T2, t.T3
		case 2:
			d1, d2, t1, t2 = t.D3, t.D4, t.T3, t.T4
		}
		if t2 == t1 {
			return 0
		}
		return int(math.Round(float64(d2-d1) / float64(t2-t1) * 2.55 * 16.0))
	}
	pkg := make([]byte, 256)
	off := 2
	for fi := 0; fi < 3; fi++ {
		t := tables[fi]
		pkg[off] = byte(t.T2)
		pkg[off+1] = byte(int(math.Round(float64(t.D2) / 100.0 * 255.0)))
		pkg[off+2] = byte(t.T3)
		pkg[off+3] = byte(int(math.Round(float64(t.D3) / 100.0 * 255.0)))
		off += 4
	}
	off = 14
	for fi := 0; fi < 3; fi++ {
		for seg := 0; seg < 3; seg++ {
			v := slope(tables[fi], seg)
			// 原生此处实际使用 BE 写入(Fan 应用为 BE)
			pkg[off] = byte(v >> 8)
			pkg[off+1] = byte(v)
			off += 2
		}
	}
	if err := c.w.SetWMIPackage(14, pkg); err != nil {
		return err
	}
	return c.persistFanInfo(tables)
}

// persistFanInfo SetFanInfo:page4 镜像(版本/InitFanMode/FanMode/FanCount/Offset + 表,斜率 LE)。
// 整页在 Store 锁内完成读→改→写,避免与并发写者互相覆盖。
func (c *Controller) persistFanInfo(tables [3]FanTable) error {
	fanCount := c.w.GetPackage(13)[12]
	return c.as.WithPage(4, func(page []byte) {
		page[6] = fanCount
		for fi := 0; fi < 3; fi++ {
			t := tables[fi]
			base := 16 + fi*18
			page[base] = byte(t.D1)
			page[base+1] = byte(t.D2)
			page[base+2] = byte(t.D3)
			page[base+3] = byte(t.D4)
			page[base+4] = byte(t.D2Def)
			page[base+5] = byte(t.D3Def)
			page[base+6] = byte(t.T1)
			page[base+7] = byte(t.T2)
			page[base+8] = byte(t.T3)
			page[base+9] = byte(t.T4)
			page[base+10] = byte(t.T2Def)
			page[base+11] = byte(t.T3Def)
		}
	})
}

// ReadCustomTable 从 page4 读取三点表。
func (c *Controller) ReadCustomTable() [3]FanTable {
	page := c.as.ReadAt(4, 0, 256)
	var out [3]FanTable
	for fi := 0; fi < 3; fi++ {
		base := 16 + fi * 18
		t := &out[fi]
		t.D1 = int(page[base])
		t.D2 = int(page[base+1])
		t.D3 = int(page[base+2])
		t.D4 = int(page[base+3])
		t.D2Def = int(page[base+4])
		t.D3Def = int(page[base+5])
		t.T1 = int(page[base+6])
		t.T2 = int(page[base+7])
		t.T3 = int(page[base+8])
		t.T4 = int(page[base+9])
		t.T2Def = int(page[base+10])
		t.T3Def = int(page[base+11])
	}
	return out
}

// AntiDust 防尘控制。
func (c *Controller) AntiDustStatus() byte { return c.w.GetAntiDustStatus() }

// RunAntiDust 立即除尘:WMI(121,41,1) + 状态写 (4,80,1)。
func (c *Controller) RunAntiDust() error {
	if err := c.w.RunAntiDust(); err != nil {
		return err
	}
	return c.as.WriteU8(appsettings.AddrAntiDustRun, 1)
}

// SetAntiDustScheduler WMI(121,40,packed) + (4,81,5) 持久。
func (c *Controller) SetAntiDustScheduler(days int, resume bool, hour int) error {
	if err := c.w.SetAntiDustScheduler(days, resume, hour); err != nil {
		return err
	}
	dayByte := byte(days & 0x7F)
	if resume {
		dayByte |= 0x80
	}
	data := []byte{1, dayByte, byte(hour), 0, 0}
	return c.as.Write(appsettings.AddrAntiDustSched, 5, data)
}

// ReadAntiDustScheduler 读回 (4,81,5)。
func (c *Controller) ReadAntiDustScheduler() (enabled bool, days int, resume bool, hour int) {
	d := c.as.Read(appsettings.AddrAntiDustSched, 5)
	enabled = d[0] == 1
	days = int(d[1] & 0x7F)
	resume = d[1]&0x80 != 0
	hour = int(d[2])
	return
}

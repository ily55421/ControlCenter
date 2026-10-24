// Package energysave 节能域(功耗采样 + 充放电调度下发;复刻 DesktopBridge_EnergySave.exe)。
package energysave

import (
	"math"

	"controlcenter/internal/wmi"
)

// Sampler 功耗采样器。
type Sampler struct {
	w        *wmi.Client
	history  [5]float64
	histIdx  int
	lastWatt float64
	BatWh    float64
	PD Watt  // 适配器功率上限
}

// Watt 功率。
type Watt int

// NewSampler 创建采样器(读电池设计容量与 PD 功率)。
func NewSampler(w *wmi.Client) *Sampler {
	s := &Sampler{w: w, PD: 300}
	s.BatWh = s.readBatteryWh()
	s.PD = s.readPDWatt()
	return s
}

// readBatteryWh 设计容量×电压(WMI4 EC 命令 223)。
func (s *Sampler) readBatteryWh() float64 {
	capMah := s.wordEC(223, [5]byte{12, 90, 22, 24, 0})
	voltMv := s.wordEC(223, [5]byte{12, 90, 22, 25, 0})
	return math.Round(float64(capMah) * float64(voltMv) / 1e6)
}

func (s *Sampler) wordEC(cmd byte, d [5]byte) int {
	r := s.w.RWEC(cmd, d)
	return int(uint16(r[1])|uint16(r[0])<<8) // (arr[1]<<8)+arr[0]
}

// readPDWatt WMI4 EC 命令 210 → bit0 45W bit1 65W bit2 90W。
func (s *Sampler) readPDWatt() Watt {
	r := s.w.RWEC(210, [5]byte{193, 0, 0, 0, 0})
	switch {
	case r[1]&1 != 0:
		return 45
	case r[1]&2 != 0:
		return 65
	case r[1]&4 != 0:
		return 90
	default:
		return 300
	}
}

// SampleAC 交流功耗采样(WMI 65 ADC 读取 + 5 点滑动平均)。
// 返回值>300 或超 PD 上限时沿用上次值(与原版一致)。
func (s *Sampler) SampleAC(psys bool) float64 {
	var watt float64
	if psys {
		// EC PSYS:WMI4 sub10 → mW = (o[1]<<8)+o[2]
		in := make([]byte, 256)
		in[0] = 10
		out := s.w.SetWMIPackageEx(4, in)
		mw := float64(uint16(out[1])<<8 | uint16(out[2]))
		watt = mw / 1000.0
	} else {
		raw := s.readADCReg(0x8B)   // 读 0x8B 寄存器
		scaleB := s.readADCReg(0x8D) // 采样电阻倍率
		var scale float64
		switch scaleB & 0xFF {
		case 10:
			scale = 0.1
		case 20:
			scale = 0.2
		case 5:
			scale = 0.05
		default:
			scale = 0.2
		}
		voltage := math.Round(float64(raw)*3.0/1023.0*100) / 100
		current := voltage / scale
		watt = current * 19.2
	}
	if watt > 300 || watt > float64(s.PD) {
		return s.smooth(s.lastWatt)
	}
	return s.smooth(watt)
}

// readADCReg WMI 65 读 EC 寄存器(返回值即读数,SetDCHU_Data 返回值携带)。
// 0x8B:0x058B01B1;0x8D:0x058D01B1。
func (s *Sampler) readADCReg(reg byte) uint16 {
	v := 0x05<<24 | uint32(reg)<<16 | 0x01<<8 | 0xB1
	ret := s.w.SetWMIValueInt(65, v)
	// 字节交换:r=(ret&0xFF)<<8|(ret&0xFF00)>>8
	r := (ret&0xFF)<<8 | (ret&0xFF00)>>8
	return uint16(r)
}

// smooth 5 点滑动平均。
func (s *Sampler) smooth(w float64) float64 {
	if w <= 0 {
		w = s.lastWatt
	}
	s.history[s.histIdx] = w
	s.histIdx = (s.histIdx + 1) % len(s.history)
	var sum float64
	for _, v := range s.history {
		sum += v
	}
	s.lastWatt = sum / float64(len(s.history))
	return s.lastWatt
}

// --- 节能调度下发(WMI 118)---

// Profile 一个节能档案。
type Profile struct {
	Name             string
	Schedule1On      bool
	Schedule1Start   int // 0..23
	Schedule1End     int
	Schedule2On      bool
	Schedule2Start   int
	Schedule2End     int
	Everyday         bool
	Weekdays         [7]bool // 日..六
	AutoBattery      bool
	ChargeLimit      int // 60..100
	DischargeLimit   int // 40..90|95
}

// Overlap 两个调度是否重叠(原版判定:e'=end+1)。
func (p Profile) Overlap() bool {
	if !p.Schedule1On || !p.Schedule2On {
		return false
	}
	s1, e1 := p.Schedule1Start, p.Schedule1End+1
	s2, e2 := p.Schedule2Start, p.Schedule2End+1
	d1 := s1 - e2
	d2 := e1 - s2
	return (d1 <= 0 || d2 <= 0) && (d1 >= 0 || d2 >= 0)
}

// Apply 完整下发序列(EnableEnergySave(false) → 5 条命令 → Enable(true))。
func Apply(w *wmi.Client, p Profile, hour, minute, sec, weekday int) error {
	if p.Overlap() {
		return errOverlap
	}
	_ = w.EnableEnergySave(false)
	_ = w.SetSystemTimeSync(sec, minute, hour, weekday)
	days := 255 // 每天
	if !p.Everyday {
		days = 0
		for i, on := range p.Weekdays {
			if on {
				days |= 1 << i
			}
		}
		if days == 0 {
			days = 255
		}
	}
	_ = w.SetChargeDays(days, p.DischargeLimit)
	_ = w.SetSchedule(1, p.Schedule1Start, p.Schedule1End, p.Schedule1On)
	_ = w.SetSchedule(2, p.Schedule2Start, p.Schedule2End, p.Schedule2On)
	_ = w.SetChargeThresholds(p.ChargeLimit, p.DischargeLimit)
	return w.EnableEnergySave(true)
}

// Disable 关闭节能(恢复供电)。
func Disable(w *wmi.Client) {
	_ = w.EnableEnergySave(false)
}

type overlapError struct{}

func (overlapError) Error() string { return "两个调度时间段重叠(至少需间隔 1 小时)" }

var errOverlap = overlapError{}

// AutoChargeLimits WMI17 [208]/[209] 读自动模式阈值(0 → 100/95)。
func AutoChargeLimits(w *wmi.Client) (charge, discharge int) {
	p := w.GetPackage(17)
	c, d := int(p[208]), int(p[209])
	if c == 0 {
		c = 100
	}
	if d == 0 {
		d = 95
	}
	return c, d
}

// Package keyboard 键盘灯控制域(复刻 LedKeyboardSetting.exe 全部命令编码)。
package keyboard

import (
	"controlcenter/internal/appsettings"
	"controlcenter/internal/wmi"
)

// KBType 键盘灯类型。
type KBType int

const (
	NoLED           KBType = 0
	White           KBType = 1
	RGBZone         KBType = 2
	Perkey          KBType = 3
	RGB15Color      KBType = 6
	RGB15Custom     KBType = 22
)

// RGB 颜色。
type Color struct{ R, G, B int }

// Effect 灯效模式(RGBKB Mode 枚举)。
type Effect int

const (
	EffRandom Effect = 0
	EffCustom Effect = 1
	EffBreath Effect = 2
	EffCycle  Effect = 3
	EffWave   Effect = 4
	EffDance  Effect = 5
	EffTempo  Effect = 6
	EffFlash  Effect = 7
	EffAll    Effect = 8
)

// Controller 键盘灯控制器。
type Controller struct {
	w  *wmi.Client
	as *appsettings.Store
}

// New 创建控制器。
func New(w *wmi.Client, as *appsettings.Store) *Controller { return &Controller{w: w, as: as} }

// Default 默认控制器。
var Default = New(wmi.Default, appsettings.Default)

// Type 读取键盘灯类型(WMI13[15],{3,19,35,51,243}→Perkey)并镜像 (2,4)。
func (c *Controller) Type() KBType {
	p := c.w.GetPackage(13)
	t := int(p[15])
	switch t {
	case 3, 19, 35, 51, 243:
		t = 3
	}
	_ = c.as.WriteU8(appsettings.AddrKBType, byte(t))
	return KBType(t)
}

// --- 白光(kb_type=1,WMI 39)---

// SetWhiteBrightness WMI(39,level):level 0..5(6 级)或 0..2(3 级)。
func (c *Controller) SetWhiteBrightness(level int) error {
	_ = c.as.WriteU8(547, byte(level)) // KBBrightness
	if level == 0 {
		_ = c.as.WriteU8(592, 0) // Status
		return c.w.SetWhiteKB(0)
	}
	_ = c.as.WriteU8(592, 1)
	return c.w.SetWhiteKB(level)
}

// WhiteBrightnessUp 白光亮度 +。
func (c *Controller) WhiteBrightnessUp(max int) error {
	cur := c.as.ReadU8(547)
	if cur < max {
		cur++
	}
	return c.SetWhiteBrightness(cur)
}

// WhiteBrightnessDown 白光亮度 −。
func (c *Controller) WhiteBrightnessDown(min int) error {
	cur := c.as.ReadU8(547)
	if cur > min {
		cur--
	}
	return c.SetWhiteBrightness(cur)
}

// --- RGB 三区(kb_type=2/6/22,WMI 103)---

// 分区命令字节。
const (
	partLeft    = 0xF0
	partMid     = 0xF1
	partRight   = 0xF2
	partLightBar = 0xF3
	partLogo    = 0xF6
	cmdBrightRGB = 0xF4
	cmdOnOff     = 0xE0
)

// SetColorRGB 分区颜色:WMI(103, part<<24 | B<<16 | R<<8 | G)。普通型蓝通道 ×40/51。
func (c *Controller) SetColorRGB(kbType KBType, part byte, col Color) error {
	b := col.B
	if kbType == RGBZone {
		b = col.B * 40 / 51
	}
	v := uint32(part)<<24 | uint32(b&0xFF)<<16 | uint32(col.R&0xFF)<<8 | uint32(col.G&0xFF)
	return c.w.SetRGBKB(v)
}

// SetAllColor 整键盘单色(三区同色)。
func (c *Controller) SetAllColor(kbType KBType, col Color) error {
	for _, p := range []byte{partLeft, partMid, partRight} {
		if err := c.SetColorRGB(kbType, p, col); err != nil {
			return err
		}
	}
	return nil
}

// SetBrightnessRGB WMI(103,0xF4000000+v):档位映射 1→47 2→95 3→143 4→191。
func (c *Controller) SetBrightnessRGB(level int) error {
	var v int
	switch level {
	case 1:
		v = 47
	case 2:
		v = 95
	case 3:
		v = 143
	case 4:
		v = 191
	default:
		v = 0
	}
	_ = c.as.WriteU8(547, byte(level))
	return c.w.SetRGBKB(0xF4000000 | uint32(v))
}

// OnOffBits 各区开关位。
type OnOffBits struct {
	Main, Left, Mid, Right, Logo, TP, LightBar bool
}

// SetOnOff WMI(103,0xE0000000|位图):bit12 总开关 bit13 Logo bit15 TP/灯带 bit16-18 左中右。
func (c *Controller) SetOnOff(b OnOffBits) error {
	var m uint32
	if b.Main {
		m = 1 // bit0 区开关集合(原版:1|num4|num5|tp_bits|num3)
	}
	v := m | 1<<12
	if b.Logo {
		v |= 1 << 13
	}
	if b.TP || b.LightBar {
		v |= 1 << 15
	}
	if b.Left {
		v |= 1 << 16
	}
	if b.Mid {
		v |= 1 << 17
	}
	if b.Right {
		v |= 1 << 18
	}
	if !b.Main {
		if err := c.w.SetRGBKB(0xF4000000); err != nil { // 亮度 0 先行
			return err
		}
	}
	_ = c.as.WriteU8(592, boolByte(b.Main))
	return c.w.SetRGBKB(0xE0000000 | v)
}

// SetEffectRGB 特效模式命令字。
func (c *Controller) SetEffectRGB(e Effect) error {
	_ = c.as.WriteU8(544, byte(e)) // Mode
	var v uint32
	switch e {
	case EffRandom:
		v = 0x70000000
	case EffCycle:
		v = 0x33000000
	case EffWave:
		v = 0xB0000000
	case EffDance:
		v = 0x80000000
	case EffTempo:
		v = 0x90000000
	case EffFlash:
		v = 0xA0000000
	}
	return c.w.SetRGBKB(v)
}

// SetSleepTimerTrigger WMI(121,0x18000000|(v<<8)|0xFF);off:0x18000000(v=0xFF 无效值)。
func (c *Controller) SetSleepTimerTrigger(on bool) error {
	if !on {
		return c.w.SetWMIValue(121, 0x18000000)
	}
	return c.w.SetWMIValue(121, 0x180000FF)
}

// SetSleepTimerMinutes WMI(121,24,(min<<8)|0xFF)。
func (c *Controller) SetSleepTimerMinutes(min int) error {
	return c.w.SetWMI(121, 24, uint32(min<<8|0xFF))
}

// SetOverrideBootEffect WMI(121,30,v)。
func (c *Controller) SetOverrideBootEffect(v int) error {
	_ = c.as.WriteU8(appsettings.AddrOverrideBootEff, byte(v))
	return c.w.SetBootEffect(v)
}

// ResetToDefault WMI(121,0x22000001) 恢复 WMI13 默认。
func (c *Controller) ResetToDefault() error {
	return c.w.SetWMIValue(121, 0x22000001)
}

// --- Perkey(kb_type=3;色彩矩阵存 AppSettings 2/128..511,由 EC 读取)---

// PerkeyAddr 单键地址:640 + row*64 + col*3 + col/5。
func PerkeyAddr(row, col int) int { return 640 + row*64 + col*3 + col/5 }

// SetPerkeyStatic 写单键静态色(AppSettings)。
func (c *Controller) SetPerkeyStatic(row, col int, colr Color) error {
	addr := PerkeyAddr(row, col)
	return c.as.Write(addr, 3, []byte{byte(colr.R), byte(colr.G), byte(colr.B)})
}

// ClearPerkeyStatic 清空 384 字节色彩矩阵。
func (c *Controller) ClearPerkeyStatic() error {
	return c.as.WriteAt(2, 128, 384, make([]byte, 384))
}

// --- 状态读写(AppSettings 镜像)---

// Status 读取/写入总开关与亮度。
func (c *Controller) Status() (on bool, brightness int) {
	return c.as.ReadU8(592) == 1, c.as.ReadU8(547)
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// SetKbLaunchFlag 写 LedKB 应用运行标志 (2,8)。
func (c *Controller) SetKbLaunchFlag(v int) error { return c.as.WriteU8(520, byte(v)) }

// Package hotkeys — Arg001 热键事件分发器(FnKey 事件表完整实现)。
package hotkeys

import (
	"fmt"
	"log"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/battery"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/uwp"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

// Dispatcher 热键分发器。
type Dispatcher struct {
	w   *wmi.Client
	as  *appsettings.Store
	kb  *keyboard.Controller
	fan *fan.Controller
	pm  *powermode.Engine
	osd OSDShower
}

// OSDShower OSD 抽象(由 UI 层注入)。
type OSDShower interface {
	Show(text string, barMax, barVal int)
}

// New 创建分发器。
func New(w *wmi.Client, as *appsettings.Store, kb *keyboard.Controller, f *fan.Controller, pm *powermode.Engine, osd OSDShower) *Dispatcher {
	return &Dispatcher{w: w, as: as, kb: kb, fan: f, pm: pm, osd: osd}
}

// Dispatch 处理 Arg001 通知值(低 8 位事件号,高位数据)。
func (d *Dispatcher) Dispatch(value uint64) {
	event := int(value & 0xFF)
	data := int(value >> 8)
	switch event {
	case 32, 129: // 键盘灯 -
		d.kbDown()
	case 33, 130: // 键盘灯 +
		d.kbUp()
	case 37: // FnLock 开关
		d.toggleFnLock()
	case 38: // 麦克风静音(CoreAudio 真实静音)
		d.toggleMicMute()
	case 47: // 电池温度
		d.checkBatteryTemp()
	case 48, 49, 50, 51, 52, 53, 54, 55, 56, 78, 79: // 亮度档
		idx := map[int]int{48: 212, 49: 213, 50: 214, 51: 215, 52: 216, 53: 217, 54: 218, 55: 219, 56: 220, 78: 210, 79: 211}[event]
		d.setBrightnessFromWMI17(idx)
	case 57: // RGB 换色
		d.osd.Show("KEYBOARD RGB", 0, 0)
		_ = eventbus.WriteOut("clevofnkey^209")
		_ = eventbus.WriteOut("clevofnkey^209")
	case 58: // 风扇 Turbo OSD
		d.osd.Show("FAN SPEED TURBO", 0, 0)
	case 59: // 电池低
		_ = d.w.SetWMI(121, 44, 2)
	case 60: // 电池低恢复
		_ = d.w.SetWMI(121, 44, 3)
	case 63, 158, 159: // 键盘灯总开关
		d.kbToggle()
	case 64: // 除尘运行
		d.osd.Show("FAN ANTI-DUST RUNNING", 0, 0)
	case 89: // 电源模式循环
		d.cyclePowerMode()
	case 93: // TP 状态
		_ = eventbus.ExecCommand(`C:\Program Files (x86)\ControlCenter\DCHU\TPStatus.exe`)
	case 106: // 风扇 MaxQ
		d.osd.Show("FAN SPEED MAX Q", 0, 0)
		d.setFanMode(fan.MaxQ)
	case 107: // 风扇 Custom
		d.osd.Show("FAN SPEED CUSTOM", 0, 0)
		d.setFanMode(fan.Custom)
	case 112: // 风扇 Auto
		mode := d.as.ReadU8(appsettings.AddrPowerMode)
		if mode == 0 {
			d.osd.Show("FAN SPEED SILENT", 0, 0)
		} else {
			d.osd.Show("FAN SPEED AUTO", 0, 0)
		}
		d.setFanMode(fan.Auto)
	case 123: // FlexiKey 开关
		d.toggleFlexikey()
	case 124: // 屏幕旋转
		d.osd.Show("SCREEN ROTATION", 0, 0)
	case 131: // 打开键盘灯设置
		uwp.Launch("keyboard")
	case 132: // 恢复界面
		_ = win32.StartAsActiveUser(`control.exe /name Microsoft.Recovery`)
	case 133: // 飞行模式
		d.toggleAirplane()
	case 138: // 白光键盘循环
		d.whiteKBCycle()
	case 139, 140, 141: // RGB 分区开关
		d.osd.Show("KEYBOARD LED", 0, 0)
	case 143: // 风扇 Max
		d.osd.Show("FAN SPEED MAX", 0, 0)
		d.setFanMode(fan.Max)
	case 149: // 打开/关闭 CC 主界面
		d.toggleCC()
	case 166, 167: // 扩展坞
		if event == 166 {
			d.osd.Show("DOCKING OFF", 0, 0)
		} else {
			d.osd.Show("DOCKING ON", 0, 0)
		}
	case 184, 185: // GPU OC
		if event == 184 {
			d.osd.Show("GPU OC OFF", 0, 0)
		} else {
			d.osd.Show("GPU OC ON", 0, 0)
		}
	case 186: // 亮度直设
		if data&0x80 != 0 {
			_ = win32.SetBrightness(uint32(data & 0x7F))
		}
	case 192: // 盖子动作
		d.lidAction()
	case 199, 200, 201, 202, 203, 204: // Num/Caps/Scroll Lock OSD
		d.lockOSD(event)
	case 215: // OLED 屏灭/TP hook
		_ = eventbus.WriteIn("clevofnkey^1ff")
	case 222, 223: // 静音
		if event == 222 {
			d.osd.Show("SILENT ON", 0, 0)
		} else {
			d.osd.Show("SILENT OFF", 0, 0)
		}
	case 234, 235: // 3G
		if event == 234 {
			d.osd.Show("3G OFF", 0, 0)
		} else {
			d.osd.Show("3G ON", 0, 0)
		}
	case 240, 241: // FlexiCharger 启停:EC 侧已切换,这里同步"记住的选择"以便开机回写
		mode, start, stop := battery.ChargerMaxBattery, 95, 100
		if event == 241 {
			en, st, sp, _, _ := d.w.QueryFlexiCharger()
			mode = battery.ChargerModeOf(en, st, sp)
			start, stop = int(st), int(sp)
		}
		_ = d.as.WriteU8(appsettings.AddrFlexiCharger, byte(mode))
		_ = battery.SaveChargerPref(battery.ChargerPref{Mode: mode, Start: start, Stop: stop})
		if event == 240 {
			d.osd.Show("FLEXICHARGER STOP", 0, 0)
		} else {
			d.osd.Show("FLEXICHARGER START", 0, 0)
		}
	case 246, 247, 248, 249: // 摄像头
		if event == 246 {
			d.setCamera(0, "CAMERA OFF")
		} else {
			d.setCamera(1, "CAMERA ON")
		}
	case 252, 253: // 触摸板
		if event == 252 {
			d.setTouchPad(false)
		} else {
			d.setTouchPad(true)
		}
	case 301, 302: // FlexiAccess
		if event == 301 {
			d.osd.Show("FLEXIACCESS ON", 0, 0)
		} else {
			d.osd.Show("FLEXIACCESS OFF", 0, 0)
		}
	case 303, 304: // GPU OC
		if event == 303 {
			d.osd.Show("GPU OC ON", 0, 0)
		} else {
			d.osd.Show("GPU OC OFF", 0, 0)
		}
	default:
		log.Printf("hotkeys: 未处理事件 %d (data=%#x)", event, data)
	}
}

func (d *Dispatcher) showOSD(text string) { d.osd.Show(text, 0, 0) }

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (d *Dispatcher) kbDown() {
	t := d.kb.Type()
	if t == keyboard.White {
		_ = d.kb.WhiteBrightnessDown(1)
	} else {
		cur := d.as.ReadU8(547)
		if cur > 1 {
			_ = d.kb.SetBrightnessRGB(cur - 1)
		}
	}
	d.osd.Show("KEYBOARD BACKLIGHT LOW", 0, 0)
	_ = eventbus.WriteOut("clevofnkey^201")
	_ = eventbus.WriteOut("clevofnkey^201")
}

func (d *Dispatcher) kbUp() {
	t := d.kb.Type()
	if t == keyboard.White {
		_ = d.kb.WhiteBrightnessUp(5)
	} else {
		cur := d.as.ReadU8(547)
		if cur < 4 {
			_ = d.kb.SetBrightnessRGB(cur + 1)
		}
	}
	d.osd.Show("KEYBOARD BACKLIGHT HIGH", 0, 0)
	_ = eventbus.WriteOut("clevofnkey^201")
	_ = eventbus.WriteOut("clevofnkey^201")
}

func (d *Dispatcher) kbToggle() {
	on, _ := d.kb.Status()
	if on {
		_ = d.kb.SetOnOff(keyboard.OnOffBits{})
		d.osd.Show("KEYBOARD BACKLIGHT OFF", 0, 0)
	} else {
		_ = d.kb.SetOnOff(keyboard.OnOffBits{Main: true, Left: true, Mid: true, Right: true})
		cur := d.as.ReadU8(547)
		if cur == 0 {
			cur = 1
		}
		_ = d.kb.SetBrightnessRGB(cur)
		d.osd.Show("KEYBOARD BACKLIGHT ON", 0, 0)
	}
	_ = eventbus.WriteOut("clevofnkey^20A")
	_ = eventbus.WriteOut("clevofnkey^20A")
}



func (d *Dispatcher) toggleFnLock() {
	fn, _, _ := d.w.QueryKeyboard()
	if fn == 1 {
		d.w.SetKeyboardSetting(2, 1, 1)
		d.osd.Show("FN UNLOCK", 0, 0)
	} else {
		d.w.SetKeyboardSetting(1, 1, 1)
		d.osd.Show("FN LOCK", 0, 0)
	}
	_ = eventbus.WriteOut("clevofnkey^20e")
	_ = eventbus.WriteOut("clevofnkey^20e")
}

func (d *Dispatcher) cyclePowerMode() {
	cur := d.as.GetPowerMode()
	next := d.pm.NextModeFor(cur)
	d.osd.Show(modeOSDText(next, d.as.ReadU8(appsettings.AddrUIStyle)), 0, 0)
	go d.pm.Change(next)
}

func modeOSDText(m appsettings.ModeType, uiStyle int) string {
	switch m {
	case appsettings.Quiet:
		return "QUIET MODE"
	case appsettings.PowerSaving:
		return "POWER SAVING MODE"
	case appsettings.Performance:
		return "PERFORMANCE MODE"
	case appsettings.QuietPlus:
		return "QUIET MODE"
	default:
		if uiStyle == 1 || uiStyle == 2 {
			return "BALANCED MODE"
		}
		return "ENTERTAINMENT MODE"
	}
}

func (d *Dispatcher) setFanMode(m fan.FanMode) {
	if d.fan != nil {
		_ = d.fan.SetMode(m)
	}
	_ = eventbus.WriteOut(fmt.Sprintf("clevofan^113^%d", int(m)))
}

func (d *Dispatcher) toggleFlexikey() {
	kb := d.as.ReadU8(appsettings.AddrFlexikeyKB)
	ms := d.as.ReadU8(appsettings.AddrFlexikeyMouse)
	if kb == 1 || ms == 1 {
		d.as.WriteU8(appsettings.AddrFlexikeyKB, 0)
		d.as.WriteU8(appsettings.AddrFlexikeyMouse, 0)
		d.osd.Show("COMBO KEY OFF", 0, 0)
	} else {
		d.as.WriteU8(appsettings.AddrFlexikeyKB, 1)
		d.as.WriteU8(appsettings.AddrFlexikeyMouse, 1)
		d.osd.Show("COMBO KEY ON", 0, 0)
	}
	_ = eventbus.WriteOut("clevofnkey^205")
	_ = eventbus.WriteOut("clevofnkey^205")
}

func (d *Dispatcher) toggleAirplane() {
	// 与原版 Insyde_AirplaneMode_Control 一致:读 SystemRadioState(1=飞行开)后翻转,
	// 同步飞行灯(WMI 121/26),OSD 显示目标状态。
	next := 1 - win32.SystemRadioState()
	_ = d.w.SetAirplaneLED(next)
	if err := win32.SetSystemRadioState(next); err != nil {
		log.Printf("hotkeys: 写飞行模式状态失败(需管理员权限): %v", err)
	}
	d.osd.Show("AIRPLANE MODE "+onOff(next == 1), 0, 0)
}

func (d *Dispatcher) toggleCC() {
	launched := d.as.ReadU8(appsettings.AddrCC30Launch) == 1
	if !launched && !win32.ProcessExistsByName("ccapp.exe") {
		uwp.Launch("power")
	} else {
		_ = eventbus.WriteOut("clevofnkey^204")
		_ = eventbus.WriteOut("clevofnkey^204")
	}
}

func (d *Dispatcher) setCamera(v int, osdText string) {
	_ = d.as.WriteU8(appsettings.AddrCamera, byte(v))
	_ = d.w.SetCameraPower(v)
	d.osd.Show(osdText, 0, 0)
}

func (d *Dispatcher) setTouchPad(on bool) {
	win32.SendWinCtrlF24(false)
	win32.SendWinCtrlF24(true)
	v := 0
	if on {
		v = 1
	}
	d.as.WriteU8(appsettings.AddrTouchPad, byte(v))
	if on {
		d.osd.Show("TOUCHPAD ON", 0, 0)
	} else {
		d.osd.Show("TOUCHPAD OFF", 0, 0)
	}
	_ = eventbus.WriteIn(fmt.Sprintf("clevofnkey^206^%d", v))
	_ = eventbus.WriteOut("clevofnkey^20d")
}

func (d *Dispatcher) setBrightnessFromWMI17(byteIdx int) {
	p := d.w.GetPackage(17)
	if byteIdx >= 0 && byteIdx < 256 {
		_ = win32.SetBrightness(uint32(p[byteIdx]))
	}
}

func (d *Dispatcher) checkBatteryTemp() {
	r, err := d.w.TryRWEC(223, [5]byte{12, 90, 22, 8, 0})
	if err != nil {
		log.Printf("hotkeys: 读电池温度失败: %v", err)
		return
	}
	temp, ok := batteryTemp(r)
	if !ok {
		return // 读数不在物理量程内(含全零),不能当真实温度用来报警
	}
	if temp < 11 {
		d.osd.Show("BATTERY LOW TEMPERATURE", 0, 0)
	}
}

// batteryTemp 解析 EC 电池温度(0.1K 定点,小端);raw 为 0 或结果超出物理量程视为无效。
func batteryTemp(r [5]byte) (int, bool) {
	raw := int(uint16(r[1])<<8 | uint16(r[0]))
	if raw == 0 {
		return 0, false
	}
	t := (raw - 2732) / 10
	if t < -30 || t > 120 {
		return 0, false
	}
	return t, true
}

func (d *Dispatcher) lidAction() {
	// 对应原版 case 192:按当前电源计划的合盖动作执行(AC/DC 分别取)
	action := win32.LidActionSleep
	if ac, dc, err := win32.GetLidCloseAction(); err == nil {
		if win32.ACOnline() {
			action = ac
		} else {
			action = dc
		}
	} else {
		log.Printf("hotkeys: 读合盖动作失败: %v(默认待机)", err)
	}
	switch action {
	case win32.LidActionNothing:
		// 不采取任何操作
	case win32.LidActionHibernate:
		_ = win32.SuspendState(true)
	case win32.LidActionShutdown:
		_ = win32.SuspendState(true) // 安全策略:关机档以休眠替代
	default: // LidActionSleep(含 Modern Standby)
		_ = win32.SuspendState(false)
	}
}

func (d *Dispatcher) lockOSD(event int) {
	var text string
	switch event {
	case 199:
		text = "NUM LOCK OFF"
	case 200:
		text = "NUM LOCK ON"
	case 201:
		text = "CAPS LOCK OFF"
	case 202:
		text = "CAPS LOCK ON"
	case 203:
		text = "SCROLL LOCK OFF"
	case 204:
		text = "SCROLL LOCK ON"
	}
	d.osd.Show(text, 0, 0)
}


func (d *Dispatcher) whiteKBCycle() {
	cur := d.w.GetWhiteKB()
	next := cur + 1
	if next > 4 {
		next = 0
	}
	_ = d.kb.SetWhiteBrightness(next)
	d.osd.Show(fmt.Sprintf("KEYBOARD BACKLIGHT %d", next), next, 4)
	_ = eventbus.WriteOut("clevofnkey^201")
	_ = eventbus.WriteOut("clevofnkey^201")
}

// toggleMicMute 真实麦克风静音(CoreAudio,对应原版 DgMic.SetMicStateTo)。
func (d *Dispatcher) toggleMicMute() {
	muted, err := win32.ToggleMicMute()
	if err != nil {
		log.Printf("hotkeys: CoreAudio 静音失败: %v(回退标志翻转)", err)
		muted = d.as.ReadU8(appsettings.AddrCallMute) == 0
	}
	flag := 0
	if muted {
		flag = 1
	}
	_ = d.as.WriteU8(appsettings.AddrCallMute, byte(flag))
	d.showOSD(fmt.Sprintf("MICROPHONE %s", onOff(muted)))
}

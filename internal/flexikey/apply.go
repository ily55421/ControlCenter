// flexikey — 下发控制器:Set_Filiter_Driver 全量重下发。
package flexikey

import (
	"time"

	"controlcenter/internal/appsettings"
)

// Controller 宏键控制器。
type Controller struct {
	drv *Driver
	as  *appsettings.Store
}

// NewController 创建控制器。
func NewController(drv *Driver, as *appsettings.Store) *Controller {
	return &Controller{drv: drv, as: as}
}

// Apply Set_Filiter_Driver:清空 → 开关 → 50ms → 全量注册。
func (c *Controller) Apply(p *Profile) error {
	_ = c.drv.ClearKB()
	_ = c.drv.ClearMouse()
	if !p.IsKbEnable && !p.IsMouseEnable {
		_ = c.drv.DisableKB()
		_ = c.drv.DisableMouse()
		return nil
	}
	if p.IsKbEnable {
		_ = c.drv.EnableKB()
	} else {
		_ = c.drv.DisableKB()
	}
	if p.IsMouseEnable {
		_ = c.drv.EnableMouse()
	} else {
		_ = c.drv.DisableMouse()
	}
	time.Sleep(50 * time.Millisecond)
	for _, k := range p.Keys {
		switch k.Action {
		case ActionKeypress:
			_ = c.drv.SetMacroKeyEx(int32(k.KeyCode), SetupMacroKeyEx(k.Macro, k.KeypressDelay))
		case ActionLaunch:
			_ = c.drv.SetSysEvent(int32(k.KeyCode), int32(k.AppEventID))
		case ActionText:
			_ = c.drv.SetStringKey(int32(k.KeyCode),
				SetupKeys(k.StartKeys, false), SetupKeys(k.SendKeys, false), k.Text)
		case ActionDisable:
			_ = c.drv.SetKeyDisable(int32(k.KeyCode))
		}
	}
	_ = c.as.WriteU8(appsettings.AddrFlexikeyKB, boolByte(p.IsKbEnable))
	_ = c.as.WriteU8(appsettings.AddrFlexikeyMouse, boolByte(p.IsMouseEnable))
	return nil
}

// ApplyMouse 鼠标侧注册(键位为 ClevoMouse 位标志)。
func (c *Controller) ApplyMouse(p *Profile) error {
	for _, k := range p.Keys {
		if k.Device != DeviceMouse {
			continue
		}
		switch k.Action {
		case ActionKeypress:
			_ = c.drv.SetMouseMacro(int32(k.KeyCode), SetupMacroKeyEx(k.Macro, k.KeypressDelay))
		case ActionLaunch:
			_ = c.drv.SetMouseSysEvent(int32(k.KeyCode), int32(k.AppEventID))
		case ActionText:
			_ = c.drv.SetMouseStringKey(int32(k.KeyCode),
				SetupKeys(k.StartKeys, false), SetupKeys(k.SendKeys, false), k.Text)
		case ActionDisable:
			_ = c.drv.SetMouseDisable(int32(k.KeyCode))
		}
	}
	return nil
}

// DisableAll 关闭过滤。
func (c *Controller) DisableAll() error {
	_ = c.drv.ClearKB()
	_ = c.drv.ClearMouse()
	_ = c.drv.DisableKB()
	_ = c.drv.DisableMouse()
	_ = c.as.WriteU8(appsettings.AddrFlexikeyKB, 0)
	_ = c.as.WriteU8(appsettings.AddrFlexikeyMouse, 0)
	return nil
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

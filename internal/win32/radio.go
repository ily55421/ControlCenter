// radio.go — 飞行模式无线状态(HKLM\SYSTEM\CurrentControlSet\Control\RadioManagement\SystemRadioState)。
// 与原版 FnKey AirplaneMode.cs 一致:1=飞行模式开(无线电关),0=正常。
package win32

import (
	"golang.org/x/sys/windows/registry"
)

const radioKeyPath = `SYSTEM\CurrentControlSet\Control\RadioManagement`

// SystemRadioState 读当前无线状态(1=飞行模式开)。读取失败按 0(正常)处理,与原版默认值一致。
func SystemRadioState() int {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, radioKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	// 原版读取默认值(名称为空)
	v, _, err := k.GetIntegerValue("")
	if err != nil {
		return 0
	}
	return int(v)
}

// SetSystemRadioState 写无线状态(需要管理员权限;RmSvc 监听该值切换飞行模式)。
func SetSystemRadioState(v int) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, radioKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetDWordValue("", uint32(v)); err != nil {
		return err
	}
	return nil
}

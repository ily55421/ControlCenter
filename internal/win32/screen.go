// screen.go — 屏幕工作区与 DPI 查询:让主窗口默认高度贴合当前显示器可用区域
// (小屏/高缩放下不至于让底栏跑出屏幕,也不盲目撑满)。
package win32

import "unsafe"

var (
	procMonitorFromPoint = moduser32.NewProc("MonitorFromPoint")
	procGetMonitorInfo   = moduser32.NewProc("GetMonitorInfoW")
	procGetDpiForSystem  = moduser32.NewProc("GetDpiForSystem")
)

const (
	monitorDefaultToPrimary = 0x00000001
	defaultDpi              = 96
	smCxScreen              = 0
	smCyScreen              = 1
)

// monitorInfo MONITORINFO(仅使用 rcWork;与 Win32 布局一致:cbSize 后两个 RECT 再 DWORD)。
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rectStruct
	rcWork    rectStruct
	dwFlags   uint32
}

// PrimaryWorkArea 返回主显示器工作区尺寸(物理像素);
// 取不到时回退到屏幕尺寸,仍失败返回 0,0。
func PrimaryWorkArea() (w, h int) {
	pt := point{} // (0,0) 必定落在主显示器上
	if hMon, _, _ := procMonitorFromPoint.Call(
		uintptr(unsafe.Pointer(&pt)), monitorDefaultToPrimary); hMon != 0 {
		var mi monitorInfo
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfo.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			if w = int(mi.rcWork.Right - mi.rcWork.Left); w > 0 {
				if h = int(mi.rcWork.Bottom - mi.rcWork.Top); h > 0 {
					return w, h
				}
			}
		}
	}
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	return int(int32(cx)), int(int32(cy))
}

// DpiScale 返回系统 DPI 缩放系数(96 DPI = 1.0);
// GetDpiForSystem 不可用(Win10 1607 以下)时按 1.0 处理。
func DpiScale() float64 {
	if r, _, _ := procGetDpiForSystem.Call(); r > 0 {
		return float64(r) / defaultDpi
	}
	return 1
}

// screen.go — 屏幕工作区与 DPI 查询:让主窗口默认高度贴合当前显示器可用区域
// (小屏/高缩放下不至于让底栏跑出屏幕,也不盲目撑满)。
package win32

import (
	"runtime"
	"unsafe"
)

var (
	procMonitorFromPoint             = moduser32.NewProc("MonitorFromPoint")
	procGetMonitorInfo               = moduser32.NewProc("GetMonitorInfoW")
	procGetDpiForSystem              = moduser32.NewProc("GetDpiForSystem")
	procSetThreadDpiAwarenessContext = moduser32.NewProc("SetThreadDpiAwarenessContext")
)

const (
	monitorDefaultToPrimary = 0x00000001
	defaultDpi              = 96
	smCxScreen              = 0
	smCyScreen              = 1
	// dpiAwarePerMonitorV2 = (DPI_AWARENESS_CONTEXT)-4
	dpiAwarePerMonitorV2 = ^uintptr(3)
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

// PrimaryWorkAreaDp 返回主显示器工作区尺寸,单位是与 gioui 同量纲的 Dp(逻辑像素)。
//
// 查询线程临时切到 Per-Monitor V2 感知:不感知的线程拿到的是 DPI **虚拟化后**的像素,
// 再除以系统 DPI 就是二次缩放 —— 单屏等比缩放时两者恰好抵消(所以长期没被发现),
// 混合 DPI 或线程一旦改为感知,默认窗口尺寸立刻偏一个缩放系数。
func PrimaryWorkAreaDp() (w, h int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiAwarePerMonitorV2)
	defer procSetThreadDpiAwarenessContext.Call(prev)

	pw, ph := PrimaryWorkArea()
	if pw <= 0 || ph <= 0 {
		return 0, 0
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi <= 0 { // Win10 1607 以下:GetDpiForSystem 不可用,按 1.0 处理
		dpi = defaultDpi
	}
	return pw * defaultDpi / int(dpi), ph * defaultDpi / int(dpi)
}

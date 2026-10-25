// dwm — DWM 窗口属性:深色标题栏/标题颜色/边框色(Windows 10 1809+/11)。
// 用于让原生 GUI 窗口与霓虹暗色主题融为一体(默认白标题栏会割裂视觉)。
package win32

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DWMWA 属性号(DWM API,Windows 11 / Win10 1809+)。
const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaBorderColor          = 34
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36
)

var (
	modDwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttr = modDwmapi.NewProc("DwmSetWindowAttribute")
	procEnumWindows      = moduser32.NewProc("EnumWindows")
	procIsWindowVisible  = moduser32.NewProc("IsWindowVisible")
	procGetWindowThread  = moduser32.NewProc("GetWindowThreadProcessId")
	procGetCurrentPID    = modkernel32.NewProc("GetCurrentProcessId")
)

// COLORREF 0x00BBGGRR。
func colorRef(r, g, b uint8) uintptr { return uintptr(b)<<16 | uintptr(g)<<8 | uintptr(r) }

func dwmSet(hwnd uintptr, attr uint32, v uintptr) {
	var val uintptr = v
	_, _, _ = procDwmSetWindowAttr.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&val)), 4)
}

// ApplyDarkWindow 给单个窗口应用深色主题标题栏。
func ApplyDarkWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dwmSet(hwnd, dwmwaUseImmersiveDarkMode, 1)               // 深色模式标题
	dwmSet(hwnd, dwmwaCaptionColor, colorRef(0x05, 0x07, 0x08)) // 标题栏底色(近黑)
	dwmSet(hwnd, dwmwaBorderColor, colorRef(0x26, 0x2C, 0x33))  // 边框(面板线色)
	dwmSet(hwnd, dwmwaTextColor, colorRef(0xE8, 0xEE, 0xF2))    // 标题文字
}

// ApplyDarkOwnWindows 枚举当前进程的全部可见顶层窗口并应用深色标题栏。
// 返回处理到的窗口数(GUI 主窗口创建后调一次;不支持 DWM 属性的系统静默忽略)。
func ApplyDarkOwnWindows() int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	self, _, _ := procGetCurrentPID.Call()
	n := 0
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		pid := uint32(0)
		procGetWindowThread.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(self) {
			return 1 // 继续枚举
		}
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1
		}
		ApplyDarkWindow(hwnd)
		n++
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return n
}

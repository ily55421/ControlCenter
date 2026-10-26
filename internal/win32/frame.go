// frame.go — 窗口外观:任务栏/标题栏图标注入。
package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procLoadImageW = moduser32.NewProc("LoadImageW")
)

const (
	wmSetIcon      = 0x0080
	iconSmall      = 0 // ICON_SMALL
	iconBig        = 1 // ICON_BIG
	imageIcon      = 1 // IMAGE_ICON
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040
)

// SetWindowIcon 把 icoPath 设为窗口的大/小图标(任务栏、Alt+Tab、标题栏)。
// 成功返回 true;文件缺失或加载失败时静默返回 false(保留默认图标)。
// 必须在窗口句柄有效后调用(Gio 的 FrameEvent 首帧即可)。
func SetWindowIcon(hwnd uintptr, icoPath string) bool {
	if hwnd == 0 || icoPath == "" {
		return false
	}
	p, err := windows.UTF16PtrFromString(icoPath)
	if err != nil {
		return false
	}
	big, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0,
		lrLoadFromFile|lrDefaultSize)
	if big == 0 {
		return false
	}
	small, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 16, 16, lrLoadFromFile)
	if small == 0 {
		small = big
	}
	procSendMessageW.Call(hwnd, wmSetIcon, iconBig, big)
	procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
	return true
}

// nav — message-only 导航窗口:ccapp 主实例监听,面板/托盘用它跨进程下发"打开某页"。
// 取代旧的"HTTP 端口文件 + URL 深链"通道:零序列化开销,且不要求主实例开监听端口。
package win32

import (
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NavWindowClass message-only 窗口类名(FindWindowW 按类名定位运行实例)。
const NavWindowClass = "ControlCenterGo-Nav"

const (
	wmCopyData = 0x004A
	// HWND_MESSAGE:仅容纳 message-only 窗口的伪父窗口。
	hwndMessage = ^uintptr(2) // (HWND)-3
)

type copyDataStruct struct {
	DwData uintptr
	CbData uint32
	LPData uintptr
}

var (
	navOnce    sync.Once
	navReady   chan uintptr
	navHandler func(string)
)

// ServeNavWindow 在专属 OS 线程上创建 message-only 窗口并常驻泵消息,
// 收到 WM_COPYDATA(UTF-16 页名)后转 goroutine 回调 onNav(不阻塞泵)。
// 窗口消息只会进创建线程的队列,所以创建与泵必须同线程;回调在窗口创建前注入,无竞态。
// 同一进程重复调用只生效一次;返回窗口句柄(0 = 失败)。
func ServeNavWindow(onNav func(string)) uintptr {
	navOnce.Do(func() {
		navReady = make(chan uintptr, 1)
		navHandler = onNav
		go func() {
			runtime.LockOSThread()
			guiInit()
			className := strPtr(NavWindowClass)
			wc := wndClassEx{
				Size:      uint32(unsafe.Sizeof(wndClassEx{})),
				WndProc:   windows.NewCallback(navWndProc),
				Instance:  windows.Handle(guiInstance),
				ClassName: className,
			}
			procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
			h, _, _ := procCreateWindow.Call(
				0,
				uintptr(unsafe.Pointer(className)),
				0,
				0, // message-only 窗口不可见
				0, 0, 0, 0,
				hwndMessage, 0, guiInstance, 0)
			navReady <- h
			if h == 0 {
				return
			}
			var m msg
			for {
				r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
				if r == 0 || r == 0xFFFFFFFF {
					return
				}
				procTranslate.Call(uintptr(unsafe.Pointer(&m)))
				procDispatch.Call(uintptr(unsafe.Pointer(&m)))
			}
		}()
	})
	return <-navReady
}

// navWndProc 只处理 WM_COPYDATA,其余交回默认处理。
// windows.NewCallback 要求固定签名,且始终运行在消息泵线程。
// 说明:lParam 是 OS 送入的 COPYDATASTRUCT 指针,uintptr→Pointer 转换是
// WndProc 处理消息的唯一途径;go vet unsafeptr 对此报误报,属预期。
func navWndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == wmCopyData && lParam != 0 {
		cd := (*copyDataStruct)(unsafe.Pointer(lParam)) //nolint:govet // WndProc 收到的 lParam 本就是有效指针
		if cd.CbData > 0 && cd.LPData != 0 {
			page := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(cd.LPData)), cd.CbData/2)) //nolint:govet // 同上
			if navHandler != nil && page != "" {
				go navHandler(page)
			}
		}
		return 1
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// NavNotify 向运行中的主实例发送导航请求(SendMessage 阻塞直至被处理)。
// 返回 false 表示当前没有带导航窗口的实例(未启动或旧版本)。
func NavNotify(page string) bool {
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(strPtr(NavWindowClass))), 0)
	if h == 0 {
		return false
	}
	u16, err := windows.UTF16FromString(page)
	if err != nil {
		return false
	}
	cd := copyDataStruct{
		DwData: 1,
		CbData: uint32(len(u16) * 2),
		LPData: uintptr(unsafe.Pointer(&u16[0])),
	}
	r, _, _ := procSendMessageW.Call(h, wmCopyData, 0, uintptr(unsafe.Pointer(&cd)))
	return r == 1
}

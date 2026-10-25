// Package win32 — 系统托盘(Shell_NotifyIcon)+ 隐藏消息窗口。
package win32

import (
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 托盘消息。
const (
	wmTrayIcon = 0x8000 // WM_APP
	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 1
	nifIcon    = 2
	nifTip     = 4
	nifInfo    = 0x10
	wmLButtonUp   = 0x0202
	wmLButtonDown = 0x0201
	wmRButtonUp   = 0x0205
	wmRButtonDown = 0x0204
	wmCommand  = 0x0111
	wmDestroy  = 0x0002
	wmClose    = 0x0010
)

type notifyIconData struct {
	Size           uint32
	HWnd           windows.HWND
	ID             uint32
	Flags          uint32
	CallbackMessage uint32
	Icon           windows.Handle
	Tip            [128]uint16
	State          uint32
	StateMask      uint32
	Info           [256]uint16
	InfoVersion    uint32
	InfoTitle      [64]uint16
	InfoFlags      uint32
}

var (
	modshell32        = windows.NewLazySystemDLL("shell32.dll")
	procShellNotify   = modshell32.NewProc("Shell_NotifyIconW")
	procCreateMenu    = moduser32.NewProc("CreatePopupMenu")
	procAppendMenu    = moduser32.NewProc("AppendMenuW")
	procSetMenuDefault = moduser32.NewProc("SetMenuDefaultItem")
	procTrackPopup    = moduser32.NewProc("TrackPopupMenuEx")
	procSetForeground = moduser32.NewProc("SetForegroundWindow")
	procPostQuit      = moduser32.NewProc("PostQuitMessage")
	procGetMessage    = moduser32.NewProc("GetMessageW")
	procTranslate     = moduser32.NewProc("TranslateMessage")
	procDispatch      = moduser32.NewProc("DispatchMessageW")
	procCreateWindow  = moduser32.NewProc("CreateWindowExW")
	procDefWindowProc = moduser32.NewProc("DefWindowProcW")
	procRegisterClass = moduser32.NewProc("RegisterClassExW")
	procDestroyWindow = moduser32.NewProc("DestroyWindow")
	procLoadIcon      = moduser32.NewProc("LoadIconW")
	procLoadCursor    = moduser32.NewProc("LoadCursorW")
	procCreateIconInd = moduser32.NewProc("CreateIconIndirect")
	procCheckMenu     = moduser32.NewProc("CheckMenuItem")
	procSetMenuInfo   = moduser32.NewProc("SetMenuInfo")
	procDestroyMenu   = moduser32.NewProc("DestroyMenu")
	procGetCursorPos  = moduser32.NewProc("GetCursorPos")
)

// point 对应 Win32 POINT。
type point struct{ X, Y int32 }

type wndClassEx struct {
	Size, Style         uint32
	WndProc             uintptr
	ClsExtra, WndExtra  int32
	Instance            windows.Handle
	Icon, Cursor        windows.Handle
	Background          windows.Handle
	MenuName, ClassName *uint16
	IconSm              windows.Handle // hIconSm(结构总大小须为 80 字节)
}

type msg struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	_       uint32
}

// MenuItem 菜单项。
type MenuItem struct {
	ID      int
	Text    string
	Checked bool
	Separator bool
	Disabled  bool
	Bold      bool
	// Submenu 非空时作为弹出子菜单挂载(ID 被忽略,子项 ID 全局唯一)。
	Submenu []MenuItem
}

// Tray 托盘图标 + 菜单。
type Tray struct {
	hwnd     windows.HWND
	nid      notifyIconData
	mu       sync.Mutex
	menu     []MenuItem
	onClick  func()
	commands map[int]func()

	IconChanged chan struct{}
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  windows.Handle
	HbmColor windows.Handle
}

// NewTray 创建托盘(生成 ARGB 图标)。
// 窗口句柄与消息循环必须同在一条 OS 线程上,故在此锁定当前 goroutine 的线程;
// 调用方须与 RunMessageLoop 使用同一 goroutine。
func NewTray(tip string, iconARGB []byte, width, height int) (*Tray, error) {
	runtime.LockOSThread()
	t := &Tray{commands: map[int]func(){}}
	// 窗口类
	className, _ := windows.UTF16PtrFromString("ControlCenterTrayWnd")
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(t.wndProc),
		ClassName: className,
		Cursor:    0,
	}
	procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, err := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return nil, err
	}
	t.hwnd = windows.HWND(hwnd)

	icon := createARGBIcon(iconARGB, width, height)
	t.nid = notifyIconData{
		Size:            uint32(unsafe.Sizeof(t.nid)),
		HWnd:            t.hwnd,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayIcon,
		Icon:            icon,
	}
	utf16Into(t.nid.Tip[:], tip, 127)
	r, _, err2 := procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	if r == 0 {
		return nil, err2
	}
	return t, nil
}

// SetIcon 更新图标(ARGB 像素)。
func (t *Tray) SetIcon(iconARGB []byte, width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	icon := createARGBIcon(iconARGB, width, height)
	old := t.nid.Icon
	t.nid.Icon = icon
	procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
	if old != 0 && old != windows.InvalidHandle {
		procDeleteObject.Call(uintptr(old))
	}
}

// SetTip 更新悬浮提示。
func (t *Tray) SetTip(tip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	utf16Into(t.nid.Tip[:], tip, 127)
	t.nid.Flags = nifMessage | nifIcon | nifTip
	procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

// ShowBalloon 气泡通知。
func (t *Tray) ShowBalloon(title, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	utf16Into(t.nid.Info[:], text, 255)
	utf16Into(t.nid.InfoTitle[:], title, 63)
	t.nid.Flags = nifInfo
	procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
	t.nid.Flags = nifMessage | nifIcon | nifTip
}

// SetMenu 设置菜单项(ID 为命令号)。
func (t *Tray) SetMenu(items []MenuItem) {
	t.mu.Lock()
	t.menu = items
	t.mu.Unlock()
}

// SetOnClick 左键单击回调。
func (t *Tray) SetOnClick(fn func()) {
	t.mu.Lock()
	t.onClick = fn
	t.mu.Unlock()
}

// OnCommand 注册菜单命令。
func (t *Tray) OnCommand(id int, fn func()) {
	t.mu.Lock()
	t.commands[id] = fn
	t.mu.Unlock()
}

// wndProc 窗口过程。
func (t *Tray) wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmLButtonUp:
			t.mu.Lock()
			fn := t.onClick
			t.mu.Unlock()
			if fn != nil {
				go fn()
			}
		case wmRButtonUp, wmRButtonDown, 0x0203:
			t.showMenu()
		}
		return 0
	case wmCommand:
		t.runCommand(int(wParam & 0xFFFF))
		return 0
	case wmDestroy:
		procPostQuit.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// showMenu 在光标处弹出上下文菜单。
// TrackPopupMenuEx 签名:(hMenu, uFlags, x, y, hWnd, lppma) 共 6 参。
// 用 TPM_RETURNCMD 时系统不发 WM_COMMAND,选中项由返回值给出。
func (t *Tray) showMenu() {
	t.mu.Lock()
	menu := t.menu
	hwnd := t.hwnd
	t.mu.Unlock()
	hmenu, _, _ := procCreateMenu.Call() // CreatePopupMenu
	if hmenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hmenu)
	const ( mfString = 0; mfSeparator = 0x800; mfChecked = 8; mfDisabled = 2; mfBold = 0x10; mfPopup = 0x10 )
	// buildMenu 递归构建(含子菜单;子菜单句柄由系统随父菜单销毁)。
	var buildMenu func(items []MenuItem) uintptr
	buildMenu = func(items []MenuItem) uintptr {
		h, _, _ := procCreateMenu.Call()
		if h == 0 {
			return 0
		}
		for _, it := range items {
			if it.Separator {
				procAppendMenu.Call(h, mfSeparator, 0, 0)
				continue
			}
			if len(it.Submenu) > 0 {
				if sub := buildMenu(it.Submenu); sub != 0 {
					txt, _ := windows.UTF16PtrFromString(it.Text)
					procAppendMenu.Call(h, uintptr(mfString|mfPopup), sub, uintptr(unsafe.Pointer(txt)))
				}
				continue
			}
			flags := uintptr(mfString)
			if it.Checked {
				flags |= mfChecked
			}
			if it.Disabled {
				flags |= mfDisabled
			}
			if it.Bold {
				flags |= mfBold
			}
			txt, _ := windows.UTF16PtrFromString(it.Text)
			procAppendMenu.Call(h, flags, uintptr(it.ID), uintptr(unsafe.Pointer(txt)))
		}
		return h
	}
	for _, it := range menu {
		if it.Separator {
			procAppendMenu.Call(hmenu, mfSeparator, 0, 0)
			continue
		}
		if len(it.Submenu) > 0 {
			if sub := buildMenu(it.Submenu); sub != 0 {
				txt, _ := windows.UTF16PtrFromString(it.Text)
				procAppendMenu.Call(hmenu, uintptr(mfString|mfPopup), sub, uintptr(unsafe.Pointer(txt)))
			}
			continue
		}
		flags := uintptr(mfString)
		if it.Checked {
			flags |= mfChecked
		}
		if it.Disabled {
			flags |= mfDisabled
		}
		if it.Bold {
			flags |= mfBold
		}
		txt, _ := windows.UTF16PtrFromString(it.Text)
		procAppendMenu.Call(hmenu, flags, uintptr(it.ID), uintptr(unsafe.Pointer(txt)))
	}
	procSetForeground.Call(uintptr(hwnd))
	// MSDN 要求:托盘菜单前置窗口后补发一条空消息,否则菜单在点击别处时不会关闭
	procSendMessageW.Call(uintptr(hwnd), 0, 0, 0)
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	const ( tpmRightAlign = 0x8; tpmBottomAlign = 0x20; tpmRetCmd = 0x100 )
	r, _, _ := procTrackPopup.Call(hmenu,
		tpmRightAlign|tpmBottomAlign|tpmRetCmd,
		uintptr(pt.X), uintptr(pt.Y), uintptr(hwnd), 0)
	if cmd := int(int32(r)); cmd > 0 {
		t.runCommand(cmd)
	}
}

// runCommand 执行菜单命令回调。
func (t *Tray) runCommand(id int) {
	t.mu.Lock()
	fn := t.commands[id]
	t.mu.Unlock()
	if fn != nil {
		go fn()
	}
}

// RunMessageLoop 阻塞运行消息循环(与 NewTray 同 goroutine)。
func (t *Tray) RunMessageLoop() {
	runtime.LockOSThread()
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || r == 0xFFFFFFFF {
			break
		}
		procTranslate.Call(uintptr(unsafe.Pointer(&m)))
		procDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// Remove 移除托盘图标。
func (t *Tray) Remove() {
	t.mu.Lock()
	defer t.mu.Unlock()
	procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	if t.nid.Icon != 0 && t.nid.Icon != windows.InvalidHandle {
		procDeleteObject.Call(uintptr(t.nid.Icon))
		t.nid.Icon = 0
	}
	if t.hwnd != 0 {
		procDestroyWindow.Call(uintptr(t.hwnd))
	}
}

// createARGBIcon 由 ARGB 像素创建 HICON(自绘纯色/图像图标)。
func createARGBIcon(argb []byte, w, h int) windows.Handle {
	if w <= 0 {
		w = 32
	}
	if h <= 0 {
		h = 32
	}
	if len(argb) < w*h*4 {
		argb = make([]byte, w*h*4)
		for i := 0; i < w*h; i++ {
			argb[i*4+2] = 0 // B
			argb[i*4+1] = 0xD2 >> 1
			argb[i*4] = 0xFF
			argb[i*4+3] = 0xFF // A
		}
	}
	mask := make([]byte, ((w+7)/8)*h) // 全 0 AND 掩码
	bmp := create32bppBitmap(argb, w, h)
	maskBmp := createBitmap(mask, 1, w, h)
	ii := iconInfo{FIcon: 1, HbmColor: bmp, HbmMask: maskBmp}
	iconH, _, _ := procCreateIconInd.Call(uintptr(unsafe.Pointer(&ii)))
	// CreateIconIndirect 已复制两张位图,原句柄必须释放(GDI 对象配额很小)
	procDeleteObject.Call(uintptr(bmp))
	procDeleteObject.Call(uintptr(maskBmp))
	return windows.Handle(iconH)
}

var (
	modgdi32          = windows.NewLazySystemDLL("gdi32.dll")
	procCreateDIBSection = modgdi32.NewProc("CreateDIBSection")
	procCreateBitmap  = modgdi32.NewProc("CreateBitmap")
	procCreateCompatibleDC = modgdi32.NewProc("CreateCompatibleDC")
	procDeleteDC      = modgdi32.NewProc("DeleteDC")
	procDeleteObject  = modgdi32.NewProc("DeleteObject")
	procAlphaBlend    = modgdi32.NewProc("AlphaBlend")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes, BitCount uint16
	Compression    uint32
	SizeImage      uint32
	XPels, YPels   int32
	ClrUsed, ClrImportant uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// create32bppBitmap 创建 32bpp DIB(返回 HBITMAP)。
func create32bppBitmap(pixels []byte, w, h int) windows.Handle {
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // top-down
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = 0 // BI_RGB
	var bitsPtr *byte
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	bmpR, _, _ := procCreateDIBSection.Call(
		hdc, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	procDeleteDC.Call(hdc)
	if bmpR != 0 && bitsPtr != nil {
		copy(unsafe.Slice(bitsPtr, len(pixels)), pixels)
	}
	return windows.Handle(bmpR)
}

// createBitmap 创建单色位图。
func createBitmap(bits []byte, planes, w, h int) windows.Handle {
	bmp2, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), uintptr(planes), 1,
		uintptr(unsafe.Pointer(&bits[0])))
	return windows.Handle(bmp2)
}

// UTF16FromStringTruncated 截断到 max 字符并写入 buf(含 NUL 终止)。
func utf16Into(buf []uint16, s string, max int) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > max+1 {
		u = u[:max+1]
		u[max] = 0
	}
	copy(buf, u)
}

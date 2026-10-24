// Package win32 — 系统托盘(Shell_NotifyIcon)+ 隐藏消息窗口。
package win32

import (
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
)

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
func NewTray(tip string, iconARGB []byte, width, height int) (*Tray, error) {
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
	t.nid.Icon = icon
	procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
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
func (t *Tray) SetOnClick(fn func()) { t.onClick = fn }

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
			if t.onClick != nil {
				go t.onClick()
			}
		case wmRButtonUp, wmRButtonDown, 0x0203:
			t.showMenu()
		}
		return 0
	case wmCommand:
		id := int(wParam & 0xFFFF)
		t.mu.Lock()
		fn := t.commands[id]
		t.mu.Unlock()
		if fn != nil {
			go fn()
		}
		return 0
	case wmDestroy:
		procPostQuit.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// showMenu 弹出上下文菜单。
func (t *Tray) showMenu() {
	t.mu.Lock()
	menu := t.menu
	t.mu.Unlock()
	hmenu, _, _ := procCreateMenu.Call() // CreatePopupMenu
	if hmenu == 0 {
		return
	}
	const ( mfString = 0; mfSeparator = 0x800; mfChecked = 8; mfDisabled = 2; mfBold = 0x10 )
	for _, it := range menu {
		if it.Separator {
			procAppendMenu.Call(hmenu, mfSeparator, 0, 0)
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
	procSetForeground.Call(uintptr(t.hwnd))
	const ( tpmRightAlign = 0x8; tpmBottomAlign = 0x20; tpmRetCmd = 0x100 )
	procTrackPopup.Call(hmenu, tpmRightAlign|tpmBottomAlign|tpmRetCmd, 0, 0, 0, uintptr(t.hwnd), 0)
}

// RunMessageLoop 阻塞运行消息循环(应在主 goroutine)。
func (t *Tray) RunMessageLoop() {
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

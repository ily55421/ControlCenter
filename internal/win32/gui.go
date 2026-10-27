// Package win32 — 基础控件窗口(按钮/只读编辑框/静态文本),纯 syscall Win32 GUI。
package win32

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsChild    = 0x40000000
	wsVisible0 = 0x10000000

	bsPushButton  = 0
	esMultiline   = 4
	esAutoVScroll = 0x40
	esReadOnly    = 0x800
	wsVScroll     = 0x00200000

	wmSetFont    = 0x0030
	wmGetTextLen = 0x000E
	emSetSel     = 0x00B1
	emReplaceSel = 0x00C2

	bnClicked = 0
)

var (
	procGetModuleHandleW  = modkernel32.NewProc("GetModuleHandleW")
	procSetWindowTextW    = moduser32.NewProc("SetWindowTextW")
	procGetWindowTextLenW = moduser32.NewProc("GetWindowTextLengthW")
	procSendMessageW      = moduser32.NewProc("SendMessageW")
	procGetStockObject    = modgdi32.NewProc("GetStockObject")
	procMoveWindow        = moduser32.NewProc("MoveWindow")
)

var (
	guiInstance uintptr
	guiFont     uintptr
	onceGUI     sync.Once
)

func guiInit() {
	onceGUI.Do(func() {
		guiInstance, _, _ = procGetModuleHandleW.Call(0)
		// DEFAULT_GUI_FONT = 17
		guiFont, _, _ = procGetStockObject.Call(17)
	})
}

// SimpleWindow 基础控件宿主窗口。
type SimpleWindow struct {
	hwnd      windows.HWND
	mu        sync.Mutex
	controls  map[int]windows.HWND // id → hwnd(按钮)
	edit      windows.HWND
	onCommand map[int]func()
	nextID    int
}

// NewSimpleWindow 创建窗口(标题/尺寸);阻塞调用 Run() 进入消息循环。
// 与 Run 必须同 goroutine 且锁定线程(窗口句柄线程亲和)。
func NewSimpleWindow(title string, width, height int) (*SimpleWindow, error) {
	runtime.LockOSThread()
	guiInit()
	w := &SimpleWindow{controls: map[int]windows.HWND{}, onCommand: map[int]func(){}}
	className, _ := windows.UTF16PtrFromString("ControlCenterGUIWnd")
	icon, _, _ := procLoadIcon.Call(guiInstance, 1)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(w.wndProc),
		Instance:  windows.Handle(guiInstance),
		ClassName: className,
		Icon:      windows.Handle(icon),
		IconSm:    windows.Handle(icon),
	}
	procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
	titlePtr, _ := windows.UTF16PtrFromString(title)
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	x := int(sw)/2 - width/2
	y := int(sh)/2 - height/2
	hwnd, _, err := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titlePtr)),
		0x00CA0000, // WS_OVERLAPPEDWINDOW
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, guiInstance, uintptr(unsafe.Pointer(w)))
	if hwnd == 0 {
		return nil, err
	}
	w.hwnd = windows.HWND(hwnd)
	return w, nil
}

// AddButton 添加按钮,返回命令 ID。
func (w *SimpleWindow) AddButton(text string, x, y, bw, bh int, onClick func()) int {
	guiInit()
	w.mu.Lock()
	w.nextID++
	id := w.nextID
	w.mu.Unlock()
	textPtr, _ := windows.UTF16PtrFromString(text)
	h, _, _ := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(strPtr("BUTTON"))),
		uintptr(unsafe.Pointer(textPtr)),
		wsChild|wsVisible0|bsPushButton,
		uintptr(x), uintptr(y), uintptr(bw), uintptr(bh),
		uintptr(w.hwnd), uintptr(id), guiInstance, 0)
	w.controls[id] = windows.HWND(h)
	procSendMessageW.Call(h, wmSetFont, guiFont, 1)
	w.mu.Lock()
	w.onCommand[id] = onClick
	w.mu.Unlock()
	return id
}

// AddEdit 添加只读多行日志框。
func (w *SimpleWindow) AddEdit(x, y, ew, eh int) {
	guiInit()
	h, _, _ := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(strPtr("EDIT"))),
		0,
		wsChild|wsVisible0|esMultiline|esAutoVScroll|esReadOnly|wsVScroll,
		uintptr(x), uintptr(y), uintptr(ew), uintptr(eh),
		uintptr(w.hwnd), 0, guiInstance, 0)
	w.edit = windows.HWND(h)
	procSendMessageW.Call(h, wmSetFont, guiFont, 1)
}

// AppendLog 向日志框追加一行。
func (w *SimpleWindow) AppendLog(text string) {
	if w.edit == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	line, _ := windows.UTF16PtrFromString(text + "\r\n")
	len, _, _ := procGetWindowTextLenW.Call(uintptr(w.edit))
	procSendMessageW.Call(uintptr(w.edit), emSetSel, len, len)
	procSendMessageW.Call(uintptr(w.edit), emReplaceSel, 0, uintptr(unsafe.Pointer(line)))
}

// SetLog 替换日志全文。
func (w *SimpleWindow) SetLog(text string) {
	if w.edit == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	t, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(uintptr(w.edit), uintptr(unsafe.Pointer(t)))
}

// SetButtonText 更新按钮文字。
func (w *SimpleWindow) SetButtonText(id int, text string) {
	h := w.controls[id]
	if h == 0 {
		return
	}
	t, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(t)))
}

// Show 显示窗口。
func (w *SimpleWindow) Show() {
	const swShow = 5
	procShowWindow.Call(uintptr(w.hwnd), swShow)
}

// Run 阻塞消息循环。
func (w *SimpleWindow) Run() {
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

// wndProc 窗口过程。
func (w *SimpleWindow) wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCommand:
		if wParam>>16 == bnClicked {
			id := int(wParam & 0xFFFF)
			w.mu.Lock()
			fn := w.onCommand[id]
			w.mu.Unlock()
			if fn != nil {
				go fn() // 按钮回调放 goroutine,避免阻塞消息循环
			}
		}
		return 0
	case wmDestroy:
		procPostQuit.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

var cachedStrPtr = map[string]*uint16{}

func strPtr(s string) *uint16 {
	if p, ok := cachedStrPtr[s]; ok {
		return p
	}
	p, _ := windows.UTF16PtrFromString(s)
	cachedStrPtr[s] = p
	return p
}

// IsElevated 当前进程是否以管理员令牌运行。
func IsElevated() bool {
	t, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer t.Close()
	return t.IsElevated()
}

// RelaunchElevated 以管理员重新启动自身(带参数);成功返回 true。
func RelaunchElevated(args string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return false
	}
	params, _ := windows.UTF16PtrFromString(args)
	modshell := windows.NewLazySystemDLL("shell32.dll")
	p := modshell.NewProc("ShellExecuteExW")
	type info struct {
		Size      uint32
		Mask      uint32
		Hwnd      uintptr
		Verb      *uint16
		File      *uint16
		Params    *uint16
		Dir       *uint16
		Show      int32
		InstApp   uintptr
		IDList    uintptr
		Class     *uint16
		HKeyClass uintptr
		HotKey    uint32
		Icon      uintptr
		Monitor   uintptr
	}
	// SEE_MASK_NOCLOSEPROCESS = 0x40
	sei := info{Size: uint32(unsafe.Sizeof(info{})), Mask: 0x40,
		Verb: verb, File: file, Params: params, Show: 5}
	r, _, _ := p.Call(uintptr(unsafe.Pointer(&sei)))
	return r != 0
}

var _ = syscall.SyscallN

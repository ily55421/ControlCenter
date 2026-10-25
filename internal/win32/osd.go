// Package win32 — OSD 弹窗(置顶分层窗口 + GDI 文本绘制,复刻 FnKey OsdForm 行为)。
package win32

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsExLayered    = 0x00080000
	wsExTopmost    = 0x00000008
	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	wsPopup        = 0x80000000
	wsVisible      = 0x10000000

	wmTimer    = 0x0113
	wmPaint    = 0x000F
	wmEraseBkg = 0x0014

	alphaOpaque = 255
)

// OSD 窗口的固定定时器 ID(常量:避免跨 goroutine 读写共享字段)。
const osdTimerID = uintptr(1)

var (
	procSetLayeredAttr = moduser32.NewProc("SetLayeredWindowAttributes")
	procSetWindowPos   = moduser32.NewProc("SetWindowPos")
	procShowWindow     = moduser32.NewProc("ShowWindow")
	procInvalidateRect = moduser32.NewProc("InvalidateRect")
	procUpdateWindow   = moduser32.NewProc("UpdateWindow")
	procBeginPaint     = moduser32.NewProc("BeginPaint")
	procEndPaint       = moduser32.NewProc("EndPaint")
	procFillRect       = moduser32.NewProc("FillRect")
	procDrawTextW      = moduser32.NewProc("DrawTextW")
	procCreateSolidBrush = modgdi32.NewProc("CreateSolidBrush")
	procDeleteObjGdi   = modgdi32.NewProc("DeleteObject")
	procSetTimer       = moduser32.NewProc("SetTimer")
	procKillTimer      = moduser32.NewProc("KillTimer")
	procGetSystemMetrics = moduser32.NewProc("GetSystemMetrics")
	procSetLayeredWindowAttributes = moduser32.NewProc("SetLayeredWindowAttributes")
	procDefWindowProcW = moduser32.NewProc("DefWindowProcW")
)

type paintStruct struct {
	Hdc         windows.Handle
	Erase       int32
	PaintRect   rectStruct
	Restore     int32
	_           [64]byte
}

type rectStruct struct {
	Left, Top, Right, Bottom int32
}

// OSD 置顶 OSD 窗口(文字/进度)。
type OSD struct {
	mu      sync.Mutex
	hwnd    windows.HWND
	text    string
	barMax  int
	barVal  int
	showBar bool
	bgColor uint32 // 0x00BBGGRR
	fgColor uint32
	alpha   byte
	hideAfterMs int
}

// NewOSD 创建 OSD 窗口(初始隐藏)。
func NewOSD() (*OSD, error) {
	o := &OSD{bgColor: 0x00101010, fgColor: 0x00FFFFFF, alpha: 230}
	className, _ := windows.UTF16PtrFromString("ControlCenterOSDWnd")
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(o.wndProc),
		ClassName: className,
	}
	procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, err := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)),
		wsPopup,
		50, 60, 429, 90,
		0, 0, 0, uintptr(unsafe.Pointer(o)))
	if hwnd == 0 {
		return nil, err
	}
	o.hwnd = windows.HWND(hwnd)
	procSetLayeredAttr.Call(uintptr(o.hwnd), 0, alphaOpaque, 2 /*LWA_ALPHA*/)
	return o, nil
}

// Show 显示 OSD(带超时自动淡出;barMax<=0 不显示进度条)。
func (o *OSD) Show(text string, barMax, barVal int) {
	o.mu.Lock()
	o.text = text
	o.barMax, o.barVal = barMax, barVal
	o.showBar = barMax > 0
	o.mu.Unlock()

	w := 429
	h := 90
	if o.showBar {
		h = 110
	}
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	x := int(sw)/2 - w/2
	y := int(sh)*8/10 - h
	procSetWindowPos.Call(uintptr(o.hwnd), ^uintptr(0), /*HWND_TOPMOST*/
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0040)
	procShowWindow.Call(uintptr(o.hwnd), 4 /*SW_SHOWNOACTIVATE*/)
	procSetLayeredAttr.Call(uintptr(o.hwnd), 0, 230, 2)
	procInvalidateRect.Call(uintptr(o.hwnd), 0, 1)
	procUpdateWindow.Call(uintptr(o.hwnd))
	// 1.5 秒后开始淡出
	procSetTimer.Call(uintptr(o.hwnd), osdTimerID, 1500, 0)
}

// Hide 立即隐藏。
func (o *OSD) Hide() {
	procKillTimer.Call(uintptr(o.hwnd), osdTimerID)
	procShowWindow.Call(uintptr(o.hwnd), 0)
}

// wndProc OSD 窗口过程。
func (o *OSD) wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTimer:
		if uintptr(wParam) == osdTimerID {
			procKillTimer.Call(uintptr(hwnd), osdTimerID)
			go o.fadeOut()
		}
		return 0
	case wmEraseBkg:
		return 1
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			o.paint(hdc)
			procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	case wmDestroy:
		procPostQuit.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

// paint GDI 绘制(黑底圆角近似 + 文本 + 进度条)。
func (o *OSD) paint(hdc uintptr) {
	o.mu.Lock()
	text := o.text
	barMax, barVal, showBar := o.barMax, o.barVal, o.showBar
	bg, fg := o.bgColor, o.fgColor
	o.mu.Unlock()

	var rc rectStruct
	rc.Left, rc.Top = 0, 0
	rc.Right, rc.Bottom = 429, 110
	bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(bg))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bgBrush)
	procDeleteObjGdi.Call(bgBrush)

	// 文本(DT_CENTER|DT_VCENTER|DT_SINGLELINE=0x25)
	if text != "" {
		tr := rc
		tr.Bottom = 80
		t16, _ := windows.UTF16PtrFromString(text)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(t16)), uintptr(len([]rune(text))),
			uintptr(unsafe.Pointer(&tr)), 0x25)
	}
	_ = fg
	// 进度条
	if showBar && barMax > 0 {
		w := barVal * 240 / barMax
		if w < 8 {
			w = 8
		}
		if w > 240 {
			w = 240
		}
		var br rectStruct
		br.Left, br.Top = 94, 84
		br.Right, br.Bottom = 94+int32(w), 100
		brush, _, _ := procCreateSolidBrush.Call(uintptr(fg))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&br)), brush)
		procDeleteObjGdi.Call(brush)
	}
}

// fadeOut 淡出(50ms 步进,透明度 −46/步,与原版节奏一致)。
func (o *OSD) fadeOut() {
	for a := 230; a > 30; a -= 46 {
		procSetLayeredAttr.Call(uintptr(o.hwnd), 0, uintptr(a), 2)
		sleepMsOsd(50)
	}
	procSetLayeredAttr.Call(uintptr(o.hwnd), 0, 30, 2)
	procShowWindow.Call(uintptr(o.hwnd), 0)
}

func sleepMsOsd(ms int) { timeSleepOsd(ms) }

func timeSleepOsd(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

var _ = syscall.SyscallN

func timerSleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func timeNewTimer(ms int) *timeTimer { t := time.NewTimer(time.Duration(ms) * time.Millisecond); return &timeTimer{t} }

type timeTimer struct{ *time.Timer }

// PumpMessages 泵消息 maxWaitMs 毫秒(PeekMessage 循环;无消息时休眠)。
func (o *OSD) PumpMessages(pollMs int) {
	const pmRemove = 1
	var m msg
	for {
		r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m)), uintptr(o.hwnd), 0, 0, pmRemove)
		if r == 0 {
			break
		}
		procTranslate.Call(uintptr(unsafe.Pointer(&m)))
		procDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
	time.Sleep(time.Duration(pollMs) * time.Millisecond)
}

var procPeekMessage = moduser32.NewProc("PeekMessageW")

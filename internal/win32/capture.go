// capture.go — 抓取窗口真实像素。
//
// 用途是开发期视觉回归:规范验收要求"真实运行截图",headless 渲染出来的
// 只是 ops 的近似,不能替代实际合成的结果(缩放、字体缓存、D3D 呈现路径都不同)。
//
// 首选 PrintWindow(PW_RENDERFULLCONTENT):按客户区尺寸取,窗口被最小化或
// 被其它窗口遮挡也能拿到内容;它拿不到东西时(GI/硬件合成路径返回全黑)
// 退回屏幕 BitBlt —— 那条路要求窗口在前台,所以顺带前置一次。
package win32

import (
	"fmt"
	"image"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetWindowRect   = moduser32.NewProc("GetWindowRect")
	procGetClientRect   = moduser32.NewProc("GetClientRect")
	procGetWindowTextW  = moduser32.NewProc("GetWindowTextW")
	procPrintWindow     = moduser32.NewProc("PrintWindow")
	procGetDC           = moduser32.NewProc("GetDC")
	procReleaseDC       = moduser32.NewProc("ReleaseDC")
	procCreateCompatBmp = modgdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject    = modgdi32.NewProc("SelectObject")
	procBitBlt          = modgdi32.NewProc("BitBlt")
	procGetDIBits       = modgdi32.NewProc("GetDIBits")
)

const (
	srccopy             = 0x00CC0020
	dibRGBColors        = 0
	pwRenderFullContent = 2 // Win8.1+:让 DirectComposition/D3D 内容也画进 DC
)

// WindowRect 返回标题匹配、面积最大的可见顶层窗口在屏幕上的物理矩形。
func WindowRect(title string) (image.Rectangle, error) {
	var rect image.Rectangle
	var err error
	withPMv2Thread(func() { rect, err = windowRect(title) })
	return rect, err
}

// windowRect 必须在 Per-Monitor V2 感知的线程上调用:
// 不感知的线程会被 DPI 虚拟化,拿到缩小后的矩形。
func windowRect(title string) (image.Rectangle, error) {
	h := largestWindowByTitle(title)
	if h == 0 {
		return image.Rectangle{}, fmt.Errorf("找不到可见窗口 %q", title)
	}
	var r rectStruct
	if ret, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return image.Rectangle{}, fmt.Errorf("GetWindowRect 失败(窗口 %q)", title)
	}
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)), nil
}

// withPMv2Thread 在一条 Per-Monitor V2 感知的专用线程上执行 fn。
// Go 会在 UnlockOSThread 后销毁这条线程,所以线程级 DPI 上下文不会波及他人。
func withPMv2Thread(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiAwarePerMonitorV2)
	defer procSetThreadDpiAwarenessContext.Call(prev)
	fn()
}

// largestWindowByTitle 挑标题相符且面积最大的顶层窗口。
// FindWindowW 只按 Z 序返回最前的一个,而 OSD 通知窗与主窗口同名(实测拿到 160×28),
// 截图必须锁定主窗口,所以按面积选。
func largestWindowByTitle(title string) uintptr {
	best, bestArea := uintptr(0), 0
	buf := make([]uint16, 512)
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1
		}
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf[:n]) != title {
			return 1
		}
		var r rectStruct
		if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
			return 1
		}
		if area := int(r.Right-r.Left) * int(r.Bottom-r.Top); area > bestArea {
			best, bestArea = hwnd, area
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return best
}

// CaptureWindow 抓取窗口客户区的真实像素。
func CaptureWindow(title string) (*image.RGBA, error) {
	var img *image.RGBA
	var err error
	withPMv2Thread(func() {
		// 最小化时窗口矩形是 160×28 的虚拟区,任何取图方式都只能拿到这么点 → 先还原
		if h := largestWindowByTitle(title); h != 0 && isIconic(h) {
			FocusWindowHandle(h)
			time.Sleep(400 * time.Millisecond)
		}
		img, err = captureViaPrintWindow(title)
		if img != nil && !isBlank(img) {
			return
		}
		// PrintWindow 拿不到实际合成内容时,前置窗口后直接抓屏幕
		if h := largestWindowByTitle(title); h == 0 || !FocusWindowHandle(h) {
			return
		}
		img, err = captureViaScreen(title)
	})
	return img, err
}

// isIconic 窗口是否处于最小化状态。
func isIconic(h uintptr) bool {
	r, _, _ := procIsIconic.Call(h)
	return r != 0
}

// TopWindows 返回全部可见顶层窗口的标题与矩形(诊断同名窗口用)。
func TopWindows() []WindowInfo {
	var out []WindowInfo
	buf := make([]uint16, 512)
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1
		}
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		var r rectStruct
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		out = append(out, WindowInfo{
			HWND:  hwnd,
			Title: windows.UTF16ToString(buf[:n]),
			Rect:  image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)),
			Icon:  isIconic(hwnd),
		})
		return 1
	})
	withPMv2Thread(func() { procEnumWindows.Call(cb, 0) })
	return out
}

// WindowInfo 一个可见顶层窗口的诊断信息。
type WindowInfo struct {
	HWND  uintptr
	Title string
	Rect  image.Rectangle
	Icon  bool
}

// captureViaPrintWindow 用 PrintWindow 把窗口内容渲染到内存位图。
func captureViaPrintWindow(title string) (*image.RGBA, error) {
	h := largestWindowByTitle(title)
	if h == 0 {
		return nil, fmt.Errorf("找不到窗口 %q", title)
	}
	var cr rectStruct
	if ret, _, _ := procGetClientRect.Call(h, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return nil, fmt.Errorf("GetClientRect 失败")
	}
	w, ht := int(cr.Right-cr.Left), int(cr.Bottom-cr.Top)
	if w <= 0 || ht <= 0 {
		return nil, fmt.Errorf("客户区尺寸无效: %dx%d", w, ht)
	}
	hdcWin, _, _ := procGetDC.Call(h)
	if hdcWin == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(h, hdcWin)

	img, err := blitInto(hdcWin, w, ht, func(hdc uintptr) bool {
		ret, _, _ := procPrintWindow.Call(h, hdc, uintptr(pwRenderFullContent))
		return ret != 0
	})
	if err != nil {
		return nil, fmt.Errorf("PrintWindow: %w", err)
	}
	return img, nil
}

// captureViaScreen 抓屏幕上的对应矩形(要求窗口在前台且未被遮挡)。
func captureViaScreen(title string) (*image.RGBA, error) {
	rect, err := windowRect(title)
	if err != nil {
		return nil, err
	}
	w, ht := rect.Dx(), rect.Dy()
	if w <= 0 || ht <= 0 {
		return nil, fmt.Errorf("窗口尺寸无效: %v", rect)
	}
	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC(NULL) 失败")
	}
	defer procReleaseDC.Call(0, hdcScreen)

	img, err := blitInto(hdcScreen, w, ht, func(hdc uintptr) bool {
		ret, _, _ := procBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(ht),
			hdcScreen, uintptr(rect.Min.X), uintptr(rect.Min.Y), srccopy)
		return ret != 0
	})
	if err != nil {
		return nil, fmt.Errorf("屏幕 BitBlt: %w", err)
	}
	return img, nil
}

// blitInto 在 srcDC 同源的内存 DC 里开一块 w×ht 位图,用 paint 回调填充,
// 再读出 RGBA。BGRA→RGBA 的字节序在这里统一处理。
func blitInto(srcDC uintptr, w, ht int, paint func(hdc uintptr) bool) (*image.RGBA, error) {
	hdcMem, _, _ := procCreateCompatibleDC.Call(srcDC)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(hdcMem)

	hbmp, _, _ := procCreateCompatBmp.Call(srcDC, uintptr(w), uintptr(ht))
	if hbmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(hbmp)

	old, _, _ := procSelectObject.Call(hdcMem, hbmp)
	defer procSelectObject.Call(hdcMem, old)

	if !paint(hdcMem) {
		return nil, fmt.Errorf("填充失败")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, ht))
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(ht) // 负值 = top-down,省去二次翻行
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = dibRGBColors
	if ret, _, _ := procGetDIBits.Call(hdcMem, hbmp, 0, uintptr(ht),
		uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors); ret == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}

// isBlank 判断是否没有任何彩色像素。
// 只看 RGB:blitInto 会把 alpha 一律写成 0xFF,连 alpha 一起判会永远"非空"。
func isBlank(img *image.RGBA) bool {
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i]|img.Pix[i+1]|img.Pix[i+2] != 0 {
			return false
		}
	}
	return true
}

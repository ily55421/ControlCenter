// Package osdnotifier — OSD 通知器(自有线程窗口 + 消息泵,兼容 hotkeys.OSDShower)。
package osdnotifier

import (
	"runtime"
	"time"

	"controlcenter/internal/win32"
)

// Notifier OSD 服务。
type Notifier struct {
	reqs chan osdReq
}

type osdReq struct {
	text   string
	barMax int
	barVal int
}

// New 创建通知器。
func New() *Notifier { return &Notifier{reqs: make(chan osdReq, 16)} }

// Show 实现 hotkeys.OSDShower(非阻塞投递)。
func (n *Notifier) Show(text string, barMax, barVal int) {
	select {
	case n.reqs <- osdReq{text, barMax, barVal}:
	default:
	}
}

// Run 阻塞运行:创建 OSD 窗口,轮询请求并泵消息。
// goroutine 不等于线程——GetMessage/DispatchMessage 重排后可能换到别的线程,
// 窗口与定时器都是线程亲和的,必须锁定当前线程。
func (n *Notifier) Run() {
	runtime.LockOSThread()
	osd, err := win32.NewOSD()
	if err != nil {
		return
	}
	for {
		select {
		case req := <-n.reqs:
			osd.Show(req.text, req.barMax, req.barVal)
		default:
		}
		osd.PumpMessages(50)
	}
}

var _ = time.Now

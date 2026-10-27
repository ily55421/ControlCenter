// gui — 原生窗口主循环与外壳(侧栏/顶栏/页调度/轮询/快照)。
//
// 性能模型:
//   - 渲染:Gio 即时模式 + D3D11,只有数据变化时 Invalidate 重绘,静止时零 GPU 负载;
//   - 执行:后台轮询协程按当前页择要采样(系统页 2s,节能页 1s),不再有 HTTP+JSON 序列化,
//     也不再让慢速 EC 读阻塞其他面板 —— 轮询与动作全部走直连引擎(见 backend.go)。
package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"controlcenter/internal/cpuoc"
	"controlcenter/internal/win32"
)

// g 当前 GUI 实例(Run 创建;主题与绘制辅助经它取主题)。
var g *GUI

var windowTitle = "Control Center"

// f32Translate 平移变换。
func f32Translate(x, y float32) f32.Affine2D {
	return f32.NewAffine2D(1, 0, x, 0, 1, y)
}

// 页索引。
const (
	pgSystem = iota
	pgKB
	pgFlexikey
	pgCharger
	pgCPU
	pgGPU
	pgBat
	pgEnergy
	pgPref
	pgCount
)

var pageTitles = [pgCount]string{"系统监控", "LED 设备", "FlexiKey", "FlexiCharger", "CPU 超频", "GPU 超频", "电池", "节能计划", "偏好"}
var pageIcons = [pgCount]string{"system", "kb", "fk", "ch", "cpu", "gpu", "bat", "en", "pf"}

// pageAlias 原版协议/启动器使用的页名 → 页索引(与 Web PAGE_ALIAS 一致)。
var pageAlias = map[string]int{
	"system": pgSystem, "home": pgSystem, "power": pgSystem, "fan": pgSystem, "mode": pgSystem, "monitor": pgSystem,
	"kb": pgKB, "keyboard": pgKB, "perkey": pgKB, "led": pgKB,
	"flexikey": pgFlexikey, "flexiacc": pgFlexikey,
	"charger": pgCharger, "flexicharger": pgCharger,
	"cpu": pgCPU, "cpuoc": pgCPU,
	"gpu": pgGPU, "gpuoc": pgGPU,
	"bat": pgBat, "battery": pgBat,
	"energy": pgEnergy, "energysave": pgEnergy,
	"pref": pgPref, "preference": pgPref, "preferences": pgPref, "setting": pgPref, "settings": pgPref,
}

// FAN_MODES 与 Web 一致。
var fanModes = [10]string{"自动", "最大", "静音", "x3", "x4", "Max Q", "自定义", "x7", "无噪声", "IFSC"}

var modeNames = [5]string{"安静模式", "节能模式", "性能模式", "娱乐模式", "安静增强"}
var modeShort = [5]string{"安静", "节能", "性能", "娱乐", "安静增强"}

// ---------- 快照 ----------

// sysSnap 系统页全量快照(页进入/动作后整体重取)。
type sysSnap struct {
	st     Status
	opts   ModeOptions
	fanCfg FanConfigOut
	tables [3]FanTableOut
	ad     AntiDustOut
	pref   Pref
	gpu    GPUOCOut
}

// liveSnap 系统页 2s 轮询快照。
// Util/FreqMHz/Temp 为自采样:page6 实时区依赖 BIOS/XTU 通道(无 XTU 服务的
// 机器恒为零),利用率=GetSystemTimes 差分、频率=powrprof、温度=EC 风扇包。
type LiveSnap struct {
	Live                FanLiveOut
	Ci                  cpuoc.Info
	Util, FreqMHz, Temp int
}

type cpuSnap struct {
	Info     cpuoc.Info
	Params   cpuoc.Params
	Defaults cpuoc.DefaultParams
}

type chargerSnap struct {
	Pref Pref
	Bat  BatteryOut
}

// ---------- GUI ----------

type GUI struct {
	win *app.Window
	th  *material.Theme
	be  *Backend
	ops op.Ops

	page       int // 当前页(UI 线程)
	pageAtomic atomic.Int32
	sideMin    bool
	maximized  bool // 窗口是否已最大化(自绘标题栏按钮图标随之切换)

	navReq <-chan string // 跨进程深链(ccapp 注入)
	msgCh  chan func()   // 后台协程 → UI 线程的 UI 更新
	quit   func()        // 窗口关闭回调(ccapp 清理标志)
	done   chan struct{}
	dying  atomic.Bool
	paused atomic.Bool

	toastS toastState

	// 表盘动画值(UI 线程;目标来自快照,每帧向目标收敛)
	cpuDial, fanCpuDial, fanGpuDial, chDial animVal

	// CPU 历史(1s 一点 × 60 = 1 分钟窗口;轮询协程写/UI 读)
	cpuTempH []float64
	cpuUtilH []float64

	// 风扇曲线拖拽状态(-1 = 无)
	curveDragFan, curveDragPt int

	// 快照(原子指针,后台写/UI 读)
	sys      atomic.Pointer[sysSnap]
	live     atomic.Pointer[LiveSnap]
	cpu      atomic.Pointer[cpuSnap]
	kb       atomic.Pointer[KBStateOut]
	charger  atomic.Pointer[chargerSnap]
	bat      atomic.Pointer[BatteryOut]
	fk       atomic.Pointer[FlexikeyOut]
	prefSnap atomic.Pointer[Pref]

	// 节能页历史
	histMu   sync.Mutex
	wattHist []float64
	histMeta EnergySampleOut

	// 色环交互
	wheelAngle float64
	wheelHex   string

	// 指针/键盘输入 tag
	wheelTag byte
	keyTag   byte

	w widgets
}

// animVal 表盘平滑动画:cur 向 target 指数收敛,收敛中持续请求下一帧。
type animVal struct {
	cur     float32
	started bool
}

// tick 推进一帧并返回当前显示值;未收敛时请求重绘(60fps 平滑)。
func (a *animVal) tick(target float32) float32 {
	if !a.started {
		a.started, a.cur = true, target
	}
	diff := target - a.cur
	if absF(diff) < 0.4 {
		a.cur = target
		return a.cur
	}
	a.cur += diff * 0.22
	g.win.Invalidate()
	return a.cur
}

// txtDims 录制一次文本绘制并返回其尺寸,供同一帧内按坐标回放。
// 用于"先量后放"的居中排版:避免为定位再 shaping 一遍。
func txtDims(gtx C, s string, st textStyle) (D, op.CallOp) {
	gtx.Constraints.Min = image.Point{}
	macro := op.Record(gtx.Ops)
	d := txt(gtx, s, st)
	return d, macro.Stop()
}

// replayText 在 (x, y) 回放 txtDims 录制的文本。
func replayText(gtx C, call op.CallOp, x, y int) {
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// windowHeightFor 返回完整展示 contentDp 高内容所需的窗口客户区高度(Dp)。
func windowHeightFor(contentDp int) int {
	return shellHdrH + modelineH + pageTopH + pageBotH + contentDp
}

// 默认窗口适配工作区,长页面滚动而不撑满屏幕。
func defaultWindowSize() (w, h int) {
	return windowSizeForWorkArea(win32.PrimaryWorkAreaDp())
}

func windowSizeForWorkArea(aw, ah int) (w, h int) {
	w, h = defWinW, defWinH
	if aw > winEdgeW {
		w = min(w, aw-winEdgeW)
	}
	if ah > winEdgeW {
		h = min(h, ah-winEdgeW)
	}
	return w, h
}

// Run 运行原生 GUI 直到窗口关闭(或收到退出命令)。
// page: 深链页名(可空);nav: 跨进程页名通道(message-only 窗口注入);
// onQuit: 窗口关闭/退出命令时回调(清理运行标志)。
func Run(be *Backend, page string, nav <-chan string, onQuit func()) error {
	g = &GUI{
		win: new(app.Window), th: newTheme(), be: be,
		navReq: nav,
		msgCh:  make(chan func(), 32),
		done:   make(chan struct{}),
		quit:   onQuit,
	}
	defW, defH := defaultWindowSize()
	// 无系统标题栏,由 layoutHeader 自绘(与暗色主题一体);
	// Gio 的自定义装饰模式仍会处理窗口四边缩放命中区。
	g.win.Option(app.Title(windowTitle),
		app.Decorated(false),
		app.Size(unit.Dp(float32(defW)), unit.Dp(float32(defH))),
		app.MinSize(unit.Dp(min(minWinW, defW)), unit.Dp(min(minWinH, defH))))
	g.initWidgets()
	if p, ok := pageAlias[page]; ok {
		g.page = p
	}
	g.pageAtomic.Store(int32(g.page))

	if g.navReq != nil {
		go func() {
			for {
				select {
				case <-g.done:
					return
				case p, ok := <-g.navReq:
					if !ok {
						return
					}
					if idx, ok := pageAlias[p]; ok {
						g.ui(func() { g.navTo(idx) })
					}
					win32.FocusWindow(windowTitle)
				}
			}
		}()
	}

	go g.pollLoop()
	g.ui(func() { g.navTo(g.page) })

	for {
		switch e := g.win.Event().(type) {
		case app.DestroyEvent:
			g.dying.Store(true)
			close(g.done)
			if g.quit != nil {
				g.quit()
			}
			return e.Err
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				go func(hwnd uintptr) {
					win32.SetWindowIcon(hwnd, extractedIconPath())
					win32.ApplyDarkOwnWindows()
				}(e.HWND)
			}
		case app.FrameEvent:
			g.drainMsgs()
			gtx := app.NewContext(&g.ops, e)
			g.layoutRoot(gtx)
			e.Frame(gtx.Ops)
		case app.ConfigEvent:
			g.maximized = e.Config.Mode == app.Maximized
			g.paused.Store(e.Config.Mode == app.Minimized)
			g.win.Invalidate()
		}
	}
}

// aliasOf 宽松匹配页名(小写)。
func aliasOf(p string) string { return p }

// RequestClose 请求关闭主窗口(托盘/OutLog 关闭命令路径)。
// 触发 DestroyEvent,Run 返回后由调用方清理。
func RequestClose() {
	if g != nil && g.win != nil {
		g.win.Perform(system.ActionClose)
	}
}

// ui 把闭包投递到 UI 线程执行。
func (g *GUI) ui(fn func()) {
	if g.dying.Load() {
		return
	}
	select {
	case <-g.done:
		return
	case g.msgCh <- fn:
		g.win.Invalidate()
	}
}

func (g *GUI) drainMsgs() {
	for {
		select {
		case fn := <-g.msgCh:
			fn()
		default:
			return
		}
	}
}

// navTo 切页并刷新该页快照(UI 线程)。
func (g *GUI) navTo(p int) {
	g.page = p
	g.pageAtomic.Store(int32(p))
	g.refreshFor(p)
}

func (g *GUI) refreshFor(p int) {
	switch p {
	case pgSystem:
		go g.loadSystem()
	case pgKB:
		go g.loadKB()
	case pgFlexikey:
		go g.loadFlexikey()
	case pgCharger:
		go g.loadCharger()
	case pgCPU:
		go g.loadCPU()
	case pgGPU:
		go g.loadGPU()
	case pgBat:
		go g.loadBat()
	case pgEnergy:
		go g.loadEnergy()
	case pgPref:
		go g.loadPref()
	}
}

// after 延迟刷新(对齐 Web setTimeout(loadX, N))。
func (g *GUI) after(d time.Duration, p int) {
	time.AfterFunc(d, func() {
		if g.dying.Load() {
			return
		}
		g.ui(func() { g.refreshFor(p) })
	})
}

// run 执行动作:后台跑 fn,完成后 toast;页 p 在 delay 后刷新(对齐 Web 动作后延时重载)。
func (g *GUI) run(toast string, p int, delay time.Duration, fn func() error) {
	go func() {
		err := fn()
		g.ui(func() {
			if toast != "" {
				if err != nil {
					g.toast("失败: " + err.Error())
				} else {
					g.toast(toast)
				}
			}
		})
		if p >= 0 && (err == nil || p == pgSystem) {
			time.AfterFunc(delay, func() {
				if g.dying.Load() {
					return
				}
				g.ui(func() { g.refreshFor(p) })
			})
		}
	}()
}

// ---------- 轮询 ----------

func (g *GUI) pollLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	var lastLive, lastWatt, lastSys time.Time
	for {
		select {
		case <-g.done:
			return
		case <-t.C:
		}
		if g.paused.Load() {
			continue
		}
		page := g.pageAtomic.Load()
		now := time.Now()
		if page == pgSystem && now.Sub(lastLive) >= time.Second {
			lastLive = now
			live := g.be.FanLive()
			ci, _, _ := g.be.CPUInfo()
			// 实时数据自采样(不依赖 BIOS/XTU 通道)
			util, freq := win32.CPULoadPercent(), win32.CPUFrequencyMHz()
			temp := live.CPU.Temp // EC 风扇包自带 CPU 温度
			if temp <= 0 {
				temp = ci.CPUTemp // page6 兜底(有 XTU 服务的机器)
			}
			if util < 0 {
				util = ci.CPUUtil
			}
			if freq < 0 {
				freq = ci.CPUMHz
			}
			g.live.Store(&LiveSnap{Live: live, Ci: ci, Util: util, FreqMHz: freq, Temp: temp})
			// CPU 历史(温度/利用率,60 点滚动,1s 采样 = 1 分钟窗口)
			g.histMu.Lock()
			g.cpuTempH = append(g.cpuTempH, float64(temp))
			g.cpuUtilH = append(g.cpuUtilH, float64(util))
			if len(g.cpuTempH) > 60 {
				g.cpuTempH = g.cpuTempH[1:]
				g.cpuUtilH = g.cpuUtilH[1:]
			}
			g.histMu.Unlock()
			g.win.Invalidate()
		}
		if page == pgEnergy && now.Sub(lastWatt) >= time.Second {
			lastWatt = now
			s := g.be.EnergySample()
			g.histMu.Lock()
			g.wattHist = append(g.wattHist, s.Watt)
			if len(g.wattHist) > 60 {
				g.wattHist = g.wattHist[1:]
			}
			g.histMeta = s
			g.histMu.Unlock()
			g.win.Invalidate()
		}
		if page == pgSystem && now.Sub(lastSys) >= 15*time.Second {
			lastSys = now
			g.loadSystem() // 模式/语言/支持位可能被托盘/热键改变
		}
	}
}

// ---------- 加载器(后台协程执行) ----------

func (g *GUI) loadSystem() {
	var s sysSnap
	s.st = g.be.Status()
	s.opts = g.be.ModeOptions()
	s.fanCfg = g.be.FanConfig()
	s.tables = g.be.FanTables()
	s.ad = g.be.AntiDust()
	s.pref = g.be.Preference()
	s.gpu = g.be.GPUInfo()
	g.sys.Store(&s)
	g.prefSnap.Store(&s.pref)
	g.win.Invalidate()
}

func (g *GUI) loadKB() {
	s := g.be.KBState()
	g.kb.Store(&s)
	g.win.Invalidate()
}

func (g *GUI) loadFlexikey() {
	s := g.be.FlexikeyState()
	g.fk.Store(&s)
	g.win.Invalidate()
}

func (g *GUI) loadCharger() {
	s := chargerSnap{Pref: g.be.Preference(), Bat: g.be.Battery()}
	g.charger.Store(&s)
	g.win.Invalidate()
}

func (g *GUI) loadCPU() {
	info, params, defaults := g.be.CPUInfo()
	g.cpu.Store(&cpuSnap{Info: info, Params: params, Defaults: defaults})
	g.win.Invalidate()
}

func (g *GUI) loadGPU() {
	g.loadSystem()
}

func (g *GUI) loadBat() {
	s := g.be.Battery()
	g.bat.Store(&s)
	g.win.Invalidate()
}

func (g *GUI) loadEnergy() {
	s := g.be.EnergySample()
	g.histMu.Lock()
	g.histMeta = s
	g.histMu.Unlock()
	g.win.Invalidate()
}

func (g *GUI) loadPref() {
	s := g.be.Preference()
	g.prefSnap.Store(&s)
	g.win.Invalidate()
}

// ---------- 外壳布局 ----------

func (g *GUI) layoutRoot(gtx C) D {
	// 全局快捷键:Ctrl+1..9 直达页
	g.hotkeys(gtx)
	drawBackdrop(gtx)
	defer g.drawToast(gtx)
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(g.layoutSidebar),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(g.layoutHeader),
				layout.Rigid(g.layoutModeline),
				layout.Flexed(1, g.layoutPage),
			)
		}),
	)
}

// hotkeys Ctrl+1..9 切页(空 Name 过滤器匹配全部按键,再按名称分发)。
func (g *GUI) hotkeys(gtx C) {
	event.Op(gtx.Ops, &g.keyTag)
	for {
		ev, ok := gtx.Event(key.Filter{Required: key.ModShortcut})
		if !ok {
			return
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		if len(ke.Name) == 1 && ke.Name[0] >= '1' && ke.Name[0] <= '9' {
			if p := int(ke.Name[0] - '1'); p < pgCount {
				g.navTo(p)
			}
		}
	}
}

func (g *GUI) layoutSidebar(gtx C) D {
	// 单行布局:图标 + 文案 在同一行 Flex{Middle} 对齐(原来图标在上、文字在下的
	// 竖排方案在窄侧栏里会被裁切,也是最刺眼的错位来源)。
	w := gtx.Dp(unit.Dp(navW))
	if g.sideMin {
		w = gtx.Dp(unit.Dp(navWMin))
	}
	h := gtx.Constraints.Max.Y
	// 底:比页面底再深一档 + 右缘半透明分隔线(不用纯灰实线)
	fillArea(gtx, image.Rect(0, 0, w, h), colBar)
	fillArea(gtx, image.Rect(w-Snap(gtx, 1), 0, w, h), colLineSoft)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// 收起/展开按钮
		layout.Rigid(func(gtx C) D {
			if g.w.sideToggle.Clicked(gtx) {
				g.sideMin = !g.sideMin
			}
			return g.w.sideToggle.Layout(gtx, func(gtx C) D {
				hh := Snap(gtx, shellHdrH)
				if g.w.sideToggle.Hovered() {
					fillArea(gtx, image.Rect(0, 0, w, hh), colHover)
				}
				hLine(gtx, image.Rect(0, hh-Snap(gtx, 1), w, hh), colLineSoft)
				td, call := txtDims(gtx, "≡", textStyle{Size: fsTitle, Col: colText2})
				replayText(gtx, call, (w-td.Size.X)/2, (hh-td.Size.Y)/2)
				return D{Size: image.Pt(w, hh)}
			})
		}),
		layout.Rigid(vSpacer(sp8)),
		// 导航项:单行,图标 17dp + 11dp 间距 + 13sp 文案
		layout.Rigid(func(gtx C) D {
			var items []layout.Widget
			for i := 0; i < pgCount; i++ {
				i := i
				items = append(items, func(gtx C) D {
					active := g.page == i
					clk := &g.w.nav[i]
					if clk.Clicked(gtx) {
						g.navTo(i)
					}
					return clk.Layout(gtx, func(gtx C) D {
						hh := Snap(gtx, navH)
						// 整行宽度锁定为侧栏宽:否则 Flex 自然宽度左贴,居中等于没居中
						gtx.Constraints.Min.X = w
						gtx.Constraints.Max.X = w
						inset := Snap(gtx, sp8)
						rowR := image.Rect(inset, 0, w-inset, hh)
						rad := Snap(gtx, rSmall)

						iconCol, lblCol := colText3, colText2
						switch {
						case active:
							// 选中:青色 12% 底 + 左侧 3dp 强调条
							fillRR(gtx, rowR, rad, alpha(colAccent, 0x1C))
							fillRR(gtx, image.Rect(inset, hh/2-Snap(gtx, 8), inset+Snap(gtx, 3), hh/2+Snap(gtx, 8)),
								Snap(gtx, 2), colAccent)
							iconCol, lblCol = colAccent, colText
						case clk.Hovered():
							fillRR(gtx, rowR, rad, alpha(colText, 0x0A))
							iconCol, lblCol = colText2, colText
						}

						iconSz := Snap(gtx, navIconDp)
						gap := Snap(gtx, sp10)
						var lblD D
						var lblCall op.CallOp
						if !g.sideMin {
							lblD, lblCall = txtDims(gtx, pageTitles[i], textStyle{Size: fsBig, Col: lblCol, Lines: 1})
						}
						x := Snap(gtx, sp20)
						if g.sideMin {
							x = (w - iconSz) / 2
						}
						icOff := op.Offset(image.Pt(x, (hh-iconSz)/2)).Push(gtx.Ops)
						drawIconOn(gtx, pageIcons[i], navIconDp, iconCol, colBar)
						icOff.Pop()
						if !g.sideMin {
							replayText(gtx, lblCall, x+iconSz+gap, (hh-lblD.Size.Y)/2)
						}
						return D{Size: image.Pt(w, hh)}
					})
				})
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, flexWrap(items...)...)
		}),
	)
}

func colorSafe(r, gg, b uint8) color.NRGBA {
	return color.NRGBA{R: r, G: gg, B: b, A: 0xFF}
}

// 标题栏右侧控件的布局尺寸(全部由这里推导,避免拖动区/按钮/信息按钮互相重叠)。
const (
	winBtnDp   = 36 // 最小化/最大化/关闭按钮宽
	infoBtnDp  = 32 // 信息按钮宽
	headerPadR = 4  // 右侧留白
)

// layoutHeader 自绘标题栏(系统标题栏已去除,配合 win32.RemoveCaption)。
// 左:应用图标 + 标题;中:整条可拖动(ActionInputOp 让 Gio 回 HTCAPTION);
// 右:信息按钮 + 最小化/最大化/关闭 三枚自绘按钮(从右往左依次排列,互不重叠)。
func (g *GUI) layoutHeader(gtx C) D {
	w := gtx.Constraints.Max.X
	h := Snap(gtx, shellHdrH)
	// 底色:标题栏与侧栏同层(colBar),底边一条弱分隔线
	fillArea(gtx, image.Rect(0, 0, w, h), colBar)
	hLine(gtx, image.Rect(0, h-Snap(gtx, 1), w, h), colLineSoft)

	// ---- 右侧控件坐标:从右边缘依次向左排 ----
	btnW, btnH := gtx.Dp(winBtnDp), h
	infoW := Snap(gtx, infoBtnDp)
	padR := Snap(gtx, headerPadR)
	gap := Snap(gtx, headerGap)
	closeX := w - padR - btnW   // 最右:关闭
	maxX := closeX - btnW       // 最大化
	minX := maxX - btnW         // 最小化
	infoX := minX - gap - infoW // 信息按钮在三枚窗口按钮左侧
	controlsX := infoX          // 右侧控件区起点
	infoTop := (h - Snap(gtx, sp20) - Snap(gtx, sp6)) / 2

	// ---- 拖动区:只铺到右侧控件左边,避免盖住按钮的点击 ----
	dragW := controlsX
	if dragW < 0 {
		dragW = 0
	}
	dragClip := clip.Rect(image.Rect(0, 0, dragW, h)).Push(gtx.Ops)
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)

	// ---- 左:图标 + 标题 ----
	iconSize := Snap(gtx, 22)
	ix, iy := Snap(gtx, sp16), (h-iconSize)/2
	drawAppIcon(gtx, ix, iy, iconSize)
	td, call := txtDims(gtx, "Control Center", textStyle{Size: fsBig, Col: colText, W: font.Medium})
	replayText(gtx, call, ix+iconSize+Snap(gtx, sp10), (h-td.Size.Y)/2)
	dragClip.Pop()

	if g.w.winMin.Clicked(gtx) {
		g.win.Perform(system.ActionMinimize)
	}
	if g.w.winMax.Clicked(gtx) {
		if g.maximized {
			g.win.Perform(system.ActionUnmaximize)
		} else {
			g.win.Perform(system.ActionMaximize)
		}
	}
	if g.w.winClose.Clicked(gtx) {
		g.win.Perform(system.ActionClose)
	}

	winBtn := func(x int, clk *widget.Clickable, kind string) {
		defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
		clk.Layout(gtx, func(gtx C) D {
			hover := clk.Hovered()
			if hover {
				bg := colHover
				if kind == "close" {
					bg = colorSafe(0xC4, 0x2B, 0x1C) // 关闭按钮常规悬停用系统红
				}
				fillArea(gtx, image.Rect(0, 0, btnW, btnH), bg)
			}
			c := colText2
			if hover {
				c = colText
			}
			cx, cy := btnW/2, btnH/2
			s := Snap(gtx, 5)
			switch kind {
			case "min": // 横线
				fillArea(gtx, image.Rect(cx-s, cy, cx+s+1, cy+1), c)
			case "max": // 方块(还原态画双框)
				if g.maximized {
					border(gtx, image.Rect(cx-s, cy-s+1, cx+s, cy+s+1), c, 1)
					border(gtx, image.Rect(cx-s+2, cy-s-1, cx+s-2, cy+s-1), c, 1)
				} else {
					border(gtx, image.Rect(cx-s, cy-s, cx+s, cy+s), c, 1)
				}
			case "close": // 叉
				fillLine(gtx, cx-s, cy-s, cx+s, cy+s, 1, c)
				fillLine(gtx, cx-s, cy+s, cx+s, cy-s, 1, c)
			}
			return D{Size: image.Pt(btnW, btnH)}
		})
	}
	winBtn(minX, &g.w.winMin, "min")
	winBtn(maxX, &g.w.winMax, "max")
	winBtn(closeX, &g.w.winClose, "close")

	// ---- 信息按钮:排在最小化左侧,与窗口按钮同高居中 ----
	infoOff := op.Offset(image.Pt(infoX, infoTop)).Push(gtx.Ops)
	defer infoOff.Pop()
	if g.w.info.Clicked(gtx) {
		if s := g.sys.Load(); s != nil {
			g.toast(fmt.Sprintf("%s %s · BIOS %s · ClevoID %s",
				s.st.Manufacturer, s.st.Product, orDash(s.st.BIOS), fmt.Sprintf("%d", s.st.ClevoID)))
		}
	}
	g.w.info.Layout(gtx, func(gtx C) D {
		bw, bh := infoW, Snap(gtx, sp20)+Snap(gtx, sp6)
		r := image.Rect(0, 0, bw, bh)
		rad := Snap(gtx, rSmall)
		bg := colSunken
		if g.w.info.Hovered() {
			bg = colHover
		}
		fillRR(gtx, r, rad, colLine)
		fillRR(gtx, r.Inset(Snap(gtx, 1)), rad-Snap(gtx, 1), bg)
		id, icall := txtDims(gtx, "i", textStyle{Size: fsSmall, Col: colText2})
		replayText(gtx, icall, (bw-id.Size.X)/2, (bh-id.Size.Y)/2)
		return D{Size: image.Pt(bw, bh)}
	})
	return D{Size: image.Pt(w, h)}
}

func (g *GUI) layoutModeline(gtx C) D {
	w, h := gtx.Constraints.Max.X, Snap(gtx, modelineH)
	s, right := "正在连接 ACPI 桥驱动…", ""
	if sp := g.sys.Load(); sp != nil {
		s = fmt.Sprintf("%s %s · BIOS %s · %s", orDash(sp.st.Manufacturer), sp.st.Product,
			orDash(sp.st.BIOS), orDash(sp.st.CPU))
		right = fmt.Sprintf("CLEVO %d · CUSTOMER %d", sp.st.ClevoID, sp.st.CustomerID)
	}
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: pageSideW, Right: pageSideW}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return txt(gtx, s, textStyle{Size: fsMicro, Col: colText3, Lines: 1})
			}),
			layout.Rigid(hSpacer(sp16)),
			layout.Rigid(func(gtx C) D {
				return txt(gtx, right, textStyle{Size: fsTiny, Col: colText4, Lines: 1})
			}),
		)
	})
	return D{Size: image.Pt(w, h)}
}

func (g *GUI) layoutPage(gtx C) D {
	// 内容区:留边距 + 自实现垂直滚动(滚轮 + 可拖滚动条)
	return layout.Inset{Top: pageTopH, Right: pageSideW, Bottom: pageBotH, Left: pageSideW}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var page layout.Widget
		switch g.page {
		case pgSystem:
			page = g.pageSystem
		case pgKB:
			page = g.pageKB
		case pgFlexikey:
			page = g.pageFlexikey
		case pgCharger:
			page = g.pageCharger
		case pgCPU:
			page = g.pageCPU
		case pgGPU:
			page = g.pageGPU
		case pgBat:
			page = g.pageBat
		case pgEnergy:
			page = g.pageEnergy
		case pgPref:
			page = g.pagePref
		}
		if page == nil {
			return D{}
		}
		return g.scrolledPage(gtx, g.page, page)
	})
}

// ---------- 页面滚动(滚轮 + 可拖滚动条) ----------

type scrollState struct {
	offset   float32
	contentH int
	viewH    int
}

func scrollRange(offset, maxOffset float32) pointer.ScrollRange {
	return pointer.ScrollRange{
		Min: -int(math.Ceil(float64(offset))),
		Max: int(math.Ceil(float64(maxOffset - offset))),
	}
}

var (
	pageScroll     [pgCount]scrollState
	scrollTags     [pgCount]byte
	scrollDrag     [pgCount]byte
	scrollDragging [pgCount]bool
	scrollDragY    [pgCount]int
)

// scrolledPage 页面垂直滚动:内容按自然高度绘制,视口裁剪 + 偏移平移,
// 右缘绘制可拖滚动条(仅在内容超高时出现),滚轮与拖拽都可用。
//
// 两个曾导致"内容被截断且没有滚动条"的坑:
//   - layout.Flexed(1, layoutPage) 传给页面的是 Exact 约束(Min.Y == Max.Y == 视口高),
//     页面沿用该约束会把内容强行压成视口高 → contentH == viewH → maxOff 恒为 0;
//     这里显式放开 Min.Y=0 / Max.Y=无穷,内容按自然高度排布。
//   - 滚动条必须画在视口裁剪之外:原先用 defer clip 覆盖整页,滚动条位于裁剪区外被整条裁掉。
func (g *GUI) scrolledPage(gtx C, page int, content layout.Widget) D {
	sc := &pageScroll[page]
	viewW, viewH := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	barSpace := gtx.Dp(scrollBarW)
	innerW := viewW - barSpace
	if innerW < gtx.Dp(160) {
		innerW = viewW
	}
	// 内容最大宽度:超宽窗口下内容左对齐、不把小卡片无限拉宽(方案 3.1)。
	if maxW := gtx.Dp(contentMaxW); innerW > maxW {
		innerW = maxW
	}
	// 统一的内容约束:宽度锁满(panel 靠 Exact 宽度铺满,原来由 layoutPage 的 Min.X 保证)、
	// 高度下限放开,让内容按自然高度排布
	bodyGtx := func(gx C) C {
		gx.Constraints.Min = image.Pt(innerW, 0)
		gx.Constraints.Max = image.Pt(innerW, math.MaxInt32/2)
		return gx
	}
	curMax := func() float32 {
		m := float32(sc.contentH - viewH)
		if m < 0 {
			return 0
		}
		return m
	}

	// 内容:视口裁剪 + 垂直偏移;滚轮热区覆盖整个视口
	clipStack := clip.Rect(image.Rect(0, 0, innerW, viewH)).Push(gtx.Ops)
	event.Op(gtx.Ops, &scrollTags[page])
	sc.offset = clamp(sc.offset, 0, curMax())
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &scrollTags[page], Kinds: pointer.Scroll,
			ScrollY: scrollRange(sc.offset, curMax())})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Scroll {
			sc.offset = clamp(sc.offset+pe.Scroll.Y, 0, curMax())
			g.win.Invalidate()
		}
	}
	off := int(sc.offset)
	offStack := op.Offset(image.Pt(0, -off)).Push(gtx.Ops)
	d := content(bodyGtx(gtx))
	offStack.Pop()
	clipStack.Pop()

	if sc.contentH != d.Size.Y || sc.viewH != viewH {
		g.win.Invalidate()
	}
	sc.contentH, sc.viewH = d.Size.Y, viewH
	maxOff := curMax()
	if sc.offset > maxOff {
		sc.offset = maxOff
		g.win.Invalidate()
	}

	// 滚动条(内容超高才显示)
	if maxOff > 0 {
		barW := gtx.Dp(4)
		x0 := viewW - barW - gtx.Dp(5)
		thumbH := int(float32(viewH) * float32(viewH) / float32(sc.contentH))
		if minH := gtx.Dp(40); thumbH < minH {
			thumbH = minH
		}
		if thumbH > viewH {
			thumbH = viewH
		}
		thumbY := int(float32(viewH-thumbH) * clamp01(sc.offset/maxOff))
		fillArea(gtx, image.Rect(x0, 0, x0+barW, viewH), alpha(pal.Line, 0x55))
		fillArea(gtx, image.Rect(x0, thumbY, x0+barW, thumbY+thumbH), alpha(pal.Cyan, 0xA8))

		// 拇指拖拽(热区比轨道宽 4dp,便于点中;按在拇指外时先对齐拇指中心)
		hitArea := clip.Rect(image.Rect(x0-gtx.Dp(4), 0, x0+barW+gtx.Dp(4), viewH)).Push(gtx.Ops)
		event.Op(gtx.Ops, &scrollDrag[page])
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &scrollDrag[page], Kinds: pointer.Drag | pointer.Press | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Press:
				gtx.Execute(pointer.GrabCmd{Tag: &scrollDrag[page], ID: pe.PointerID})
				py := int(pe.Position.Y)
				if py < thumbY || py > thumbY+thumbH {
					// 点击轨道空白:把拇指中心移到点击处
					thumbY = py - thumbH/2
				}
				scrollDragging[page] = true
				scrollDragY[page] = py - thumbY
				if span := viewH - thumbH; span > 0 {
					sc.offset = clamp01(float32(thumbY)/float32(span)) * maxOff
				}
				g.win.Invalidate()
			case pointer.Drag:
				if !scrollDragging[page] {
					continue
				}
				if span := viewH - thumbH; span > 0 {
					frac := clamp01(float32(int(pe.Position.Y)-scrollDragY[page]) / float32(span))
					sc.offset = frac * maxOff
				}
				g.win.Invalidate()
			case pointer.Release, pointer.Cancel:
				scrollDragging[page] = false
			}
		}
		hitArea.Pop()
	}
	return D{Size: image.Pt(viewW, viewH)}
}

// grid2 双列栅格(窄窗自动变单列),权重 wl:wr。
//
// 两列之间必须显式插入 gap:先前只从列宽里扣掉 gap 却没有放占位子项,
// 宽度虽然守恒,但两列内容直接贴在一起 —— 卡片圆角/描边重叠。
// 这里列宽 = 扣 gap 后按权重分配,中间插入 gap 占位,保证 lw + gap + rw == 可用宽。
func grid2(gtx C, wl, wr float32, l, r layout.Widget) D {
	w := gtx.Constraints.Max.X
	gap := gtx.Dp(gridGap)
	if wl <= 0 || wr <= 0 {
		wl, wr = 1, 1
	}
	lw := int(float32(w-gap) * wl / (wl + wr))
	rw := w - gap - lw
	// 列宽不足最小可用宽度时提前回退单列,而不是把卡片压到放不下内容。
	if w < gtx.Dp(gridPairW) || lw < gtx.Dp(halfMinW) || rw < gtx.Dp(halfMinW) {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(l), layout.Rigid(vSpacer(gridGap)), layout.Rigid(r))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = lw
			gtx.Constraints.Max.X = lw
			gtx.Constraints.Min.Y = 0
			return l(gtx)
		}),
		layout.Rigid(hSpacerSP(gap)),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = rw
			gtx.Constraints.Max.X = rw
			gtx.Constraints.Min.Y = 0
			return r(gtx)
		}),
	)
}

// hSpacerSP 像素级横向占位(已由调用方按 Dp 折算)。
func hSpacerSP(n int) layout.Widget {
	return func(gtx C) D { return D{Size: image.Pt(n, 0)} }
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// kbTypeName 键盘类型名(与 Web 数组一致)。
func kbTypeName(t int) string {
	names := []string{"无", "白光", "RGB 三区", "每键 RGB", "", "", "RGB15 色"}
	if t >= 0 && t < len(names) && names[t] != "" {
		return names[t]
	}
	if t == 25 {
		return "RGB15 自定义"
	}
	return fmt.Sprintf("type %d", t)
}

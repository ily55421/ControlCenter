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
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"controlcenter/internal/cpuoc"
)

// g 当前 GUI 实例(Run 创建;主题与绘制辅助经它取主题)。
var g *GUI

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
type LiveSnap struct {
	Live FanLiveOut
	Ci   cpuoc.Info
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

	page       int   // 当前页(UI 线程)
	pageAtomic int32 // 轮询协程读取
	sideMin    bool

	navReq <-chan string // 跨进程深链(ccapp 注入)
	msgCh  chan func()   // 后台协程 → UI 线程的 UI 更新
	quit   func()        // 窗口关闭回调(ccapp 清理标志)
	dying  atomic.Bool

	toastS toastState

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

	// 色环指针输入 tag
	wheelTag struct{}

	w widgets
}

// Run 运行原生 GUI 直到窗口关闭(或收到退出命令)。
// page: 深链页名(可空);nav: 跨进程页名通道(message-only 窗口注入);
// onQuit: 窗口关闭/退出命令时回调(清理运行标志)。
func Run(be *Backend, page string, nav <-chan string, onQuit func()) error {
	g = &GUI{
		win: new(app.Window), th: newTheme(), be: be,
		navReq: nav,
		msgCh:  make(chan func(), 32),
		quit:   onQuit,
	}
	g.win.Option(app.Title("Control Center"),
		app.Size(unit.Dp(1240), unit.Dp(820)), app.MinSize(unit.Dp(1000), unit.Dp(660)))
	g.initWidgets()
	if p, ok := pageAlias[page]; ok {
		g.page = p
	}
	g.pageAtomic = int32(g.page)

	// 深链:消息窗口 goroutine → 切页
	go func() {
		for p := range g.navReq {
			if idx, ok := pageAlias[p]; ok {
				g.ui(func() { g.navTo(idx) })
			}
			g.win.Invalidate()
		}
	}()

	go g.pollLoop()
	g.ui(func() { g.navTo(g.page) })

	for {
		g.drainMsgs()
		switch e := g.win.Event().(type) {
		case app.DestroyEvent:
			g.dying.Store(true)
			if g.quit != nil {
				g.quit()
			}
			return nil
		case app.FrameEvent:
			gtx := app.NewContext(&g.ops, e)
			g.layoutRoot(gtx)
			e.Frame(gtx.Ops)
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
	select {
	case g.msgCh <- fn:
	default:
		go func() { g.msgCh <- fn }() // 满时退避(几乎不会发生)
	}
	g.win.Invalidate()
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
	g.pageAtomic = int32(p)
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
		if p >= 0 {
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
	for range t.C {
		if g.dying.Load() {
			return
		}
		page := atomic.LoadInt32(&g.pageAtomic)
		now := time.Now()
		if page == pgSystem && now.Sub(lastLive) >= 2*time.Second {
			lastLive = now
			live := g.be.FanLive()
			ci, _, _ := g.be.CPUInfo()
			g.live.Store(&LiveSnap{Live: live, Ci: ci})
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
	// GPU 快照随系统页更新;单独进入时重取
	if s := g.sys.Load(); s != nil {
		g.win.Invalidate()
		return
	}
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
	// 背景底色
	fillRect(gtx, pal.BG)
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

func (g *GUI) layoutSidebar(gtx C) D {
	w := gtx.Dp(118)
	if g.sideMin {
		w = gtx.Dp(62)
	}
	fillArea(gtx, image.Rect(0, 0, w, gtx.Constraints.Max.Y), colorSafe(0x0B, 0x0D, 0x0F))
	fillArea(gtx, image.Rect(w-1, 0, w, gtx.Constraints.Max.Y), colorSafe(0x15, 0x18, 0x1B))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			if g.w.sideToggle.Clicked(gtx) {
				g.sideMin = !g.sideMin
			}
			return g.w.sideToggle.Layout(gtx, func(gtx C) D {
				h := gtx.Dp(52)
				fillArea(gtx, image.Rect(0, 0, w, h), colorSafe(0x10, 0x13, 0x16))
				fillArea(gtx, image.Rect(0, h-1, w, h), colorSafe(0x15, 0x18, 0x1B))
				td, call := textDims(gtx, "≡", 18, colorSafe(0x9A, 0xA4, 0xAD))
				replayText(gtx, call, (w-td.Size.X)/2, (h-td.Size.Y)/2)
				return D{Size: image.Pt(w, h)}
			})
		}),
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
						iconCol := colorSafe(0x59, 0x63, 0x6D)
						lblCol := iconCol
						if active {
							fillArea(gtx, image.Rect(0, 0, w, gtx.Dp(64)), colorSafe(0x17, 0x19, 0x1C))
							iconCol, lblCol = pal.Lime, pal.Lime
						} else if clk.Hovered() {
							fillArea(gtx, image.Rect(0, 0, w, gtx.Dp(64)), colorSafe(0x0E, 0x11, 0x14))
							iconCol = colorSafe(0xAE, 0xB8, 0xC1)
							lblCol = iconCol
						}
						return layout.Inset{Top: unit.Dp(13), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return drawIcon(gtx, pageIcons[i], 26, iconCol) }),
								layout.Rigid(vSpacer(7)),
								layout.Rigid(func(gtx C) D {
									if g.sideMin {
										return D{}
									}
									return label(gtx, pageTitles[i], 12, lblCol)
								}),
							)
						})
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

func (g *GUI) layoutHeader(gtx C) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(46)
	fillArea(gtx, image.Rect(0, 0, w, h), colorSafe(0x0A, 0x0C, 0x0E))
	fillArea(gtx, image.Rect(0, h-1, w, h), colorSafe(0x14, 0x17, 0x1A))
	td, call := textDims(gtx, "Control Center", 17, pal.Txt)
	replayText(gtx, call, (w-td.Size.X)/2, (h-td.Size.Y)/2)
	// 系统信息按钮(右上角):先平移再布局,保证画在正确位置
	defer op.Affine(f32Translate(float32(w-gtx.Dp(44)), float32((h-gtx.Dp(26))/2))).Push(gtx.Ops).Pop()
	if g.w.info.Clicked(gtx) {
		if s := g.sys.Load(); s != nil {
			g.toast(fmt.Sprintf("%s %s · BIOS %s · ClevoID %s",
				s.st.Manufacturer, s.st.Product, orDash(s.st.BIOS), fmt.Sprintf("%d", s.st.ClevoID)))
		}
	}
	g.w.info.Layout(gtx, func(gtx C) D {
		bw, bh := gtx.Dp(32), gtx.Dp(26)
		fillArea(gtx, image.Rect(0, 0, bw, bh), colorSafe(0x10, 0x13, 0x16))
		border(gtx, image.Rect(0, 0, bw, bh), colorSafe(0x23, 0x28, 0x2D), 1)
		// ⓘ 缺字形,用 info 图标代替
		drawIcon(gtx, "info", 15, colorSafe(0x9A, 0xA4, 0xAD))
		replayText(gtx, call, (bw-td.Size.X)/2, (bh-td.Size.Y)/2)
		return D{Size: image.Pt(bw, bh)}
	})
	return D{Size: image.Pt(w, h)}
}

func (g *GUI) layoutModeline(gtx C) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(26)
	txt := "正在连接 ACPI 桥驱动…"
	if s := g.sys.Load(); s != nil {
		txt = fmt.Sprintf("%s %s · BIOS %s · %s · ClevoID %s · 客户 %d",
			orDash(s.st.Manufacturer), s.st.Product, orDash(s.st.BIOS), orDash(s.st.CPU), fmt.Sprintf("%d", s.st.ClevoID), s.st.CustomerID)
	}
	td, call := textDims(gtx, txt, 12, colorSafe(0x5F, 0x6A, 0x74))
	replayText(gtx, call, gtx.Dp(20), (h-td.Size.Y)/2)
	return D{Size: image.Pt(w, h)}
}

func (g *GUI) layoutPage(gtx C) D {
	// 内容区:留边距 + 垂直滚动
	return layout.Inset{Top: unit.Dp(2), Right: unit.Dp(20), Bottom: unit.Dp(40), Left: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var lst layout.List
		lst.Axis = layout.Vertical
		switch g.page {
		case pgSystem:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageSystem(gtx) })
		case pgKB:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageKB(gtx) })
		case pgFlexikey:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageFlexikey(gtx) })
		case pgCharger:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageCharger(gtx) })
		case pgCPU:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageCPU(gtx) })
		case pgGPU:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageGPU(gtx) })
		case pgBat:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageBat(gtx) })
		case pgEnergy:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pageEnergy(gtx) })
		case pgPref:
			return lst.Layout(gtx, 1, func(gtx C, _ int) D { return g.pagePref(gtx) })
		}
		return D{}
	})
}

// grid2 双列栅格(窄窗自动变单列),权重 wl:wr。
func grid2(gtx C, wl, wr float32, l, r layout.Widget) D {
	w := gtx.Constraints.Max.X
	gap := gtx.Dp(16)
	if w < gtx.Dp(900) {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(l), layout.Rigid(vSpacer(14)), layout.Rigid(r))
	}
	lw := int(float32(w-gap) * wl / (wl + wr))
	rw := w - gap - lw
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(lw, gtx.Constraints.Max.Y))
			return l(gtx)
		}),
		layout.Rigid(hSpacerSP(gap)),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
			return r(gtx)
		}),
	)
}

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

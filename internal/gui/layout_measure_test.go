package gui

import (
	"image"
	"testing"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"controlcenter/internal/cpuoc"
)

// 本文件把「默认窗口尺寸必须能完整展示每一页」固化成回归测试。
//
// 背景:defaultWindowSize 的高度依赖"最高一页内容的自然高度",而这个高度会随着
// 页面布局改动变化。之前 contentMaxDp 是人工维护的常量,卡片/页头改完后忘了同步,
// 于是默认窗口打开就出现滚动条。这里用假快照离屏测量真实高度,直接和常量对比。

// fakeSnaps 给 GUI 注入一组有代表性的快照,让各页走真实布局而不是"正在加载"占位。
func fakeSnaps() {
	if g == nil {
		g = &GUI{th: newTheme()}
	}
	if g.th == nil {
		g.th = newTheme()
	}
	// 页面里用到 fanOffset/blSlider 等控件指针。initWidgets 会重建这些指针
	// (非幂等),所以只在缺失时调用一次,避免把其它测试已建立的状态冲掉。
	if g.w.fanOffset == nil {
		g.initWidgets()
	}
	// animVal.tick 会调 g.win.Invalidate();未 Run 的 app.Window 没有 driver,
	// Invalidate 是空操作(mayInvalidate==false),给个零值窗口即可。
	if g.win == nil {
		g.win = new(app.Window)
	}
	g.sys.Store(&sysSnap{
		st: Status{
			Mode: 2, KBType: 2, ClevoID: 25980, Cores: 24,
			Manufacturer: "COLORFUL", Product: "P16 Pro IxA1",
			BIOS: "BIOS 1.07.14COLO", CPU: "Intel(R) Core(TM) i9-14900HX",
		},
		opts:   ModeOptions{BlueLight: 3, LowRefreshSupport: true},
		fanCfg: FanConfigOut{Mode: 0, Offset: 0},
		tables: [3]FanTableOut{
			{T1: 30, T2: 60, T3: 90, D1: 30, D2: 60, D3: 100},
			{T1: 30, T2: 60, T3: 90, D1: 30, D2: 60, D3: 100},
			{T1: 30, T2: 60, T3: 90, D1: 30, D2: 60, D3: 100},
		},
		ad:  AntiDustOut{Days: 7, Hour: 3},
		gpu: GPUOCOut{Count: 2, BaseClock: 1600, Ready: true, Driver: "610.00"},
	})
	g.live.Store(&LiveSnap{Util: 43, FreqMHz: 2068, Temp: 72})
	g.cpu.Store(&cpuSnap{Info: cpuoc.Info{CPUMHz: 2068, CPUVolt: 1.2, CPUTemp: 72, CPUUtil: 43}})
	g.kb.Store(&KBStateOut{Type: 2, On: true, Brightness: 3, SleepSeconds: 30})
	g.bat.Store(&BatteryOut{CycleCount: 40, FullCharge: 60000, Design: 62000, CapacityPct: 97, HealthCode: 0})
	g.charger.Store(&chargerSnap{Bat: BatteryOut{CapacityPct: 97}})
	g.fk.Store(&FlexikeyOut{Path: "x", Exists: true, KBEnabled: true,
		Profiles: []FKProfile{{Name: "默认", Selected: true, KB: true, Slots: 3}}})
	g.prefSnap.Store(&Pref{})
	g.page = pgSystem
}

// contentWidthFor 返回给定窗口宽度下的页面内容宽度,与 scrolledPage 保持一致。
func contentWidthFor(winW int) int {
	return winW - navW - 2*pageSideDp - 12
}

// measurePage 以给定内容宽度离屏测量一页的自然高度(Dp=px @1x)。
func measurePage(w layout.Widget, contentW int) int {
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(contentW, 20000)},
	}
	return w(gtx).Size.Y
}

// TestDefaultWindowFitsAllPages 默认窗口高度必须能完整展示每一页(含页边距与顶栏)。
func TestDefaultWindowFitsAllPages(t *testing.T) {
	fakeSnaps()
	contentW := contentWidthFor(defaultWidthDp)

	pages := []struct {
		name string
		w    layout.Widget
	}{
		{"系统监控", g.pageSystem},
		{"LED 设备", g.pageKB},
		{"FlexiKey", g.pageFlexikey},
		{"FlexiCharger", g.pageCharger},
		{"CPU 超频", g.pageCPU},
		{"GPU 超频", g.pageGPU},
		{"电池", g.pageBat},
		{"节能计划", g.pageEnergy},
		{"偏好", g.pagePref},
	}
	maxH, maxName := 0, ""
	for _, p := range pages {
		h := measurePage(p.w, contentW)
		t.Logf("%-12s contentH = %d", p.name, h)
		if h > maxH {
			maxH, maxName = h, p.name
		}
	}

	// 1) contentMaxDp 必须与实际最高页一致(容差 8dp,留字体度量抖动)
	if diff := maxH - contentMaxDp; diff > 8 || diff < -8 {
		t.Errorf("contentMaxDp=%d 与实测最高页(%s)=%d 不符,请同步 contentMaxDp",
			contentMaxDp, maxName, maxH)
	}

	// 2) 默认窗口高度必须 >= 顶栏+型号栏+页边距+最高页内容
	need := windowHeightFor(maxH)
	_, h := defaultWindowSize()
	t.Logf("默认窗口高度=%d,完整展示需要=%d", h, need)
	if h < need {
		// 只有屏幕装不下时才会被夹小,此时必须已退化为滚动
		t.Logf("默认高度被屏幕工作区限制(%d < %d),该页将出现滚动条", h, need)
	}
}

// TestWideEnoughForTwoColumns 默认宽度必须让子栅格保持并排(否则内容高度暴涨)。
func TestWideEnoughForTwoColumns(t *testing.T) {
	if defaultWidthDp < wideMinDp {
		t.Fatalf("默认宽度 %d < 并排阈值 %d,子栅格会退化为单列", defaultWidthDp, wideMinDp)
	}
	// 阈值上/下的真实高度应显著不同(证明这个阈值确实有意义)
	fakeSnaps()
	narrow := measurePage(g.pageSystem, contentWidthFor(wideMinDp-200))
	wide := measurePage(g.pageSystem, contentWidthFor(wideMinDp))
	t.Logf("窄窗(系统页)=%d  宽窗(系统页)=%d", narrow, wide)
	if narrow <= wide {
		t.Errorf("窗口变窄后系统页高度(%d)未超过宽窗(%d),阈值 %d 可能已失效",
			narrow, wide, wideMinDp)
	}
}

// TestContentHeightAtMatches 估算函数必须与实际测量一致,不能低估。
func TestContentHeightAtMatches(t *testing.T) {
	fakeSnaps()
	for _, w := range []int{1000, 1240, 1320, 1400} {
		got := contentHeightAt(w)
		actual := measurePage(g.pageSystem, contentWidthFor(w))
		if got < actual {
			t.Errorf("win=%d: contentHeightAt=%d 低于实测 %d(会算出过矮的窗口)", w, got, actual)
		}
	}
	if got := contentHeightAt(defaultWidthDp); got != contentMaxDp {
		t.Errorf("宽窗口下 contentHeightAt=%d,期望 %d", got, contentMaxDp)
	}
}

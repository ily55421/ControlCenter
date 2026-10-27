package gui

import (
	"image"
	"testing"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"controlcenter/internal/cpuoc"
	"controlcenter/internal/win32"
)

// 本文件离屏测量每一页的自然高度,守住两件事:
// 1) 页面不会因为约束传错而"高度失控"(实测暴涨 = 栅格退化或 Exact 传导 bug);
// 2) 响应式断点(gridWideW)确实是真断点,窄一点内容就真的变高。
//
// 注意:这里不再拿实测值去对账某个"实测常量"。开窗时页面数据仍在异步加载,
// 手抄的实测高度必然随每次布局改动漂移(历史上正是 contentMaxDp 反复失效的原因);
// 页面高度只守失控上限,默认窗口高度不依赖测量结果。

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
	return winW - navW - 2*pageSideW - scrollBarW
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

// measurePagesAt 在给定窗口宽度下离屏量出全部页面高度。
func measurePagesAt(t *testing.T, winW int) (maxH int, maxName string) {
	t.Helper()
	contentW := contentWidthFor(winW)
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
	for _, p := range pages {
		h := measurePage(p.w, contentW)
		if h <= 0 {
			t.Errorf("%s 量得高度 %d,页面被压扁或未布局(控件会直接消失)", p.name, h)
		}
		// 失控上限:栅格整体退化成单列也不该超过预算的两倍
		if h > pageBudget*2 {
			t.Errorf("%s 高度 %d 超过失控阈值 %d,多半是约束传导把内容撑爆(见 gui.go scrolledPage 注释)",
				p.name, h, pageBudget*2)
		}
		if h > maxH {
			maxH, maxName = h, p.name
		}
		t.Logf("%-12s contentH = %d", p.name, h)
	}
	return maxH, maxName
}

// TestPageHeightsAgainstBudget 量出各页真实高度,并说明默认窗口是否装得下最高页。
// 装不下不是错误(规范允许滚动),这里只保证"量得到、不失控、说得清"。
func TestPageHeightsAgainstBudget(t *testing.T) {
	fakeSnaps()
	maxH, maxName := measurePagesAt(t, defWinW)

	_, h := defaultWindowSize()
	need := windowHeightFor(maxH)
	aw, ah := win32.PrimaryWorkAreaDp()
	if h >= need {
		t.Logf("默认窗口高 %d ≥ 最高页(%s)所需 %d:开窗无滚动", h, maxName, need)
	} else {
		t.Logf("默认窗口高 %d < 最高页(%s)所需 %d:该页滚动显示(工作区 %dx%d Dp)", h, maxName, need, aw, ah)
	}
	if maxH <= 0 {
		t.Fatal("未量到任何页面高度")
	}
}

// TestWideEnoughForTwoColumns 默认宽度必须让子栅格保持并排(否则内容高度暴涨)。
func TestWideEnoughForTwoColumns(t *testing.T) {
	if defWinW < gridWideW {
		t.Fatalf("默认宽度 %d < 并排阈值 %d,子栅格会退化为单列", defWinW, gridWideW)
	}
	// 阈值上/下的真实高度应显著不同(证明这个阈值确实有意义)
	fakeSnaps()
	narrow := measurePage(g.pageSystem, contentWidthFor(gridWideW-200))
	wide := measurePage(g.pageSystem, contentWidthFor(gridWideW))
	t.Logf("窄窗(系统页)=%d  宽窗(系统页)=%d", narrow, wide)
	if narrow <= wide {
		t.Errorf("窗口变窄后系统页高度(%d)未超过宽窗(%d),阈值 %d 可能已失效",
			narrow, wide, gridWideW)
	}
}

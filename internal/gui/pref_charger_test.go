package gui

import (
	"image"
	"reflect"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// 隔离全局主题/动画状态,只注入内存快照;不创建 Backend 或调用硬件。
func draftTestGUI(t *testing.T) *GUI {
	t.Helper()
	previous := g
	g = nil
	t.Cleanup(func() { g = previous })
	fakeSnaps()
	return g
}

func draftPageFrame(page layout.Widget) func() {
	var ops op.Ops
	return func() {
		ops.Reset()
		page(layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(1200, 20000)},
		})
	}
}

func TestPrefDraftSnapshotSync(t *testing.T) {
	gui := draftTestGUI(t)
	s := &Pref{BlueLight: 3, Language: 8, PgUpDn: true}
	s.Keyboard.FnLock, s.Keyboard.WinKey = true, true
	s.GPUSwitch.Support, s.GPUSwitch.Current = 15, 3
	gui.prefSnap.Store(s)
	frame := draftPageFrame(gui.pagePref)
	frame()
	if gui.w.prefSource != s || gui.w.pfLang.selected() != 8 || gui.w.pfGPU.selected() != 2 {
		t.Fatal("首帧没有同步偏好、语言和 GPU 模式")
	}

	gui.w.pfFnLock.Value, gui.w.pfWin.Value = false, false
	gui.w.pfFnWin.Value, gui.w.pfPg.Value = true, false
	gui.w.pfBL.set(7)
	gui.w.pfLang.sel = 1
	// 通过真实下拉选项点击路径选择动态切换,而不是改快照。
	gui.w.pfGPU.open = true
	gui.w.pfGPU.optClks[3].Click()
	frame()
	option := &gui.w.pfGPU.optClks[0]
	for i := 0; i < 4; i++ {
		frame()
		if gui.w.pfFnLock.Value || gui.w.pfWin.Value || !gui.w.pfFnWin.Value || gui.w.pfPg.Value ||
			gui.w.pfBL.value() != 7 || gui.w.pfLang.selected() != 1 || gui.w.pfGPU.selected() != 3 {
			t.Fatalf("第 %d 次重复布局丢失偏好草稿或 GPU 选择", i+1)
		}
		if option != &gui.w.pfGPU.optClks[0] {
			t.Fatal("同一快照的重复布局重建了 GPU 点击状态")
		}
	}
	if s.BlueLight != 3 || s.Language != 8 || s.GPUSwitch.Current != 3 {
		t.Fatal("编辑草稿不应修改快照")
	}

	// 即便刷新数据没有变化,新快照也必须重新同步所有草稿。
	refreshed := *s
	gui.prefSnap.Store(&refreshed)
	frame()
	if gui.w.prefSource != &refreshed || !gui.w.pfFnLock.Value || !gui.w.pfWin.Value ||
		gui.w.pfFnWin.Value || !gui.w.pfPg.Value || gui.w.pfBL.value() != 3 ||
		gui.w.pfLang.selected() != 8 || gui.w.pfGPU.selected() != 2 {
		t.Fatal("新快照没有重新同步偏好草稿")
	}
}

func TestPrefGPUOptionsFollowSupport(t *testing.T) {
	gui := draftTestGUI(t)
	frame := draftPageFrame(gui.pagePref)
	labels := []string{"仅集成显卡", "仅独立显卡", "MS Hybrid", "动态切换"}
	for support := 0; support < 16; support++ {
		for current := 1; current <= 5; current++ {
			s := &Pref{}
			s.GPUSwitch.Support, s.GPUSwitch.Current = support, current
			gui.prefSnap.Store(s)
			frame()
			var want []string
			selected := -1
			for i, label := range labels {
				if support&(1<<i) != 0 {
					if selected == -1 || current == i+1 {
						selected = len(want)
					}
					want = append(want, label)
				}
			}
			if len(gui.w.pfGPU.opts) != len(want) || len(gui.w.pfGPU.optClks) != len(want) ||
				(len(want) > 0 && !reflect.DeepEqual(gui.w.pfGPU.opts, want)) || gui.w.pfGPU.selected() != selected {
				t.Fatalf("support=%d current=%d: opts=%v clicks=%d sel=%d; want=%v sel=%d",
					support, current, gui.w.pfGPU.opts, len(gui.w.pfGPU.optClks), gui.w.pfGPU.selected(), want, selected)
			}
			// 打开缩减后的下拉也不能越界访问旧的点击数组。
			gui.w.pfGPU.open = true
			frame()
		}
	}
}

func TestPrefGPUConfirmationWithoutSupport(t *testing.T) {
	gui := draftTestGUI(t)
	s := &Pref{}
	s.GPUSwitch.Support, s.GPUSwitch.Current = 2, 2
	gui.prefSnap.Store(s)
	frame := draftPageFrame(gui.pagePref)
	frame()
	gui.w.pfGPUApply.clk.Click()
	frame()
	armed := gui.w.pfGPUApply.arm
	if armed.IsZero() {
		t.Fatal("应用并重启没有要求二次确认")
	}
	for i := 0; i < 3; i++ {
		frame()
		if !gui.w.pfGPUApply.arm.Equal(armed) {
			t.Fatal("同一快照的重复布局丢失二次确认")
		}
	}

	gui.prefSnap.Store(&Pref{})
	frame()
	if len(gui.w.pfGPU.opts) != 0 || len(gui.w.pfGPU.optClks) != 0 || !gui.w.pfGPUApply.arm.IsZero() {
		t.Fatal("无支持位时没有清空 GPU 选项或旧确认状态")
	}
	// 即便有遗留的选中下标和确认点击,无支持位也不能触发 Backend 请求。
	// Backend 保持 nil;错误下发会让测试直接失败,不接触真实硬件。
	gui.w.pfGPU.sel = 0
	for i := 0; i < 2; i++ {
		gui.w.pfGPUApply.clk.Click()
		frame()
	}
}

func TestChargerCustomDraftSnapshotSync(t *testing.T) {
	gui := draftTestGUI(t)
	s := &chargerSnap{}
	s.Pref.FlexiCharger.Mode = 1
	s.Pref.FlexiCharger.Start, s.Pref.FlexiCharger.Stop = 70, 80
	gui.charger.Store(s)
	frame := draftPageFrame(gui.pageCharger)
	frame()
	if gui.w.chargerSource != s || gui.w.fcSelectedMode != 1 || gui.w.fcStart.value() != 70 || gui.w.fcStop.value() != 80 {
		t.Fatal("首帧没有同步充电模式和阈值")
	}
	gui.w.fcMode[2].Click()
	frame()
	if gui.w.fcSelectedMode != 2 {
		t.Fatal("点击自定义没有保存本地模式")
	}
	gui.w.fcStart.set(65)
	gui.w.fcStop.set(90)
	for i := 0; i < 4; i++ {
		frame()
		if gui.w.fcSelectedMode != 2 || gui.w.fcStart.value() != 65 || gui.w.fcStop.value() != 90 {
			t.Fatalf("第 %d 次重复布局丢失自定义模式或阈值草稿", i+1)
		}
	}
	if s.Pref.FlexiCharger.Mode != 1 || s.Pref.FlexiCharger.Start != 70 || s.Pref.FlexiCharger.Stop != 80 {
		t.Fatal("选择自定义不应修改快照或立即下发")
	}
	refreshed := *s
	gui.charger.Store(&refreshed)
	frame()
	if gui.w.chargerSource != &refreshed || gui.w.fcSelectedMode != 1 || gui.w.fcStart.value() != 70 || gui.w.fcStop.value() != 80 {
		t.Fatal("新快照没有重新同步充电草稿")
	}
}

func TestChargerSnapshotModeClamped(t *testing.T) {
	gui := draftTestGUI(t)
	frame := draftPageFrame(gui.pageCharger)
	for _, tc := range []struct{ mode, want int }{{-1, 0}, {0, 0}, {1, 1}, {2, 2}, {99, 2}} {
		s := &chargerSnap{}
		s.Pref.FlexiCharger.Mode = tc.mode
		gui.charger.Store(s)
		frame()
		if gui.w.fcSelectedMode != tc.want {
			t.Fatalf("快照模式 %d: 本地模式=%d, want=%d", tc.mode, gui.w.fcSelectedMode, tc.want)
		}
	}
}

func TestDraftWidgetInitialization(t *testing.T) {
	gui := draftTestGUI(t)
	if gui.curveDragFan != -1 || gui.curveDragPt != -1 {
		t.Fatal("初始曲线状态不应表示正在拖动 CPU 点")
	}
	if gui.w.pfBL.onChange != nil {
		t.Fatal("偏好蓝光滑杆必须仅编辑草稿,不应拖动即下发")
	}
}

func TestKeyboardSnapshotAndTimer(t *testing.T) {
	gui := draftTestGUI(t)
	s := &KBStateOut{Type: 2, On: true, OverrideBoot: 1, SleepSeconds: 3665}
	gui.kb.Store(s)
	frame := draftPageFrame(gui.pageKB)
	frame()
	if !gui.w.kbOn.Value || !gui.w.kbOvr.Value || gui.w.slH.value() != 1 || gui.w.slM.value() != 1 || gui.w.slS.value() != 5 {
		t.Fatal("键盘快照或完整睡眠时间未同步")
	}
	gui.w.slM.setText(8)
	frame()
	if gui.w.slM.value() != 8 {
		t.Fatal("重复布局覆盖了睡眠时间草稿")
	}
	gui.kb.Store(&KBStateOut{Type: 2, SleepSeconds: 120})
	frame()
	if gui.w.kbOn.Value || gui.w.kbOvr.Value || gui.w.slM.value() != 2 {
		t.Fatal("新键盘快照未同步")
	}
}

func TestSystemSwitchSnapshotSync(t *testing.T) {
	gui := draftTestGUI(t)
	s := *gui.sys.Load()
	s.opts.Noiseless, s.ad.Resume = true, true
	gui.sys.Store(&s)
	frame := draftPageFrame(gui.pageSystem)
	frame()
	if !gui.w.noiseless.Value || !gui.w.dustResume.Value {
		t.Fatal("系统开关未显示真实快照")
	}
	updated := s
	updated.opts.Noiseless, updated.ad.Resume = false, false
	gui.sys.Store(&updated)
	frame()
	if gui.w.noiseless.Value || gui.w.dustResume.Value {
		t.Fatal("系统开关未同步新快照")
	}
}

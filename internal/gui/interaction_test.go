package gui

import (
	"image"
	"os"
	"testing"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

// tapAt 在指定坐标模拟一次完整点击(按下 + 抬起)。
// Gio v0.10:事件先 Queue,再随 Frame 一起分发给命中区域。
func tapAt(r *input.Router, pos image.Point) {
	p := f32.Pt(float32(pos.X), float32(pos.Y))
	r.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
	)
}

// clickAt 渲染一帧并点击指定位置,再渲染一帧让点击生效。
func clickAt(render func(), r *input.Router, pos image.Point) {
	render()      // 建立命中区域
	tapAt(r, pos) // 投递点击
	render()      // 分发并处理
}

// TestSwitchTogglesValue 开关点击必须真正改写调用方的 widget.Bool
// (switchBoxAnimated 以 bool 为唯一真相,写回漏掉会导致"点了没反应")。
func TestSwitchTogglesValue(t *testing.T) {
	if g == nil {
		g = &GUI{th: newTheme()}
	}
	var (
		b    widget.Bool
		ops  op.Ops
		rtr  input.Router
		size = image.Pt(120, 60)
	)
	render := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(size),
			Source:      rtr.Source(),
		}
		switchBoxAnimated(gtx, &b) // 直接布局命中区域 42×24
		rtr.Frame(&ops)
	}

	before := b.Value
	clickAt(render, &rtr, image.Pt(20, 12))
	if b.Value == before {
		t.Fatalf("点击开关后 Value 未变化(仍为 %v)", b.Value)
	}
	t.Logf("开关状态:%v → %v", before, b.Value)
}

func TestSwitchDispatchesChangesBeforeLayout(t *testing.T) {
	regressionTheme(t)
	previous := switchAnims
	switchAnims = make(map[*widget.Bool]*toggle)
	t.Cleanup(func() { switchAnims = previous })
	for _, source := range []struct {
		name string
		kind pointer.Source
	}{{"mouse", pointer.Mouse}, {"touch", pointer.Touch}} {
		t.Run(source.name, func(t *testing.T) {
			var b widget.Bool
			var ops op.Ops
			var router input.Router
			var dispatched []bool
			render := func() {
				ops.Reset()
				gtx := regressionContext(&ops, &router, layout.Exact(image.Pt(120, 60)))
				if b.Update(gtx) {
					dispatched = append(dispatched, b.Value)
				}
				switchBox(gtx, &b)
				router.Frame(&ops)
			}
			queue := func(kind pointer.Kind) {
				buttons := pointer.Buttons(0)
				if kind == pointer.Press {
					buttons = pointer.ButtonPrimary
				}
				router.Queue(pointer.Event{
					Kind: kind, Source: source.kind, Position: f32.Pt(20, 12), Buttons: buttons,
				})
			}
			render()
			if len(dispatched) != 0 {
				t.Fatal("initial layout dispatched a command")
			}
			for i, want := range []bool{true, false} {
				queue(pointer.Press)
				render()
				if len(dispatched) != i {
					t.Fatal("press dispatched before release")
				}
				queue(pointer.Release)
				render()
				for range 3 {
					render()
				}
				if b.Value != want || len(dispatched) != i+1 || dispatched[i] != want {
					t.Fatalf("value=%v dispatched=%v, want one command with value %v", b.Value, dispatched, want)
				}
			}
		})
	}
}

func TestSwitchNativeDispatch(t *testing.T) {
	if os.Getenv("CC_SWITCH_NATIVE") != "1" {
		t.Skip("CC_SWITCH_NATIVE=1")
	}
	regressionTheme(t)
	win := new(app.Window)
	win.Option(app.Title("Control Center Switch Probe"), app.Size(unit.Dp(600), unit.Dp(240)))
	g.win = win
	var b widget.Bool
	var ops op.Ops
	calls := 0
	for {
		switch e := win.Event().(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if b.Update(gtx) {
				calls++
				t.Logf("noiseless dispatch %d: %v (recording only, no hardware)", calls, b.Value)
			}
			fillRect(gtx, colBg)
			layout.Inset{Top: 20, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					func(gtx C) D {
						return txt(gtx, "Noiseless dispatch probe - no hardware writes", textStyle{Size: fsBody2, Col: colText})
					},
					vSpacer(20),
					func(gtx C) D { return switchBox(gtx, &b) },
					vSpacer(20),
					func(gtx C) D { return txt(gtx, "Dispatch count: "+itoa(calls), textStyle{Size: fsBig, Col: colAccent}) },
				)
			})
			e.Frame(gtx.Ops)
		case app.DestroyEvent:
			if e.Err != nil {
				t.Fatal(e.Err)
			}
			if calls != 2 || b.Value {
				t.Fatalf("calls=%d value=%v, want two dispatches ending off", calls, b.Value)
			}
			return
		}
	}
}

// TestSegmentedPicks 分段控件点击回传下标,且选中态随之更新。
func TestSegmentedPicks(t *testing.T) {
	if g == nil {
		g = &GUI{th: newTheme()}
	}
	var (
		seg  segmented
		ops  op.Ops
		rtr  input.Router
		size = image.Pt(300, 40)
	)
	labels := []string{"自动", "最大", "静音", "MaxQ", "IFSS"}
	picked := -1
	render := func() {
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(size),
			Source:      rtr.Source(),
		}
		_, picked = seg.Layout(gtx, labels)
		rtr.Frame(&ops)
	}
	render()
	if picked != -1 {
		t.Fatalf("首帧不应产生点击,得到 %d", picked)
	}
	// 点第 3 段(每段 60px,中心 x≈150)
	clickAt(render, &rtr, image.Pt(150, 20))
	if picked != 2 {
		t.Fatalf("期望命中第 3 段(下标 2),实际 %d", picked)
	}
	if seg.sel != 2 {
		t.Fatalf("选中态未更新,sel=%d", seg.sel)
	}
}

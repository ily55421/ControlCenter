package gui

import (
	"image"
	"testing"

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

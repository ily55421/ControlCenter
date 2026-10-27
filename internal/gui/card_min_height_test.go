package gui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestCardHonoursMinHeight 卡片在给定 Min.Y 时必须撑到该高度(页面"卡片撑满"依赖它)。
func TestCardHonoursMinHeight(t *testing.T) {
	fakeSnaps()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(400, 100000)},
	}
	natural := panel(gtx, "测试", func(gtx C) D { return kvRow(gtx, "键", "值") }).Size.Y
	// 撑高时卡片占用高度 == 给定 Min.Y(底部 16dp 间距算在卡片内)
	for _, want := range []int{natural + 100, natural + 400} {
		g := gtx
		g.Constraints.Min.Y = want
		g.Constraints.Max.Y = 100000
		got := panel(g, "测试", func(gtx C) D { return kvRow(gtx, "键", "值") }).Size.Y
		if got != want {
			t.Errorf("Min.Y=%d 时卡片高 %d(自然高 %d),期望 %d", want, got, natural, want)
		}
	}
	t.Logf("自然高=%d", natural)
}

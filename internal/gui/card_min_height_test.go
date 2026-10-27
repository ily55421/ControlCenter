package gui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestCardKeepsNaturalHeight(t *testing.T) {
	regressionTheme(t)
	var ops op.Ops
	gtx := layout.Context{
		Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(400, 100000)},
	}
	body := func(gtx C) D { return kvRow(gtx, "键", "值") }
	natural := panel(gtx, "测试", body).Size.Y
	for _, inherited := range []int{natural + 100, natural + 400} {
		ops.Reset()
		gtx.Constraints.Min.Y = inherited
		if got := panel(gtx, "测试", body).Size.Y; got != natural {
			t.Errorf("inherited Min.Y=%d stretched card to %d, natural height=%d", inherited, got, natural)
		}
	}
}

package gui

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestFillStability 余量分配必须逐帧稳定(不来回跳),否则页面会抖。
func TestFillStability(t *testing.T) {
	fakeSnaps()
	size := image.Pt(1400, 1326)
	for _, p := range []struct {
		n  string
		id int
	}{{"bat", pgBat}, {"pref", pgPref}, {"gpu", pgGPU}, {"ch", pgCharger}} {
		g.page = p.id
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size)}
		var prev int = -1
		for i := 0; i < 6; i++ {
			g.layoutRoot(gtx)
			if i >= 3 && prev >= 0 && absI(pageSlack-prev) > 1 {
				t.Errorf("%s 第 %d 帧余量不稳定: %d → %d", p.n, i, prev, pageSlack)
			}
			prev = pageSlack
			fmt.Printf("%s f%d slack=%d used=%d natH=%d\n", p.n, i, pageSlack, pageSlackUsed, pageScroll[p.id].natH)
			ops.Reset()
		}
	}
}

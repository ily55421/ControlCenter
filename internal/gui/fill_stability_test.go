package gui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestFillStability(t *testing.T) {
	fakeSnaps()
	for _, page := range []int{pgBat, pgPref, pgGPU, pgCharger} {
		g.page = page
		var ops op.Ops
		var previous int
		for frame := 0; frame < 6; frame++ {
			ops.Reset()
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1400, 1326))}
			g.layoutRoot(gtx)
			height := pageScroll[page].contentH
			if frame > 0 && height != previous {
				t.Fatalf("page %d: content height changed from %d to %d", page, previous, height)
			}
			previous = height
		}
	}
}

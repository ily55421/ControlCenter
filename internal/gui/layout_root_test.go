package gui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestLayoutRootRepeatsFrames 离屏连续渲染整窗若干帧:守住"首帧卡死/无法提交"这类
// 会让真机窗口一片黑的回归(真机黑屏时先跑这条)。
func TestLayoutRootRepeatsFrames(t *testing.T) {
	fakeSnaps()
	g.page = pgSystem
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(defWinW, 1326)),
	}
	for i := 0; i < 4; i++ {
		d := g.layoutRoot(gtx)
		if d.Size.X == 0 || d.Size.Y == 0 {
			t.Fatalf("第 %d 帧尺寸为 0", i+1)
		}
	}
	if g.page != pgSystem {
		t.Fatalf("页面被意外切换")
	}
}

func TestPagesAtViewportAndScale(t *testing.T) {
	previous, previousScroll := g, pageScroll
	g = nil
	t.Cleanup(func() { g, pageScroll = previous, previousScroll })
	fakeSnaps()
	for _, tc := range []struct {
		size  image.Point
		scale float32
	}{
		{image.Pt(1000, 560), 1},
		{image.Pt(1250, 700), 1.25},
		{image.Pt(2800, 1800), 2},
	} {
		for _, collapsed := range []bool{false, true} {
			g.sideMin = collapsed
			for page := 0; page < pgCount; page++ {
				g.page = page
				pageScroll[page] = scrollState{}
				var ops op.Ops
				for range 2 {
					ops.Reset()
					gtx := layout.Context{
						Ops: &ops, Constraints: layout.Exact(tc.size),
						Metric: unit.Metric{PxPerDp: tc.scale, PxPerSp: tc.scale},
					}
					if got := g.layoutRoot(gtx).Size; got != tc.size {
						t.Fatalf("page=%d scale=%v collapsed=%v: root=%v, want=%v", page, tc.scale, collapsed, got, tc.size)
					}
				}
				if sc := pageScroll[page]; sc.contentH <= 0 || sc.viewH <= 0 || sc.viewH >= tc.size.Y {
					t.Fatalf("page=%d scale=%v collapsed=%v: invalid viewport %+v", page, tc.scale, collapsed, sc)
				}
			}
		}
	}
}

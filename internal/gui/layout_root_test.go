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

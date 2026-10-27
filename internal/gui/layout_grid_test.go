package gui

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// 本文件守住两个曾经真实出错的栅格/编号不变量:
//   1) grid2 两列之间必须有显式间距,且 列宽 + 间距 == 可用宽度(P0);
//   2) E 核 1-16 的显示编号与 ecoreEd[0..15] 的绑定一一对应(P0)。

// measureGrid2 在给定内容宽度下量一次 grid2,返回左右两列实际拿到的宽度。
func measureGrid2(t *testing.T, contentW int, wl, wr float32) (lw, rw int) {
	t.Helper()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(contentW, 20000)},
	}
	grid2(gtx, wl, wr,
		func(gtx C) D {
			lw = gtx.Constraints.Max.X
			return D{Size: image.Pt(lw, 10)}
		},
		func(gtx C) D {
			rw = gtx.Constraints.Max.X
			return D{Size: image.Pt(rw, 10)}
		})
	return lw, rw
}

// TestGrid2InsertsGapAndConservesWidth 列宽之和加间距必须等于可用宽度。
// 修复前 gap 只从列宽里扣除、没有占位子项,lw+rw 恰好等于 w,两列贴在一起。
func TestGrid2InsertsGapAndConservesWidth(t *testing.T) {
	for _, contentW := range []int{defWinW - navW - 2*pageSideW - scrollBarW, 1320, 1600} {
		lw, rw := measureGrid2(t, contentW, 5, 4)
		gap := gridGap // Dp == px @1x
		if lw <= 0 || rw <= 0 {
			t.Fatalf("内容宽 %d:列宽异常 lw=%d rw=%d", contentW, lw, rw)
		}
		if got := lw + gap + rw; got != contentW {
			t.Errorf("内容宽 %d:列宽+间距=%d(lw=%d gap=%d rw=%d),期望 %d",
				contentW, got, lw, gap, rw, contentW)
		}
		// 权重 5:4 —— 左列应明显更宽,且差值要与权重成比例
		wantL := int(float32(contentW-gap) * 5 / 9)
		if diff := lw - wantL; diff < -1 || diff > 1 {
			t.Errorf("内容宽 %d:左列宽 %d,按 5:4 权重期望 %d", contentW, lw, wantL)
		}
	}
}

// TestGrid2FallsBackToSingleColumn 断点以下、以及任一列窄于 halfMinW 时必须单列,
// 否则卡片会被压到放不下内容(标签换行/输入框被裁)。
func TestGrid2FallsBackToSingleColumn(t *testing.T) {
	cases := []struct {
		name     string
		contentW int
		wl, wr   float32
	}{
		{"低于双列断点", gridPairW - 1, 1, 1},
		{"极限窗口内容宽", minWinW - navW - 2*pageSideW - scrollBarW, 5, 4},
		{"权重悬殊导致右列过窄", gridPairW, 1, 8},
	}
	for _, c := range cases {
		lw, rw := measureGrid2(t, c.contentW, c.wl, c.wr)
		if lw != c.contentW || rw != c.contentW {
			t.Errorf("%s(内容宽 %d):应为单列全宽,实得 lw=%d rw=%d", c.name, c.contentW, lw, rw)
		}
	}
	// 断点之上必须真的分两列(证明断点不是摆设)
	lw, rw := measureGrid2(t, gridPairW, 1, 1)
	if lw == gridPairW || rw == gridPairW {
		t.Errorf("内容宽 %d 应并排两列,实得 lw=%d rw=%d", gridPairW, lw, rw)
	}
}

// TestCoreGroupLabelsFollowIndex 显示编号必须由字段起始下标推导。
// 修复前 E 核第二列绑定 ecoreEd[8+i] 却显示「E核 1..8」。
func TestCoreGroupLabelsFollowIndex(t *testing.T) {
	cases := []struct {
		gr   coreGroup
		i    int
		want string
	}{
		{coreGroup{off: 0, n: 10, pre: "P"}, 0, "P 核 1"},
		{coreGroup{off: 0, n: 10, pre: "P"}, 9, "P 核 10"},
		{coreGroup{off: 0, n: 8, pre: "E"}, 7, "E 核 8"},
		{coreGroup{off: 8, n: 8, pre: "E"}, 0, "E 核 9"},
		{coreGroup{off: 8, n: 8, pre: "E"}, 7, "E 核 16"},
	}
	for _, c := range cases {
		if got := c.gr.label(c.i); got != c.want {
			t.Errorf("coreGroup{off:%d,n:%d,pre:%q}.label(%d) = %q,期望 %q",
				c.gr.off, c.gr.n, c.gr.pre, c.i, got, c.want)
		}
	}
}

// TestEcoreFieldGroupsCoverAllFields E 核两列必须连续覆盖 ecoreEd[0..15],
// 且每个显示编号只出现一次(1-16 齐全、无重复、无缺口)。
func TestEcoreFieldGroupsCoverAllFields(t *testing.T) {
	seen := map[int]string{}
	for _, gr := range ecoreFieldGroups() {
		for i := 0; i < gr.n; i++ {
			idx := gr.off + i
			label := gr.label(i)
			if idx < 0 || idx >= 16 {
				t.Fatalf("字段下标 %d 越界(E 核只有 16 项),标签 %q", idx, label)
			}
			if prev, dup := seen[idx]; dup {
				t.Fatalf("字段下标 %d 被两个标签绑定:%q 与 %q", idx, prev, label)
			}
			seen[idx] = label
			// 显示编号 = 字段下标 + 1
			if want := fmt.Sprintf("E 核 %d", idx+1); label != want {
				t.Errorf("字段 ecoreEd[%d] 显示 %q,期望 %q", idx, label, want)
			}
		}
	}
	if len(seen) != 16 {
		t.Fatalf("E 核覆盖字段 %d 个,期望 16", len(seen))
	}
}

package gui

import (
	"image"
	"testing"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// 本文件守住键盘图的几何不变量(方案 4.2/4.3 的 P0 项):
//   1) 键帽几何随 DPI 缩放(不再写死 36/5 设备像素);
//   2) 可用宽度不足时保持最小可读宽度,由局部横向滚动补足;
//   3) 在最小宽度下每个键帽都放得下自己的标签(不再"右侧被裁切");
//   4) 命中测试与绘制几何一致(点哪里就是哪里)。

// mkGtx 造一个离屏布局上下文(pxPerDp 可调,用于验证 DPI 缩放)。
func mkGtx(w, h int, pxPerDp float32) layout.Context {
	var ops op.Ops
	return layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: pxPerDp, PxPerSp: pxPerDp},
		Constraints: layout.Constraints{Max: image.Pt(w, h)},
	}
}

// TestKeyboardGeometryScalesWithDPI 键高与间距必须随 DPI 缩放:
// 修复前 renderKeyboardImage/kbKeyRect/kbHitTest 全部写死 36/5 设备像素,
// 200% 缩放下键盘图仍是 36px 高,文字与命中区随之偏小。
func TestKeyboardGeometryScalesWithDPI(t *testing.T) {
	for _, dpi := range []float32{1, 1.25, 1.5, 2} {
		avail := int(float32(1200) * dpi)
		gtx := mkGtx(avail, int(800*dpi), dpi)
		gm := kbGeomFor(gtx, avail)
		wantKeyH := int(float32(kbKeyH)*dpi + 0.5)
		if diff := gm.keyH - wantKeyH; diff < -1 || diff > 1 {
			t.Errorf("DPI %.2f:键高 %d,期望约 %d(≈%ddp)", dpi, gm.keyH, wantKeyH, kbKeyH)
		}
		wantGap := int(float32(kbKeyGap)*dpi + 0.5)
		if diff := gm.gap - wantGap; diff < -1 || diff > 1 {
			t.Errorf("DPI %.2f:键间距 %d,期望约 %d(≈%ddp)", dpi, gm.gap, wantGap, kbKeyGap)
		}
		if want := len(kbRows)*(gm.keyH+gm.gap) - gm.gap; gm.h != want {
			t.Errorf("DPI %.2f:键盘高 %d,期望 %d", dpi, gm.h, want)
		}
		if gm.w != avail || gm.viewW != avail {
			t.Errorf("DPI %.2f:宽度 %d/可见 %d,宽裕时应等于可用宽 %d", dpi, gm.w, gm.viewW, avail)
		}
		if off := gm.maxOff(); off != 0 {
			t.Errorf("DPI %.2f:宽裕时不应可滚动,实得最大偏移 %d", dpi, off)
		}
	}
}

// TestKeyboardKeepsMinWidthAndScrolls 窄卡片下不得压缩键帽:
// 保持 kbMinW 渲染宽度,差量由水平滚动承担。
func TestKeyboardKeepsMinWidthAndScrolls(t *testing.T) {
	const avail = 400
	gtx := mkGtx(avail, 400, 1)
	gm := kbGeomFor(gtx, avail)
	if gm.viewW != avail {
		t.Errorf("可见宽 %d,期望可用宽 %d", gm.viewW, avail)
	}
	if gm.w != kbMinW {
		t.Errorf("渲染宽 %d,期望最小可读宽度 %d", gm.w, kbMinW)
	}
	if off := gm.maxOff(); off != kbMinW-avail {
		t.Errorf("最大水平偏移 %d,期望 %d", off, kbMinW-avail)
	}
	// 约束未定(无限大)时必须退回最小宽度,不能去分配天量离屏位图
	if g := kbGeomFor(mkGtx(1<<30, 400, 1), 1<<30); g.w != kbMinW {
		t.Errorf("无限宽约束下渲染宽 %d,期望退回 %d", g.w, kbMinW)
	}
}

// TestKeyboardLabelsFitAtMinWidth 最小渲染宽度下每个键帽都要放得下标签。
func TestKeyboardLabelsFitAtMinWidth(t *testing.T) {
	gtx := mkGtx(kbMinW, 400, 1)
	gm := kbGeomFor(gtx, kbMinW)
	for ri, row := range kbRows {
		for ci, k := range row {
			if k.label == "" {
				continue
			}
			r := kbKeyRect(gm, ri, ci)
			// 键帽文字用 basicfont 7x13,等宽 7px
			need := utf8.RuneCountInString(k.label) * 7
			if r.Dx() < need {
				t.Errorf("%q(行 %d 列 %d)键宽 %d < 标签宽 %d:最小宽度 %ddp 仍会裁字",
					k.label, ri, ci, r.Dx(), need, kbMinW)
			}
		}
	}
}

// TestKeyboardHitMatchesGeometry 命中测试与键帽矩形必须逐键一致(DPI 无关)。
func TestKeyboardHitMatchesGeometry(t *testing.T) {
	for _, tc := range []struct {
		dpi   float32
		avail int
	}{
		{1, kbMinW},
		{1.25, 900},
		{1.5, 1200},
	} {
		gtx := mkGtx(tc.avail, 400, tc.dpi)
		gm := kbGeomFor(gtx, tc.avail)
		for ri, row := range kbRows {
			// 每行的键必须铺满渲染宽度(最后一键右缘 = 渲染宽),否则右侧就是被裁掉的
			last := kbKeyRect(gm, ri, len(row)-1)
			if last.Max.X != gm.w {
				t.Errorf("DPI %.2f 行 %d:最后一键右缘 %d,渲染宽 %d(右侧有缺口)",
					tc.dpi, ri, last.Max.X, gm.w)
			}
			for ci := range row {
				r := kbKeyRect(gm, ri, ci)
				cx, cy := r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2
				gotR, gotC, hit := kbHitTest(gm, cx, cy)
				if !hit || gotR != ri || gotC != ci {
					t.Errorf("DPI %.2f:键 (%d,%d) 中心 (%d,%d) 命中 (%d,%d,hit=%v)",
						tc.dpi, ri, ci, cx, cy, gotR, gotC, hit)
				}
			}
		}
	}
}

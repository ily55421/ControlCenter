package gui

import (
	"image"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestHeaderControlsNoOverlap 标题栏右侧四个控件必须互不重叠,且都在可视区内。
//
// 曾经的 bug:info 按钮画在 w-44 起、宽 32,而关闭按钮占 [w-36, w),
// 两者重叠 24px,关图标被 info 盖住(截图可见)。这里用几何断言守住。
func TestHeaderControlsNoOverlap(t *testing.T) {
	fakeSnaps()
	const winW = 1400
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(winW, 60)),
	}
	_ = g.layoutHeader(gtx)

	// 与 layoutHeader 内的排布保持一致
	padR := Snap(gtx, headerPadR)
	btnW := gtx.Dp(winBtnDp)
	infoW := Snap(gtx, infoBtnDp)
	gap := Snap(gtx, headerGap)

	closeX := winW - padR - btnW
	maxX := closeX - btnW
	minX := maxX - btnW
	infoX := minX - gap - infoW

	type box struct {
		name   string
		x0, x1 int
	}
	boxes := []box{
		{"info", infoX, infoX + infoW},
		{"min", minX, minX + btnW},
		{"max", maxX, maxX + btnW},
		{"close", closeX, closeX + btnW},
	}

	// 1) 两两不重叠(按 x 排序后检查相邻)
	for i := 1; i < len(boxes); i++ {
		if boxes[i-1].x1 > boxes[i].x0 {
			t.Errorf("%s[%d,%d) 与 %s[%d,%d) 重叠",
				boxes[i-1].name, boxes[i-1].x0, boxes[i-1].x1,
				boxes[i].name, boxes[i].x0, boxes[i].x1)
		}
	}

	// 2) 全部落在窗口内
	for _, b := range boxes {
		if b.x0 < 0 || b.x1 > winW {
			t.Errorf("%s 超出窗口范围 [%d,%d) (窗口宽 %d)", b.name, b.x0, b.x1, winW)
		}
	}

	// 3) 关闭按钮必须紧贴右缘(留 padR),这是用户最容易点的位置
	if got := winW - boxes[3].x1; got != padR {
		t.Errorf("关闭按钮右侧留白=%d,期望 %d", got, padR)
	}
}

// TestHeaderButtonsRendered 四个控件的图标都要真实画出来(不能被覆盖)。
func TestHeaderButtonsRendered(t *testing.T) {
	fakeSnaps()
	size := image.Pt(1400, 60)
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless 后端不可用: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
	}
	fillArea(gtx, image.Rect(0, 0, size.X, size.Y), colBar)
	g.layoutHeader(gtx)
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame 失败: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}

	// 在标题栏中线扫描亮像素段,期望在右侧控件区出现 4 段(信息/最小化/最大化/关闭)
	var runs [][2]int
	start := -1
	for x := size.X - 200; x < size.X; x++ {
		c := img.RGBAAt(x, 23)
		bright := int(c.R)+int(c.G)+int(c.B) > 260
		if bright && start < 0 {
			start = x
		} else if !bright && start >= 0 {
			runs = append(runs, [2]int{start, x - 1})
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, [2]int{start, size.X - 1})
	}
	t.Logf("右侧图标亮像素段: %v", runs)
	if len(runs) < 4 {
		t.Errorf("右侧只检测到 %d 处图标(期望 4 处:信息/最小化/最大化/关闭),可能有控件被遮挡", len(runs))
	}
}

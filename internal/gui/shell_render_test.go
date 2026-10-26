package gui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// renderShell 离屏渲染应用外壳(背景 + 侧栏 + 标题栏 + 型号栏 + 页头 + 一张卡片)。
// 用于布局改动的视觉回归:不做像素级比对,只校验关键区域被真实绘制。
func renderShell(t *testing.T, size image.Point) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless 后端不可用: %v", err)
	}
	defer win.Release()

	if g == nil {
		g = &GUI{th: newTheme()}
		g.initWidgets()
	}

	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
	}
	drawBackdrop(gtx)
	// 与 layoutRoot 一致:侧栏固定宽,右侧内容区吃剩余宽度并按可用高度排布
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(g.layoutSidebar),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(g.layoutHeader),
				layout.Rigid(g.layoutModeline),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return pageHead(gtx, "系统监控", "实时利用率 / 温度 / 风扇转速与模式切换",
						func(gtx C) D { return pillBadge(gtx, "性能模式", colAccent) })
				}),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					gtx.Constraints.Min.Y = 0
					gtx.Constraints.Max.Y = math.MaxInt32 / 2
					return card(gtx, cardOpt{title: "风扇转速控制", sub: "曲线编辑", icon: "system"}, func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return kvRow(gtx, "自动偏移", "12%") },
							vSpacer(sp8),
							func(gtx C) D { return kvRow(gtx, "当前转速", "2150 RPM") },
							vSpacer(sp12),
							func(gtx C) D {
								return row(gtx,
									func(gtx C) D { return button(gtx, &g.w.fanSave, "保存曲线", btnPrimary, false) },
									hSpacer(sp10),
									func(gtx C) D { return button(gtx, &g.w.fanReset, "恢复默认", btnSoft, false) },
									hSpacer(sp20),
									func(gtx C) D {
										return g.w.noiseless.Layout(gtx, func(gtx C) D {
											return switchBoxAnimated(gtx, &g.w.noiseless)
										})
									},
									hSpacer(sp8),
									func(gtx C) D { return txt(gtx, "寂靜模式", textStyle{Size: fsBody2, Col: colText3}) })
							})
					})
				}),
			)
		}),
	)

	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame 失败: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

// countBright 统计区域内"明显亮于背景"的像素数(用于验证文字/控件真的画出来了)。
func countBright(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 90 && c.G > 90 && c.B > 90 {
				n++
			}
		}
	}
	return n
}

// TestShellLayout 外壳布局回归:背景分层、侧栏单行文案、标题栏、卡片内容与控件都要被绘制。
func TestShellLayout(t *testing.T) {
	size := image.Pt(1100, 620)
	img := renderShell(t, size)
	dumpShot(t, img)

	// 1) 背景不是纯黑(深色渐变底 + 网格)
	bg := img.RGBAAt(size.X-30, 40)
	if bg.R < 4 && bg.G < 4 && bg.B < 4 {
		t.Errorf("背景仍是纯黑,渐变底未绘制: %v", bg)
	}

	// 2) 侧栏导航文案区(单行布局)必须有文字像素
	if n := countBright(img, image.Rect(10, 90, 190, 400)); n < 200 {
		t.Errorf("侧栏文字区亮像素过少(%d),单行布局或文字未绘制", n)
	}

	// 3) 标题栏标题
	if n := countBright(img, image.Rect(40, 0, 260, 46)); n < 40 {
		t.Errorf("标题栏标题亮像素过少(%d)", n)
	}

	// 4) 卡片内容区(卡片头部下方)
	if n := countBright(img, image.Rect(240, 200, 700, 330)); n < 150 {
		t.Errorf("卡片内容亮像素过少(%d)", n)
	}

	// 5) 卡片底部按钮行(主按钮为青色实底,亮度明显)
	if n := countBright(img, image.Rect(240, 260, 620, 340)); n < 150 {
		t.Errorf("按钮行亮像素过少(%d)", n)
	}
}

// TestBackdropLayers 背景必须分层(渐变底 + 网格),不能是单一大色块。
func TestBackdropLayers(t *testing.T) {
	size := image.Pt(400, 300)
	img := renderShell(t, size)
	seen := map[color.RGBA]int{}
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			seen[img.RGBAAt(x, y)]++
		}
	}
	t.Logf("画面颜色种数=%d", len(seen))
	if len(seen) < 20 {
		t.Errorf("背景颜色种数过少(%d),渐变/网格未绘制", len(seen))
	}
}

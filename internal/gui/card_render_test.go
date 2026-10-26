package gui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// dumpShot 在 CC_SHOT_DIR 设置时把渲染结果落盘,便于人工核对视觉效果。
func dumpShot(t *testing.T, img image.Image) {
	t.Helper()
	dir := os.Getenv("CC_SHOT_DIR")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, t.Name()+".png"))
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}

// renderWidget 用 headless 后端离屏渲染一个 widget(dp/sp 与 px 1:1)。
func renderWidget(t *testing.T, size image.Point, w layout.Widget) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless 后端不可用: %v", err)
	}
	defer win.Release()

	// label/textDims 依赖包级主题(g.th),真实运行时由 Run 注入
	if g == nil {
		g = &GUI{th: newTheme()}
	} else if g.th == nil {
		g.th = newTheme()
	}

	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size),
	}
	fillRect(gtx, color.NRGBA{A: 0xFF}) // 纯黑底,便于分辨卡片边界
	w(gtx)

	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame 失败: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	return img
}

// TestCardRenders 卡片必须画出底色与头部标题。
func TestCardRenders(t *testing.T) {
	size := image.Pt(420, 240)
	img := renderWidget(t, size, func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(400, 200))
		return card(gtx, cardOpt{
			title: "CPU 监控",
			sub:   "每 1 秒刷新",
			icon:  "system",
		}, func(gtx C) D {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
				return label(gtx, "内容区域", 13, pal.Txt)
			})
		})
	})
	dumpShot(t, img)

	// 采样点取卡片内部(头部下方的内容区):卡片高 46 + 26 + 2×13 = 98,
	// 取 (40, 80) 落在内容区,不会被圆角/描边干扰。
	in := img.RGBAAt(40, 80)
	if in.R < 8 && in.G < 8 && in.B < 8 {
		t.Errorf("卡片内部仍是纯黑,卡片底未绘制: %v", in)
	}

	bright := 0
	for y := 6; y < 50; y++ {
		for x := 6; x < 260; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 150 && c.G > 150 && c.B > 150 {
				bright++
			}
		}
	}
	if bright < 30 {
		t.Errorf("头部标题区几乎没有亮像素(bright=%d),标题未绘制或位置错误", bright)
	}
	t.Logf("header bright=%d cardInner=%v", bright, in)
}

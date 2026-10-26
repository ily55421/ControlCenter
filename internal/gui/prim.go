// prim — 绘制原语:像素吸附、圆角填充/描边、柔和投影、顶边高光、渐变强调线、
// 文本、分隔线、绘制底色。
//
// Gio 没有 CSS 的 box-shadow / ::after / 多段渐变,这里用纯 clip + paint 做等价实现,
// 每个原语都刻意控制在 1~3 个 op(见 docs/gio-optimization.md 第 2 节)。
package gui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// Snap 把 dp 尺寸吸附到整像素(125%/150% 缩放下 1dp 会被插值成 2px 灰雾横线)。
func Snap(gtx C, v unit.Dp) int {
	px := float32(gtx.Dp(v))
	if px > 0 && px < 1 {
		return 1 // 永远至少 1px,细线不会消失
	}
	return int(px + 0.5)
}

// fillRR 圆角矩形填充(圆角自动夹到尺寸的一半以内)。
func fillRR(gtx C, r image.Rectangle, radius int, col color.NRGBA) {
	if r.Dx() <= 0 || r.Dy() <= 0 || col.A == 0 {
		return
	}
	if radius > r.Dx()/2 {
		radius = r.Dx() / 2
	}
	if radius > r.Dy()/2 {
		radius = r.Dy() / 2
	}
	if radius <= 0 {
		fillArea(gtx, r, col)
		return
	}
	defer clip.RRect{Rect: r, NE: radius, NW: radius, SE: radius, SW: radius}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

// strokeRR 圆角矩形描边 w 像素(用「外填 + 内缩覆盖」两层矩形,比路径描边省 op,
// 圆角处也不会出现 1px 亮缝)。inner 为挖空时重绘的底色。
func strokeRR(gtx C, r image.Rectangle, radius, w int, col, inner color.NRGBA) {
	if r.Dx() <= 0 || r.Dy() <= 0 || w <= 0 || col.A == 0 {
		return
	}
	fillRR(gtx, r, radius, col)
	ir := r.Inset(w)
	innerR := radius - w
	if innerR < 0 {
		innerR = 0
	}
	fillRR(gtx, ir, innerR, inner)
}

// softShadow 双层柔和投影:Gio 没有 blur,用「下移 5dp / α=0x38」+「下移 2dp / α=0x28」
// 两层圆角矩形近似,深色背景下与模糊视觉等价,只多 2 个 op。
func softShadow(gtx C, r image.Rectangle, radius int) {
	const (
		dy1, in1, a1 = 5, 3, 0x38
		dy2, in2, a2 = 2, 1, 0x28
	)
	d1 := gtx.Dp(unit.Dp(dy1))
	d2 := gtx.Dp(unit.Dp(dy2))
	fillRR(gtx, image.Rect(r.Min.X+Snap(gtx, in1), r.Min.Y+d1, r.Max.X-Snap(gtx, in1), r.Max.Y+d1),
		radius, color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: a1})
	fillRR(gtx, image.Rect(r.Min.X+Snap(gtx, in2), r.Min.Y+d2, r.Max.X-Snap(gtx, in2), r.Max.Y+d2),
		radius, color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: a2})
}

// vGradRR 圆角矩形内的自上而下渐隐高光:用 8 条横带模拟,避免离屏渐变的开销。
func vGradRR(gtx C, r image.Rectangle, radius int, top, bottom color.NRGBA) {
	if r.Dy() <= 0 || r.Dx() <= 0 {
		return
	}
	defer clip.RRect{Rect: r, NE: radius, NW: radius, SE: radius, SW: radius}.Push(gtx.Ops).Pop()
	const bands = 8
	h := r.Dy()
	for i := 0; i < bands; i++ {
		y0 := r.Min.Y + h*i/bands
		y1 := r.Min.Y + h*(i+1)/bands
		if y1 <= y0 {
			continue
		}
		t := float32(i) / float32(bands-1)
		c := mixNRGBA(top, bottom, t)
		if c.A == 0 {
			continue
		}
		fillArea(gtx, image.Rect(r.Min.X, y0, r.Max.X, y1), c)
	}
}

// topHi 顶边 1px 内高光:让卡片上沿有一道几乎看不见的亮线,压出"浮起来"的层次。
func topHi(gtx C, r image.Rectangle, radius int, col color.NRGBA) {
	h := Snap(gtx, 1)
	if r.Dx() <= 2*radius || h <= 0 {
		return
	}
	// 只在顶边直段画,避免圆角处出现直角硬边
	seg := image.Rect(r.Min.X+radius, r.Min.Y, r.Max.X-radius, r.Min.Y+h)
	fillArea(gtx, seg, col)
}

// glowLine 顶边居中的强调色渐变线(3 个 op 的"伪外发光")。
// 整圈 stroke 太花;收窄到中间 1/3 更聚焦,也更容易被眼睛捕捉。
func glowLine(gtx C, r image.Rectangle, col color.NRGBA, a uint8) {
	const bands = 10
	third := r.Dx() / 3
	if third <= 0 {
		return
	}
	h := Snap(gtx, 1)
	l := image.Rect(r.Min.X+third/2, r.Min.Y, r.Min.X+third+third/2, r.Min.Y+h)
	m := image.Rect(l.Max.X, r.Min.Y, r.Max.X-third-third/2, r.Min.Y+h)
	rr := image.Rect(m.Max.X, r.Min.Y, r.Max.X-third/2, r.Min.Y+h)
	// 左段:透明 → 实色
	bw := l.Dx() / bands
	for i := 0; i < bands; i++ {
		f := float32(i+1) / float32(bands)
		seg := image.Rect(l.Min.X+i*bw, l.Min.Y, l.Min.X+(i+1)*bw, l.Max.Y)
		fillArea(gtx, seg, alpha(col, uint8(float32(a)*f)))
	}
	fillArea(gtx, m, alpha(col, a))
	// 右段:实色 → 透明
	for i := 0; i < bands; i++ {
		f := float32(bands-i) / float32(bands)
		seg := image.Rect(rr.Min.X+i*bw, rr.Min.Y, rr.Min.X+(i+1)*bw, rr.Max.Y)
		fillArea(gtx, seg, alpha(col, uint8(float32(a)*f)))
	}
}

// hLine 1px 水平分隔线(半透明,不用纯灰实线)。
func hLine(gtx C, r image.Rectangle, col color.NRGBA) {
	h := Snap(gtx, 1)
	fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+h), col)
}

// mixNRGBA 线性插值两个颜色(t=0 → a,t=1 → b)。
func mixNRGBA(a, b color.NRGBA, t float32) color.NRGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	lerp := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

// ---------- 文本 ----------

// textStyle 文本样式(值语义,便于按令牌组合)。
type textStyle struct {
	Size  float32
	Col   color.NRGBA
	W     font.Weight
	Lines int // 0 = 不限行
}

// txt 按样式绘制一段文本。
func txt(gtx C, s string, st textStyle) D {
	return materialLabel(s, st).Layout(gtx)
}

// txtDims 只测文本尺寸,绘制 ops 留待回放(先底后字的层序靠它保证)。
func txtDims(gtx C, s string, st textStyle) (D, op.CallOp) {
	m := op.Record(gtx.Ops)
	d := txt(gtx, s, st)
	return d, m.Stop()
}

// materialLabel 把 textStyle 转成带主题的文本样式。
func materialLabel(s string, st textStyle) material.LabelStyle {
	l := material.Label(g.th, unit.Sp(st.Size), s)
	l.Color = st.Col
	if st.W != 0 {
		l.Font.Weight = st.W
	}
	if st.Lines > 0 {
		l.MaxLines = st.Lines
		l.Truncator = "…"
	}
	return l
}

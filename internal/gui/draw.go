// draw — 自绘仪表:表盘(刻度环/进度弧/指针/辉光)、功耗折线图、色环、
// 键盘键帽图、电池轮廓。全部矢量绘制,D3D11 硬件渲染,无位图缩放损耗。
package gui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// strokeSegs 一次 stroke 描多条不相连线段(共享路径,省 draw op)。
func strokeSegs(gtx C, segs [][2]f32.Point, w float32, col color.NRGBA) {
	if len(segs) == 0 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	for _, s := range segs {
		p.MoveTo(s[0])
		p.LineTo(s[1])
	}
	st := clip.Stroke{Path: p.End(), Width: w}.Op()
	defer st.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

func dir(a float64) (float32, float32) { return float32(math.Cos(a)), float32(math.Sin(a)) }

// ---------- 表盘 ----------

type dialOpt struct {
	ticks  int      // 刻度数(默认 50)
	color  color.NRGBA
	labels []string // 弧上刻度标签
	center string   // 弧下中心文本
}

// drawDial 霓虹仪表盘:刻度环 + 发光进度弧 + 指针 + 中心文本(对齐 Web drawDial)。
func drawDial(gtx C, val, maxV float32, o dialOpt) D {
	w := gtx.Constraints.Max.X
	h := gtx.Constraints.Min.Y
	if h == 0 {
		h = w * 300 / 330
	}
	cx, cy := float32(w)/2, float32(h)/2
	R := float32(math.Min(float64(w), float64(h)))/2 - px(gtx, 8)
	a0, a1 := math.Pi*0.75, math.Pi*2.25
	n := o.ticks
	if n == 0 {
		n = 50
	}
	col := o.color
	if col.A == 0 {
		col = pal.Cyan
	}
	// 刻度:共享路径一次描边
	segs := make([][2]f32.Point, 0, n+1)
	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		long := i%(n/5) == 0
		l := px(gtx, 4)
		if long {
			l = px(gtx, 9)
		}
		c, s := dir(a)
		rOut := float32(R)
		segs = append(segs, [2]f32.Point{
			{X: cx + c*(rOut-l), Y: cy + s*(rOut-l)},
			{X: cx + c*rOut, Y: cy + s*rOut},
		})
	}
	strokeSegs(gtx, segs, 1, color.NRGBA{R: 0x33, G: 0x3C, B: 0x45, A: 0xFF})
	// 弧上标签
	if len(o.labels) > 0 {
		for i, t := range o.labels {
			a := a0 + (a1-a0)*float64(i)/float64(len(o.labels)-1)
			c, s := dir(a)
			td, call := textDims(gtx, t, 12, color.NRGBA{R: 0x8A, G: 0x94, B: 0x9E, A: 0xFF})
			x := int(cx + c*(float32(R)-px(gtx, 22))) - td.Size.X/2
			y := int(cy + s*(float32(R)-px(gtx, 22))) - td.Size.Y/2
			replayText(gtx, call, x, y)
		}
	}
	// 进度弧:三层描边模拟辉光
	p := clamp01(val / maxV)
	sweep := (a1 - a0) * float64(p)
	if p > 0 {
		arcPts := make([]f32.Point, 0, 48)
		const segsN = 46
		for i := 0; i <= segsN; i++ {
			a := a0 + sweep*float64(i)/segsN
			c, s := dir(a)
			arcPts = append(arcPts, f32.Pt(cx+c*(float32(R)-px(gtx, 13)), cy+s*(float32(R)-px(gtx, 13))))
		}
		polyline(gtx, arcPts, px(gtx, 9), alpha(col, 0x2E), false)
		polyline(gtx, arcPts, px(gtx, 5), alpha(col, 0x70), false)
		polyline(gtx, arcPts, px(gtx, 3), col, false)
	}
	// 指针
	an := a0 + sweep
	ac, as := dir(an)
	polyline(gtx, []f32.Point{
		{X: cx - ac*px(gtx, 12), Y: cy - as*px(gtx, 12)},
		{X: cx + ac*(float32(R)-px(gtx, 18)), Y: cy + as*(float32(R)-px(gtx, 18))},
	}, px(gtx, 4), alpha(pal.Cyan, 0x70), false)
	polyline(gtx, []f32.Point{
		{X: cx - ac*px(gtx, 12), Y: cy - as*px(gtx, 12)},
		{X: cx + ac*(float32(R)-px(gtx, 18)), Y: cy + as*(float32(R)-px(gtx, 18))},
	}, px(gtx, 2), pal.Txt, false)
	// 中心毂
	fillCircle(gtx, cx, cy, px(gtx, 7)+1.5, alpha(pal.Cyan, 0x90))
	fillCircle(gtx, cx, cy, px(gtx, 7), color.NRGBA{R: 0x05, G: 0x07, B: 0x08, A: 0xFF})
	// 弧下中心文本
	if o.center != "" {
		td, call := textDims(gtx, o.center, 14, color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF})
		replayText(gtx, call, int(cx)-td.Size.X/2, int(cy+float32(R)*0.62)-td.Size.Y/2)
	}
	return D{Size: image.Pt(w, h)}
}

// dialSized 居中放一个固定尺寸的表盘。
func dialSized(gtx C, wDp, hDp int, val, maxV float32, o dialOpt) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(unit.Dp(wDp)), gtx.Dp(unit.Dp(hDp))))
		return drawDial(gtx, val, maxV, o)
	})
}

// ---------- 功耗折线图 ----------

// drawWattChart 60 点历史功耗曲线(网格 + 发光折线)。
func drawWattChart(gtx C, data []float64) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(170)
	for i := 1; i < 4; i++ {
		y := h * i / 4
		fillArea(gtx, image.Rect(0, y, w, y+1), color.NRGBA{R: 0x15, G: 0x1A, B: 0x1E, A: 0xFF})
	}
	if len(data) < 2 {
		return D{Size: image.Pt(w, h)}
	}
	maxV := 30.0
	for _, v := range data {
		if v > maxV {
			maxV = v
		}
	}
	maxV *= 1.15
	W, H := float32(w), float32(h)
	pts := make([]f32.Point, len(data))
	for i, v := range data {
		x := float32(i) / 59 * W
		y := H - float32(v/maxV)*H
		pts[i] = f32.Pt(x, y)
	}
	polyline(gtx, pts, px(gtx, 6), alpha(pal.Cyan, 0x30), false)
	polyline(gtx, pts, px(gtx, 2), pal.Cyan, false)
	return D{Size: image.Pt(w, h)}
}

// ---------- 色环 ----------

// colorWheel 色环:360 个 1° 环形扇区填充 HSL 纯色环 + 外圈柔光 + 中心文本。
// 返回中心与半径,供调用方把点击坐标换算成色相角。
func colorWheel(gtx C, sizeDp int, zoneName, hex string, selAngle *float64) D {
	s := gtx.Dp(unit.Dp(sizeDp))
	cx, cy := float32(s)/2, float32(s)/2
	outer := float32(s)/2
	inner := outer * 0.57
	// 柔光晕:外圈 6 段宽弧,低透明度(替代 CSS blur)
	haloCols := []color.NRGBA{pal.Red, pal.Yellow, pal.Lime, pal.Cyan, pal.Blue, color.NRGBA{R: 0xFF, G: 0x00, B: 0xFF, A: 0xFF}}
	for i, hc := range haloCols {
		a0 := float64(i) * math.Pi / 3
		a1 := a0 + math.Pi/3
		pts := arcPts(cx, cy, outer*1.18, a0, a1, 10)
		polyline(gtx, pts, px(gtx, 10), alpha(hc, 0x28), false)
	}
	// 纯色环
	const step = 1.0
	for deg := 0; deg < 360; deg += int(step) {
		r, gg, b := hsl2rgb(float64(deg)/360, 1, 0.5)
		col := color.NRGBA{R: uint8(r), G: uint8(gg), B: uint8(b), A: 0xFF}
		a0 := float64(deg) * math.Pi / 180
		a1 := a0 + step*math.Pi/180
		fillPoly(gtx, []f32.Point{
			{X: cx + inner*float32(math.Cos(a0)), Y: cy + inner*float32(math.Sin(a0))},
			{X: cx + outer*float32(math.Cos(a0)), Y: cy + outer*float32(math.Sin(a0))},
			{X: cx + outer*float32(math.Cos(a1)), Y: cy + outer*float32(math.Sin(a1))},
			{X: cx + inner*float32(math.Cos(a1)), Y: cy + inner*float32(math.Sin(a1))},
		}, col)
	}
	// 选中角标记
	if selAngle != nil {
		c, s2 := dir(*selAngle)
		fillCircle(gtx, cx+c*(inner+outer)/2, cy+s2*(inner+outer)/2, px(gtx, 5), pal.Txt)
	}
	// 中心区名 + hex
	td1, c1 := textDims(gtx, zoneName, 14, color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF})
	replayText(gtx, c1, int(cx)-td1.Size.X/2, int(cy)-td1.Size.Y-gtx.Dp(2))
	td2, c2 := textDims(gtx, hex, 13, pal.Dim)
	replayText(gtx, c2, int(cx)-td2.Size.X/2, int(cy)+gtx.Dp(4))
	return D{Size: image.Pt(s, s)}
}

// arcPts 圆弧折线点。
func arcPts(cx, cy, r float32, a0, a1 float64, n int) []f32.Point {
	pts := make([]f32.Point, 0, n+1)
	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		pts = append(pts, f32.Pt(cx+float32(r)*float32(math.Cos(a)), cy+float32(r)*float32(math.Sin(a))))
	}
	return pts
}

// wheelAngle 点击坐标 → 色相角(0..2π,顶部为 0,顺时针)。
// 返回 false 表示点击不在环带上。
func wheelAngle(s int, x, y int) (float64, bool) {
	cx, cy := float64(s)/2, float64(s)/2
	dx, dy := float64(x)-cx, float64(y)-cy
	r := math.Hypot(dx, dy)
	outer := float64(s) / 2
	inner := outer * 0.57
	if r < inner || r > outer {
		return 0, false
	}
	ang := math.Atan2(dy, dx) + math.Pi/2
	for ang < 0 {
		ang += 2 * math.Pi
	}
	return ang, true
}

// ---------- 键盘键帽图 ----------

type kbKey struct {
	label string
	w     float32 // 相对宽度(1 = 标准键)
}

var kbRows = [][]kbKey{
	{{"Esc", 1.3}, {"F1", 1}, {"F2", 1}, {"F3", 1}, {"F4", 1}, {"F5", 1}, {"F6", 1}, {"F7", 1}, {"F8", 1}, {"F9", 1}, {"F10", 1}, {"F11", 1}, {"F12", 1}, {"PrtSc", 1}, {"Ins", 1}, {"Del", 1}},
	{{"~", 1}, {"1", 1}, {"2", 1}, {"3", 1}, {"4", 1}, {"5", 1}, {"6", 1}, {"7", 1}, {"8", 1}, {"9", 1}, {"0", 1}, {"-", 1}, {"=", 1}, {"", 1.8}, {"NumLK", 1.2}, {"/", 0.8}, {"*", 0.8}},
	{{"Tab", 1.5}, {"Q", 1}, {"W", 1}, {"E", 1}, {"R", 1}, {"T", 1}, {"Y", 1}, {"U", 1}, {"I", 1}, {"O", 1}, {"P", 1}, {"[", 1}, {"]", 1}, {"\\", 1}, {"7", 0.8}, {"8", 0.8}, {"9", 0.8}},
	{{"Caps", 1.8}, {"A", 1}, {"S", 1}, {"D", 1}, {"F", 1}, {"G", 1}, {"H", 1}, {"J", 1}, {"K", 1}, {"L", 1}, {";", 1}, {"'", 1}, {"↵", 1.9}, {"4", 0.8}, {"5", 0.8}, {"6", 0.8}},
	{{"Shift", 2.3}, {"Z", 1}, {"X", 1}, {"C", 1}, {"V", 1}, {"B", 1}, {"N", 1}, {"M", 1}, {",", 1}, {".", 1}, {"/", 1}, {"Shift", 1.9}, {"1", 0.8}, {"2", 0.8}, {"3", 0.8}},
	{{"Ctrl", 1.3}, {"Fn", 1.1}, {"Win", 1.2}, {"Alt", 1.2}, {"", 4.6}, {"Alt", 1.2}, {"Menu", 1.2}, {"Ctrl", 1.3}, {"←", 0.9}, {"↓", 0.9}, {"→", 0.9}},
}

// kbLayout 键帽图;zoneColors 为 nil 时不发光,否则按 Web 规则给第 2~5 行分区上色。
// zoneColors: [left, mid, right]。
func kbLayout(gtx C, zoneColors *[3]color.NRGBA) D {
	gap := gtx.Dp(5)
	keyH := gtx.Dp(36)
	totalW := gtx.Constraints.Max.X
	y := 0
	for ri, r := range kbRows {
		sum := 0.0
		for _, k := range r {
			sum += float64(k.w)
		}
		x := 0
		for ci, k := range r {
			kw := int(float64(totalW-(len(r)-1)*gap) * float64(k.w) / sum)
			col := color.NRGBA{}
			if zoneColors != nil && ri >= 1 && ri <= 4 {
				switch {
				case ci <= 5:
					col = zoneColors[0]
				case ci <= 10:
					col = zoneColors[1]
				default:
					col = zoneColors[2]
				}
			}
			drawKeycap(gtx, image.Rect(x, y, x+kw, y+keyH), k.label, col)
			x += kw + gap
		}
		y += keyH + gap
	}
	return D{Size: image.Pt(totalW, y-gap)}
}

// drawKeycap 单个键帽。
func drawKeycap(gtx C, r image.Rectangle, lbl string, zone color.NRGBA) {
	fillArea(gtx, r, pal.KeyBG2)
	if zone.A > 0 {
		// 分区辉光:底部内发光 + 顶部淡填充 + 彩色描边
		fillArea(gtx, image.Rect(r.Min.X, r.Max.Y-gtx.Dp(8), r.Max.X, r.Max.Y), alpha(zone, 0x30))
		fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+gtx.Dp(4)), alpha(zone, 0x18))
		border(gtx, r, alpha(zone, 0x77), 1)
	} else {
		fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+(r.Dy()*2/5)), pal.KeyBG)
		border(gtx, r, color.NRGBA{R: 0x23, G: 0x28, B: 0x2D, A: 0xFF}, 1)
	}
	if lbl == "" {
		return
	}
	td, call := textDims(gtx, lbl, 11, color.NRGBA{R: 0xB9, G: 0xC2, B: 0xCA, A: 0xFF})
	replayText(gtx, call, (r.Min.X+r.Max.X-td.Size.X)/2, (r.Min.Y+r.Max.Y-td.Size.Y)/2)
}

// ---------- 电池轮廓 ----------

// batteryOutline 电池轮廓:边框 + 极耳 + 点阵填充 + 居中大号百分比。
func batteryOutline(gtx C, pct int, hasBat bool) D {
	w := gtx.Dp(270)
	h := gtx.Dp(118)
	// 极耳
	fillArea(gtx, image.Rect(w, gtx.Dp(36), w+gtx.Dp(10), gtx.Dp(36)+gtx.Dp(42)), color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF})
	// 外框
	bd := color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF}
	border(gtx, image.Rect(0, 0, w, h), bd, 2)
	// 点阵填充
	if hasBat {
		inset := gtx.Dp(5)
		fillW := float32(w-inset*2) * clamp01(float32(pct)/100)
		fillH := float32(h - inset*2)
		for yy := 3; float32(yy) < fillH; yy += 7 {
			for xx := 3; float32(xx) < fillW; xx += 7 {
				fillCircle(gtx, float32(inset+xx), float32(inset+yy), px(gtx, 1.4), alpha(color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF}, 0xC0))
			}
		}
	}
	// 百分比
	txt := "—"
	if hasBat {
		txt = itoa(pct) + " %"
	}
	td, call := textDims(gtx, txt, 50, pal.Txt)
	replayText(gtx, call, (w-td.Size.X)/2, (h-td.Size.Y)/2)
	return D{Size: image.Pt(w+gtx.Dp(10), h)}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ---------- FN+Backspace 图形 ----------

// fnBsChip "FN + Backspace ⌫" 提示块。
func fnBsChip(gtx C) D {
	td1, c1 := textDims(gtx, "FN", 18, pal.Txt)
	td2, c2 := textDims(gtx, "+", 14, pal.Dim)
	td3, c3 := textDims(gtx, "Backspace", 18, pal.Txt)
	w := td1.Size.X + td2.Size.X + td3.Size.X + gtx.Dp(36)
	h := td1.Size.Y + gtx.Dp(20)
	border(gtx, image.Rect(0, 0, w, h), color.NRGBA{R: 0x2C, G: 0x34, B: 0x3C, A: 0xFF}, 1)
	x := gtx.Dp(18)
	replayText(gtx, c1, x, (h-td1.Size.Y)/2)
	x += td1.Size.X + gtx.Dp(10)
	replayText(gtx, c2, x, (h-td2.Size.Y)/2)
	x += td2.Size.X + gtx.Dp(10)
	replayText(gtx, c3, x, (h-td3.Size.Y)/2)
	return D{Size: image.Pt(w, h)}
}

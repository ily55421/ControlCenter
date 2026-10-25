// draw — 自绘仪表:表盘(刻度环/进度弧/指针/辉光)、功耗折线图、色环、
// 键盘键帽图、电池轮廓。全部矢量绘制,D3D11 硬件渲染,无位图缩放损耗。
package gui

import (
	"fmt"
	"image"
	"image/color"
	draw "image/draw"
	"math"
	"os"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"sync"
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
	ticks  int // 刻度数(默认 50)
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
			x := int(cx+c*(float32(R)-px(gtx, 22))) - td.Size.X/2
			y := int(cy+s*(float32(R)-px(gtx, 22))) - td.Size.Y/2
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
	outer := float32(s) / 2
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

// KBGrid 键盘点选状态(单一热区 + 坐标换算;LED 页用)。
// 92 个子 Clickable 在 panel 宏录制 + List 视口裁剪的组合下绘制/命中行为
// 不可控(实测只剩首键),改为整块键盘一个热区,点击坐标反查键位。
type KBGrid struct {
	Tag        event.Tag
	SelR, SelC int         // 选中键(-1 无)
	sz         image.Point // 最近一帧键盘尺寸(点击换算用)
}

// kbLayout 键盘可视化:渲染为离屏 image 再贴图(paint.ImageOp)。
// 弃用矢量 op 流的原因:92 键的绘制在 panel 宏 + List 视口组合下出现
// 系统性偏移/截断(多轮插桩无法根治),image 贴图 100% 确定性。
// 键帽文字用 basicfont(拉丁,键帽标签全为拉丁/符号)。
// 缓存键:宽度+选中键+分区色 —— 变化才重渲染,静止零开销。
func kbLayout(gtx C, zoneColors *[3]color.NRGBA, grid *KBGrid, onTap func(r, c int)) D {
	totalW := gtx.Constraints.Max.X
	// 交互:单一热区,点击坐标反查键位
	defer func() {
		if grid == nil {
			return
		}
	}()
	h := len(kbRows)*(36+5) - 5

	// 缓存判定
	selKey := -1
	if grid != nil && grid.SelR >= 0 && grid.SelC < len(kbRows[grid.SelR]) {
		selKey = grid.SelR*100 + grid.SelC
	}
	zoneKey := 0
	if zoneColors != nil {
		zoneKey = int(zoneColors[0].R)<<16 | int(zoneColors[0].G)<<8 | int(zoneColors[0].B)
	}
	cacheKey := fmt.Sprintf("%d|%d|%d", totalW, selKey, zoneKey)
	kbImgMu.Lock()
	if kbImg.key != cacheKey || kbImg.img == nil {
		kbImg.img = renderKeyboardImage(totalW, selKey, zoneColors)
		kbImg.key = cacheKey
	}
	imgOp := kbImg.op
	if kbImg.key == cacheKey && kbImg.op == (paint.ImageOp{}) {
		kbImg.op = paint.NewImageOp(kbImg.img)
		imgOp = kbImg.op
	}
	kbImgMu.Unlock()

	// 贴图
	defer clip.Rect(image.Rect(0, 0, totalW, h)).Push(gtx.Ops).Pop()
	imgOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	// 选中键描边(矢量,实时)
	if grid != nil && grid.SelR >= 0 {
		rect := kbKeyRect(totalW, grid.SelR, grid.SelC)
		border(gtx, rect.Inset(-1), pal.Cyan, 2)
	}
	// 点击热区 + 坐标反查
	if grid != nil {
		event.Op(gtx.Ops, grid.Tag)
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: grid.Tag, Kinds: pointer.Press})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok || pe.Kind != pointer.Press {
				continue
			}
			if r, c, hit := kbHitTest(totalW, int(pe.Position.X), int(pe.Position.Y)); hit {
				grid.SelR, grid.SelC = r, c
				if onTap != nil {
					onTap(r, c)
				}
				g.win.Invalidate()
			}
		}
	}
	return D{Size: image.Pt(totalW, h)}
}

// kbKeyRect 键位 → 矩形(与 renderKeyboardImage 同一几何)。
func kbKeyRect(totalW, ri, ci int) image.Rectangle {
	gap, keyH := 5, 36
	y := ri * (keyH + gap)
	sum := 0.0
	for _, k := range kbRows[ri] {
		sum += float64(k.w)
	}
	x := 0
	for i := 0; i < ci && i < len(kbRows[ri]); i++ {
		x += int(float64(totalW-(len(kbRows[ri])-1)*gap)*float64(kbRows[ri][i].w)/sum) + gap
	}
	kw := int(float64(totalW-(len(kbRows[ri])-1)*gap) * float64(kbRows[ri][ci].w) / sum)
	return image.Rect(x, y, x+kw, y+keyH)
}

// kbHitTest 屏幕坐标 → 键位。
func kbHitTest(totalW, px, py int) (r, c int, hit bool) {
	gap, keyH := 5, 36
	r = py / (keyH + gap)
	if r < 0 || r >= len(kbRows) || py > r*(keyH+gap)+keyH {
		return 0, 0, false
	}
	x := 0
	sum := 0.0
	for _, k := range kbRows[r] {
		sum += float64(k.w)
	}
	for ci, k := range kbRows[r] {
		kw := int(float64(totalW-(len(kbRows[r])-1)*gap) * float64(k.w) / sum)
		if px >= x && px < x+kw {
			return r, ci, true
		}
		x += kw + gap
	}
	return 0, 0, false
}

var (
	kbImgMu sync.Mutex
	kbImg   struct {
		key string
		img *image.RGBA
		op  paint.ImageOp
	}
)

// renderKeyboardImage 离屏渲染 92 键(basicfont 拉丁标签)。
func renderKeyboardImage(totalW, selKey int, zoneColors *[3]color.NRGBA) *image.RGBA {
	gap, keyH := 5, 36
	h := len(kbRows)*(keyH+gap) - gap
	img := image.NewRGBA(image.Rect(0, 0, totalW, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.NRGBA{R: 0x0B, G: 0x0D, B: 0x0F, A: 0xFF}}, image.Point{}, draw.Src)

	for ri, r := range kbRows {
		sum := 0.0
		for _, k := range r {
			sum += float64(k.w)
		}
		x := 0
		for ci, k := range r {
			kw := int(float64(totalW-(len(r)-1)*gap) * float64(k.w) / sum)
			rect := image.Rect(x, ri*(keyH+gap), x+kw, ri*(keyH+gap)+keyH)
			zone := color.NRGBA{}
			if zoneColors != nil && ri >= 1 && ri <= 4 {
				switch {
				case ci <= 5:
					zone = zoneColors[0]
				case ci <= 10:
					zone = zoneColors[1]
				default:
					zone = zoneColors[2]
				}
			}
			// 键帽底(上亮下暗两段模拟渐变)
			draw.Draw(img, rect, &image.Uniform{color.NRGBA{R: 0x19, G: 0x1D, B: 0x21, A: 0xFF}}, image.Point{}, draw.Src)
			draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+rect.Dy()*2/5), &image.Uniform{color.NRGBA{R: 0x19, G: 0x1D, B: 0x21, A: 0xFF}}, image.Point{}, draw.Src)
			// 分区光晕
			if zone.R > 0 || zone.G > 0 || zone.B > 0 {
				draw.Draw(img, image.Rect(rect.Min.X, rect.Max.Y-8, rect.Max.X, rect.Max.Y), &image.Uniform{color.NRGBA{R: zone.R, G: zone.G, B: zone.B, A: 0x30}}, image.Point{}, draw.Src)
				draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+4), &image.Uniform{color.NRGBA{R: zone.R, G: zone.G, B: zone.B, A: 0x18}}, image.Point{}, draw.Src)
			}
			// 边框
			borderCol := color.NRGBA{R: 0x23, G: 0x28, B: 0x2D, A: 0xFF}
			if zone.R > 0 || zone.G > 0 || zone.B > 0 {
				borderCol = color.NRGBA{R: zone.R, G: zone.G, B: zone.B, A: 0x77}
			}
			if ri*100+ci == selKey {
				borderCol = color.NRGBA{R: 0x25, G: 0xC8, B: 0xFF, A: 0xFF}
			}
			draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1), &image.Uniform{borderCol}, image.Point{}, draw.Src)
			draw.Draw(img, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), &image.Uniform{borderCol}, image.Point{}, draw.Src)
			draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+1, rect.Max.Y), &image.Uniform{borderCol}, image.Point{}, draw.Src)
			draw.Draw(img, image.Rect(rect.Max.X-1, rect.Min.Y, rect.Max.X, rect.Max.Y), &image.Uniform{borderCol}, image.Point{}, draw.Src)
			// 标签
			if k.label != "" {
				w := len(k.label) * 7
				tx := rect.Min.X + (rect.Dx()-w)/2
				ty := rect.Min.Y + (rect.Dy()-13)/2
				fd := font.Drawer{Dst: img, Src: image.White, Face: basicfont.Face7x13}
				fd.Dot = fixed.P(tx, ty+11)
				fd.DrawString(k.label)
			}
			x += kw + gap
		}
	}
	return img
}

// KBPartOf 键位 → 背光分区(主键区三段,其余行整键盘)。
func KBPartOf(r, c int) string {
	if r >= 1 && r <= 4 {
		switch {
		case c <= 5:
			return "left"
		case c <= 10:
			return "mid"
		default:
			return "right"
		}
	}
	return "all"
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

// ---------- 多序列实时图表(CPU 监控:温度/利用率) ----------

// chartSeries 一条曲线(数据 0..max)。
type chartSeries struct {
	data  []float64
	max   float64
	col   color.NRGBA
	label string
}

// drawMultiChart 双序列实时曲线:横向网格 + 图例当前值,60 点滚动窗口。
// 超越原版的关键可视化:原版只有瞬时数字,无历史趋势。
func drawMultiChart(gtx C, hDp int, series []chartSeries) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(unit.Dp(hDp))
	// 网格:25/50/75%
	for i := 1; i < 4; i++ {
		y := h * i / 4
		fillArea(gtx, image.Rect(0, y, w, y+1), color.NRGBA{R: 0x15, G: 0x1A, B: 0x1E, A: 0xFF})
	}
	for _, s := range series {
		if len(s.data) < 2 {
			continue
		}
		pts := make([]f32.Point, len(s.data))
		for i, v := range s.data {
			x := float32(i) / 59 * float32(w)
			y := float32(h) - clamp01(float32(v/s.max))*float32(h)
			pts[i] = f32.Pt(x, y)
		}
		polyline(gtx, pts, px(gtx, 5), alpha(s.col, 0x28), false)
		polyline(gtx, pts, px(gtx, 1.6), s.col, false)
	}
	// 图例(左上角):色块 + 标签 + 当前值
	x := gtx.Dp(6)
	for _, s := range series {
		cur := s.label + " —"
		if len(s.data) > 0 {
			cur = fmt.Sprintf("%s %.0f", s.label, s.data[len(s.data)-1])
		}
		td, call := textDims(gtx, cur, 11, s.col)
		fillArea(gtx, image.Rect(x, gtx.Dp(7), x+gtx.Dp(8), gtx.Dp(7)+gtx.Dp(3)), s.col)
		replayText(gtx, call, x+gtx.Dp(12), gtx.Dp(5))
		x += td.Size.X + gtx.Dp(26)
	}
	return D{Size: image.Pt(w, h)}
}

// ---------- 风扇曲线编辑器(可视化拖拽,超越原版的数字框) ----------

// curvePad 编辑器内边距(dp)。
const curvePad = 10

// curveXY 温度/占空(35..100℃ × 0..100%)→ 画布坐标。
func curveXY(gtx C, w, h int, t, d int) f32.Point {
	p := gtx.Dp(unit.Dp(curvePad))
	x := float32(p) + float32(t-35)/65*float32(w-2*p)
	y := float32(h-p) - float32(d)/100*float32(h-2*p)
	return f32.Pt(x, y)
}

// curveTD 画布坐标 → 温度/占空(四舍五入并夹紧到合法域)。
func curveTD(gtx C, w, h int, x, y float32) (t, d int) {
	p := float32(gtx.Dp(curvePad))
	t = 35 + int((x-p)/float32(w-2*gtx.Dp(curvePad))*65+0.5)
	d = int((float32(h)-p-y)/float32(h-2*gtx.Dp(curvePad))*100 + 0.5)
	t = clampI(t, 35, 100)
	d = clampI(d, 0, 100)
	return
}

// drawFanCurve 单风扇三点曲线编辑区(只画;拖拽交互在 page_system 处理)。
// pts 为三点 (T,D);dragIdx 当前拖拽点(-1 无)。
func drawFanCurve(gtx C, w, h int, pts [3][2]int, dragIdx, curTemp int) D {
	// 背景网格:温度 50/65/80/95 竖线,占空 25/50/75 横线
	gridCol := color.NRGBA{R: 0x15, G: 0x1A, B: 0x1E, A: 0xFF}
	pad := gtx.Dp(curvePad)
	for t := 50; t <= 95; t += 15 {
		x := int(curveXY(gtx, w, h, t, 0).X)
		fillArea(gtx, image.Rect(x, pad, x+1, h-pad), gridCol)
	}
	for d := 25; d <= 75; d += 25 {
		y := int(curveXY(gtx, w, h, 35, d).Y)
		fillArea(gtx, image.Rect(pad, y, w-pad, y+1), gridCol)
	}
	// 曲线:左沿 → 三点 → (100,100)
	pp := make([]f32.Point, 0, 5)
	pp = append(pp, f32.Pt(float32(pad), curveXY(gtx, w, h, pts[0][0], pts[0][1]).Y))
	for _, p := range pts {
		pp = append(pp, curveXY(gtx, w, h, p[0], p[1]))
	}
	pp = append(pp, f32.Pt(float32(w-pad), float32(h-pad)))
	polyline(gtx, pp, px(gtx, 4), alpha(pal.Cyan, 0x30), false)
	polyline(gtx, pp, px(gtx, 1.5), pal.Cyan, false)
	// 当前温度叠加:温度竖线(黄)+ 曲线上的工作点(风扇调参所见即所得)
	if curTemp > 0 {
		ct := clampI(curTemp, 35, 100)
		// 按三点曲线规则求当前占空:分段线性,两端夹紧
		d := pts[0][1]
		switch {
		case ct >= pts[2][0]:
			d = pts[2][1]
		case ct >= pts[1][0]:
			d = interp(ct, pts[1][0], pts[1][1], pts[2][0], pts[2][1])
		case ct >= pts[0][0]:
			d = interp(ct, pts[0][0], pts[0][1], pts[1][0], pts[1][1])
		}
		cx := int(curveXY(gtx, w, h, ct, d).X)
		fillArea(gtx, image.Rect(cx, pad, cx+1, h-pad), alpha(pal.Yellow, 0x45))
		wp := curveXY(gtx, w, h, ct, d)
		fillCircle(gtx, wp.X, wp.Y, px(gtx, 6), alpha(pal.Yellow, 0x35))
		fillCircle(gtx, wp.X, wp.Y, px(gtx, 3), pal.Yellow)
	}
	// 拖拽点:辉光圆 + 白芯;拖拽中放大
	for i, p := range pts {
		c := curveXY(gtx, w, h, p[0], p[1])
		r := px(gtx, 4.5)
		if i == dragIdx {
			r = px(gtx, 6)
		}
		fillCircle(gtx, c.X, c.Y, r+px(gtx, 3), alpha(pal.Cyan, 0x35))
		fillCircle(gtx, c.X, c.Y, r, pal.Cyan)
		fillCircle(gtx, c.X, c.Y, r*0.45, color.NRGBA{R: 0x00, G: 0x16, B: 0x1C, A: 0xFF})
		// 点位标签(温度,占空),贴点下方
		lbl := fmt.Sprintf("%d,%d%%", p[0], p[1])
		td, call := textDims(gtx, lbl, 10, pal.Dim)
		lx := clampI(int(c.X)-td.Size.X/2, 2, w-td.Size.X-2)
		ly := clampI(int(c.Y)+gtx.Dp(6), 0, h-td.Size.Y)
		replayText(gtx, call, lx, ly)
	}
	return D{Size: image.Pt(w, h)}
}

// interp 分段线性插值:(x 在 x0..x1)→ y0..y1。
func interp(x, x0, y0, x1, y1 int) int {
	if x1 <= x0 {
		return y0
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

func dbgLog(v ...any) {
	f, err := os.OpenFile(`D:\code\ControlCenter\bin\dbg.txt`, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintln(f, v...)
	f.Close()
}

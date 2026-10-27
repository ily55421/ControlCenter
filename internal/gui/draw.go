// draw — 自绘仪表:表盘(刻度环/进度弧/指针/辉光)、功耗折线图、色环、
// 键盘键帽图、电池轮廓。全部矢量绘制,D3D11 硬件渲染,无位图缩放损耗。
package gui

import (
	"fmt"
	"image"
	"image/color"
	draw "image/draw"
	"math"
	"sync"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
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
		st := textStyle{Size: fsBody2, Col: colText3}
		for i, t := range o.labels {
			a := a0 + (a1-a0)*float64(i)/float64(len(o.labels)-1)
			c, s := dir(a)
			centerTextAt(gtx, cx+c*(float32(R)-px(gtx, 22)), cy+s*(float32(R)-px(gtx, 22)), t, st)
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
		centerTextAt(gtx, cx, cy+float32(R)*0.62, o.center, textStyle{Size: fsBig, Col: colText})
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
	stZone := textStyle{Size: fsBig, Col: colText}
	d1 := measureText(gtx, zoneName, stZone)
	drawTextAt(gtx, int(cx)-d1.Size.X/2, int(cy)-d1.Size.Y-gtx.Dp(2), zoneName, stZone)
	stHex := textStyle{Size: fsBody, Col: colText3}
	d2 := measureText(gtx, hex, stHex)
	drawTextAt(gtx, int(cx)-d2.Size.X/2, int(cy)+gtx.Dp(4), hex, stHex)
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

// wheelAngle 与绘制共用右侧为 0、顺时针的色相角。
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
	ang := math.Atan2(dy, dx)
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

// KBGrid 键盘点选状态(单一热区 + 坐标换算;LED 页与 FlexiKey 槽位共用)。
// 92 个子 Clickable 在 panel 宏录制 + List 视口裁剪的组合下绘制/命中行为
// 不可控(实测只剩首键),改为整块键盘一个热区,点击坐标反查键位。
type KBGrid struct {
	Tag        event.Tag
	ScrollTag  event.Tag   // 滚轮热区(与点击热区分开,避免滚轮事件被当作按下)
	SelR, SelC int         // 选中键(-1 无)
	off        float32     // 水平滚动偏移(px;键盘比卡片宽时)
	sz         image.Point // 最近一帧键盘尺寸(点击换算用)
}

// kbGeom 键盘图的几何(设备像素),全部由 Dp 令牌折算。
// 原先 kbLayout / kbKeyRect / kbHitTest / renderKeyboardImage 各自裸写 36/5
// 设备像素:高 DPI 下整图不放大、键帽标签相对更小,视觉与命中会分叉(P2)。
type kbGeom struct {
	w, h  int // 渲染宽(≥可见宽)与键盘本体高
	viewW int // 可见宽(卡片内可用宽度)
	keyH  int
	gap   int
}

// kbGeomFor 按可用宽度算几何:不足 kbMinW 时保持最小可读宽度,由调用方横向滚动。
func kbGeomFor(gtx C, availW int) kbGeom {
	keyH, gap := gtx.Dp(kbKeyH), gtx.Dp(kbKeyGap)
	minW := gtx.Dp(kbMinW)
	viewW := availW
	// 约束未定(Max.X 无限大)或异常值时退回最小宽度,避免分配超大离屏位图
	if viewW <= 0 || viewW > minW*8 {
		viewW = minW
	}
	w := viewW
	if w < minW {
		w = minW
	}
	h := len(kbRows)*(keyH+gap) - gap
	return kbGeom{w: w, h: h, viewW: viewW, keyH: keyH, gap: gap}
}

// maxOff 水平滚动最大偏移。
func (k kbGeom) maxOff() int { return k.w - k.viewW }

// kbZoneKey 贴图缓存键里的分区色摘要:三段颜色都要进 key。
// 原先只用 zoneColors[0],只改中/右分区时贴图不刷新(颜色不跟随选择)。
func kbZoneKey(c *[3]color.NRGBA) string {
	if c == nil {
		return "-"
	}
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x",
		c[0].R, c[0].G, c[0].B, c[1].R, c[1].G, c[1].B, c[2].R, c[2].G, c[2].B)
}

// kbLayout 键盘可视化:渲染为离屏 image 再贴图(paint.ImageOp)。
// 弃用矢量 op 流的原因:92 键的绘制在 panel 宏 + List 视口组合下出现
// 系统性偏移/截断(多轮插桩无法根治),image 贴图 100% 确定性。
// 键帽文字用 basicfont(拉丁,键帽标签全为拉丁/符号)。
// 缓存键:渲染宽 + DPI 派生的键高/间距 + 选中键 + 三段分区色 —— 变化才重渲染,
// 静止零开销。可用宽度不足 kbMinW 时按最小宽度渲染,并在卡片内局部横向滚动
// (滚轮),底部给出可见滚动条;点击命中会加上滚动偏移,与显示位置一致。
func kbLayout(gtx C, zoneColors *[3]color.NRGBA, grid *KBGrid, onTap func(r, c int)) D {
	gm := kbGeomFor(gtx, gtx.Constraints.Max.X)
	if grid != nil && grid.ScrollTag == nil {
		grid.ScrollTag = &grid.ScrollTag // 必须是独立 tag,不能与点击共用
	}
	selKey := -1
	if grid != nil && grid.SelR >= 0 && grid.SelC >= 0 && grid.SelC < len(kbRows[grid.SelR]) {
		selKey = grid.SelR*100 + grid.SelC
	}

	// 缓存判定
	cacheKey := fmt.Sprintf("%d|%d|%d|%d|%s", gm.w, gm.keyH, gm.gap, selKey, kbZoneKey(zoneColors))
	kbImgMu.Lock()
	if kbImg.key != cacheKey || kbImg.img == nil {
		kbImg.img = renderKeyboardImage(gm, selKey, zoneColors)
		kbImg.key = cacheKey
		kbImg.op = paint.NewImageOp(kbImg.img)
	}
	imgOp := kbImg.op
	kbImgMu.Unlock()

	// 滚动偏移与滚动条占位
	offPx := 0
	barH := 0
	if grid != nil && gm.maxOff() > 0 {
		offPx = int(clamp(grid.off, 0, float32(gm.maxOff())))
		barH = gtx.Dp(6)
	}

	// 绘制:可见区裁剪 + 水平偏移
	area := clip.Rect(image.Rect(0, 0, gm.viewW, gm.h)).Push(gtx.Ops)
	offStack := op.Offset(image.Pt(-offPx, 0)).Push(gtx.Ops)
	imgOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	// 选中键描边(矢量,实时)
	if grid != nil && selKey >= 0 {
		border(gtx, kbKeyRect(gm, grid.SelR, grid.SelC).Inset(-1), pal.Cyan, 2)
	}
	offStack.Pop()
	area.Pop()

	// 交互:热区声明在未偏移坐标系,命中时把坐标换算回渲染坐标
	if grid != nil {
		hitArea := clip.Rect(image.Rect(0, 0, gm.viewW, gm.h)).Push(gtx.Ops)
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
			if r, c, hit := kbHitTest(gm, int(pe.Position.X)+offPx, int(pe.Position.Y)); hit {
				grid.SelR, grid.SelC = r, c
				if onTap != nil {
					onTap(r, c)
				}
				g.win.Invalidate()
			}
		}
		// 只消费横向滚动,普通滚轮留给页面。
		grid.off = clamp(grid.off, 0, float32(gm.maxOff()))
		event.Op(gtx.Ops, grid.ScrollTag)
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: grid.ScrollTag, Kinds: pointer.Scroll,
				ScrollX: scrollRange(grid.off, float32(gm.maxOff()))})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok || pe.Kind != pointer.Scroll {
				continue
			}
			if pe.Scroll.X != 0 && gm.maxOff() > 0 {
				grid.off = clamp(grid.off+pe.Scroll.X, 0, float32(gm.maxOff()))
				g.win.Invalidate()
			}
		}
		hitArea.Pop()
	}

	// 局部横向滚动指示:内容比卡片宽时给出可见提示
	if barH > 0 {
		y := gm.h
		fillArea(gtx, image.Rect(0, y, gm.viewW, y+barH), alpha(pal.Line, 0x33))
		thumbW := int(float32(gm.viewW) * float32(gm.viewW) / float32(gm.w))
		if minW := gtx.Dp(28); thumbW < minW {
			thumbW = minW
		}
		x0 := int(float32(gm.viewW-thumbW) * clamp01(float32(offPx)/float32(gm.maxOff())))
		fillArea(gtx, image.Rect(x0, y, x0+thumbW, y+barH), alpha(pal.Cyan, 0xB0))
	}
	return D{Size: image.Pt(gm.viewW, gm.h+barH)}
}

// kbRowEdge 行内第 i 个键的右边界(像素)。
// 用累计边界取整,整行正好铺满渲染宽度:逐键截断会让每行累计出几像素空隙,
// 17 列 × 150% 缩放下右侧能空出 20+px(实测),看起来就是"右侧被裁/不齐"。
func kbRowEdge(gm kbGeom, ri, i int) int {
	row := kbRows[ri]
	if i < 0 {
		return 0
	}
	if i >= len(row)-1 {
		return gm.w
	}
	sum := 0.0
	for _, k := range row {
		sum += float64(k.w)
	}
	avail := float64(gm.w - (len(row)-1)*gm.gap)
	acc := 0.0
	for j := 0; j <= i; j++ {
		acc += float64(row[j].w)
	}
	return int(math.Round(avail*acc/sum)) + i*gm.gap
}

// kbKeyRect 键位 → 矩形(绘制与命中共用同一几何)。
func kbKeyRect(gm kbGeom, ri, ci int) image.Rectangle {
	y := ri * (gm.keyH + gm.gap)
	return image.Rect(kbRowEdge(gm, ri, ci-1), y, kbRowEdge(gm, ri, ci), y+gm.keyH)
}

// kbHitTest 渲染坐标 → 键位(调用方负责把可见坐标加上滚动偏移)。
func kbHitTest(gm kbGeom, px, py int) (r, c int, hit bool) {
	r = py / (gm.keyH + gm.gap)
	if r < 0 || r >= len(kbRows) || py > r*(gm.keyH+gm.gap)+gm.keyH {
		return 0, 0, false
	}
	for ci := range kbRows[r] {
		if x0, x1 := kbRowEdge(gm, r, ci-1), kbRowEdge(gm, r, ci); px >= x0 && px < x1 {
			return r, ci, true
		}
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
func renderKeyboardImage(gm kbGeom, selKey int, zoneColors *[3]color.NRGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, gm.w, gm.h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.NRGBA{R: 0x0B, G: 0x0D, B: 0x0F, A: 0xFF}}, image.Point{}, draw.Src)

	for ri, r := range kbRows {
		for ci, k := range r {
			// 与 kbKeyRect 同一几何(累计边界),绘制/描边/命中不会互相偏离
			rect := kbKeyRect(gm, ri, ci)
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
			label := k.label
			switch label {
			case "↵":
				label = "Enter"
			case "←":
				label = "<"
			case "↓":
				label = "v"
			case "→":
				label = ">"
			}
			if label != "" {
				w := utf8.RuneCountInString(label) * 7
				tx := rect.Min.X + (rect.Dx()-w)/2
				ty := rect.Min.Y + (rect.Dy()-13)/2
				fd := font.Drawer{Dst: img, Src: image.White, Face: basicfont.Face7x13}
				fd.Dot = fixed.P(tx, ty+11)
				fd.DrawString(label)
			}
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
	centerTextAt(gtx, float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2, lbl,
		textStyle{Size: fsSmall, Col: colText2})
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
	pctStr := "—"
	if hasBat {
		pctStr = itoa(pct) + " %"
	}
	centerTextAt(gtx, float32(w)/2, float32(h)/2, pctStr, textStyle{Size: fsHero, Col: colText})
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

// fnBsChip "FN + Backspace" 提示块:三段文本用 Flex 排,边框作为 Stack 的底铺满。
func fnBsChip(gtx C) D {
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return txt(gtx, "FN", textStyle{Size: fsTitle, Col: colText})
					}),
					layout.Rigid(hSpacer(10)),
					layout.Rigid(func(gtx C) D {
						return txt(gtx, "+", textStyle{Size: fsBig, Col: colText3})
					}),
					layout.Rigid(hSpacer(10)),
					layout.Rigid(func(gtx C) D {
						return txt(gtx, "Backspace", textStyle{Size: fsTitle, Col: colText})
					}),
				)
			})
		}),
		layout.Expanded(func(gtx C) D {
			r := image.Rectangle{Max: gtx.Constraints.Min}
			border(gtx, r, colLineHi, 1)
			return D{Size: r.Max}
		}),
	)
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
		st := textStyle{Size: fsSmall, Col: s.col}
		td := measureText(gtx, cur, st)
		fillArea(gtx, image.Rect(x, gtx.Dp(7), x+gtx.Dp(8), gtx.Dp(7)+gtx.Dp(3)), s.col)
		drawTextAt(gtx, x+gtx.Dp(12), gtx.Dp(5), cur, st)
		x += td.Size.X + gtx.Dp(26)
	}
	return D{Size: image.Pt(w, h)}
}

// ---------- 风扇曲线编辑器(可视化拖拽,超越原版的数字框) ----------

// curvePad 右侧留白(dp);其余边距额外预留坐标刻度和轴标题。
const curvePad = 14

func curveBounds(gtx C, w, h int) image.Rectangle {
	left := min(gtx.Dp(44), maxI(w-2, 0))
	top := min(gtx.Dp(24), maxI(h-2, 0))
	return image.Rect(left, top, maxI(left+1, w-gtx.Dp(curvePad)), maxI(top+1, h-gtx.Dp(40)))
}

// curveXY 真实温度/占空(0..100℃ × 0..100%)→ 画布坐标。
func curveXY(gtx C, w, h int, t, d int) f32.Point {
	r := curveBounds(gtx, w, h)
	x := float32(r.Min.X) + float32(t)/100*float32(r.Dx())
	y := float32(r.Max.Y) - float32(d)/100*float32(r.Dy())
	return f32.Pt(x, y)
}

// curveTD 与绘图共用边界;图外拖拽夹紧,避免小尺寸时除零。
func curveTD(gtx C, w, h int, x, y float32) (t, d int) {
	r := curveBounds(gtx, w, h)
	t = int(math.Round(float64((x - float32(r.Min.X)) / float32(r.Dx()) * 100)))
	d = int(math.Round(float64((float32(r.Max.Y) - y) / float32(r.Dy()) * 100)))
	return clampI(t, 0, 100), clampI(d, 0, 100)
}

// curvePath 与工作点插值共用端点:0℃保持 D1,100℃达到100%。
func curvePath(gtx C, w, h int, pts [3][2]int) []f32.Point {
	pp := []f32.Point{curveXY(gtx, w, h, 0, pts[0][1])}
	for _, p := range pts {
		pp = append(pp, curveXY(gtx, w, h, p[0], p[1]))
	}
	return append(pp, curveXY(gtx, w, h, 100, 100))
}

// curveDuty 保留插值小数,使工作点准确落在渲染线段上。
func curveDuty(pts [3][2]int, t int) float32 {
	if t >= 100 {
		return 100
	}
	prev := [2]int{0, pts[0][1]}
	for _, next := range append(pts[:], [2]int{100, 100}) {
		if t <= next[0] {
			if next[0] == prev[0] {
				return float32(next[1])
			}
			return float32(prev[1]) + float32(next[1]-prev[1])*float32(t-prev[0])/float32(next[0]-prev[0])
		}
		prev = next
	}
	return 100
}

// drawFanCurve 仅绘图;数值在图外逐行展示,不会与密集圆点重叠。
func drawFanCurve(gtx C, w, h int, pts [3][2]int, dragIdx, curTemp int) D {
	r := curveBounds(gtx, w, h)
	gridCol := color.NRGBA{R: 0x25, G: 0x2E, B: 0x36, A: 0xFF}
	st := textStyle{Size: fsSmall, Col: colText3}
	for v := 0; v <= 100; v += 25 {
		p := curveXY(gtx, w, h, v, v)
		x, y := int(p.X), int(p.Y)
		fillArea(gtx, image.Rect(x, r.Min.Y, x+1, r.Max.Y), gridCol)
		fillArea(gtx, image.Rect(r.Min.X, y, r.Max.X, y+1), gridCol)
		label := fmt.Sprintf("%d", v)
		td := measureText(gtx, label, st)
		drawTextAt(gtx, r.Min.X-gtx.Dp(8)-td.Size.X, y-td.Size.Y/2, label, st)
		drawTextAt(gtx, clampI(x-td.Size.X/2, 0, maxI(w-td.Size.X, 0)), r.Max.Y+gtx.Dp(6), label, st)
	}
	drawTextAt(gtx, r.Min.X, 0, "占空 (%)", st)
	centerTextAt(gtx, float32(r.Min.X+r.Max.X)/2, float32(h-gtx.Dp(8)), "温度 (℃)", st)
	pp := curvePath(gtx, w, h, pts)
	polyline(gtx, pp, px(gtx, 4), alpha(pal.Cyan, 0x30), false)
	polyline(gtx, pp, px(gtx, 1.5), pal.Cyan, false)
	if curTemp > 0 {
		ct := clampI(curTemp, 0, 100)
		wp := curveXY(gtx, w, h, ct, 0)
		wp.Y -= curveDuty(pts, ct) / 100 * float32(r.Dy())
		cx := int(wp.X)
		fillArea(gtx, image.Rect(cx, r.Min.Y, cx+1, r.Max.Y), alpha(pal.Yellow, 0x45))
		fillCircle(gtx, wp.X, wp.Y, px(gtx, 6), alpha(pal.Yellow, 0x35))
		fillCircle(gtx, wp.X, wp.Y, px(gtx, 3), pal.Yellow)
	}
	for i, p := range pts {
		c := curveXY(gtx, w, h, p[0], p[1])
		r := px(gtx, 4.5)
		if i == dragIdx {
			r = px(gtx, 6)
		}
		fillCircle(gtx, c.X, c.Y, r+px(gtx, 3), alpha(pal.Cyan, 0x35))
		fillCircle(gtx, c.X, c.Y, r, pal.Cyan)
		fillCircle(gtx, c.X, c.Y, r*0.45, color.NRGBA{R: 0x00, G: 0x16, B: 0x1C, A: 0xFF})
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

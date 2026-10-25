// theme — 霓虹视觉主题与共享控件:配色、角标面板、斜切标签/按钮、点阵条、
// 霓虹滑杆、方块开关、下拉框、确认按钮、侧栏图标、toast。视觉对齐原 Web UI(CC40 风格)。
//
// 绘制顺序约定:先画底(背景/边框/几何),再画文本 —— 文本尺寸先用宏测得,
// 画完底后回放文本(带居中变换),避免文字被底色盖住。
package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// ---------- 配色(与 Web UI :root 变量一致) ----------

var pal = struct {
	BG, Panel, Line, Tick, Txt, Dim      color.NRGBA
	Cyan, CyanD, Lime, Yellow, Red, Blue color.NRGBA
	KeyBG, KeyBG2, Track                 color.NRGBA
}{
	BG:     color.NRGBA{R: 0x0D, G: 0x11, B: 0x17, A: 0xFF}, // 页面底(GitHub dark)
	Panel:  color.NRGBA{R: 0x16, G: 0x1B, B: 0x22, A: 0xE0}, // 卡片底(半透明毛玻璃)
	Line:   color.NRGBA{R: 0x30, G: 0x36, B: 0x3D, A: 0xFF}, // 卡片描边
	Tick:   color.NRGBA{R: 0x8B, G: 0x94, B: 0x9E, A: 0xFF},
	Txt:    color.NRGBA{R: 0xE6, G: 0xED, B: 0xF3, A: 0xFF},
	Dim:    color.NRGBA{R: 0x8B, G: 0x94, B: 0x9E, A: 0xFF},
	Cyan:   color.NRGBA{R: 0x25, G: 0xC8, B: 0xFF, A: 0xFF},
	CyanD:  color.NRGBA{R: 0x0A, G: 0x7F, B: 0xA8, A: 0xFF},
	Lime:   color.NRGBA{R: 0xC8, G: 0xFF, B: 0x00, A: 0xFF},
	Yellow: color.NRGBA{R: 0xFF, G: 0xC4, B: 0x00, A: 0xFF},
	Red:    color.NRGBA{R: 0xFF, G: 0x3B, B: 0x4D, A: 0xFF},
	Blue:   color.NRGBA{R: 0x2F, G: 0x7B, B: 0xFF, A: 0xFF},
	KeyBG:  color.NRGBA{R: 0x19, G: 0x1D, B: 0x21, A: 0xFF},
	KeyBG2: color.NRGBA{R: 0x0B, G: 0x0D, B: 0x0F, A: 0xFF},
	Track:  color.NRGBA{R: 0x0C, G: 0x10, B: 0x13, A: 0xFF},
}

// alpha 返回带透明度的颜色副本。
func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// ---------- 主题(微软雅黑,确定性渲染) ----------

func newTheme() *material.Theme {
	th := material.NewTheme()
	if faces := loadCJKFonts(); len(faces) > 0 {
		th.Shaper = text.NewShaper(text.WithCollection(faces), text.NoSystemFonts())
	}
	th.TextSize = unit.Sp(14)
	return th
}

// loadCJKFonts 加载系统微软雅黑(全部 Windows 自带);缺失时保留默认 shaper(系统字体回退)。
// 注册顺序即优先级:msyh.ttc 常规在前,msyhbd.ttc 粗体在后(带 OS/2 权重,可被粗体标签匹配)。
func loadCJKFonts() []text.FontFace {
	var faces []text.FontFace
	for _, f := range []string{`C:\Windows\Fonts\msyh.ttc`, `C:\Windows\Fonts\msyhbd.ttc`} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		ff, err := opentype.ParseCollection(b)
		if err != nil {
			continue
		}
		for i := range ff {
			// 统一字体族名,粗体也能被同一族名匹配到
			ff[i].Font.Typeface = "MS YaHei"
		}
		faces = append(faces, ff...)
	}
	return faces
}

// ---------- 基础绘制 ----------

// fillRect 填充当前约束矩形。
func fillRect(gtx C, col color.NRGBA) D {
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
	return D{Size: gtx.Constraints.Max}
}

// fillArea 在指定矩形内填充。
func fillArea(gtx C, r image.Rectangle, col color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

// fillCircle 圆填充。
func fillCircle(gtx C, cx, cy, r float32, col color.NRGBA) {
	if r <= 0 {
		return
	}
	rr := int(r + 0.5)
	defer clip.Ellipse(image.Rect(int(cx)-rr, int(cy)-rr, int(cx)+rr, int(cy)+rr)).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

// polyline 描边一条折线。
func polyline(gtx C, pts []f32.Point, w float32, col color.NRGBA, closed bool) {
	if len(pts) < 2 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pts[0])
	for _, q := range pts[1:] {
		p.LineTo(q)
	}
	if closed {
		p.LineTo(pts[0])
	}
	st := clip.Stroke{Path: p.End(), Width: w}.Op()
	defer st.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

// strokePoly 多边形描边。
func strokePoly(gtx C, pts []f32.Point, w float32, col color.NRGBA) { polyline(gtx, pts, w, col, true) }

// fillPoly 多边形填充。
func fillPoly(gtx C, pts []f32.Point, col color.NRGBA) {
	if len(pts) < 3 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pts[0])
	for _, q := range pts[1:] {
		p.LineTo(q)
	}
	op2 := clip.Outline{Path: p.End()}.Op()
	defer op2.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}

// px dp→像素(float,供路径用)。
func px(gtx C, dp float32) float32 { return dp * gtx.Metric.PxPerDp }

// ---------- 布局小件 ----------

// flexWrap 把 widget 列表包成 FlexChild(rigid)。
func flexWrap(items ...layout.Widget) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, len(items))
	for _, it := range items {
		out = append(out, layout.Rigid(it))
	}
	return out
}

// hSpacer 水平占位。
func hSpacer(dp int) layout.Widget {
	return func(gtx C) D { return D{Size: image.Pt(gtx.Dp(unit.Dp(dp)), 0)} }
}

// vSpacer 垂直占位。
func vSpacer(dp int) layout.Widget {
	return func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(unit.Dp(dp)))} }
}

// col 垂直排列。
func col(gtx C, items ...layout.Widget) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, flexWrap(items...)...)
}

// row 水平排列(居中对齐);子项可为 layout.Widget(自动 Rigid)或 layout.FlexChild(加权)。
func row(gtx C, items ...any) D {
	kids := make([]layout.FlexChild, 0, len(items))
	for _, it := range items {
		switch v := it.(type) {
		case layout.FlexChild:
			kids = append(kids, v)
		case layout.Widget:
			kids = append(kids, layout.Rigid(v))
		}
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, kids...)
}

// centerRow 水平居中一行。
func centerRow(gtx C, items ...layout.Widget) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceAround}.Layout(gtx, flexWrap(items...)...)
}

// label 文本(样式简写)。
func label(gtx C, txt string, sizeSp float32, colr color.NRGBA) D {
	l := material.Label(g.th, unit.Sp(sizeSp), txt)
	l.Color = colr
	return l.Layout(gtx)
}

// textDims 只测文本尺寸,绘制 ops 留待回放(先底后字的次序靠它保证)。
func textDims(gtx C, txt string, sizeSp float32, colr color.NRGBA) (D, op.CallOp) {
	m := op.Record(gtx.Ops)
	d := label(gtx, txt, sizeSp, colr)
	return d, m.Stop()
}

// replayText 在 (x,y) 偏移处回放测好的文本。
func replayText(gtx C, call op.CallOp, x, y int) {
	// 变换必须用完即弹,否则整个后续帧都会被累计平移(曾致表盘文字/布局漂移)
	defer op.Affine(f32.NewAffine2D(1, 0, float32(x), 0, 1, float32(y))).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// ---------- 角标面板 ----------

// panel 角标面板:边框 + 四角 L 形标记 + 标题 + 内容。
func panel(gtx C, title string, body layout.Widget) D {
	return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
		// 先记录内容布局,得到尺寸;再画底;最后回放内容
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Top: unit.Dp(30), Right: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, body)
		call := macro.Stop()
		w, h := inner.Size.X, inner.Size.Y
		r := gtx.Dp(10)
		// 毛玻璃质感:半透明圆角底(透出窗口渐变背景)+ 圆角描边
		rr := clip.UniformRRect(image.Rect(0, 0, w, h), r)
		defer rr.Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, pal.Panel)
		// 边框描边:画完立即弹,否则后续标题/内容会被裁到 1.2px 边框线上(视觉消失)
		stOp := clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1.2}.Op().Push(gtx.Ops)
		paint.Fill(gtx.Ops, pal.Line)
		stOp.Pop()
		cornerMarks(gtx, w, h)
		td, tcall := textDims(gtx, title, 12, color.NRGBA{R: 0xC2, G: 0xCC, B: 0xD4, A: 0xFF})
		replayText(gtx, tcall, gtx.Dp(16), gtx.Dp(8)+(gtx.Dp(16)-td.Size.Y)/2)
		call.Add(gtx.Ops)
		return D{Size: image.Pt(w, h)}
	})
}
// border 矩形描边(4 条细矩形,避免 stroke 路径开销)。
func border(gtx C, r image.Rectangle, col color.NRGBA, wdp int) {
	w := gtx.Dp(unit.Dp(wdp))
	fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), col)
	fillArea(gtx, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), col)
	fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), col)
	fillArea(gtx, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// cornerMarks 面板四角 L 形角标(9dp,1dp 粗)。
func cornerMarks(gtx C, w, h int) {
	s := gtx.Dp(9)
	t := gtx.Dp(1)
	c := pal.Tick
	fillArea(gtx, image.Rect(0, 0, s, t), c)
	fillArea(gtx, image.Rect(0, 0, t, s), c)
	fillArea(gtx, image.Rect(w-s, 0, w, t), c)
	fillArea(gtx, image.Rect(w-t, 0, w, s), c)
	fillArea(gtx, image.Rect(0, h-t, s, h), c)
	fillArea(gtx, image.Rect(0, h-s, t, h), c)
	fillArea(gtx, image.Rect(w-s, h-t, w, h), c)
	fillArea(gtx, image.Rect(w-t, h-s, w, h), c)
}

// ---------- 斜切几何 ----------

// skewPts 斜切平行四边形四点:顶边右移(CSS skewX(-ang) 的视觉)。
func skewPts(gtx C, w, h int, deg float32) []f32.Point {
	k := float32(math.Tan(float64(deg) * math.Pi / 180))
	sk := px(gtx, k) * float32(h) / 2
	return []f32.Point{
		{X: sk, Y: 0},
		{X: float32(w) + sk, Y: 0},
		{X: float32(w), Y: float32(h)},
		{X: 0, Y: float32(h)},
	}
}

// ---------- 斜切标签(tab) ----------

// drawTab 斜切标签(选中填充青/黄,未选中 hover 描边)。
func drawTab(gtx C, clk *widget.Clickable, txt string, active, small, yellow bool) D {
	padH, size := 28, float32(15)
	if small {
		padH, size = 16, 13
	}
	bg := color.NRGBA{}
	fg := pal.Dim
	if active {
		if yellow {
			bg, fg = pal.Yellow, color.NRGBA{R: 0x20, G: 0x15, B: 0x00, A: 0xFF}
		} else {
			bg, fg = pal.Cyan, color.NRGBA{R: 0x00, G: 0x16, B: 0x1C, A: 0xFF}
		}
	} else if clk.Hovered() {
		fg = color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF}
	}
	return clk.Layout(gtx, func(gtx C) D {
		textD, call := textDims(gtx, txt, size, fg)
		w := textD.Size.X + gtx.Dp(unit.Dp(2*padH))
		h := textD.Size.Y + gtx.Dp(10)
		if active {
			fillPoly(gtx, skewPts(gtx, w, h, 14), bg)
		} else if clk.Hovered() {
			strokePoly(gtx, skewPts(gtx, w, h, 14), 1, pal.Line)
		}
		replayText(gtx, call, (w-textD.Size.X)/2, (h-textD.Size.Y)/2)
		return D{Size: image.Pt(w, h)}
	})
}

// ---------- 斜切按钮(act) ----------

type btnVariant int

const (
	btnPlain btnVariant = iota
	btnPri
	btnWarn
)

// actBtn 斜切动作按钮;disabled 时 40% 透明度(点击是否受理由调用方保证)。
func actBtn(gtx C, clk *widget.Clickable, txt string, v btnVariant, disabled bool) D {
	fg := color.NRGBA{R: 0xCF, G: 0xD6, B: 0xDC, A: 0xFF}
	bg := pal.Track
	bd := color.NRGBA{R: 0x2C, G: 0x34, B: 0x3C, A: 0xFF}
	switch v {
	case btnPri:
		bg, bd, fg = pal.Cyan, pal.Cyan, color.NRGBA{R: 0x00, G: 0x16, B: 0x1C, A: 0xFF}
	case btnWarn:
		bg, bd, fg = pal.Yellow, pal.Yellow, color.NRGBA{R: 0x20, G: 0x15, B: 0x00, A: 0xFF}
	}
	if disabled {
		bg, bd, fg = alpha(bg, 0x40), alpha(bd, 0x40), alpha(fg, 0x40)
	} else if clk.Hovered() && v == btnPlain {
		bd = pal.Cyan
		fg = pal.Txt
	}
	return clk.Layout(gtx, func(gtx C) D {
		textD, call := textDims(gtx, txt, 13, fg)
		w := textD.Size.X + gtx.Dp(40)
		h := textD.Size.Y + gtx.Dp(14)
		fillPoly(gtx, skewPts(gtx, w, h, 10), bg)
		if v == btnPlain {
			strokePoly(gtx, skewPts(gtx, w, h, 10), 1, bd)
		}
		replayText(gtx, call, (w-textD.Size.X)/2, (h-textD.Size.Y)/2)
		return D{Size: image.Pt(w, h)}
	})
}

// confirmBtn 两段式确认按钮:第一次点击进入待确认态(黄色,5 秒超时),再次点击才执行。
type confirmBtn struct {
	clk widget.Clickable
	arm time.Time
}

func (c *confirmBtn) clicked(gtx C) bool {
	if c.clk.Clicked(gtx) {
		if !c.arm.IsZero() && time.Since(c.arm) < 5*time.Second {
			c.arm = time.Time{}
			return true
		}
		c.arm = time.Now()
	}
	if !c.arm.IsZero() && time.Since(c.arm) >= 5*time.Second {
		c.arm = time.Time{}
	}
	return false
}

func (c *confirmBtn) layout(gtx C, base string) D {
	txt, v := base, btnPlain
	if !c.arm.IsZero() {
		txt, v = "确认执行?", btnWarn
	}
	return actBtn(gtx, &c.clk, txt, v, false)
}

// ---------- 点阵条(bar) ----------

// dotBar 点阵进度条:亮点点阵表示进度(与 Web .bar 一致)。
func dotBar(gtx C, frac float32, cyan bool) D {
	width := gtx.Constraints.Max.X
	if width == 0 {
		width = gtx.Dp(120)
	}
	h := gtx.Dp(7)
	col := pal.Lime
	if cyan {
		col = pal.Cyan
	}
	fillArea(gtx, image.Rect(0, 0, width, h), color.NRGBA{R: 0x10, G: 0x14, B: 0x18, A: 0xFF})
	cy, r := float32(h)/2, px(gtx, 1.7)
	n := (width + gtx.Dp(9) - 1) / gtx.Dp(9)
	lit := float32(n) * clamp01(frac)
	for i := 0; i < n; i++ {
		x := float32(gtx.Dp(3) + i*gtx.Dp(9))
		if float32(i) < lit-0.55 {
			fillCircle(gtx, x, cy, r, col)
		} else {
			fillCircle(gtx, x, cy, r, color.NRGBA{R: 0x1A, G: 0x1F, B: 0x24, A: 0xFF})
		}
	}
	return D{Size: image.Pt(width, h)}
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---------- 霓虹滑杆 ----------

// neonSlider 自绘滑杆:点阵轨道 + 矩形拇指;拖动结束(或点击)后提交 onChange。
type neonSlider struct {
	f          widget.Float
	committed  float32
	onChange   func(int)
	min, max   float32
	cyan       bool
	lastSynced int // 快照同步去重:避免"提交后、刷新前"的旧快照把值拉回去
}

func newSlider(min, max int, cyan bool, onChange func(int)) *neonSlider {
	return &neonSlider{min: float32(min), max: float32(max), cyan: cyan, onChange: onChange,
		committed: float32(min), lastSynced: math.MinInt32}
}

// syncExt 用快照值同步滑杆(仅当快照值变化且未在拖动时)。
func (s *neonSlider) syncExt(v int) {
	if !s.f.Dragging() && v != s.lastSynced {
		s.lastSynced = v
		s.set(v)
	}
}

func (s *neonSlider) set(v int) {
	s.committed = clamp(float32(v), s.min, s.max)
	s.f.Value = (s.committed - s.min) / (s.max - s.min)
}

// value 当前(可能拖动中)的取整值。
func (s *neonSlider) value() int {
	return int(math.Round(float64(s.min + s.f.Value*(s.max-s.min))))
}

func (s *neonSlider) layout(gtx C) D {
	prev := s.f.Value
	dims := s.f.Layout(gtx, layout.Horizontal, unit.Dp(8))
	changed := s.f.Value != prev
	width := dims.Size.X
	height := gtx.Dp(22)
	trackY := height/2 - gtx.Dp(2)
	fillArea(gtx, image.Rect(0, trackY, width, trackY+gtx.Dp(5)), pal.Track)
	frac := clamp01(s.f.Value)
	col := pal.Lime
	if s.cyan {
		col = pal.Cyan
	}
	n := (width + gtx.Dp(14) - 1) / gtx.Dp(14)
	lit := float32(n) * frac
	for i := 0; i < n; i++ {
		x := float32(gtx.Dp(3) + i*gtx.Dp(14))
		if float32(i) < lit-0.4 {
			fillCircle(gtx, x, float32(trackY)+px(gtx, 2.5), px(gtx, 1.5), col)
		}
	}
	// 拇指:5×19 白色竖条 + 辉光
	tx := clampI(int(float32(width)*frac), gtx.Dp(2), width-gtx.Dp(3))
	fillArea(gtx, image.Rect(tx-gtx.Dp(4), height/2-gtx.Dp(11), tx+gtx.Dp(7), height/2+gtx.Dp(11)), alpha(col, 0x55))
	fillArea(gtx, image.Rect(tx-gtx.Dp(2), height/2-gtx.Dp(9), tx+gtx.Dp(3), height/2+gtx.Dp(9)), pal.Txt)
	// 拖动结束且值变化 → 提交
	if !s.f.Dragging() && changed {
		v := s.value()
		if int(s.committed) != v && s.onChange != nil {
			s.committed = float32(v)
			s.onChange(v)
		}
	}
	return D{Size: image.Pt(width, height)}
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------- 方块开关(与 Web .sw 一致) ----------

// switchBox 40×20 方块开关。
func switchBox(gtx C, b *widget.Bool) D {
	return b.Layout(gtx, func(gtx C) D {
		w, h := gtx.Dp(40), gtx.Dp(20)
		bd := color.NRGBA{R: 0x2C, G: 0x34, B: 0x3C, A: 0xFF}
		knob := color.NRGBA{R: 0x59, G: 0x63, B: 0x6D, A: 0xFF}
		if b.Value {
			bd = pal.Lime
			knob = pal.Lime
		}
		fillArea(gtx, image.Rect(0, 0, w, h), pal.Track)
		border(gtx, image.Rect(0, 0, w, h), bd, 1)
		s := gtx.Dp(14)
		x := gtx.Dp(2)
		if b.Value {
			x = w - s - gtx.Dp(2)
		}
		fillArea(gtx, image.Rect(x, gtx.Dp(2), x+s, gtx.Dp(2)+s), knob)
		return D{Size: image.Pt(w, h)}
	})
}

// ---------- 下拉框 ----------

// dropdown 简易下拉框(流内展开,最多 8 项内滚)。
type dropdown struct {
	clk     widget.Clickable
	opts    []string
	sel     int
	optClks []widget.Clickable
	open    bool
	widthDp int
}

func newDropdown(opts []string, sel, widthDp int) *dropdown {
	d := &dropdown{opts: opts, sel: sel, widthDp: widthDp}
	d.optClks = make([]widget.Clickable, len(opts))
	return d
}

// selected 当前选中下标(布局循环中更新)。
func (d *dropdown) selected() int { return d.sel }

func (d *dropdown) layout(gtx C) D {
	w := gtx.Dp(unit.Dp(d.widthDp))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			if d.clk.Clicked(gtx) {
				d.open = !d.open
			}
			txt := ""
			if d.sel >= 0 && d.sel < len(d.opts) {
				txt = d.opts[d.sel]
			}
			arrow := "▼"
			if d.open {
				arrow = "▲"
			}
			textD, call := textDims(gtx, txt, 13, pal.Txt)
			arrowD, acall := textDims(gtx, arrow, 11, pal.Dim)
			h := textD.Size.Y + gtx.Dp(12)
			fillArea(gtx, image.Rect(0, 0, w, h), pal.Track)
			border(gtx, image.Rect(0, 0, w, h), pal.Line, 1)
			replayText(gtx, call, gtx.Dp(10), (h-textD.Size.Y)/2)
			replayText(gtx, acall, w-arrowD.Size.X-gtx.Dp(8), (h-arrowD.Size.Y)/2)
			return D{Size: image.Pt(w, h)}
		}),
		layout.Rigid(func(gtx C) D {
			if !d.open || len(d.opts) == 0 {
				return D{}
			}
			rows := len(d.opts)
			if rows > 8 {
				rows = 8
			}
			h := gtx.Dp(30) * rows
			fillArea(gtx, image.Rect(0, 0, w, h), color.NRGBA{R: 0x0E, G: 0x12, B: 0x16, A: 0xFF})
			border(gtx, image.Rect(0, 0, w, h), pal.Line, 1)
			var lst layout.List
			lst.Axis = layout.Vertical
			lst.Layout(gtx, len(d.opts), func(gtx C, i int) D {
				hl := i == d.sel
				if d.optClks[i].Clicked(gtx) {
					d.sel = i
					d.open = false
				}
				return d.optClks[i].Layout(gtx, func(gtx C) D {
					bg := color.NRGBA{}
					if hl {
						bg = alpha(pal.Cyan, 0x22)
					} else if d.optClks[i].Hovered() {
						bg = alpha(pal.Cyan, 0x10)
					}
					fillArea(gtx, image.Rect(0, 0, w, gtx.Dp(30)), bg)
					td, call := textDims(gtx, d.opts[i], 13, func() color.NRGBA {
						if hl {
							return pal.Cyan
						}
						return pal.Txt
					}())
					replayText(gtx, call, gtx.Dp(12), (gtx.Dp(30)-td.Size.Y)/2)
					return D{Size: image.Pt(w, gtx.Dp(30))}
				})
			})
			return D{Size: image.Pt(w, h)}
		}))
}

// ---------- 徽章 ----------

// badge 描边徽章(ok/warn/err/默认)。
func badge(gtx C, txt, kind string) D {
	fg := color.NRGBA{R: 0xAE, G: 0xB8, B: 0xC1, A: 0xFF}
	switch kind {
	case "ok":
		fg = pal.Lime
	case "warn":
		fg = pal.Yellow
	case "err":
		fg = pal.Red
	}
	textD, call := textDims(gtx, txt, 12, fg)
	w := textD.Size.X + gtx.Dp(24)
	h := textD.Size.Y + gtx.Dp(6)
	border(gtx, image.Rect(0, 0, w, h), fg, 1)
	replayText(gtx, call, (w-textD.Size.X)/2, (h-textD.Size.Y)/2)
	return D{Size: image.Pt(w, h)}
}

// ---------- toast ----------

type toastState struct {
	msg   string
	until time.Time
}

func (g *GUI) toast(msg string) {
	g.toastS.msg = msg
	g.toastS.until = time.Now().Add(1900 * time.Millisecond)
	time.AfterFunc(2100*time.Millisecond, g.win.Invalidate)
}

func (g *GUI) drawToast(gtx C) {
	if g.toastS.msg == "" || !time.Now().Before(g.toastS.until) {
		return
	}
	textD, call := textDims(gtx, g.toastS.msg, 14, pal.Txt)
	w := textD.Size.X + gtx.Dp(40)
	h := textD.Size.Y + gtx.Dp(20)
	x, y := gtx.Constraints.Max.X-w-gtx.Dp(26), gtx.Constraints.Max.Y-h-gtx.Dp(26)
	fillArea(gtx, image.Rect(x, y, x+w, y+h), color.NRGBA{R: 0x0C, G: 0x10, B: 0x13, A: 0xE6})
	border(gtx, image.Rect(x, y, x+w, y+h), pal.Cyan, 1)
	replayText(gtx, call, x+(w-textD.Size.X)/2, y+(h-textD.Size.Y)/2)
}

// ---------- 侧栏图标(矢量描边,对齐 Web 的内联 SVG) ----------

// drawIcon 按名字绘制 size dp 的线性图标(24×24 视框)。
func drawIcon(gtx C, name string, size int, c color.NRGBA) D {
	s := float32(gtx.Dp(unit.Dp(size)))
	u := s / 24
	line := func(pts ...[2]float32) {
		out := make([]f32.Point, len(pts))
		for i, p := range pts {
			out[i] = f32.Pt(p[0]*u, p[1]*u)
		}
		polyline(gtx, out, u*1.5, c, false)
	}
	poly := func(pts ...[2]float32) {
		out := make([]f32.Point, len(pts))
		for i, p := range pts {
			out[i] = f32.Pt(p[0]*u, p[1]*u)
		}
		fillPoly(gtx, out, c)
	}
	ring := func(cx, cy, r float32) {
		fillCircle(gtx, cx*u, cy*u, r*u, c)
		fillCircle(gtx, cx*u, cy*u, (r-1.2)*u, pal.BG)
	}
	rect := func(x, y, w, h float32) {
		fillArea(gtx, image.Rect(int(x*u), int(y*u), int((x+w)*u), int((y+h)*u)), c)
		m := int(1.2 * u)
		fillArea(gtx, image.Rect(int(x*u)+m, int(y*u)+m, int((x+w)*u)-m, int((y+h)*u)-m), pal.Panel)
	}
	switch name {
	case "system": // 显示器
		rect(3, 4, 18, 12)
		line([2]float32{9, 20}, [2]float32{15, 20})
		line([2]float32{12, 16}, [2]float32{12, 20})
		line([2]float32{7, 9}, [2]float32{11, 9})
		line([2]float32{7, 12}, [2]float32{14, 12})
	case "kb": // 键盘
		rect(2, 7, 20, 11)
		for i := 0; i < 5; i++ {
			fillCircle(gtx, (4.5+float32(i)*3.6)*u, 10.5*u, 0.8*u, c)
		}
		for i := 0; i < 5; i++ {
			fillCircle(gtx, (4.5+float32(i)*3.6)*u, 13.5*u, 0.8*u, c)
		}
		line([2]float32{7, 16}, [2]float32{17, 16})
	case "fk": // FlexiKey 旗标
		poly([2]float32{7, 3}, [2]float32{14, 3}, [2]float32{14, 11}, [2]float32{10, 11}, [2]float32{10, 21}, [2]float32{7, 21})
		line([2]float32{10, 7}, [2]float32{14, 7})
	case "ch": // 闪电
		poly([2]float32{13, 2}, [2]float32{5, 13}, [2]float32{10, 13}, [2]float32{9, 22}, [2]float32{17, 11}, [2]float32{12, 11})
	case "cpu": // 芯片
		rect(7, 7, 10, 10)
		for _, x := range []float32{10, 14} {
			line([2]float32{x, 3}, [2]float32{x, 7})
			line([2]float32{x, 17}, [2]float32{x, 21})
			line([2]float32{3, x}, [2]float32{7, x})
			line([2]float32{17, x}, [2]float32{21, x})
		}
	case "gpu": // 显卡
		rect(3, 6, 18, 12)
		ring(9, 12, 3)
		line([2]float32{16, 9}, [2]float32{16, 15})
		line([2]float32{19, 9}, [2]float32{19, 15})
	case "bat": // 电池
		rect(2, 8, 17, 9)
		line([2]float32{21, 11}, [2]float32{21, 14})
		poly([2]float32{6, 11}, [2]float32{9, 11}, [2]float32{7.5, 14}, [2]float32{10, 14}, [2]float32{7, 17}, [2]float32{8, 14}, [2]float32{6, 14})
	case "en": // 叶子
		line([2]float32{6, 20}, [2]float32{8, 14}, [2]float32{11, 10}, [2]float32{15, 7}, [2]float32{20, 4})
		line([2]float32{6, 20}, [2]float32{8, 14}, [2]float32{11, 11}, [2]float32{16, 8})
		line([2]float32{20, 4}, [2]float32{19, 9}, [2]float32{17, 14}, [2]float32{13, 18}, [2]float32{8, 20})
	case "pf": // 齿轮
		ring(12, 12, 3)
		for i := 0; i < 8; i++ {
			a := float32(i) * math.Pi / 4
			line([2]float32{12 + 6*float32(math.Cos(float64(a))), 12 + 6*float32(math.Sin(float64(a)))},
				[2]float32{12 + 8.5*float32(math.Cos(float64(a))), 12 + 8.5*float32(math.Sin(float64(a)))})
		}
	case "info": // ⓘ
		ring(12, 12, 8)
		fillCircle(gtx, 12*u, 8.6*u, 1.1*u, c)
		line([2]float32{12, 11.5}, [2]float32{12, 16.5})
	}
	return D{Size: image.Pt(int(s), int(s))}
}

// ---------- 键值行 ----------

// kvRow 一行键值(key 灰定宽,value 白)。
func kvRow(gtx C, k, v string) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(96)
			return label(gtx, k, 13, pal.Dim)
		}),
		layout.Rigid(func(gtx C) D { return label(gtx, v, 13, pal.Txt) }),
	)
}

// labelRow 带标签的控件行(标签定宽,后跟控件)。
func labelRow(gtx C, k string, w layout.Widget) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(96)
			return label(gtx, k, 13, pal.Dim)
		}),
		layout.Rigid(w),
	)
}

// ---------- 数字输入 ----------

// numField 单行数字输入框。
type numField struct {
	ed    widget.Editor
	inited bool
}

func (n *numField) init(v int) {
	if n.inited {
		return
	}
	n.inited = true
	n.ed.SingleLine = true
	n.ed.Filter = "0123456789.-"
	n.ed.SetText(strconv.Itoa(v))
}

// setText 直接覆盖文本。
func (n *numField) setText(v int) {
	if s := strconv.Itoa(v); n.ed.Text() != s {
		n.ed.SetText(s)
	}
}

// sync 用快照值同步输入框(聚焦时不打断输入)。
func (n *numField) sync(gtx C, v int) {
	if !gtx.Focused(&n.ed) {
		n.setText(v)
	}
}

func (n *numField) value() int {
	v, _ := strconv.Atoi(strings.TrimSpace(n.ed.Text()))
	return v
}

// numBox 数字输入框布局(固定宽)。
func numBox(gtx C, n *numField, wDp int) D {
	ed := material.Editor(g.th, &n.ed, "")
	ed.TextSize = unit.Sp(13)
	ed.Color = pal.Txt
	w := gtx.Dp(unit.Dp(wDp))
	h := gtx.Dp(30)
	fillArea(gtx, image.Rect(0, 0, w, h), pal.Track)
	border(gtx, image.Rect(0, 0, w, h), pal.Line, 1)
	layout.Inset{Top: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Pt(w-gtx.Dp(16), h-gtx.Dp(10))
		gtx.Constraints.Max = gtx.Constraints.Min
		return ed.Layout(gtx)
	})
	return D{Size: image.Pt(w, h)}
}

// ---------- 工具 ----------

// hsl2rgb HSL(角 0..1, s, l)→ RGB 0..255(与 Web hsl2rgb 一致)。
func hsl2rgb(h, s, l float64) (int, int, int) {
	f := func(n int) float64 {
		k := math.Mod(float64(n)+h*12, 12)
		a := s * math.Min(l, 1-l)
		return math.Round(255 * (l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))))
	}
	return int(f(0)), int(f(8)), int(f(4))
}

// rgbHex 颜色 → #rrggbb。
func rgbHex(r, g, b int) string { return fmt.Sprintf("#%02x%02x%02x", r, g, b) }

// maxI 取大者。
func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

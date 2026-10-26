// theme — 共享控件与容器:下拉框、确认按钮、图标、键值行、数字输入、toast。
// 配色/度量/字阶在 tokens.go,绘制原语在 prim.go,卡片与按钮/开关等在 comp.go。
// 这里只保留"有状态控件"与需要和页面紧密配合的部分。
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
	"gioui.org/font"
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

	// 新卡片风格(参考目标样式):圆角深色卡 + 分隔线 + 图标胶囊
	Card, CardLine, CardTop color.NRGBA // 卡片底/边框/顶部提亮
	IconBox, IconBoxLine    color.NRGBA // 头部图标胶囊
	Sep                     color.NRGBA // 卡内分隔线
	SubBg                   color.NRGBA // 卡内浅色小块(信息条)
	Head                    color.NRGBA // 标题文字(略亮于正文)
}{
	// 全部映射到 tokens.go 的设计令牌:页面里既有的 pal.X 引用自动升级到新配色,
	// 不再有裸写色值(改配色只改 tokens.go)。
	BG:     colBg,
	Panel:  colSurface,
	Line:   colLine,
	Tick:   colText2,
	Txt:    colText,
	Dim:    colText3,
	Cyan:   colAccent,
	CyanD:  color.NRGBA{R: 0x0E, G: 0x74, B: 0x90, A: 0xFF}, // 深青(次要强调/图表填充)
	Lime:   colOk,
	Yellow: colWarn,
	Red:    colDanger,
	Blue:   colViolet,
	KeyBG:  color.NRGBA{R: 0x18, G: 0x1F, B: 0x29, A: 0xFF}, // 键帽渐变顶
	KeyBG2: colSunken,                                       // 键帽渐变底
	Track:  colSunken,                                       // 输入底

	// 卡片色阶:比页面底亮一档,靠半透明描边与顶边高光区分层次
	Card:        colSurface,
	CardTop:     colHover,
	CardLine:    colLine,
	IconBox:     colSunken,
	IconBoxLine: colLineHi,
	Sep:         colLineSoft,
	SubBg:       colSunken,
	Head:        colText,
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

// ---------- 圆角原语 ----------
// 圆角填充/描边统一走 prim.go 的 fillRR / strokeRR(带像素吸附),
// 旧的 fillRoundRect / strokeRoundRectCol / gradCardTop* 已随卡片改版移除。

// hSep 卡内水平分隔线(1dp,左右可内缩)。
func hSep(gtx C, inset int) D {
	w := gtx.Constraints.Max.X - 2*gtx.Dp(unit.Dp(inset))
	if w < 0 {
		w = 0
	}
	fillArea(gtx, image.Rect(gtx.Dp(unit.Dp(inset)), 0, gtx.Dp(unit.Dp(inset))+w, gtx.Dp(1)), pal.Sep)
	return D{Size: image.Pt(w, gtx.Dp(1))}
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
		case layout.Widget: // 命名函数类型(hSpacer 等返回值)
			kids = append(kids, layout.Rigid(v))
		case func(gtx C) D: // 无名闭包:动态类型不是 layout.Widget,必须单独匹配,
			// 否则整行除 spacer 外全部被丢弃(节能页等行消失的根因)
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
// spreadCn 中文标题字距(模拟 CSS letter-spacing:2px —— 每字符后插入薄空格)。
func spreadCn(s string) string {
	rs := []rune(s)
	out := make([]rune, 0, len(rs)*2)
	for i, r := range rs {
		if i > 0 {
			out = append(out, '\u2009')
		}
		out = append(out, r)
	}
	return string(out)
}

// card 卡片:柔和投影 + 半透明描边 + 16dp 圆角 + 顶边高光,可选头部图标与副标题、右上角动作位。
// 层序严格为 底 → 头部 → 内容:内容先录制再回放,避免文字被底色盖住。
func card(gtx C, o cardOpt, body layout.Widget) D {
	// 宽度锁满(页面靠它铺满一列),高度必须放开让内容按自然高度排布:
	// 若沿用 Exact 约束,内层 Flex/Inset 会把高度压成剩余空间,卡片被"压扁"。
	if gtx.Constraints.Max.X > 0 {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
	}
	if gtx.Constraints.Min.Y == gtx.Constraints.Max.Y {
		gtx.Constraints.Max.Y = math.MaxInt32 / 2
		gtx.Constraints.Min.Y = 0
	}
	return layout.Inset{Bottom: unit.Dp(sp12)}.Layout(gtx, func(gtx C) D {
		// 1) 量内容:用独立的 scratchOps 测量,只拿尺寸不落任何 op。
		// 不能用「op.Record 录制 + 回放」,因为录制期的 material 宏与回放期的 glyph 宏 ID 会错位,
		// 表现为卡片里文字整段消失(实测)。
		padX := Snap(gtx, cardPadX)
		padY := Snap(gtx, cardPadY)
		var scratch op.Ops
		gxBody := gtx
		gxBody.Ops = &scratch
		gxBody.Constraints.Min = image.Pt(0, 0)
		gxBody.Constraints.Max = image.Pt(maxI(gtx.Constraints.Max.X-2*padX, 0), gtx.Constraints.Max.Y)
		inner := body(gxBody)

		w := inner.Size.X + 2*padX
		if gtx.Constraints.Min.X == gtx.Constraints.Max.X && gtx.Constraints.Max.X > 0 {
			w = gtx.Constraints.Max.X // 宽度锁满时铺满约束
		}
		if w <= 2*padX {
			w = 2*padX + gtx.Dp(unit.Dp(120)) // 兜底:内容没报宽度时也给一个可用宽度
		}
		headH := 0
		if o.title != "" {
			headH = Snap(gtx, hdrH)
		}
		h := headH + inner.Size.Y + 2*padY
		r := image.Rect(0, 0, w, h)
		rad := Snap(gtx, rCard)

		// 2) 卡片底:柔和投影 → 描边色铺满 → 内缩 1px 的底(即 1px 边框)→ 顶边高光
		softShadow(gtx, r, rad)
		one := Snap(gtx, 1)
		fillRR(gtx, r, rad, colLine)
		innerRR := r.Inset(one)
		fillRR(gtx, innerRR, rad-one, colSurface)
		topHi(gtx, innerRR, rad-one, colTopHi)
		if o.accent {
			glowLine(gtx, r, colAccent, 0x66)
		}

		// 3) 头部:图标胶囊 + 标题 + 副标题 + 右上角动作
		if o.title != "" {
			x := padX
			if o.icon != "" {
				bs := Snap(gtx, iconChipSz)
				by := (headH - bs) / 2
				off := op.Offset(image.Pt(x, by)).Push(gtx.Ops)
				iconChip(gtx, o.icon, colAccent)
				off.Pop()
				x += bs + Snap(gtx, sp10)
			}
			// 标题:实测高度居中(不用估计行高)
			td, tcall := txtDims(gtx, o.title, textStyle{Size: fsBig, Col: colText, W: font.Medium})
			ty := (headH - td.Size.Y) / 2
			if o.sub != "" {
				sd, scall := txtDims(gtx, o.sub, textStyle{Size: fsSmall, Col: colText3})
				block := td.Size.Y + Snap(gtx, 2) + sd.Size.Y
				ty = (headH - block) / 2
				replayText(gtx, tcall, x, ty)
				replayText(gtx, scall, x, ty+td.Size.Y+Snap(gtx, 2))
			} else {
				replayText(gtx, tcall, x, ty)
			}
			if o.action != nil {
				// 右上角动作区(按钮/徽标),右对齐
				aw := maxI(w-padX-x, gtx.Dp(80))
				if aw > gtx.Dp(200) {
					aw = gtx.Dp(200)
				}
				ax := w - padX - aw
				if ax < x+td.Size.X+Snap(gtx, sp8) {
					ax = x + td.Size.X + Snap(gtx, sp8)
				}
				aOff := op.Offset(image.Pt(ax, 0)).Push(gtx.Ops)
				aGtx := gtx
				aGtx.Constraints = layout.Constraints{
					Min: image.Pt(0, 0),
					Max: image.Pt(aw, headH),
				}
				layout.W.Layout(aGtx, o.action)
				aOff.Pop()
			}
			// 头部与内容的分隔线(弱描边,不用纯灰实线)
			hLine(gtx, image.Rect(padX, headH-Snap(gtx, 1), w-padX, headH), colLineSoft)
		}

		// 4) 内容:真实布局一遍(此时 ops 归位,文字宏与位置都对得上)
		off := op.Offset(image.Pt(padX, headH+padY)).Push(gtx.Ops)
		gxBody = gtx
		gxBody.Constraints.Min = image.Pt(inner.Size.X, 0)
		gxBody.Constraints.Max = image.Pt(maxI(w-2*padX, inner.Size.X), maxI(inner.Size.Y, 0))
		body(gxBody)
		off.Pop()
		return D{Size: image.Pt(w, h)}
	})
}

// cardOpt 卡片配置。
type cardOpt struct {
	title  string
	sub    string        // 副标题(标题下方小字,可空)
	icon   string        // 头部图标名(drawIcon),可空
	action layout.Widget // 右上角动作位,可空
	accent bool          // 顶边强调线(焦点卡片用)
}

// panel 兼容旧调用:等价于无图标、无副标题、无动作位的基础卡片。
func panel(gtx C, title string, body layout.Widget) D {
	return card(gtx, cardOpt{title: title}, body)
}

// textH 13sp 文字行高。
func textH(gtx C, sp float32) int { return gtx.Sp(unit.Sp(sp)) }

// border 矩形描边(4 条细矩形,避免 stroke 路径开销)。
func border(gtx C, r image.Rectangle, col color.NRGBA, wdp int) {
	w := gtx.Dp(unit.Dp(wdp))
	fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), col)
	fillArea(gtx, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), col)
	fillArea(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), col)
	fillArea(gtx, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// ---------- 斜切几何 ----------
// 旧的斜切平行四边形(skewPts / cornerMarks)已移除:深色圆角体系里斜切会显得突兀,
// 标签与按钮统一改为圆角胶囊(comp.go 的 button / 下方 drawTab)。

// ---------- 标签(tab) ----------

// drawTab 标签:选中态用青色 18% 底 + 青描边 + 青字(圆角胶囊),
// 替代原来的斜切平行四边形——斜切在深色圆角体系里会显得突兀。
func drawTab(gtx C, clk *widget.Clickable, txt string, active, small, yellow bool) D {
	padH, size := float32(sp16), float32(fsBody)
	if small {
		padH, size = float32(sp10), float32(fsBody2)
	}
	col := colText3
	if yellow {
		col = colWarn
	}
	if active {
		col = colAccent
		if yellow {
			col = colWarn
		}
	}
	return clk.Layout(gtx, func(gtx C) D {
		td, call := txtDims(gtx, txt, textStyle{Size: size, Col: col})
		w := td.Size.X + 2*Snap(gtx, unit.Dp(padH))
		h := Snap(gtx, ctlH)
		r := image.Rect(0, 0, w, h)
		rad := Snap(gtx, rChip)
		switch {
		case active:
			fillRR(gtx, r, rad, alpha(col, 0x24))
			strokeRR(gtx, r, rad, Snap(gtx, 1), alpha(col, 0x88), alpha(col, 0x24))
		case clk.Hovered():
			fillRR(gtx, r, rad, alpha(colText, 0x0A))
			strokeRR(gtx, r, rad, Snap(gtx, 1), colLine, alpha(colText, 0x0A))
		}
		replayText(gtx, call, (w-td.Size.X)/2, (h-td.Size.Y)/2)
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

// actBtn 动作按钮:斜切的霓虹按钮换成圆角分级按钮(primary/soft/warn)。
// 保留原签名,页面调用点零改动;disabled 时整体降透明度(点击是否受理由调用方保证)。
func actBtn(gtx C, clk *widget.Clickable, s string, v btnVariant, disabled bool) D {
	kind := btnSoft
	switch v {
	case btnPri:
		kind = btnPrimary
	case btnWarn:
		kind = btnDanger
	}
	return button(gtx, clk, s, kind, disabled)
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

// dotBar 进度条:圆角轨道 + 渐变填充(取代点阵小圆,静止时 op 更少也更耐看)。
func dotBar(gtx C, frac float32, cyan bool) D {
	width := gtx.Constraints.Max.X
	if width == 0 {
		width = gtx.Dp(120)
	}
	h := Snap(gtx, 5)
	col := colOk
	if cyan {
		col = colAccent
	}
	r := image.Rect(0, 0, width, h)
	rad := h / 2
	fillRR(gtx, r, rad, colSunken)
	fillRR(gtx, r, rad, alpha(colLine, 0x1F))
	fw := int(float32(width) * clamp01(frac))
	if fw > 0 {
		fillRR(gtx, image.Rect(0, 0, fw, h), rad, col)
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
	height := Snap(gtx, 22)
	col := colOk
	if s.cyan {
		col = colAccent
	}
	sliderTrack(gtx, image.Rect(0, 0, width, height), s.f.Value, col)
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

// ---------- 开关 ----------

// switchBox 开关(兼容旧的 widget.Bool 调用点):内部用 comp.go 的 42×24 圆滑轨开关。
// 页面只要持有 *widget.Bool 即可,动画与像素吸附都由组件负责。
func switchBox(gtx C, b *widget.Bool) D {
	return switchBoxAnimated(gtx, b)
}

// switchAnims 每个 *widget.Bool 对应一份动画状态(按指针复用,页面无感)。
// 用 sync.Map 之外的简单 map 即可:只在 UI 线程访问。
var switchAnims = map[*widget.Bool]*toggle{}

// switchBoxAnimated 用带缓动动画的开关绘制 widget.Bool,并把点击写回 bool。
// 状态的唯一真相是 *widget.Bool:动画组件只读它、并在点击后写回。
func switchBoxAnimated(gtx C, b *widget.Bool) D {
	t := switchAnims[b]
	if t == nil {
		t = &toggle{}
		switchAnims[b] = t
	}
	t.clk = *b // 以调用方的 bool 为准(快照可能已改过值)
	d := t.layout(gtx)
	*b = t.clk // 回写点击结果
	return d
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
	rad := Snap(gtx, rCtl)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			if d.clk.Clicked(gtx) {
				d.open = !d.open
			}
			s := ""
			if d.sel >= 0 && d.sel < len(d.opts) {
				s = d.opts[d.sel]
			}
			arrow := "▾"
			if d.open {
				arrow = "▴"
			}
			textD, call := txtDims(gtx, s, textStyle{Size: fsBody2, Col: colText})
			arrowD, acall := txtDims(gtx, arrow, textStyle{Size: fsSmall, Col: colText3})
			h := Snap(gtx, ctlH)
			r := image.Rect(0, 0, w, h)
			// 内嵌输入底 + 半透明描边;悬停时描边提亮
			bd := colLine
			if d.clk.Hovered() {
				bd = colLineHi
			}
			fillRR(gtx, r, rad, bd)
			fillRR(gtx, r.Inset(Snap(gtx, 1)), rad-Snap(gtx, 1), colSunken)
			replayText(gtx, call, Snap(gtx, sp10), (h-textD.Size.Y)/2)
			replayText(gtx, acall, w-arrowD.Size.X-Snap(gtx, sp8), (h-arrowD.Size.Y)/2)
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
			rowH := Snap(gtx, 30)
			h := rowH * rows
			r := image.Rect(0, 0, w, h)
			fillRR(gtx, r, rad, colLine)
			fillRR(gtx, r.Inset(Snap(gtx, 1)), rad-Snap(gtx, 1), colSunken)
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
						bg = alpha(colAccent, 0x22)
					} else if d.optClks[i].Hovered() {
						bg = alpha(colText, 0x0A)
					}
					rr := image.Rect(0, 0, w, rowH)
					if bg.A != 0 {
						fillRR(gtx, rr.Inset(Snap(gtx, 2)), rad-Snap(gtx, 2), bg)
					}
					fg := colText
					if hl {
						fg = colAccent
					}
					td, call := txtDims(gtx, d.opts[i], textStyle{Size: fsBody2, Col: fg})
					replayText(gtx, call, Snap(gtx, sp10), (rowH-td.Size.Y)/2)
					return D{Size: image.Pt(w, rowH)}
				})
			})
			return D{Size: image.Pt(w, h)}
		}))
}

// ---------- 徽章 ----------

// badge 状态徽章(ok/warn/err/默认):同色微底 + 胶囊圆角,取代 1px 直角描边框。
func badge(gtx C, txt, kind string) D {
	col := colText3
	switch kind {
	case "ok":
		col = colOk
	case "warn":
		col = colWarn
	case "err":
		col = colDanger
	}
	return pillBadge(gtx, txt, col)
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
	td, call := txtDims(gtx, g.toastS.msg, textStyle{Size: fsBody, Col: colText})
	padX, padY := Snap(gtx, sp16), Snap(gtx, sp10)
	w := td.Size.X + 2*padX
	h := td.Size.Y + 2*padY
	m := Snap(gtx, sp16)
	x, y := gtx.Constraints.Max.X-w-m, gtx.Constraints.Max.Y-h-m
	r := image.Rect(x, y, x+w, y+h)
	rad := Snap(gtx, rCtl)
	softShadow(gtx, r, rad)
	fillRR(gtx, r, rad, alpha(colAccent, 0x55))
	fillRR(gtx, r.Inset(Snap(gtx, 1)), rad-Snap(gtx, 1), colHover)
	replayText(gtx, call, x+padX, y+padY)
}

// ---------- 侧栏图标(矢量描边,对齐 Web 的内联 SVG) ----------

// iconHoleBG 图标描边挖空色覆盖(调用方按所在背景设置,默认卡片底)。
var iconHoleBG *color.NRGBA

// drawIconOn 在指定背景色上绘制图标(挖空色与之匹配,避免出现色块)。
func drawIconOn(gtx C, name string, size int, c color.NRGBA, bg color.NRGBA) D {
	old := iconHoleBG
	iconHoleBG = &bg
	d := drawIcon(gtx, name, size, c)
	iconHoleBG = old
	return d
}

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
	// 挖空色跟随当前背景(卡片头部/侧栏等),否则描边内部会出现错误色块
	iconHole := pal.Card
	if iconHoleBG != nil {
		iconHole = *iconHoleBG
	}
	ring := func(cx, cy, r float32) {
		fillCircle(gtx, cx*u, cy*u, r*u, c)
		fillCircle(gtx, cx*u, cy*u, (r-1.2)*u, iconHole)
	}
	rect := func(x, y, w, h float32) {
		fillArea(gtx, image.Rect(int(x*u), int(y*u), int((x+w)*u), int((y+h)*u)), c)
		m := int(1.2 * u)
		fillArea(gtx, image.Rect(int(x*u)+m, int(y*u)+m, int((x+w)*u)-m, int((y+h)*u)-m), iconHole)
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

// kvRow 一行键值(key 定宽弱色,value 主色):数值用次级文本,不抢标题的视觉权重。
func kvRow(gtx C, k, v string) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Baseline}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(96))
			return txt(gtx, k, textStyle{Size: fsBody2, Col: colText3})
		}),
		layout.Rigid(func(gtx C) D { return txt(gtx, v, textStyle{Size: fsBody2, Col: colText2}) }),
	)
}

// labelRow 带标签的控件行(标签定宽,后跟控件)。
func labelRow(gtx C, k string, w layout.Widget) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(96))
			return txt(gtx, k, textStyle{Size: fsBody2, Col: colText3})
		}),
		layout.Rigid(w),
	)
}

// ---------- 数字输入 ----------

// numField 单行数字输入框。
type numField struct {
	ed     widget.Editor
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

// numBox 数字输入框布局(固定宽,统一控件高与圆角)。
func numBox(gtx C, n *numField, wDp int) D {
	ed := material.Editor(g.th, &n.ed, "")
	ed.TextSize = unit.Sp(fsBody2)
	ed.Color = colText
	ed.SelectionColor = alpha(colAccent, 0x44)
	w := gtx.Dp(unit.Dp(wDp))
	h := Snap(gtx, ctlH)
	rad := Snap(gtx, rCtl)
	r := image.Rect(0, 0, w, h)
	bd := colLine
	if gtx.Focused(&n.ed) {
		bd = alpha(colAccent, 0x88)
	}
	fillRR(gtx, r, rad, bd)
	fillRR(gtx, r.Inset(Snap(gtx, 1)), rad-Snap(gtx, 1), colSunken)
	layout.Inset{Top: unit.Dp(sp6), Left: unit.Dp(sp10), Right: unit.Dp(sp10)}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Pt(maxI(w-gtx.Dp(2*sp10), 1), maxI(h-gtx.Dp(2*sp6), 1))
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

// drawBackdrop 背景分层:垂直渐变底 → 64dp 网格 → 两处极淡提亮 → 四边暗角。
// 全部用填充矩形/椭圆实现,单帧 op 数在 60 以内;不做离屏 tile,避免采样/平铺差异。
func drawBackdrop(gtx C) {
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return
	}
	// ① 垂直渐变底(#0B1018 → #070A0F),用 12 条横带近似
	const bands = 12
	top := color.NRGBA{R: 0x0B, G: 0x10, B: 0x18, A: 0xFF}
	bottom := colBg
	for i := 0; i < bands; i++ {
		y0 := h * i / bands
		y1 := h * (i + 1) / bands
		t := float32(i) / float32(bands-1)
		fillArea(gtx, image.Rect(0, y0, w, y1), mixNRGBA(top, bottom, t))
	}

	// ② 网格:整屏按 64dp 间距画 1px 弱线(逐行/逐列,不与视口尺寸求余,保证对齐无残块)
	const cell = 64
	gridCol := color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0x12}
	for y := cell; y < h; y += cell {
		hLine(gtx, image.Rect(0, y, w, y), gridCol)
	}
	for x := cell; x < w; x += cell {
		fillArea(gtx, image.Rect(x, 0, x+Snap(gtx, 1), h), gridCol)
	}

	// ③ 两处极淡径向光:只在上缘/下缘做极弱提亮,避免出现带硬边的色块
	radial(gtx, float32(w)*0.24, float32(h)*0.02, float32(w)*0.34, alpha(colAccent, 0x06))
	radial(gtx, float32(w)*0.82, float32(h)*0.98, float32(w)*0.30, alpha(colViolet, 0x05))

	// ④ 四边暗角:上/下/左/右各一条向内渐隐的暗带,压暗四周、聚焦中心
	const vig = 6
	vh := h / 14 // 上下暗带高度
	vw := w / 20 // 左右暗带宽度
	for i := 0; i < vig; i++ {
		f := 1 - float32(i)/float32(vig)
		a := uint8(float32(0x24) * f * f)
		if a == 0 {
			continue
		}
		col := color.NRGBA{A: a}
		fillArea(gtx, image.Rect(0, i*vh/vig, w, (i+1)*vh/vig), col)
		fillArea(gtx, image.Rect(0, h-(i+1)*vh/vig, w, h-i*vh/vig), col)
		fillArea(gtx, image.Rect(i*vw/vig, 0, (i+1)*vw/vig, h), col)
		fillArea(gtx, image.Rect(w-(i+1)*vw/vig, 0, w-i*vw/vig, h), col)
	}
}

// radial 用同心椭圆叠出柔和光斑(cx,cy 像素中心,r 为最大半径)。
// 裁剪必须就地开合(闭包内 defer),否则后续背景绘制会被限制在椭圆内(实测坑)。
func radial(gtx C, cx, cy, r float32, col color.NRGBA) {
	const layers = 6
	// 单层 alpha = 总量 / 层数:叠加后中心刚好等于设计值,不会叠成实心色块
	layerA := uint8(maxI(int(col.A)/layers, 1))
	for i := 0; i < layers; i++ {
		f := 1 - float32(i)/float32(layers)
		rr := r * f
		if rr < 2 {
			continue
		}
		rect := image.Rect(int(cx-rr), int(cy-rr*0.7), int(cx+rr), int(cy+rr*0.7))
		func() {
			defer clip.Ellipse(rect).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, alpha(col, layerA))
		}()
	}
}

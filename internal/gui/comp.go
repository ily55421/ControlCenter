// comp — 组件层:卡片、图标胶囊、开关、滑杆、分段、按钮、胶囊药丸、磁贴。
//
// 每个组件都是「值 + 内部状态」,由调用方持有指针跨帧保存状态。
// 交互控件只在需要动画的那几帧请求下一帧(op.InvalidateCmd),走完后自动停止,
// 静止时 GPU 占用为 0 —— 与全局「只有数据变化才重绘」的约定一致。
package gui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
)

// ---------- 卡片 ----------
// 卡片本体只有一个实现:theme.go 的 card()(柔和投影 + 半透明描边 + 16dp 圆角 + 顶边高光,
// 用「先测量、再真实布局」两遍绘制,避免 op.Record 回放导致的文字宏错位)。

// ---------- 图标胶囊 ----------

// iconChip 卡片头部的图标胶囊:同色 16% 底 + 同色描边 + 顶边高光(4 op)。
func iconChip(gtx C, name string, col color.NRGBA) D {
	s := Snap(gtx, iconChipSz)
	r := image.Rect(0, 0, s, s)
	rad := Snap(gtx, rSmall)
	fillRR(gtx, r, rad, alpha(col, 0x29))
	strokeRR(gtx, r, rad, Snap(gtx, 1), alpha(col, 0x44))
	topHi(gtx, r.Inset(Snap(gtx, 1)), rad, colTopHi)
	off := op.Offset(image.Pt(Snap(gtx, 5), Snap(gtx, 5))).Push(gtx.Ops)
	drawIconOn(gtx, name, 18, col, alpha(col, 0x29))
	off.Pop()
	return D{Size: image.Pt(s, s)}
}

// ---------- 开关 ----------

// toggle 只保存动画,输入控件由页面持有。
type toggle struct {
	anim   float32
	last   time.Time
	inited bool
}

const toggleDur = 220 * time.Millisecond

func (t *toggle) layout(gtx C, b *widget.Bool) D {
	b.Update(gtx)
	target := float32(0)
	if b.Value {
		target = 1
	}
	if !t.inited {
		t.inited, t.anim, t.last = true, target, gtx.Now // 首帧直接就位,避免开窗时整排滑块一起滑
	}
	if t.anim != target {
		// 按真实经过时间推进(与帧率无关):每帧走完剩余距离的 1/3 左右,
		// 220ms 后误差 < 2% 直接落位;落位后不再请求下一帧。
		dt := gtx.Now.Sub(t.last)
		if dt <= 0 || dt > time.Second {
			dt = 16 * time.Millisecond
		}
		step := float32(dt.Seconds()) / float32(toggleDur.Seconds())
		t.anim += (target - t.anim) * clamp01(step*2.2)
		if absF(target-t.anim) < 0.02 {
			t.anim = target
		}
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	t.last = gtx.Now
	return b.Layout(gtx, func(gtx C) D {
		w, h := Snap(gtx, switchW), Snap(gtx, switchH)
		r := image.Rect(0, 0, w, h)
		rad := h / 2
		// 轨道:关 → 内嵌灰;开 → 青色
		track := mixNRGBA(colSunken, colAccent, t.anim)
		fillRR(gtx, r, rad, track)
		strokeRR(gtx, r, rad, Snap(gtx, 1), mixNRGBA(colLine, colAccent, t.anim))
		if b.Hovered() {
			fillRR(gtx, r, rad, alpha(colText, 0x08))
		}
		// 滑块:白色圆 + 下方 1px 阴影
		kn := h - Snap(gtx, 6)
		kr := kn / 2
		pad := Snap(gtx, 3)
		x := pad + int(float32(w-kn-2*pad)*t.anim)
		y := (h - kn) / 2
		circle(gtx, float32(x+kr), float32(y+kr)+1.5, float32(kr), alpha(colBg, 0x66))
		circle(gtx, float32(x+kr), float32(y+kr), float32(kr), colText)
		return D{Size: image.Pt(w, h)}
	})
}

// circle 填充一个圆(坐标为像素)。
func circle(gtx C, cx, cy, r float32, col color.NRGBA) {
	if r <= 0 || col.A == 0 {
		return
	}
	ri := int(r + 0.5)
	rect := image.Rect(int(cx)-ri, int(cy)-ri, int(cx)+ri, int(cy)+ri)
	defer clip.Ellipse(rect).Push(gtx.Ops).Pop()
	fillArea(gtx, rect, col)
}

// ---------- 按钮 ----------

// btnKind 按钮分级:柔和 / 主操作 / 幽灵 / 危险。
type btnKind int

const (
	btnSoft    btnKind = iota // 柔和:半透明底 + 描边(默认次要操作)
	btnPrimary                // 主操作:青色实底
	btnGhost                  // 幽灵:仅文本,hover 才出底
	btnDanger                 // 危险:红色实底
)

// button 自绘按钮:内容(标签+内边距)撑出尺寸,底作为 Stack 的 Expanded 层铺满,
// 文字由 Stack 的居中对齐摆放 —— 不再"录制一遍、偏移回放一遍"。
func button(gtx C, clk *widget.Clickable, s string, kind btnKind, disabled bool) D {
	if disabled {
		gtx = gtx.Disabled()
	}
	body := func(gtx C) D {
		fg := colText
		bg := alpha(colText, 0x0A)
		bd := colLine
		switch kind {
		case btnPrimary:
			fg, bg, bd = colBg, colAccent, colAccent
		case btnDanger:
			fg, bg, bd = colBg, colDanger, colDanger
		case btnGhost:
			fg, bg, bd = colText2, color.NRGBA{}, color.NRGBA{}
			if clk.Hovered() {
				bg, fg = alpha(colText, 0x0C), colText
			}
		default:
			if clk.Hovered() {
				bg, bd, fg = alpha(colText, 0x14), colLineHi, colText
			}
		}
		if disabled {
			bg, bd, fg = alpha(bg, 0x40), alpha(bd, 0x40), alpha(fg, 0x50)
		}

		rad := Snap(gtx, rCtl)
		pressed := !disabled && clk.Pressed()
		return layout.Stack{Alignment: layout.Center}.Layout(gtx,
			// 底:尺寸取 Expanded 层的 Min 约束(= 标签+内边距撑出的内容尺寸)
			layout.Expanded(func(gtx C) D {
				sz := gtx.Constraints.Min
				if min := gtx.Dp(unit.Dp(72)); sz.X < min {
					sz.X = min
				}
				sz.Y = gtx.Dp(unit.Dp(btnH))
				r := image.Rect(0, 0, sz.X, sz.Y)
				if bg.A != 0 {
					fillRR(gtx, r, rad, bg)
				}
				if bd.A != 0 {
					strokeRR(gtx, r, rad, Snap(gtx, 1), bd)
				}
				if pressed {
					fillRR(gtx, r, rad, alpha(colBg, 0x22))
				}
				return D{Size: sz}
			}),
			layout.Stacked(func(gtx C) D {
				return layout.Inset{Left: 14, Right: 14, Top: 4, Bottom: 4}.Layout(gtx,
					func(gtx C) D { return txt(gtx, s, textStyle{Size: fsBody2, Col: fg, Lines: 1}) })
			}),
		)
	}
	if disabled {
		return body(gtx)
	}
	return clk.Layout(gtx, body)
}

// ---------- 胶囊药丸 / 徽章 ----------

// pillBadge 状态徽章:同色 12% 底 + 同色描边 + 小字。
func pillBadge(gtx C, s string, col color.NRGBA) D {
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			sz := gtx.Constraints.Min
			r := image.Rect(0, 0, sz.X, sz.Y)
			rad := sz.Y / 2
			fillRR(gtx, r, rad, alpha(col, 0x1C))
			strokeRR(gtx, r, rad, Snap(gtx, 1), alpha(col, 0x38))
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			return layout.Inset{Left: 8, Right: 8, Top: 2, Bottom: 2}.Layout(gtx,
				func(gtx C) D { return txt(gtx, s, textStyle{Size: fsTiny, Col: col, W: font.Medium}) })
		}),
	)
}

// ---------- 分段 ----------

// segmented 等分分段控件;选中态用「青色底 + 青描边」而非仅文字变色。
type segmented struct {
	clks []widget.Clickable
	sel  int
}

// Layout 排布分段控件,返回被点中的下标(-1 表示本帧无点击)。
func (s *segmented) Layout(gtx C, labels []string) (D, int) {
	if len(labels) == 0 {
		return D{}, -1
	}
	if len(s.clks) != len(labels) {
		s.clks = make([]widget.Clickable, len(labels))
	}
	clicked := -1
	h := gtx.Dp(unit.Dp(ctlH))
	total := gtx.Constraints.Max.X
	rad := Snap(gtx, rCtl)
	r := image.Rect(0, 0, total, h)
	fillRR(gtx, r, rad, colSunken)
	strokeRR(gtx, r, rad, Snap(gtx, 1), colLine)
	gtx.Constraints = layout.Exact(image.Pt(total, h))

	var kids []layout.FlexChild
	for i := range labels {
		i := i
		kids = append(kids, layout.Flexed(1, func(gtx C) D {
			if s.clks[i].Clicked(gtx) {
				s.sel = i
				clicked = i
			}
			sz := image.Pt(gtx.Constraints.Min.X, h)
			gtx.Constraints = layout.Exact(sz)
			return s.clks[i].Layout(gtx, func(gtx C) D {
				gtx.Constraints = layout.Exact(sz)
				st := textStyle{Size: fsBody2, Col: colText3, Lines: 1}
				if i == s.sel {
					st.Col, st.W = colAccent, font.Medium
				} else if s.clks[i].Hovered() {
					st.Col = colText
				}
				return layout.Stack{Alignment: layout.Center}.Layout(gtx,
					layout.Expanded(func(gtx C) D {
						if i == s.sel {
							rr := image.Rect(0, 0, sz.X, sz.Y).Inset(Snap(gtx, 2))
							fillRR(gtx, rr, rad-Snap(gtx, 2), alpha(colAccent, 0x1C))
							strokeRR(gtx, rr, rad-Snap(gtx, 2), Snap(gtx, 1),
								alpha(colAccent, 0x77))
						}
						return D{Size: sz}
					}),
					layout.Stacked(func(gtx C) D { return txt(gtx, labels[i], st) }),
				)
			})
		}))
	}
	layout.Flex{Axis: layout.Horizontal}.Layout(gtx, kids...)
	return D{Size: image.Pt(total, h)}, clicked
}

// ---------- 滑杆轨道 ----------

// sliderTrack 绘制轨道 + 已填充段 + 光晕滑块(3 层圆),返回拇指中心 x(像素)。
func sliderTrack(gtx C, r image.Rectangle, frac float32, col color.NRGBA) int {
	frac = clamp01(frac)
	h := Snap(gtx, 5)
	cy := r.Min.Y + r.Dy()/2
	track := image.Rect(r.Min.X, cy-h/2, r.Max.X, cy-h/2+h)
	rad := h / 2
	fillRR(gtx, track, rad, colSunken)
	fillRR(gtx, track, rad, alpha(colLine, 0x1F))
	fw := int(float32(track.Dx()) * frac)
	if fw > 0 {
		fillRR(gtx, image.Rect(track.Min.X, track.Min.Y, track.Min.X+fw, track.Max.Y), rad, col)
	}
	// 拇指:光晕 → 白心
	tr := Snap(gtx, 7)
	if r.Dx() < 2*tr+2 {
		return r.Min.X
	}
	tx := clampI(r.Min.X+fw, r.Min.X+tr, r.Max.X-tr)
	circle(gtx, float32(tx), float32(cy), float32(tr+gtx.Dp(unit.Dp(2))), alpha(col, 0x44))
	circle(gtx, float32(tx), float32(cy), float32(tr), colText)
	return tx
}

// elide 简单截断(供定宽文本用)。
func elide(s string, n int) string {
	if n <= 0 {
		return s
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// healthCol 语义色名 → 颜色(badge/pillBadge 共用)。
func healthCol(kind string) color.NRGBA {
	switch kind {
	case "ok":
		return colOk
	case "warn":
		return colWarn
	case "err":
		return colDanger
	}
	return colText3
}

// ---------- 页头 / 页脚 ----------

// pageHead 页面标题区:大标题 + 副标题(说明该页做什么) + 右侧徽标,
// 把原来"标题与正文同色同重"的信息层级问题一次性修掉。
func pageHead(gtx C, title, sub string, right layout.Widget) D {
	return layout.Inset{Bottom: unit.Dp(sp16)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return txt(gtx, title, textStyle{Size: fsTitle, Col: colText, W: font.Medium})
					}),
					layout.Rigid(func(gtx C) D {
						if sub == "" {
							return D{}
						}
						return layout.Inset{Top: unit.Dp(sp4)}.Layout(gtx, func(gtx C) D {
							return txt(gtx, sub, textStyle{Size: fsSmall, Col: colText3})
						})
					}),
				)
			}),
			layout.Flexed(1, func(gtx C) D { return D{Size: gtx.Constraints.Min} }), // 弹性占位:把右侧内容推到右端
			layout.Rigid(func(gtx C) D {
				if right == nil {
					return D{}
				}
				return right(gtx)
			}),
		)
	})
}

// pageFooter 页脚信息条:收住页面底部,同时承载实时状态摘要。
// 左右两段用 SpaceBetween 分配,左段单行截断 —— 原先各自按绝对坐标画,
// 左串一长就叠到右串上(实测底部出现两行字互相压字的乱码)。
func pageFooter(gtx C, left, right string) D {
	return layout.Inset{Top: sp8}.Layout(gtx, func(gtx C) D {
		w := gtx.Constraints.Max.X
		h := Snap(gtx, footH)
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D {
				fillRR(gtx, image.Rect(0, 0, w, h), Snap(gtx, rChip), alpha(colText, 0x06))
				hLine(gtx, image.Rect(0, 0, w, 0), colLineSoft)
				return D{Size: image.Pt(w, h)}
			}),
			layout.Expanded(func(gtx C) D {
				return layout.Inset{Left: sp12, Right: sp12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return txt(gtx, left, textStyle{Size: fsTiny, Col: colText4, Lines: 1})
						}),
						layout.Rigid(func(gtx C) D {
							if right == "" {
								return D{}
							}
							return txt(gtx, right, textStyle{Size: fsTiny, Col: colText4, Lines: 1})
						}),
					)
				})
			}),
		)
	})
}

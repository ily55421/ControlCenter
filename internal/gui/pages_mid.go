// pages_mid — LED 设备 / FlexiKey / FlexiCharger 三页。
package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/unit"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"controlcenter/internal/keyboard"
)

// actDur 动作后刷新的标准延迟。
const actDur = 400 * time.Millisecond

// ---------- LED 设备 ----------

func (g *GUI) pageKB(gtx C) D {
	s := g.kb.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return label(gtx, "正在读取键盘灯状态…", 14, pal.Dim) })
	}
	if g.w.kbOn.Update(gtx) {
		m := 0
		if g.w.kbOn.Value {
			m = 1
		}
		g.run("已下发", pgKB, actDur, func() error { return g.be.KBCmd(KBRequest{Cmd: "onoff", Mode: m}) })
	}
	if g.w.kbOvr.Update(gtx) {
		v := 0
		if g.w.kbOvr.Value {
			v = 1
		}
		g.run("已下发", pgKB, actDur, func() error { return g.be.KBCmd(KBRequest{Cmd: "override", Override: &v}) })
	}
	if g.w.kbSleepOff.Clicked(gtx) {
		g.run("睡眠计时器已禁用", pgKB, actDur,
			func() error { return g.be.KBCmd(KBRequest{Cmd: "sleep", Mode: 0}) })
	}
	if g.w.kbSleepOn.Clicked(gtx) {
		sec := g.w.slH.value()*3600 + g.w.slM.value()*60 + g.w.slS.value()
		if sec < 5 {
			sec = 5
		}
		g.run("睡眠计时器已启用", pgKB, actDur,
			func() error { return g.be.KBCmd(KBRequest{Cmd: "sleep", Mode: 1, Seconds: sec}) })
	}
	if g.w.kbApplyEff.Clicked(gtx) {
		eff := kbEffVals[g.w.kbEff.selected()]
		g.run("灯效已应用", pgKB, actDur,
			func() error { return g.be.KBCmd(KBRequest{Cmd: "effect", Effect: eff}) })
	}
	if g.w.kbReset.Clicked(gtx) {
		g.run("已恢复默认", pgKB, actDur, func() error { return g.be.KBCmd(KBRequest{Cmd: "reset"}) })
	}
	g.w.kbBr.syncExt(s.Brightness)
	// 色环交互(点击取色并下发所选分区)
	g.wheelLayout(gtx, 250)

	zone := [3]color.NRGBA{nrgba(s.LeftColor), nrgba(s.MidColor), nrgba(s.RightColor)}
	var zoneP *[3]color.NRGBA
	if s.Type > 1 {
		zoneP = &zone
	}

	return grid2(gtx, 4, 5,
		func(gtx C) D {
			return col(gtx,
				func(gtx C) D {
					return panel(gtx, "键盘睡眠计时器", func(gtx C) D {
						return col(gtx,
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return label(gtx, "状态", 13, pal.Dim) },
									hSpacer(12),
									func(gtx C) D { return actBtn(gtx, &g.w.kbSleepOff, "禁用", btnPlain, s.SleepTrigger) },
									hSpacer(10),
									func(gtx C) D { return actBtn(gtx, &g.w.kbSleepOn, "启用", btnPlain, !s.SleepTrigger) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return label(gtx, "时 / 分 / 秒", 13, pal.Dim) },
									hSpacer(8),
									func(gtx C) D { g.w.slH.sync(gtx, 0); return numBox(gtx, &g.w.slH, 64) },
									func(gtx C) D { return label(gtx, ":", 13, pal.Dim) },
									func(gtx C) D { g.w.slM.sync(gtx, 0); return numBox(gtx, &g.w.slM, 64) },
									func(gtx C) D { return label(gtx, ":", 13, pal.Dim) },
									func(gtx C) D { g.w.slS.sync(gtx, s.SleepSeconds%60); return numBox(gtx, &g.w.slS, 64) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return label(gtx, "覆盖引导效果", 13, pal.Dim) },
									hSpacer(10), func(gtx C) D { return switchBox(gtx, &g.w.kbOvr) })
							})
					})
				},
				func(gtx C) D {
					return panel(gtx, "彩色背光", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return g.wheelLayout(gtx, 250) },
							vSpacer(14),
							func(gtx C) D { return centerRow(gtx, func(gtx C) D { return g.w.kbPart.layout(gtx) }) },
							vSpacer(10),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return label(gtx, "亮度", 13, pal.Dim) },
									hSpacer(10), func(gtx C) D { return label(gtx, "OFF", 12, pal.Dim) },
									hSpacer(8),
									layout.Flexed(1, func(gtx C) D { return g.w.kbBr.layout(gtx) }),
									hSpacer(8),
									func(gtx C) D { return label(gtx, fmt.Sprintf("%d", g.w.kbBr.value()), 14, pal.Lime) },
									hSpacer(4), func(gtx C) D { return label(gtx, "4", 12, pal.Dim) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return label(gtx, "灯效", 13, pal.Dim) },
									hSpacer(10), func(gtx C) D { return g.w.kbEff.layout(gtx) },
									hSpacer(12),
									func(gtx C) D { return actBtn(gtx, &g.w.kbApplyEff, "应用灯效", btnPri, false) },
									hSpacer(10),
									func(gtx C) D { return actBtn(gtx, &g.w.kbReset, "恢复默认", btnPlain, false) })
							},
							vSpacer(6),
							func(gtx C) D { return label(gtx, "点击色环取色并下发到所选分区(WMI 103 · 0xF0-0xF6)", 12, pal.Dim) })
					})
				})
		},
		func(gtx C) D {
			return panel(gtx, "LED 键盘", func(gtx C) D {
				return col(gtx,
					func(gtx C) D { return kbLayout(gtx, zoneP) },
					vSpacer(12),
					func(gtx C) D {
						return row(gtx,
							func(gtx C) D { return label(gtx, kbTypeName(int(s.Type)), 12, pal.Dim) },
							layout.Flexed(1, func(gtx C) D { return D{} }),
							func(gtx C) D { return switchBox(gtx, &g.w.kbOn) },
							hSpacer(8),
							func(gtx C) D { return label(gtx, "灯光开关", 13, pal.Dim) })
					})
			})
		})
}

// wheelLayout 色环布局 + 指针交互(按下取色并下发)。
func (g *GUI) wheelLayout(gtx C, sizeDp int) D {
	s := gtx.Dp(unit.Dp(sizeDp))
	hex := g.wheelHex
	if hex == "" {
		hex = "点击取色"
	}
	var sel *float64
	if g.wheelAngle > 0 {
		sel = &g.wheelAngle
	}
	dims := colorWheel(gtx, sizeDp, kbPartNames[g.w.kbPart.selected()], hex, sel)
	// 指针输入区(裁剪到色环矩形;v0.10 输入模型:声明区域 + Filter 拉取)
	defer clip.Rect(image.Rect(0, 0, s, s)).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &g.wheelTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &g.wheelTag, Kinds: pointer.Press | pointer.Drag})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok || pe.Kind != pointer.Press {
			continue
		}
		if ang, ok := wheelAngle(s, int(pe.Position.X), int(pe.Position.Y)); ok {
			g.wheelAngle = ang
			r, gg, b := hsl2rgb(ang/(2*math.Pi), 1, 0.5)
			g.wheelHex = fmt.Sprintf("rgb(%d,%d,%d)", r, gg, b)
			part := kbPartVals[g.w.kbPart.selected()]
			cc := part
			rr, ccc, bb := r, gg, b
			g.run("已下发", -1, 0, func() error {
				return g.be.KBCmd(KBRequest{Cmd: "color", Part: cc, R: rr, G: ccc, B: bb})
			})
		}
	}
	return dims
}

// nrgba keyboard.Color → color.NRGBA。
func nrgba(c keyboard.Color) color.NRGBA {
	return color.NRGBA{R: uint8(c.R), G: uint8(c.G), B: uint8(c.B), A: 0xFF}
}

// ---------- FlexiKey ----------

func (g *GUI) pageFlexikey(gtx C) D {
	s := g.fk.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return label(gtx, "正在读取 profile.xml…", 14, pal.Dim) })
	}
	// 配置档按钮与快照对齐
	if g.w.fkProfN != len(s.Profiles) {
		g.w.fkProf = make([]widget.Clickable, len(s.Profiles))
		g.w.fkProfN = len(s.Profiles)
	}
	for i := range s.Profiles {
		i := i
		if g.w.fkProf[i].Clicked(gtx) {
			name := s.Profiles[i].Name
			g.run("已选中 "+name, pgFlexikey, actDur,
				func() error { return g.be.FlexikeyApply("select", name) })
		}
	}
	if g.w.fkApply.Clicked(gtx) {
		g.run("宏键配置已应用", pgFlexikey, 500*time.Millisecond, func() error { return g.be.FlexikeyApply("apply", "") })
	}
	if g.w.fkDisable.Clicked(gtx) {
		g.run("已停用宏键", pgFlexikey, 500*time.Millisecond, func() error { return g.be.FlexikeyApply("disable", "") })
	}
	if g.w.fkStatTab.Clicked(gtx) {
		g.toast("统计:驱动日志接口已封装,UI 待开放")
	}
	if g.w.fkMouseTab.Clicked(gtx) {
		g.toast("鼠标宏:配置经 profile.xml 的 Device=2 槽位管理")
	}

	pathTxt := "未创建(应用后生成)"
	if s.Exists {
		pathTxt = s.Path
	}
	kbKind, mouseKind := "", ""
	if s.KBEnabled {
		kbKind = "ok"
	}
	if s.MouseEnabled {
		mouseKind = "ok"
	}
	return col(gtx,
		func(gtx C) D {
			return centerRow(gtx,
				func(gtx C) D { return drawTab(gtx, &g.w.fkMainTab, "键盘宏", true, false, true) },
				func(gtx C) D { return drawTab(gtx, &g.w.fkStatTab, "统计", false, false, true) },
				func(gtx C) D { return drawTab(gtx, &g.w.fkMouseTab, "鼠标宏", false, false, true) })
		},
		vSpacer(10),
		func(gtx C) D {
			return grid2(gtx, 4, 5,
				func(gtx C) D {
					return panel(gtx, "配置档", func(gtx C) D {
						profBtns := make([]layout.Widget, 0, len(s.Profiles))
						for i := range s.Profiles {
							i := i
							profBtns = append(profBtns, func(gtx C) D {
								v := btnPlain
								if s.Profiles[i].Selected {
									v = btnWarn
								}
								return actBtn(gtx, &g.w.fkProf[i], s.Profiles[i].Name, v, false)
							})
						}
						return col(gtx,
							func(gtx C) D { return centerRow(gtx, profBtns...) },
							vSpacer(8),
							func(gtx C) D { return centerLabel(gtx, "文件:"+pathTxt+" · 与原版 CC.exe 格式兼容") },
							vSpacer(16),
							func(gtx C) D { return centerRow(gtx, func(gtx C) D { return fnBsChip(gtx) }) },
							vSpacer(8),
							func(gtx C) D { return centerLabel(gtx, "使用 Fn+Backspace 启用或禁用 Flexikey") },
							vSpacer(16),
							func(gtx C) D {
								return centerRow(gtx,
									func(gtx C) D { return actBtn(gtx, &g.w.fkDisable, "禁用", btnPlain, false) },
									hSpacer(12),
									func(gtx C) D { return actBtn(gtx, &g.w.fkApply, "启用/应用", btnPri, false) })
							},
							vSpacer(10),
							func(gtx C) D {
								return centerRow(gtx,
									func(gtx C) D { return badge(gtx, "键盘过滤 "+sel2(s.KBEnabled, "开", "关"), kbKind) },
									hSpacer(10),
									func(gtx C) D { return badge(gtx, "鼠标过滤 "+sel2(s.MouseEnabled, "开", "关"), mouseKind) })
							})
					})
				},
				func(gtx C) D {
					return panel(gtx, "键盘宏槽位", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return kbLayout(gtx, nil) },
							vSpacer(8),
							func(gtx C) D {
								return label(gtx, "宏键明细编辑请直接修改 profile.xml(键位/宏序列/文本宏/启动程序),此处选择配置档并下发到 HKFltr 过滤驱动。", 12, pal.Dim)
							})
					})
				})
		})
}

func sel2(b bool, a, c string) string {
	if b {
		return a
	}
	return c
}

// ---------- FlexiCharger ----------

func (g *GUI) pageCharger(gtx C) D {
	s := g.charger.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return label(gtx, "正在读取充电配置…", 14, pal.Dim) })
	}
	fc := s.Pref.FlexiCharger
	for i := 0; i < 3; i++ {
		i := i
		if g.w.fcMode[i].Clicked(gtx) {
			switch i {
			case 0:
				g.run("最大电池电量已下发", pgCharger, actDur, func() error { return g.be.SetFlexiCharger(0, 95, 100) })
			case 1:
				g.run("推荐电池充电已下发", pgCharger, actDur, func() error { return g.be.SetFlexiCharger(1, 70, 80) })
			}
			// 2 = 自定义:仅选中,调整滑杆后点"应用"
		}
	}
	g.w.fcStart.syncExt(fc.Start)
	g.w.fcStop.syncExt(fc.Stop)
	if g.w.fcApply.Clicked(gtx) {
		switch fc.Mode {
		case 0:
			g.run("FlexiCharger 已下发并记住", pgCharger, actDur, func() error { return g.be.SetFlexiCharger(0, 95, 100) })
		case 1:
			g.run("FlexiCharger 已下发并记住", pgCharger, actDur, func() error { return g.be.SetFlexiCharger(1, 70, 80) })
		default:
			st, sp := g.w.fcStart.value(), g.w.fcStop.value()
			g.run("FlexiCharger 已下发并记住", pgCharger, actDur,
				func() error { return g.be.SetFlexiCharger(2, st, sp) })
		}
	}
	modeLabel := [3]string{"最大电池电量", "推荐电池充电", "自定义电池充电"}
	modeDesc := [3]string{"持续将电池充电至 100%", "低于 70% 充电,80% 停止", "设置启动和停止充电阈值"}

	return grid2(gtx, 5, 4,
		func(gtx C) D {
			return panel(gtx, "电池充电选项", func(gtx C) D {
				rows := make([]layout.Widget, 0, 7)
				for i := 0; i < 3; i++ {
					i := i
					rows = append(rows, func(gtx C) D {
						return g.fcRadioRow(gtx, &g.w.fcMode[i], i == fc.Mode, modeLabel[i], modeDesc[i])
					}, vSpacer(6))
				}
				rows = append(rows,
					vSpacer(14),
					func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceAround}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return col(gtx,
									func(gtx C) D { return label(gtx, "容量不足时开始充电", 12, pal.Dim) },
									func(gtx C) D { return label(gtx, fmt.Sprintf("%d", g.w.fcStart.value()), 34, pal.Lime) },
									func(gtx C) D {
										gtx.Constraints.Min.X = gtx.Dp(180)
										return g.w.fcStart.layout(gtx)
									})
							}),
							layout.Rigid(func(gtx C) D {
								return col(gtx,
									func(gtx C) D { return label(gtx, "容量达到时停止充电", 12, pal.Dim) },
									func(gtx C) D { return label(gtx, fmt.Sprintf("%d", g.w.fcStop.value()), 34, pal.Lime) },
									func(gtx C) D {
										gtx.Constraints.Min.X = gtx.Dp(180)
										return g.w.fcStop.layout(gtx)
									})
							}))
					},
					vSpacer(10),
					func(gtx C) D {
						return row(gtx, func(gtx C) D { return actBtn(gtx, &g.w.fcApply, "应用", btnPri, false) },
							hSpacer(12),
							func(gtx C) D { return label(gtx, "下发:WMI4 sub31 · 记住选择,开机自动回写 EC", 12, pal.Dim) })
					})
				return col(gtx, rows...)
			})
		},
		func(gtx C) D {
			return panel(gtx, "电池状态", func(gtx C) D {
				hasBat := s.Bat.Design > 0
				return col(gtx,
					func(gtx C) D {
						txt := "— 本机无电池管理支持位 —"
						if hasBat {
							txt = "已插入电源"
						}
						td, call := textDims(gtx, txt, 13, pal.Lime)
						replayText(gtx, call, (gtx.Constraints.Max.X-td.Size.X)/2, 0)
						return D{Size: image.Pt(gtx.Constraints.Max.X, td.Size.Y)}
					},
					vSpacer(8),
					func(gtx C) D {
						return layout.Center.Layout(gtx, func(gtx C) D { return batteryOutline(gtx, s.Bat.CapacityPct, hasBat) })
					},
					vSpacer(14),
					func(gtx C) D {
						return dialSized(gtx, 220, 200, float32(fc.Stop-40), 60, dialOpt{
							ticks: 30, color: pal.Lime, labels: []string{"40", "60", "80", "100"}, center: fmt.Sprintf("%d%%", fc.Stop)})
					},
					func(gtx C) D { return centerLabel(gtx, fmt.Sprintf("阈值区间 %d%% → %d%%", fc.Start, fc.Stop)) })
			})
		})
}

// fcRadioRow 单选行(圆点 + 标签 + 描述)。
func (g *GUI) fcRadioRow(gtx C, clk *widget.Clickable, on bool, lbl, desc string) D {
	return clk.Layout(gtx, func(gtx C) D {
		dotCol := color.NRGBA{R: 0x59, G: 0x63, B: 0x6D, A: 0xFF}
		if on {
			dotCol = pal.Cyan
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				fillCircle(gtx, float32(gtx.Dp(8)), float32(gtx.Dp(8)), float32(gtx.Dp(8)), dotCol)
				if on {
					fillCircle(gtx, float32(gtx.Dp(8)), float32(gtx.Dp(8)), px(gtx, 3.5), pal.Panel)
				}
				return D{Size: image.Pt(gtx.Dp(16), gtx.Dp(16))}
			}),
			layout.Rigid(hSpacer(10)),
			layout.Rigid(func(gtx C) D { return label(gtx, lbl, 13, pal.Txt) }),
			layout.Rigid(hSpacer(12)),
			layout.Rigid(func(gtx C) D { return label(gtx, desc, 12, pal.Dim) }),
		)
	})
}

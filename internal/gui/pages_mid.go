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
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"controlcenter/internal/keyboard"
)

// actDur 动作后刷新的标准延迟。
const actDur = 400 * time.Millisecond

// ---------- LED 设备 ----------

func (g *GUI) pageKB(gtx C) D {
	s := g.kb.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D {
			return txt(gtx, "正在读取键盘灯状态…", textStyle{Size: fsBig, Col: colText3})
		})
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
	// 色环交互在下方面板内 wheelLayout 一并处理(此处不再调用,避免色环双绘)

	zone := [3]color.NRGBA{nrgba(s.LeftColor), nrgba(s.MidColor), nrgba(s.RightColor)}
	var zoneP *[3]color.NRGBA
	if s.Type > 1 {
		zoneP = &zone
	}

	// 单列布局:双栏在特定窗口尺寸下触发 panel 宏内容跨栏/双绘(kbLayout/色环),
	// 改为纵向全宽面板,确定性收敛。
	return spreadCol(gtx,
		func(gtx C) D {
			return pageHead(gtx, "LED 设备", "分区取色 / 灯效 / 亮度与键盘睡眠计时器", func(gtx C) D {
				return pillBadge(gtx, kbTypeName(int(s.Type)), colAccent)
			})
		},
		func(gtx C) D {
			return col(gtx,
				func(gtx C) D {
					return panel(gtx, "键盘睡眠计时器", func(gtx C) D {
						return col(gtx,
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "状态", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(12),
									func(gtx C) D { return actBtn(gtx, &g.w.kbSleepOff, "禁用", btnPlain, s.SleepTrigger) },
									hSpacer(10),
									func(gtx C) D { return actBtn(gtx, &g.w.kbSleepOn, "启用", btnPlain, !s.SleepTrigger) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "时 / 分 / 秒", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(8),
									func(gtx C) D { g.w.slH.sync(gtx, 0); return numBox(gtx, &g.w.slH, 64) },
									func(gtx C) D { return txt(gtx, ":", textStyle{Size: fsBody, Col: colText3}) },
									func(gtx C) D { g.w.slM.sync(gtx, 0); return numBox(gtx, &g.w.slM, 64) },
									func(gtx C) D { return txt(gtx, ":", textStyle{Size: fsBody, Col: colText3}) },
									func(gtx C) D { g.w.slS.sync(gtx, s.SleepSeconds%60); return numBox(gtx, &g.w.slS, 64) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "覆盖引导效果", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(10), func(gtx C) D { return switchBox(gtx, &g.w.kbOvr) })
							})
					})
				},
				func(gtx C) D {
					return panel(gtx, "彩色背光", func(gtx C) D {
						// 设置区保持紧凑宽度:分区/色环/亮度/灯效组成一列控件,
						// 不随超宽卡片散开(方案 4.2)。
						if maxW := gtx.Dp(ledSettingsW); gtx.Constraints.Max.X > maxW {
							gtx.Constraints.Max.X = maxW
							if gtx.Constraints.Min.X > maxW {
								gtx.Constraints.Min.X = maxW
							}
						}
						return col(gtx,
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "分区", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(8), func(gtx C) D { return g.w.kbPart.layout(gtx) })
							},
							vSpacer(8),
							func(gtx C) D {
								// 色环直径取令牌值,窄卡片时按可用宽度收缩;绘制与命中用同一尺寸
								return layout.Center.Layout(gtx, func(gtx C) D { return g.wheelLayout(gtx, g.wheelDp(gtx)) })
							},
							vSpacer(8),
							func(gtx C) D {
								// 亮度行:OFF ↔ 当前值 + 滑块。单列宽卡片里若跟随容器,
								// 滑块会被拉到上千 dp,与色环比例严重失衡(P0)。
								if w := gtx.Constraints.Max.X; w > gtx.Dp(sliderMaxW) {
									gtx.Constraints.Max.X = gtx.Dp(sliderMaxW)
								}
								return col(gtx,
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return txt(gtx, "OFF", textStyle{Size: fsSmall, Col: colText3}) },
											layout.Flexed(1, func(gtx C) D { return D{} }),
											func(gtx C) D {
												return txt(gtx, fmt.Sprintf("%d / 4", g.w.kbBr.value()), textStyle{Size: fsBody2, Col: colOk})
											})
									},
									func(gtx C) D {
										gtx.Constraints.Min.X = gtx.Constraints.Max.X
										return g.w.kbBr.layout(gtx)
									})
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "灯效", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(8), func(gtx C) D { return g.w.kbEff.layout(gtx) },
									layout.Flexed(1, func(gtx C) D { return D{} }),
									func(gtx C) D { return actBtn(gtx, &g.w.kbApplyEff, "应用灯效", btnPri, false) },
									hSpacer(8),
									func(gtx C) D { return actBtn(gtx, &g.w.kbReset, "恢复默认", btnPlain, false) })
							},
							vSpacer(6),
							func(gtx C) D {
								return txt(gtx, "点击键帽选中分区,再点色环取色并立即下发", textStyle{Size: fsSmall, Col: colText3})
							})
					})
				})
		},
		func(gtx C) D {
			return panel(gtx, "LED 键盘", func(gtx C) D {
				return col(gtx,
					func(gtx C) D { return kbLayout(gtx, zoneP, &g.w.kbGrid, nil) },
					vSpacer(12),
					func(gtx C) D {
						return row(gtx,
							func(gtx C) D { return txt(gtx, kbTypeName(int(s.Type)), textStyle{Size: fsBody2, Col: colText3}) },
							layout.Flexed(1, func(gtx C) D { return D{} }),
							func(gtx C) D { return switchBox(gtx, &g.w.kbOn) },
							hSpacer(8),
							func(gtx C) D { return txt(gtx, "灯光开关", textStyle{Size: fsBody, Col: colText3}) })
					})
			})
		})
}

// wheelDp 色环直径:取令牌值,但不超出当前卡片可用宽度(窄卡片等比收缩)。
// 绘制与命中用同一尺寸,避免"看得见却点不到"。
func (g *GUI) wheelDp(gtx C) int {
	dp := wheelSz
	if pxPerDp := float32(gtx.Dp(1)); pxPerDp > 0 {
		if avail := gtx.Constraints.Max.X; avail > 0 {
			if maxDp := int(float32(avail)/pxPerDp) - sp16; maxDp < dp {
				dp = maxDp
			}
		}
	}
	if dp < 150 {
		dp = 150
	}
	return dp
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
	// 选中键优先:中心显示该键所属分区名
	zoneName := kbPartNames[g.w.kbPart.selected()]
	if g.w.kbGrid.SelR >= 0 {
		zoneName = "键:" + kbRows[g.w.kbGrid.SelR][g.w.kbGrid.SelC].label + " → " + kbPartNameOf(KBPartOf(g.w.kbGrid.SelR, g.w.kbGrid.SelC))
	}
	dims := colorWheel(gtx, sizeDp, zoneName, hex, sel)
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
			if g.w.kbGrid.SelR >= 0 {
				part = KBPartOf(g.w.kbGrid.SelR, g.w.kbGrid.SelC)
			}
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
		return panel(gtx, "正在加载", func(gtx C) D { return txt(gtx, "正在读取 profile.xml…", textStyle{Size: fsBig, Col: colText3}) })
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
			return pageHead(gtx, "FlexiKey", "键盘宏配置档与鼠标宏统计", func(gtx C) D {
				return pillBadge(gtx, sel2(s.KBEnabled, "键盘宏已启用", "键盘宏已停用"), healthCol(kbKind))
			})
		},
		func(gtx C) D {
			return centerRow(gtx,
				func(gtx C) D { return drawTab(gtx, &g.w.fkMainTab, "键盘宏", true, false, true) },
				func(gtx C) D { return drawTab(gtx, &g.w.fkStatTab, "统计", false, false, true) },
				func(gtx C) D { return drawTab(gtx, &g.w.fkMouseTab, "鼠标宏", false, false, true) })
		},
		vSpacer(10),
		func(gtx C) D {
			return fkSplit(gtx,
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
							func(gtx C) D { return kbLayout(gtx, nil, &g.w.kbGrid, nil) },
							vSpacer(8),
							func(gtx C) D {
								return txt(gtx, "宏键明细编辑请直接修改 profile.xml(键位/宏序列/文本宏/启动程序),此处选择配置档并下发到 HKFltr 过滤驱动。", textStyle{Size: fsBody2, Col: colText3})
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
		return panel(gtx, "正在加载", func(gtx C) D { return txt(gtx, "正在读取充电配置…", textStyle{Size: fsBig, Col: colText3}) })
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

	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "FlexiCharger", "充电模式与起停阈值", func(gtx C) D {
				return pillBadge(gtx, modeLabel[clampI(fc.Mode, 0, 2)], colOk)
			})
		},
		func(gtx C) D {
			return grid2Fill(gtx, 5, 4,
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
											func(gtx C) D { return txt(gtx, "容量不足时开始充电", textStyle{Size: fsBody2, Col: colText3}) },
											func(gtx C) D {
												return txt(gtx, fmt.Sprintf("%d", g.w.fcStart.value()), textStyle{Size: 34, Col: colOk})
											},
											func(gtx C) D {
												gtx.Constraints.Min.X = gtx.Dp(180)
												return g.w.fcStart.layout(gtx)
											})
									}),
									layout.Rigid(func(gtx C) D {
										return col(gtx,
											func(gtx C) D { return txt(gtx, "容量达到时停止充电", textStyle{Size: fsBody2, Col: colText3}) },
											func(gtx C) D { return txt(gtx, fmt.Sprintf("%d", g.w.fcStop.value()), textStyle{Size: 34, Col: colOk}) },
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
									func(gtx C) D {
										return txt(gtx, "应用后立即生效,开机时自动回写", textStyle{Size: fsBody2, Col: colText3})
									})
							})
						return col(gtx, rows...)
					})
				},
				func(gtx C) D {
					return panel(gtx, "电池状态", func(gtx C) D {
						hasBat := s.Bat.Design > 0
						return col(gtx,
							func(gtx C) D {
								msg := "— 本机无电池管理支持位 —"
								if hasBat {
									msg = "已插入电源"
								}
								if w := gtx.Constraints.Max.X; w > 0 {
									gtx.Constraints.Min.X = w
								}
								return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceAround}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										return txt(gtx, msg, textStyle{Size: fsBody, Col: colOk})
									}))
							},
							vSpacer(8),
							func(gtx C) D {
								return layout.Center.Layout(gtx, func(gtx C) D { return batteryOutline(gtx, s.Bat.CapacityPct, hasBat) })
							},
							vSpacer(14),
							func(gtx C) D {
								return dialSized(gtx, 220, 200, g.chDial.tick(float32(fc.Stop-40)), 60, dialOpt{
									ticks: 30, color: pal.Lime, labels: []string{"40", "60", "80", "100"}, center: fmt.Sprintf("%d%%", fc.Stop)})
							},
							func(gtx C) D { return centerLabel(gtx, fmt.Sprintf("阈值区间 %d%% → %d%%", fc.Start, fc.Stop)) })
					})
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
			layout.Rigid(func(gtx C) D { return txt(gtx, lbl, textStyle{Size: fsBody, Col: colText}) }),
			layout.Rigid(hSpacer(12)),
			layout.Rigid(func(gtx C) D { return txt(gtx, desc, textStyle{Size: fsBody2, Col: colText3}) }),
		)
	})
}

// kbPartNameOf 分区值 → 显示名。
func kbPartNameOf(part string) string {
	for i, v := range kbPartVals {
		if v == part {
			return kbPartNames[i]
		}
	}
	return part
}

// fkSplit FlexiKey 的「配置档 / 键盘槽位」两栏:键盘至少需要 kbMinW 才能
// 完整显示键帽标签,右列拿不到该宽度时改为上下排列,而不是把键盘压扁
// 或塞进横向滚动(方案 4.3)。
func fkSplit(gtx C, l, r layout.Widget) D {
	const wl, wr float32 = 1, 2
	w := gtx.Constraints.Max.X
	gap := gtx.Dp(sp16)
	rw := int(float32(w-gap) * wr / (wl + wr))
	if w < gtx.Dp(gridPairW) || rw < gtx.Dp(kbMinW) {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(l), layout.Rigid(vSpacer(sp16)), layout.Rigid(r))
	}
	return grid2Fill(gtx, wl, wr, l, r)
}

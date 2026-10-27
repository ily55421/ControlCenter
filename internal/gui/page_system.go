// page_system — 系统监控页:模式切换/CPU 仪表/风扇控制与自定义曲线/除尘/系统资讯/偏好磁贴。
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
)

// fanTabModes 风扇标签对应的模式值(与 Web [0,1,6,3,5] 一致)。
var fanTabModes = [5]int{0, 1, 6, 3, 5}

func onoff(b bool) string {
	if b {
		return "启用"
	}
	return "禁用"
}

func (g *GUI) pageSystem(gtx C) D {
	s := g.sys.Load()
	if s == nil {
		return panel(gtx, "正在连接", func(gtx C) D {
			return txt(gtx, "正在连接 ACPI 桥驱动…", textStyle{Size: fsBig, Col: colText3})
		})
	}
	live := g.live.Load()
	if g.w.systemSource != s {
		g.w.systemSource = s
		g.w.noiseless.Value = s.opts.Noiseless
		g.w.dustResume.Value = s.ad.Resume
	}

	// ----- 交互(布局期处理点击) -----
	for i := 0; i < 5; i++ {
		if g.w.modeTab[i].Clicked(gtx) {
			m := i
			g.run("已切换到 "+modeNames[m], pgSystem, 400*time.Millisecond, func() error { return g.be.RequestMode(m) })
		}
	}
	// 风扇模式改由分段控件在布局期回传下标(见 fanTabPicked)
	if g.w.noiseless.Update(gtx) {
		v := g.w.noiseless.Value
		g.run("已下发", pgSystem, 400*time.Millisecond, func() error { return g.be.SetModeOption("noiseless", v) })
	}
	// 偏好磁贴
	pk := s.pref.Keyboard
	if g.w.tileWin.Clicked(gtx) {
		g.run("Win 键"+onoff(!pk.WinKey), pgSystem, 400*time.Millisecond,
			func() error { return g.be.SetKeyboardPref(pk.FnLock, !pk.WinKey, pk.FnWin) })
	}
	if g.w.tileFnWin.Clicked(gtx) {
		g.run("Win-Fn 交换"+onoff(!pk.FnWin), pgSystem, 400*time.Millisecond,
			func() error { return g.be.SetKeyboardPref(pk.FnLock, pk.WinKey, !pk.FnWin) })
	}
	if g.w.tileFnLock.Clicked(gtx) {
		g.run("Fn 键锁"+onoff(!pk.FnLock), pgSystem, 400*time.Millisecond,
			func() error { return g.be.SetKeyboardPref(!pk.FnLock, pk.WinKey, pk.FnWin) })
	}
	toggleOpt := func(key string, cur bool, name string) {
		g.run("已下发", pgSystem, 400*time.Millisecond, func() error { return g.be.SetModeOption(key, !cur) })
	}
	if g.w.tileTurbo.Clicked(gtx) {
		toggleOpt("turboFan", s.opts.TurboFan, "涡轮风扇")
	}
	if g.w.tileDTT.Clicked(gtx) {
		toggleOpt("dtt", s.opts.DTT, "CPU DTT")
	}
	if g.w.tileLow.Clicked(gtx) {
		toggleOpt("lowRefresh", s.opts.LowRefresh, "低刷新率")
	}
	if g.w.dustRun.Clicked(gtx) {
		g.run("除尘已启动", pgSystem, 600*time.Millisecond, func() error { return g.be.RunAntiDust() })
	}
	if g.w.dustApply.Clicked(gtx) {
		days, hour, resume := g.w.dustDays.value(), g.w.dustHour.value(), g.w.dustResume.Value
		g.run("除尘计划已下发", pgSystem, 400*time.Millisecond,
			func() error { return g.be.SetAntiDustSchedule(days, hour, resume) })
	}
	if g.w.fanSave.Clicked(gtx) {
		var in [3]FanTableOut
		for i := 0; i < 3; i++ {
			e := &g.w.curveEd[i]
			in[i] = FanTableOut{T1: e[0].value(), T2: e[1].value(), T3: e[2].value(), D1: e[3].value(), D2: e[4].value(), D3: e[5].value()}
		}
		g.run("自定义曲线已下发", pgSystem, 400*time.Millisecond, func() error { return g.be.SaveFanTables(in) })
	}
	if g.w.fanReset.Clicked(gtx) {
		g.run("已恢复 EC 默认曲线", pgSystem, 400*time.Millisecond, func() error { return g.be.ResetFanTables() })
	}
	if g.w.gpuSwitch.Clicked(gtx) {
		g.navTo(pgPref)
	}
	// 滑杆同步(快照去重,防提交后回跳)
	g.w.fanOffset.syncExt(s.fanCfg.Offset)
	g.w.blSlider.syncExt(s.opts.BlueLight)

	// ----- 布局 -----
	// 页头(标题层级) → 模式标签 → 双列内容 → 页脚信息条(收口,消灭底部死黑)
	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "系统监控", "实时利用率 / 温度 / 风扇转速与模式切换", func(gtx C) D {
				return pillBadge(gtx, modeNames[s.st.Mode], colAccent)
			})
		},
		func(gtx C) D { return g.modeTabsRow(gtx, s) },
		vSpacer(14),
		func(gtx C) D {
			return grid2(gtx, 5, 4,
				func(gtx C) D { return g.systemLeft(gtx, s, live) },
				func(gtx C) D { return g.systemRight(gtx, s, live) })
		},
		func(gtx C) D {
			right := ""
			if live != nil {
				right = fmt.Sprintf("%d MHz · %d%% · %d℃ · 风扇偏移 %d%%",
					live.FreqMHz, live.Util, live.Temp, s.fanCfg.Offset)
			}
			return pageFooter(gtx, fmt.Sprintf("%s %s · %s", orDash(s.st.Manufacturer),
				s.st.Product, modeNames[s.st.Mode]), right)
		},
	)
}

// fanTabPicked 分段控件选中风扇模式后下发(下标对应 fanTabModes)。
func (g *GUI) fanTabPicked(j int) {
	if j < 0 || j >= len(fanTabModes) {
		return
	}
	m := fanTabModes[j]
	g.run("风扇模式:"+fanModes[m], pgSystem, 400*time.Millisecond, func() error { return g.be.SetFanMode(m) })
}

func (g *GUI) modeTabsRow(gtx C, s *sysSnap) D {
	return centerRow(gtx,
		func(gtx C) D {
			var tabs []layout.Widget
			for i := 0; i < 5; i++ {
				i := i
				tabs = append(tabs, func(gtx C) D { return drawTab(gtx, &g.w.modeTab[i], modeShort[i], s.st.Mode == i, false, false) })
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceAround}.Layout(gtx, flexWrap(tabs...)...)
		})
}

func (g *GUI) systemLeft(gtx C, s *sysSnap, live *LiveSnap) D {
	util, mhz, temp := float32(0), 0, 0
	fanTemps := [3]int{} // CPU/GPU1/GPU2 当前温度(曲线叠加工作点)
	if live != nil {
		// 实时数据自采样:page6 实时区依赖 BIOS/XTU 通道(无 XTU 服务的机器恒零),
		// 利用率=GetSystemTimes 差分、频率=powrprof、温度=EC 风扇包,全部自持。
		util = float32(live.Util)
		mhz, temp = live.FreqMHz, live.Temp
		if mhz < 0 {
			mhz = 0
		}
		if temp <= 0 {
			temp = live.Live.CPU.Temp
		}
		fanTemps = [3]int{live.Live.CPU.Temp, live.Live.GPU1.Temp, live.Live.GPU2.Temp}
	}
	utilA := g.cpuDial.tick(util)
	mhzText, utilText, tempText := "—", "—", "—"
	if live != nil {
		mhzText = numOrDash(mhz)
		utilText = fmt.Sprintf("%d%%", live.Util)
		tempText = numOrDash(temp)
	}
	return col(gtx,
		// CPU 大表盘(动画:指针平滑扫向目标)
		func(gtx C) D {
			return dialSized(gtx, 240, 220, utilA, 100, dialOpt{
				ticks: 50, labels: []string{"0", "20", "40", "60", "80", "100"}, center: "CPU"})
		},
		vSpacer(4),
		// 三格数字(动画平滑;三等分均匀分布,数字+标签成组:左组左对齐/中组居中/右组右对齐)
		func(gtx C) D {
			statCol := func(num string, numSize float32, lbl string, align layout.Alignment) layout.Widget {
				return func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Alignment: align}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return bigNum(gtx, num, numSize) }),
						layout.Rigid(func(gtx C) D { return txt(gtx, lbl, textStyle{Size: fsBody2, Col: colText3}) }),
					)
				}
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, statCol(mhzText, fsStat, "MHz · 频率", layout.Start)),
				layout.Flexed(1, statCol(utilText, fsStat, "利用率", layout.Middle)),
				layout.Flexed(1, statCol(tempText, fsStat, "℃ · 温度", layout.End)),
			)
		},
		vSpacer(18),
		// 风扇模式(等分分段控件,单行更整齐) + 寂静模式开关
		func(gtx C) D {
			labels := make([]string, 0, 5)
			cur := 0
			for j := 0; j < 5; j++ {
				labels = append(labels, fanModes[fanTabModes[j]])
				if s.fanCfg.Mode == fanTabModes[j] {
					cur = j
				}
			}
			g.w.fanSeg.sel = cur // 以快照为准(控件只负责展示与点击)
			var d D
			var picked int
			layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					d, picked = g.w.fanSeg.Layout(gtx, labels)
					// 段内点击与旧的 fanTab 语义一致:立刻下发风扇模式
					if picked >= 0 {
						g.fanTabPicked(picked)
					}
					return d
				}),
				layout.Rigid(func(gtx C) D { return hSpacer(sp16)(gtx) }),
				layout.Rigid(func(gtx C) D { return switchBox(gtx, &g.w.noiseless) }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: unit.Dp(sp8)}.Layout(gtx, func(gtx C) D {
						return txt(gtx, "寂靜模式", textStyle{Size: fsBody2, Col: colText3})
					})
				}),
			)
			return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
		},
		vSpacer(4),
		func(gtx C) D {
			return centerLabel(gtx, fmt.Sprintf("风扇转速控制 · 当前偏移 %d%%", s.fanCfg.Offset))
		},
		vSpacer(8),
		// 风扇控制面板
		func(gtx C) D {
			return panel(gtx, "风扇转速控制", func(gtx C) D {
				return col(gtx,
					func(gtx C) D {
						return labelRow(gtx, "自动偏移", func(gtx C) D {
							return txt(gtx, fmt.Sprintf("%d%%", g.w.fanOffset.value()), textStyle{Size: fsBig, Col: colOk})
						})
					},
					func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return g.w.fanOffset.layout(gtx)
					},
					vSpacer(12),
					func(gtx C) D {
						badgeTxt := "无计划"
						if s.ad.Enabled {
							badgeTxt = fmt.Sprintf("计划:%d天/%d时", s.ad.Days, s.ad.Hour)
						}
						if s.ad.Running {
							badgeTxt += " · 运行中"
						}
						return col(gtx,
							func(gtx C) D { return txt(gtx, badgeTxt, textStyle{Size: fsBody2, Col: colText3}) },
							vSpacer(8),
							func(gtx C) D {
								run := func(gtx C) D { return actBtn(gtx, &g.w.dustRun, "立即除尘", btnPlain, false) }
								apply := func(gtx C) D { return actBtn(gtx, &g.w.dustApply, "下发除尘计划", btnPlain, false) }
								if gtx.Constraints.Max.X < gtx.Dp(300) {
									return col(gtx, func(gtx C) D { return row(gtx, run) }, vSpacer(8),
										func(gtx C) D { return row(gtx, apply) })
								}
								return row(gtx, run, hSpacer(12), apply)
							})
					},
					vSpacer(12),
					func(gtx C) D {
						field := func(label string, n *numField, value int) layout.Widget {
							return func(gtx C) D {
								return row(gtx,
									func(gtx C) D { return txt(gtx, label, textStyle{Size: fsBody2, Col: colText3}) },
									hSpacer(8),
									func(gtx C) D { n.sync(gtx, value); return numBox(gtx, n, 64) })
							}
						}
						return col(gtx,
							func(gtx C) D {
								days, hour := field("周期(天)", &g.w.dustDays, s.ad.Days), field("执行(时)", &g.w.dustHour, s.ad.Hour)
								if gtx.Constraints.Max.X < gtx.Dp(300) {
									return col(gtx, days, vSpacer(10), hour)
								}
								return row(gtx, days, hSpacer(16), hour)
							},
							vSpacer(10),
							func(gtx C) D {
								return row(gtx,
									func(gtx C) D { return switchBox(gtx, &g.w.dustResume) }, hSpacer(8),
									func(gtx C) D { return txt(gtx, "唤醒补跑", textStyle{Size: fsBody2, Col: colText3}) })
							})
					},
					vSpacer(16),
					func(gtx C) D { return g.curveRow(gtx, s, fanTemps) },
					vSpacer(10),
					func(gtx C) D {
						save := func(gtx C) D { return actBtn(gtx, &g.w.fanSave, "保存并启用自定义曲线", btnPri, false) }
						reset := func(gtx C) D { return actBtn(gtx, &g.w.fanReset, "恢复默认曲线", btnPlain, false) }
						if gtx.Constraints.Max.X < gtx.Dp(420) {
							return col(gtx, func(gtx C) D { return row(gtx, save) }, vSpacer(10),
								func(gtx C) D { return row(gtx, reset) })
						}
						return row(gtx, save, hSpacer(12), reset)
					},
				)
			})
		},
	)
}

// curveColumns 按最小可读宽度决定列数,余下宽度由各列等分。
func curveColumns(gtx C, avail int) int {
	return clampI((avail+gtx.Dp(sp10))/(gtx.Dp(curveColW)+gtx.Dp(sp10)), 1, 3)
}

// curveRow 最多三列,末行保持同样列宽;不会把第三张卡片压成零宽。
func (g *GUI) curveRow(gtx C, s *sysSnap, fanTemps [3]int) D {
	names := [3]string{"CPU", "GPU1", "GPU2"}
	avail, gap := gtx.Constraints.Max.X, gtx.Dp(sp10)
	cols := curveColumns(gtx, avail)
	usable := avail - (cols-1)*gap
	var rows []layout.Widget
	for start := 0; start < 3; start += cols {
		if start > 0 {
			rows = append(rows, vSpacer(16))
		}
		start := start
		rows = append(rows, func(gtx C) D {
			var kids []layout.FlexChild
			for j := 0; j < cols && start+j < 3; j++ {
				if j > 0 {
					kids = append(kids, layout.Rigid(hSpacerSP(gap)))
				}
				i := start + j
				// 累计取整,第一行连同间距准确填满分配宽度。
				w := usable*(j+1)/cols - usable*j/cols
				kids = append(kids, layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
					gtx.Constraints.Min.Y = 0
					return col(gtx,
						func(gtx C) D { return txt(gtx, names[i]+" 曲线", textStyle{Size: fsBody2, Col: colText2}) },
						vSpacer(8),
						func(gtx C) D { return g.curveEditor(gtx, i, s, fanTemps[i]) })
				}))
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, kids...)
		})
	}
	return col(gtx, rows...)
}

// 活跃拖拽的指针 ID;使用非零大小的 curveEd 地址作 tag,避免 struct{} 数组的
// 三个地址可能相同。结束/失效即删除,不保留已结束的 GUI。
var curvePointerIDs = make(map[*[6]numField]pointer.ID)

// curveMovePoint 保持温度严格递增、占空非递减,只编辑草稿。
func curveMovePoint(pts [3][2]int, p, t, d int) [3][2]int {
	loT, hiT, loD, hiD := 0, 99, 0, 100
	if p > 0 {
		loT, loD = pts[p-1][0]+1, pts[p-1][1]
	}
	if p < 2 {
		hiT, hiD = pts[p+1][0]-1, pts[p+1][1]
	}
	pts[p] = [2]int{clampI(t, loT, hiT), clampI(d, loD, hiD)}
	return pts
}

// curveEditor 绘制真实三点表;拖拽仅更新 curveEd,保存接口仍统一提交。
func (g *GUI) curveEditor(gtx C, i int, s *sysSnap, curTemp int) D {
	tbl, ed := s.tables[i], &g.w.curveEd[i]
	available := validFanTable(tbl)
	if !available && g.curveDragFan == i {
		g.curveDragFan, g.curveDragPt = -1, -1
	}
	if g.curveDragFan != i {
		delete(curvePointerIDs, ed)
		vals := [6]int{tbl.T1, tbl.T2, tbl.T3, tbl.D1, tbl.D2, tbl.D3}
		for j := range vals {
			ed[j].sync(gtx, vals[j])
		}
	}
	pts := [3][2]int{{ed[0].value(), ed[3].value()}, {ed[1].value(), ed[4].value()}, {ed[2].value(), ed[5].value()}}
	available = available && validFanTable(FanTableOut{T1: pts[0][0], T2: pts[1][0], T3: pts[2][0], D1: pts[0][1], D2: pts[1][1], D3: pts[2][1]})
	w, h := gtx.Constraints.Max.X, gtx.Dp(220)
	invalidate := func() {
		if g.win != nil {
			g.win.Invalidate()
		}
	}
	finish := func() {
		g.curveDragFan, g.curveDragPt = -1, -1
		delete(curvePointerIDs, ed)
		invalidate()
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ed, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if !available || g.curveDragFan >= 0 || (pe.Source == pointer.Mouse && pe.Buttons != pointer.ButtonPrimary) {
				continue
			}
			nearest, distance := -1, float64(gtx.Dp(14))
			for p := range pts {
				c := curveXY(gtx, w, h, pts[p][0], pts[p][1])
				if d := math.Hypot(float64(pe.Position.X-c.X), float64(pe.Position.Y-c.Y)); d < distance {
					nearest, distance = p, d
				}
			}
			if nearest >= 0 {
				g.curveDragFan, g.curveDragPt = i, nearest
				curvePointerIDs[ed] = pe.PointerID
				gtx.Execute(pointer.GrabCmd{Tag: ed, ID: pe.PointerID})
				invalidate()
			}
		case pointer.Drag, pointer.Release:
			if g.curveDragFan != i || pe.PointerID != curvePointerIDs[ed] {
				continue
			}
			if available {
				nt, nd := curveTD(gtx, w, h, pe.Position.X, pe.Position.Y)
				p := g.curveDragPt
				pts = curveMovePoint(pts, p, nt, nd)
				ed[p].setText(pts[p][0])
				ed[p+3].setText(pts[p][1])
				invalidate()
			}
			if pe.Kind == pointer.Release {
				finish() // 抓取保证图外释放也会送达;Gio 自动释放 pointer grab。
			}
		case pointer.Cancel:
			if g.curveDragFan == i {
				finish()
			}
		}
	}
	if !available {
		if g.curveDragFan == i {
			finish()
		}
		return col(gtx,
			func(gtx C) D { return txt(gtx, "曲线不可用", textStyle{Size: fsBody2, Col: colText3}) },
			vSpacer(6),
			func(gtx C) D {
				return txt(gtx, "未读取到有效硬件表，无法预览或拖拽。", textStyle{Size: fsSmall, Col: colText3})
			})
	}
	return col(gtx,
		func(gtx C) D {
			dragIdx := -1
			if g.curveDragFan == i {
				dragIdx = g.curveDragPt
			}
			dims := drawFanCurve(gtx, w, h, pts, dragIdx, curTemp)
			defer clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, ed)
			return dims
		},
		vSpacer(8),
		func(gtx C) D {
			var labels []layout.Widget
			for p := range pts {
				p := p
				labels = append(labels, func(gtx C) D {
					return txt(gtx, fmt.Sprintf("点 %d · %d℃ / %d%%", p+1, pts[p][0], pts[p][1]), textStyle{Size: fsBody2, Col: colAccent})
				})
			}
			labels = append(labels, vSpacer(6), func(gtx C) D {
				return txt(gtx, "拖拽圆点调整 · 保存后启用", textStyle{Size: fsSmall, Col: colText3})
			})
			return col(gtx, labels...)
		})
}

// gridPair 两个等宽半列(窄窗自动退化为单列堆叠)。
func gridPair(gtx C, l, r layout.Widget) D {
	w := gtx.Constraints.Max.X
	gap := gtx.Dp(14)
	half := (w - gap) / 2
	if half < gtx.Dp(210) {
		return col(gtx, l, vSpacer(2), r)
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			// 只锁宽度;Exact 会把巨大 Min.Y 传导给末位子项(面板拉伸/行丢失)
			gtx.Constraints.Min.X = half
			gtx.Constraints.Max.X = half
			gtx.Constraints.Min.Y = 0
			return l(gtx)
		}),
		layout.Rigid(hSpacerSP(gap)),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = half
			gtx.Constraints.Max.X = half
			gtx.Constraints.Min.Y = 0
			return r(gtx)
		}),
	)
}

func (g *GUI) systemRight(gtx C, s *sysSnap, live *LiveSnap) D {
	kbDuty, kbRpm, kbTemp := 0, 0, 0
	gDuty, gRpm, gTemp := 0, 0, 0
	cpuMHz := 0
	if live != nil {
		kbDuty, kbRpm, kbTemp = live.Live.CPU.Duty, live.Live.CPU.RPM, live.Live.CPU.Temp
		gDuty, gRpm, gTemp = live.Live.GPU1.Duty, live.Live.GPU1.RPM, live.Live.GPU1.Temp
		cpuMHz = live.FreqMHz
		if cpuMHz < 0 {
			cpuMHz = 0
		}
	}
	gpuDead := gDuty == 0 && gRpm == 0 && gTemp == 0 // dGPU 断电(安静/安静增强)时 EC 全零

	// ---- 半宽面板部件 ----
	fanPanel := func(gtx C) D {
		return panel(gtx, "风扇转速", func(gtx C) D {
			dialCol := func(rpm, duty, temp int, anim *animVal, name string) layout.Widget {
				return func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Dp(132)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return dialSized(gtx, 128, 118, anim.tick(float32(duty)), 100,
								dialOpt{ticks: 24, color: pal.Lime, center: fmt.Sprintf("%d%%", duty)})
						}),
						layout.Rigid(func(gtx C) D {
							if rpm == 0 && temp == 0 {
								return txt(gtx, name+" 无数据", textStyle{Size: fsSmall, Col: colText3})
							}
							return col(gtx,
								func(gtx C) D {
									return txt(gtx, fmt.Sprintf("%s · %d RPM", name, rpm), textStyle{Size: fsSmall, Col: colText2})
								},
								func(gtx C) D { return txt(gtx, fmt.Sprintf("%d ℃", temp), textStyle{Size: fsSmall, Col: colText3}) })
						}),
					)
				}
			}
			return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceAround}.Layout(gtx,
				layout.Rigid(dialCol(kbRpm, kbDuty, kbTemp, &g.fanCpuDial, "CPU")),
				layout.Rigid(dialCol(gRpm, gDuty, gTemp, &g.fanGpuDial, "GPU")),
			)
		})
	}
	gpuPanel := func(gtx C) D {
		return panel(gtx, "GPU 资讯", func(gtx C) D {
			readyTxt := "未就绪"
			if s.gpu.Ready {
				readyTxt = "就绪"
			}
			if gpuDead {
				return col(gtx,
					func(gtx C) D {
						return badge(gtx, "独显遥测不可用", "warn")
					},
					vSpacer(4),
					func(gtx C) D {
						return txt(gtx, "EC 未返回温度或转速,不能据此判断独显电源状态", textStyle{Size: fsSmall, Col: colText3})
					},
					vSpacer(4),
					func(gtx C) D { return kvRow(gtx, "NVAPI", readyTxt) },
				)
			}
			return col(gtx,
				func(gtx C) D {
					if s.gpu.BaseClock > 0 {
						return col(gtx,
							func(gtx C) D { return kvRow(gtx, "时序", fmt.Sprintf("%d MHz", s.gpu.BaseClock)) },
							func(gtx C) D { return dotBar(gtx, clamp01(float32(s.gpu.BaseClock)/3000), true) })
					}
					return kvRow(gtx, "时序", "—")
				},
				vSpacer(5),
				func(gtx C) D { return kvRow(gtx, "GPU 温度", fmt.Sprintf("%d ℃", gTemp)) },
				func(gtx C) D { return dotBar(gtx, clamp01(float32(gTemp)/100), false) },
				vSpacer(5),
				func(gtx C) D { return kvRow(gtx, "风扇占空", fmt.Sprintf("%d %%", gDuty)) },
				func(gtx C) D { return dotBar(gtx, clamp01(float32(gDuty)/100), false) },
				vSpacer(5),
				func(gtx C) D { return kvRow(gtx, "NVAPI", readyTxt) },
			)
		})
	}
	sysPanel := func(gtx C) D {
		return panel(gtx, "系统资讯", func(gtx C) D {
			return col(gtx,
				func(gtx C) D { return kvRow(gtx, "CPU", orDash(s.st.CPU)) },
				func(gtx C) D { return kvRow(gtx, "核心", fmt.Sprintf("%d", s.st.Cores)) },
				func(gtx C) D { return kvRow(gtx, "产品", s.st.Manufacturer+" "+s.st.Product) },
				func(gtx C) D { return kvRow(gtx, "BIOS", orDash(s.st.BIOS)) },
				func(gtx C) D { return kvRow(gtx, "键盘灯", kbTypeName(int(s.st.KBType))) },
			)
		})
	}
	prefPanel := func(gtx C) D {
		return panel(gtx, "偏好", func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return g.tileGrid(gtx, s) }),
			)
		})
	}
	cfgPanel := func(gtx C) D {
		return panel(gtx, "配置", func(gtx C) D {
			gpuName := []string{"", "仅集显", "仅独显", "MS Hybrid", "动态"}
			gn := ""
			if s.pref.GPUSwitch.Current >= 0 && s.pref.GPUSwitch.Current < len(gpuName) {
				gn = gpuName[s.pref.GPUSwitch.Current]
			}
			return col(gtx,
				func(gtx C) D {
					return row(gtx, func(gtx C) D { return txt(gtx, "蓝光", textStyle{Size: fsBody2, Col: colText3}) },
						hSpacer(6),
						func(gtx C) D {
							return txt(gtx, fmt.Sprintf("%d/10", g.w.blSlider.value()), textStyle{Size: fsBody, Col: colOk})
						})
				},
				func(gtx C) D { return g.w.blSlider.layout(gtx) },
				vSpacer(6),
				func(gtx C) D {
					return col(gtx,
						func(gtx C) D {
							return row(gtx, func(gtx C) D { return txt(gtx, "GPU 模式", textStyle{Size: fsBody2, Col: colText3}) },
								hSpacer(6), func(gtx C) D { return badge(gtx, gn, "") })
						},
						vSpacer(4),
						func(gtx C) D {
							// 包一层 row:col 的 Rigid 会把按钮拉伸到全宽
							return row(gtx, func(gtx C) D { return actBtn(gtx, &g.w.gpuSwitch, "切换…", btnPlain, false) })
						},
					)
				},
			)
		})
	}

	// ---- 组装:全宽监控 + 全宽风扇 + 两对半宽(默认一屏显示完) ----
	return col(gtx,
		func(gtx C) D {
			return panel(gtx, "CPU 监控", func(gtx C) D {
				g.histMu.Lock()
				tempH := append([]float64(nil), g.cpuTempH...)
				utilH := append([]float64(nil), g.cpuUtilH...)
				g.histMu.Unlock()
				return col(gtx,
					func(gtx C) D {
						return drawMultiChart(gtx, 92, []chartSeries{
							{data: tempH, max: 100, col: pal.Yellow, label: "温度℃"},
							{data: utilH, max: 100, col: pal.Cyan, label: "利用率%"},
						})
					},
					func(gtx C) D {
						return txt(gtx, fmt.Sprintf("当前频率 %d MHz · 每秒采样,最近 1 分钟", cpuMHz), textStyle{Size: fsSmall, Col: colText3})
					})
			})
		},
		fanPanel,
		func(gtx C) D { return gridPair(gtx, gpuPanel, sysPanel) },
		func(gtx C) D { return gridPair(gtx, prefPanel, cfgPanel) },
	)
}

// tileGrid 偏好磁贴(半宽面板内 2 列 × 3 行,紧凑排布)。
func (g *GUI) tileGrid(gtx C, s *sysSnap) D {
	type tileDef struct {
		clk *widget.Clickable
		ico string
		lbl string
		on  bool
	}
	defs := []tileDef{
		{&g.w.tileWin, "pf", "Win 键锁", s.pref.Keyboard.WinKey},
		{&g.w.tileFnWin, "fk", "Win-Fn 交换", s.pref.Keyboard.FnWin},
		{&g.w.tileFnLock, "kb", "Fn 键锁", s.pref.Keyboard.FnLock},
		{&g.w.tileTurbo, "en", "涡轮风扇", s.opts.TurboFan},
		{&g.w.tileDTT, "cpu", "CPU DTT", s.opts.DTT},
		{&g.w.tileLow, "system", "低刷新率", s.opts.LowRefresh},
	}
	tileRow := func(ds []tileDef) layout.Widget {
		return func(gtx C) D {
			kids := make([]layout.FlexChild, 0, len(ds))
			for _, d := range ds {
				d := d
				kids = append(kids, layout.Rigid(func(gtx C) D {
					return g.tile(gtx, d.clk, d.ico, d.lbl, d.on)
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceAround}.Layout(gtx, kids...)
		}
	}
	return col(gtx,
		tileRow(defs[0:2]),
		tileRow(defs[2:4]),
		tileRow(defs[4:6]),
	)
}

// tile 磁贴(图标 + 文本,点亮为荧光绿)。
func (g *GUI) tile(gtx C, clk *widget.Clickable, ico, lbl string, on bool) D {
	colr := color.NRGBA{R: 0x4D, G: 0x57, B: 0x61, A: 0xFF}
	if on {
		colr = pal.Lime
	} else if clk.Hovered() {
		colr = color.NRGBA{R: 0xAE, G: 0xB8, B: 0xC1, A: 0xFF}
	}
	return clk.Layout(gtx, func(gtx C) D {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return drawIcon(gtx, ico, 30, colr) }),
				layout.Rigid(vSpacer(4)),
				layout.Rigid(func(gtx C) D { return txt(gtx, lbl, textStyle{Size: fsSmall, Col: colr}) }),
			)
		})
	})
}

// ---------- 通用小助手 ----------

func bigNum(gtx C, s string, size float32) D {
	return txt(gtx, s, textStyle{Size: size, Col: colText})
}

func pctOrDash(v float32) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func numOrDash(v int) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", v)
}

// mAhOrDash 容量(mAh):0 表示未取到,显示占位符而不是假读数。
func mAhOrDash(v int) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d mAh", v)
}

// yearsOrDash 使用年限:未取到时不显示 0.0 年。
func yearsOrDash(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f 年", v)
}

// centerLabel 单行居中的说明小字:宽度锁满,由 Flex 居中,不再测量后回放。
func centerLabel(gtx C, s string) D {
	if w := gtx.Constraints.Max.X; w > 0 {
		gtx.Constraints.Min.X = w
	}
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceAround}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return txt(gtx, s, textStyle{Size: fsBody, Col: colText3}) }))
}

// rightLabel 单行右对齐的说明小字。
func rightLabel(gtx C, s string) D {
	if w := gtx.Constraints.Max.X; w > 0 {
		gtx.Constraints.Min.X = w
	}
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceEnd}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return txt(gtx, s, textStyle{Size: fsBody, Col: colText3}) }))
}

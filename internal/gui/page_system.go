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
		return panel(gtx, "正在连接", func(gtx C) D { return label(gtx, "正在连接 ACPI 桥驱动…", 14, pal.Dim) })
	}
	live := g.live.Load()

	// ----- 交互(布局期处理点击) -----
	for i := 0; i < 5; i++ {
		if g.w.modeTab[i].Clicked(gtx) {
			m := i
			g.run("已请求切换到 "+modeNames[m], -1, 0, func() error { g.be.RequestMode(m); return nil })
			g.after(900*time.Millisecond, pgSystem)
		}
	}
	for j := 0; j < 5; j++ {
		if g.w.fanTab[j].Clicked(gtx) {
			m := fanTabModes[j]
			g.run("风扇模式:"+fanModes[m], pgSystem, 400*time.Millisecond, func() error { g.be.SetFanMode(m); return nil })
		}
	}
	if g.w.noiseless.Update(gtx) {
		v := g.w.noiseless.Value
		g.run("已下发", pgSystem, 400*time.Millisecond, func() error { g.be.SetModeOption("noiseless", v); return nil })
	}
	// 偏好磁贴
	pk := s.pref.Keyboard
	if g.w.tileWin.Clicked(gtx) {
		g.run("Win 键"+onoff(!pk.WinKey), pgSystem, 400*time.Millisecond,
			func() error { g.be.SetKeyboardPref(pk.FnLock, !pk.WinKey, pk.FnWin); return nil })
	}
	if g.w.tileFnWin.Clicked(gtx) {
		g.run("Win-Fn 交换"+onoff(!pk.FnWin), pgSystem, 400*time.Millisecond,
			func() error { g.be.SetKeyboardPref(pk.FnLock, pk.WinKey, !pk.FnWin); return nil })
	}
	if g.w.tileFnLock.Clicked(gtx) {
		g.run("Fn 键锁"+onoff(!pk.FnLock), pgSystem, 400*time.Millisecond,
			func() error { g.be.SetKeyboardPref(!pk.FnLock, pk.WinKey, pk.FnWin); return nil })
	}
	toggleOpt := func(key string, cur bool, name string) {
		g.run("已下发", pgSystem, 400*time.Millisecond, func() error { g.be.SetModeOption(key, !cur); return nil })
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
		g.run("除尘已启动", pgSystem, 600*time.Millisecond, func() error { g.be.RunAntiDust(); return nil })
	}
	if g.w.dustApply.Clicked(gtx) {
		days, hour, resume := g.w.dustDays.value(), g.w.dustHour.value(), g.w.dustResume.Value
		g.run("除尘计划已下发", pgSystem, 400*time.Millisecond,
			func() error { g.be.SetAntiDustSchedule(days, hour, resume); return nil })
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
		g.run("已恢复 EC 默认曲线", pgSystem, 400*time.Millisecond, func() error { g.be.ResetFanTables(); return nil })
	}
	if g.w.gpuSwitch.Clicked(gtx) {
		g.navTo(pgPref)
	}
	// 滑杆同步(快照去重,防提交后回跳)
	g.w.fanOffset.syncExt(s.fanCfg.Offset)
	g.w.blSlider.syncExt(s.opts.BlueLight)

	// ----- 布局 -----
	return col(gtx,
		func(gtx C) D { return g.modeTabsRow(gtx, s) },
		vSpacer(10),
		func(gtx C) D {
			return grid2(gtx, 5, 4,
				func(gtx C) D { return g.systemLeft(gtx, s, live) },
				func(gtx C) D { return g.systemRight(gtx, s, live) })
		},
	)
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
		util = float32(live.Ci.CPUUtil)
		mhz, temp = live.Ci.CPUMHz, live.Ci.CPUTemp
		fanTemps = [3]int{live.Live.CPU.Temp, live.Live.GPU1.Temp, live.Live.GPU2.Temp}
	}
	// 动画值:表盘与大数字共用,数字随动画平滑滚动(1s 采样也不显跳变)
	utilA := g.cpuDial.tick(util)
	mhzA := g.cpuMHz.tick(float32(mhz))
	tempA := g.cpuTemp.tick(float32(temp))
	return col(gtx,
		// CPU 大表盘(动画:指针平滑扫向目标)
		func(gtx C) D {
			return dialSized(gtx, 300, 272, utilA, 100, dialOpt{
				ticks: 50, labels: []string{"0", "20", "40", "60", "80", "100"}, center: "CPU"})
		},
		vSpacer(4),
		// 三格数字(动画平滑;三等分均匀分布,数字+标签成组:左组左对齐/中组居中/右组右对齐)
		func(gtx C) D {
			statCol := func(num string, numSize float32, lbl string, align layout.Alignment) layout.Widget {
				return func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Alignment: align}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return bigNum(gtx, num, numSize) }),
						layout.Rigid(func(gtx C) D { return label(gtx, lbl, 12, pal.Dim) }),
					)
				}
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, statCol(fmt.Sprintf("%.0f", mhzA), 38, "MHz · 时序", layout.Start)),
				layout.Flexed(1, statCol(fmt.Sprintf("%.0f%%", utilA), 30, "利用率", layout.Middle)),
				layout.Flexed(1, statCol(fmt.Sprintf("%.0f", tempA), 38, "℃ · 温度", layout.End)),
			)
		},
		vSpacer(18),
		// 风扇模式标签 + 寂静开关
		func(gtx C) D {
			var tabs []layout.Widget
			for j := 0; j < 5; j++ {
				j := j
				tabs = append(tabs, func(gtx C) D {
					return drawTab(gtx, &g.w.fanTab[j], fanModes[fanTabModes[j]], s.fanCfg.Mode == fanTabModes[j], true, false)
				})
			}
			tabs = append(tabs, hSpacer(14), func(gtx C) D {
				return row(gtx, func(gtx C) D { return switchBox(gtx, &g.w.noiseless) },
					func(gtx C) D { return label(gtx, "寂靜模式", 13, pal.Dim) })
			})
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Spacing: layout.SpaceAround}.Layout(gtx, flexWrap(tabs...)...)
		},
		vSpacer(4),
		func(gtx C) D { return centerLabel(gtx, fmt.Sprintf("风扇转速控制 · WMI 121/1 + 偏移 %d%%", s.fanCfg.Offset)) },
		vSpacer(8),
		// 风扇控制面板
		func(gtx C) D {
			return panel(gtx, "风扇转速控制", func(gtx C) D {
				var curveCols []layout.FlexChild
				names := [3]string{"CPU", "GPU1", "GPU2"}
				for i := 0; i < 3; i++ {
					i := i
					curveCols = append(curveCols, layout.Rigid(func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return centerLabel(gtx, names[i]+" 曲线") },
							func(gtx C) D { return g.curveEditor(gtx, i, s, fanTemps[i]) },
							func(gtx C) D { return centerLabel(gtx, "温度℃ / 转速% · 拖拽圆点") },
						)
					}))
				}
				return col(gtx,
					func(gtx C) D {
						return labelRow(gtx, "自动偏移", func(gtx C) D {
							return label(gtx, fmt.Sprintf("%d%%", g.w.fanOffset.value()), 14, pal.Lime)
						})
					},
					func(gtx C) D { return g.w.fanOffset.layout(gtx) },
					vSpacer(6),
					func(gtx C) D {
						badgeTxt := "无计划"
						if s.ad.Enabled {
							badgeTxt = fmt.Sprintf("计划:%d天/%d时", s.ad.Days, s.ad.Hour)
						}
						if s.ad.Running {
							badgeTxt += " · 运行中"
						}
						return row(gtx,
							func(gtx C) D { return actBtn(gtx, &g.w.dustRun, "立即除尘", btnPlain, false) },
							hSpacer(12),
							func(gtx C) D { return actBtn(gtx, &g.w.dustApply, "下发除尘计划", btnPlain, false) },
							hSpacer(12),
							func(gtx C) D { return label(gtx, badgeTxt, 12, pal.Dim) },
						)
					},
					func(gtx C) D {
						return row(gtx,
							func(gtx C) D { return label(gtx, "周期(天)", 12, pal.Dim) },
							func(gtx C) D { g.w.dustDays.sync(gtx, s.ad.Days); return numBox(gtx, &g.w.dustDays, 64) },
							hSpacer(10),
							func(gtx C) D { return label(gtx, "执行(时)", 12, pal.Dim) },
							func(gtx C) D { g.w.dustHour.sync(gtx, s.ad.Hour); return numBox(gtx, &g.w.dustHour, 64) },
							hSpacer(10),
							func(gtx C) D { return switchBox(gtx, &g.w.dustResume) },
							func(gtx C) D { return label(gtx, "唤醒补跑", 12, pal.Dim) },
						)
					},
					vSpacer(10),
					func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceAround}.Layout(gtx, curveCols...)
					},
					vSpacer(10),
					func(gtx C) D {
						return row(gtx,
							func(gtx C) D { return actBtn(gtx, &g.w.fanSave, "保存并启用自定义曲线", btnPri, false) },
							hSpacer(12),
							func(gtx C) D { return actBtn(gtx, &g.w.fanReset, "恢复默认曲线", btnPlain, false) })
					},
				)
			})
		},
	)
}

// curveEditor 单风扇可视化三点曲线编辑器(拖拽圆点调整;数值落回 curveEd,
// 由"保存并启用"统一提交)。超越原版:原 CC40 与旧 Web 版均为纯数字输入。
func (g *GUI) curveEditor(gtx C, i int, s *sysSnap, curTemp int) D {
	tbl := s.tables[i]
	ed := &g.w.curveEd[i]
	// 快照同步(拖拽中不回写,避免拽着的点被拉回)
	if g.curveDragFan != i {
		vals := [6]int{tbl.T1, tbl.T2, tbl.T3, tbl.D1, tbl.D2, tbl.D3}
		for j := 0; j < 6; j++ {
			ed[j].sync(gtx, vals[j])
		}
	}
	pts := [3][2]int{
		{maxI(ed[0].value(), 35), ed[3].value()},
		{maxI(ed[1].value(), 35), ed[4].value()},
		{maxI(ed[2].value(), 35), ed[5].value()},
	}
	w, h := gtx.Dp(158), gtx.Dp(112)
	dragIdx := -1
	if g.curveDragFan == i {
		dragIdx = g.curveDragPt
	}
	dims := drawFanCurve(gtx, w, h, pts, dragIdx, curTemp)

	// 指针交互:按下命中检测(半径 16px)→ 拖拽实时改 T/D → 释放结束
	defer clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops).Pop()
	tag := &g.w.curveTag[i]
	event.Op(gtx.Ops, tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			for p := 0; p < 3; p++ {
				c := curveXY(gtx, w, h, pts[p][0], pts[p][1])
				if math.Hypot(float64(pe.Position.X-c.X), float64(pe.Position.Y-c.Y)) < 16 {
					g.curveDragFan, g.curveDragPt = i, p
					g.win.Invalidate()
					break
				}
			}
		case pointer.Drag:
			if g.curveDragFan == i {
				nt, nd := curveTD(gtx, w, h, pe.Position.X, pe.Position.Y)
				p := g.curveDragPt
				// 夹紧并保持 T1 < T2 < T3
				lo, hi := 35, 100
				if p > 0 {
					lo = pts[p-1][0] + 1
				}
				if p < 2 {
					hi = pts[p+1][0] - 1
				}
				nt = clampI(nt, lo, hi)
				ed[p].setText(nt)
				ed[p+3].setText(nd)
				g.win.Invalidate()
			}
		case pointer.Release, pointer.Cancel:
			if g.curveDragFan == i {
				g.curveDragFan, g.curveDragPt = -1, -1
				g.win.Invalidate()
			}
		}
	}
	return dims
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
		cpuMHz = live.Ci.CPUMHz
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
								return label(gtx, name+" 无数据", 11, pal.Dim)
							}
							return label(gtx, fmt.Sprintf("%s %d RPM·%d℃", name, rpm, temp), 11, pal.Dim)
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
						return badge(gtx, "独显已断电", "warn")
					},
					vSpacer(4),
					func(gtx C) D { return label(gtx, "安静模式下 EC 不上报独显数据", 11, pal.Dim) },
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
					return row(gtx, func(gtx C) D { return label(gtx, "蓝光", 12, pal.Dim) },
						hSpacer(6),
						func(gtx C) D { return label(gtx, fmt.Sprintf("%d/10", g.w.blSlider.value()), 13, pal.Lime) })
				},
				func(gtx C) D { return g.w.blSlider.layout(gtx) },
				vSpacer(6),
				func(gtx C) D {
					return col(gtx,
						func(gtx C) D {
							return row(gtx, func(gtx C) D { return label(gtx, "GPU 模式", 12, pal.Dim) },
								hSpacer(6), func(gtx C) D { return badge(gtx, gn, "") })
						},
						vSpacer(4),
						func(gtx C) D { return actBtn(gtx, &g.w.gpuSwitch, "切换…", btnPlain, false) },
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
						return label(gtx, fmt.Sprintf("当前频率 %d MHz · 每秒采样,最近 1 分钟", cpuMHz), 11, pal.Dim)
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
				layout.Rigid(func(gtx C) D { return label(gtx, lbl, 11, colr) }),
			)
		})
	})
}

// ---------- 通用小助手 ----------

func bigNum(gtx C, txt string, size float32) D {
	return label(gtx, txt, size, pal.Txt)
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

func centerLabel(gtx C, txt string) D {
	td, call := textDims(gtx, txt, 13, pal.Dim)
	replayText(gtx, call, (gtx.Constraints.Max.X-td.Size.X)/2, 0)
	return D{Size: image.Pt(gtx.Constraints.Max.X, td.Size.Y)}
}

func rightLabel(gtx C, txt string) D {
	td, call := textDims(gtx, txt, 13, pal.Dim)
	replayText(gtx, call, gtx.Constraints.Max.X-td.Size.X, 0)
	return D{Size: image.Pt(gtx.Constraints.Max.X, td.Size.Y)}
}

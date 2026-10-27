// pages_hw — CPU 超频 / GPU 超频 / 电池 / 节能计划 / 偏好 五页。
package gui

import (
	"fmt"
	"strconv"
	"time"

	"gioui.org/layout"

	"controlcenter/internal/cpuoc"
	"controlcenter/internal/energysave"
)

// coreGroup 一组核心倍频输入列:off 为字段数组起始下标,n 为行数,pre 为前缀。
// 显示编号恒为 off+1 起 —— 编号与绑定不能分开传参,否则第二列会重演
// 「绑定 ECores[8..15] 却显示 1-8」的错位。
type coreGroup struct {
	off int
	n   int
	pre string
}

// label 第 i 行(0 基)的显示标签。
func (c coreGroup) label(i int) string {
	if c.pre == "" {
		return fmt.Sprintf("核 %d", c.off+i+1)
	}
	return fmt.Sprintf("%s 核 %d", c.pre, c.off+i+1)
}

// ecoreFieldGroups E 核倍频的两列布局参数(每列 8 行,合计覆盖 16 个字段)。
// 页面与测试共用:任一侧改动都会让「编号 1-16 与 ecoreEd[0..15] 一一对应」
// 的断言失败,而不是等用户点错核。
func ecoreFieldGroups() []coreGroup {
	return []coreGroup{{off: 0, n: 8, pre: "E"}, {off: 8, n: 8, pre: "E"}}
}

// ---------- CPU 超频 ----------

func (g *GUI) pageCPU(gtx C) D {
	s := g.cpu.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D {
			return txt(gtx, "正在读取超频参数…", textStyle{Size: fsBig, Col: colText3})
		})
	}
	p := s.Params
	// 输入框同步(快照到达时刷新显示,聚焦不打断)
	g.w.cpuPL1.sync(gtx, p.PL1Watt)
	g.w.cpuPL2.sync(gtx, p.PL2Watt)
	g.w.cpuPLT.sync(gtx, int(p.PLTimeSec))
	g.w.cpuVR.sync(gtx, p.VRLimit)
	g.w.cpuVO.sync(gtx, p.CoreOffset)
	g.w.cpuOV.sync(gtx, int(p.OverrideVolt))
	for i := 0; i < 10; i++ {
		g.w.pcoreEd[i].sync(gtx, p.PCores[i])
	}
	for i := 0; i < 16; i++ {
		g.w.ecoreEd[i].sync(gtx, p.ECores[i])
	}
	// VR Auto:用户改动后不被旧快照覆盖,直到与快照一致
	if g.w.cpuVRA.Update(gtx) {
		g.w.vraDirty = true
	}
	if !g.w.vraDirty {
		g.w.cpuVRA.Value = p.VRAuto
	} else if g.w.cpuVRA.Value == p.VRAuto {
		g.w.vraDirty = false
	}

	if g.w.cpuApplyB.clicked(gtx) {
		params := cpuoc.Params{
			PL1Watt: g.w.cpuPL1.value(), PL2Watt: g.w.cpuPL2.value(), PLTimeSec: float32(g.w.cpuPLT.value()),
			VRAuto: g.w.cpuVRA.Value, VRLimit: g.w.cpuVR.value(), CoreOffset: g.w.cpuVO.value(),
			OverrideVolt: float32(g.w.cpuOV.value()), GraphicsOff: 0,
		}
		for i := 0; i < 10; i++ {
			params.PCores[i] = g.w.pcoreEd[i].value()
		}
		for i := 0; i < 16; i++ {
			params.ECores[i] = g.w.ecoreEd[i].value()
		}
		g.run("参数已写入 page6,后台 XTU 执行中", pgCPU, 800*time.Millisecond, func() error { return g.be.CPUApply(params) })
	}
	if g.w.cpuDefB.clicked(gtx) {
		g.run("已恢复默认", pgCPU, 500*time.Millisecond, func() error { return g.be.CPURestoreDefaults() })
	}

	inf := s.Info
	// 核心输入列:显示编号由「字段数组起始下标」推导(编号 = off+1 起),
	// 原先 E 核第二列绑定 ECores[8+i] 却仍按组内 i+1 显示 1-8(P0)。
	coreInputs := func(get func(i int) *numField, gr coreGroup) layout.Widget {
		rows := make([]layout.Widget, 0, gr.n)
		for i := 0; i < gr.n; i++ {
			i := i
			rows = append(rows, func(gtx C) D {
				return row(gtx,
					func(gtx C) D { return txt(gtx, gr.label(i), textStyle{Size: fsBody, Col: colText3}) },
					hSpacer(sp8),
					func(gtx C) D { return numBox(gtx, get(gr.off+i), 86) })
			})
		}
		return func(gtx C) D { return col(gtx, rows...) }
	}
	pget := func(i int) *numField { return &g.w.pcoreEd[i] }
	eget := func(i int) *numField { return &g.w.ecoreEd[i] }

	return spreadCol(gtx,
		func(gtx C) D {
			return pageHead(gtx, "CPU 超频", "XTU 通道的功耗/电压与 P/E 核倍频", func(gtx C) D {
				return pillBadge(gtx, fmt.Sprintf("PL1 %dW", g.w.cpuPL1.value()), colAccent)
			})
		},
		func(gtx C) D {
			return grid2(gtx, 1, 1,
				func(gtx C) D {
					return panel(gtx, "实时状态", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return kvRow(gtx, "当前频率", mhzOrDash(inf.CPUMHz)) },
							func(gtx C) D { return kvRow(gtx, "电压", fmt.Sprintf("%.3f V", inf.CPUVolt)) },
							func(gtx C) D { return kvRow(gtx, "温度", fmt.Sprintf("%d ℃", inf.CPUTemp)) },
							func(gtx C) D { return kvRow(gtx, "利用率", fmt.Sprintf("%d%%", inf.CPUUtil)) },
							func(gtx C) D { return kvRow(gtx, "内存频率", mhzOrDash(inf.MemMHz)) },
							func(gtx C) D {
								return kvRow(gtx, "Turbo 范围", rangeOrDash(inf.TurboMinMHz, inf.TurboMaxMHz, "MHz"))
							},
							func(gtx C) D { return kvRow(gtx, "倍频范围", ratioOrDash(inf.RatioMin, inf.RatioMax)) },
						)
					})
				},
				func(gtx C) D {
					return panel(gtx, "功耗与电压", func(gtx C) D {
						return col(gtx,
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "PL1 (W)", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuPL1, 86) },
									hSpacer(14), func(gtx C) D { return txt(gtx, "PL2 (W)", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuPL2, 86) })
							},
							vSpacer(6),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "PL 时间 (s)", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuPLT, 86) },
									hSpacer(14), func(gtx C) D { return txt(gtx, "VR Limit", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuVR, 86) })
							},
							vSpacer(6),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "VR Auto", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return switchBox(gtx, &g.w.cpuVRA) },
									hSpacer(14), func(gtx C) D { return txt(gtx, "Override (V)", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuOV, 86) })
							},
							vSpacer(6),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "核心电压偏移", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return numBox(gtx, &g.w.cpuVO, 86) },
									hSpacer(6), func(gtx C) D { return txt(gtx, "mV", textStyle{Size: fsBody2, Col: colText3}) })
							})
					})
				})
		},
		func(gtx C) D {
			return grid2(gtx, 1, 1,
				func(gtx C) D {
					return panel(gtx, "P 核倍频 (1-10)", coreInputs(pget, coreGroup{off: 0, n: 10, pre: "P"}))
				},
				func(gtx C) D {
					return panel(gtx, "E 核倍频 (1-16)", func(gtx C) D {
						egs := ecoreFieldGroups()
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, coreInputs(eget, egs[0])),
							layout.Flexed(1, coreInputs(eget, egs[1])))
					})
				})
		},
		func(gtx C) D {
			return row(gtx,
				func(gtx C) D { return g.w.cpuApplyB.layout(gtx, "保存并应用") },
				hSpacer(12),
				func(gtx C) D { return g.w.cpuDefB.layout(gtx, "恢复默认") })
		})
}

// ---------- GPU 超频 ----------

func (g *GUI) pageGPU(gtx C) D {
	s := g.sys.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return txt(gtx, "正在读取 GPU 信息…", textStyle{Size: fsBig, Col: colText3}) })
	}
	gi := s.gpu
	dual := gi.Count > 1
	g.w.gc0.syncExt(int(gi.Saved0.Core))
	g.w.gm0.syncExt(int(gi.Saved0.Mem))
	g.w.gc1.syncExt(int(gi.Saved1.Core))
	g.w.gm1.syncExt(int(gi.Saved1.Mem))
	if g.w.gpuSaveB.Clicked(gtx) {
		c0, m0, c1, m1 := g.w.gc0.value(), g.w.gm0.value(), g.w.gc1.value(), g.w.gm1.value()
		g.run("GPU 偏移已下发", pgGPU, actDur, func() error { return g.be.GPUSave(c0, m0, c1, m1, dual) })
	}
	if g.w.gpuResetB.Clicked(gtx) {
		g.run("已恢复默认", pgGPU, actDur, func() error { return g.be.GPUReset(dual) })
	}
	readyTxt := "未就绪"
	if gi.Ready {
		readyTxt = "就绪"
	}
	offsetPanel := func(gtx C) D {
		return col(gtx,
			func(gtx C) D {
				return row(gtx, func(gtx C) D { return txt(gtx, "GPU0 核心", textStyle{Size: fsBody, Col: colText3}) },
					layout.Flexed(1, func(gtx C) D { return D{} }),
					func(gtx C) D {
						return txt(gtx, fmt.Sprintf("%d MHz", g.w.gc0.value()), textStyle{Size: fsBig, Col: colOk})
					})
			},
			func(gtx C) D { return g.w.gc0.layout(gtx) },
			func(gtx C) D {
				return row(gtx, func(gtx C) D { return txt(gtx, "GPU0 显存", textStyle{Size: fsBody, Col: colText3}) },
					layout.Flexed(1, func(gtx C) D { return D{} }),
					func(gtx C) D {
						return txt(gtx, fmt.Sprintf("%d MHz", g.w.gm0.value()), textStyle{Size: fsBig, Col: colOk})
					})
			},
			func(gtx C) D { return g.w.gm0.layout(gtx) },
			func(gtx C) D {
				if !dual {
					return D{}
				}
				return col(gtx,
					vSpacer(8),
					func(gtx C) D {
						return row(gtx, func(gtx C) D { return txt(gtx, "GPU1 核心", textStyle{Size: fsBody, Col: colText3}) },
							layout.Flexed(1, func(gtx C) D { return D{} }),
							func(gtx C) D {
								return txt(gtx, fmt.Sprintf("%d MHz", g.w.gc1.value()), textStyle{Size: fsBig, Col: colOk})
							})
					},
					func(gtx C) D { return g.w.gc1.layout(gtx) },
					func(gtx C) D {
						return row(gtx, func(gtx C) D { return txt(gtx, "GPU1 显存", textStyle{Size: fsBody, Col: colText3}) },
							layout.Flexed(1, func(gtx C) D { return D{} }),
							func(gtx C) D {
								return txt(gtx, fmt.Sprintf("%d MHz", g.w.gm1.value()), textStyle{Size: fsBig, Col: colOk})
							})
					},
					func(gtx C) D { return g.w.gm1.layout(gtx) })
			},
			vSpacer(12),
			func(gtx C) D {
				return row(gtx, func(gtx C) D { return actBtn(gtx, &g.w.gpuSaveB, "保存并应用", btnPri, false) },
					hSpacer(12), func(gtx C) D { return actBtn(gtx, &g.w.gpuResetB, "恢复默认", btnPlain, false) })
			},
			vSpacer(6),
			func(gtx C) D {
				return txt(gtx, "偏移量以设备设置格式写入,保存后立即生效", textStyle{Size: fsBody2, Col: colText3})
			})
	}
	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "GPU 超频", "核心与显存偏移,保存后立即应用", func(gtx C) D {
				return pillBadge(gtx, fmt.Sprintf("%d GPU", gi.Count), colViolet)
			})
		},
		func(gtx C) D {
			return grid2Fill(gtx, 1, 1,
				func(gtx C) D {
					return panel(gtx, "GPU 资讯", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return kvRow(gtx, "GPU 数量", strconv.Itoa(gi.Count)) },
							func(gtx C) D { return kvRow(gtx, "基础频率", fmt.Sprintf("%d MHz", gi.BaseClock)) },
							func(gtx C) D { return kvRow(gtx, "NVAPI", readyTxt) },
							func(gtx C) D { return kvRow(gtx, "驱动", orDash(gi.Driver)) },
							func(gtx C) D {
								return kvRow(gtx, "当前生效", currentOffsetTxt(gi))
							})
					})
				},
				func(gtx C) D { return panel(gtx, "超频偏移 (MHz)", offsetPanel) })
		})
}

// ---------- 电池 ----------

func (g *GUI) pageBat(gtx C) D {
	s := g.bat.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return txt(gtx, "正在读取电池信息…", textStyle{Size: fsBig, Col: colText3}) })
	}
	if g.w.batStartB.clicked(gtx) {
		g.run("已下发", pgBat, 600*time.Millisecond, func() error { g.be.BatteryRefresh("start"); return nil })
	}
	if g.w.batStopB.Clicked(gtx) {
		g.run("已下发", pgBat, 400*time.Millisecond, func() error { g.be.BatteryRefresh("stop"); return nil })
	}
	// 无电池 / 未取到容量时不得显示"健康":Evaluate 在数据全空时同样返回 Healthy,
	// 只能以设备是否报告设计容量作为"有电池数据"的判据(与 FlexiCharger 页一致)。
	hasBat := s.Design > 0
	hc := []string{"健康", "老化", "需要更换", "永久失效"}
	hcTxt, kind := "无电池数据", ""
	if hasBat {
		if s.HealthCode >= 0 && s.HealthCode < len(hc) {
			hcTxt = hc[s.HealthCode]
			switch s.HealthCode {
			case 0:
				kind = "ok"
			case 1:
				kind = "warn"
			default:
				kind = "err"
			}
		} else {
			hcTxt = "未知"
		}
	}
	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "电池", "健康度 / 校准状态与充放电控制", func(gtx C) D {
				return pillBadge(gtx, hcTxt, healthCol(kind))
			})
		},
		func(gtx C) D {
			return grid2Fill(gtx, 1, 1,
				func(gtx C) D {
					return panel(gtx, "电池信息", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return kvRow(gtx, "循环次数", numOrDash(s.CycleCount)) },
							func(gtx C) D { return kvRow(gtx, "满充容量", mAhOrDash(s.FullCharge)) },
							func(gtx C) D { return kvRow(gtx, "设计容量", mAhOrDash(s.Design)) },
							func(gtx C) D { return kvRow(gtx, "容量保持", pctOrDash(float32(s.CapacityPct))) },
							func(gtx C) D { return kvRow(gtx, "使用年限", yearsOrDash(s.AgeYears)) },
							func(gtx C) D { return kvRow(gtx, "停充阈值", pctOrDash(float32(s.StopThresh))) },
							vSpacer(12),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "健康判定:", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(10), func(gtx C) D { return badge(gtx, hcTxt, kind) })
							})
					})
				},
				func(gtx C) D {
					return panel(gtx, "电池校准", func(gtx C) D {
						return col(gtx,
							func(gtx C) D {
								return txt(gtx, "校准会强制断开适配器放电,请接入电源后开始;整个过程可能耗时数小时。", textStyle{Size: fsBody2, Col: colText3})
							},
							vSpacer(14),
							func(gtx C) D {
								if s.Ongoing == 0 {
									return row(gtx, func(gtx C) D { return g.w.batStartB.layout(gtx, "开始校准") })
								}
								return row(gtx,
									func(gtx C) D { return badge(gtx, fmt.Sprintf("校准进行中(阶段 %d)", s.Ongoing), "warn") },
									hSpacer(12),
									func(gtx C) D { return actBtn(gtx, &g.w.batStopB, "停止", btnPlain, false) })
							})
					})
				})
		})
}

// ---------- 节能计划 ----------

func (g *GUI) pageEnergy(gtx C) D {
	g.histMu.Lock()
	hist := make([]float64, len(g.wattHist))
	copy(hist, g.wattHist)
	meta := g.histMeta
	g.histMeta = meta
	g.histMu.Unlock()

	if g.w.enApply.Clicked(gtx) {
		p := energysave.Profile{
			Name: "go", Schedule1On: g.w.es1on.Value,
			Schedule1Start: g.w.es1s.value(), Schedule1End: g.w.es1e.value(),
			Schedule2On:    g.w.es2on.Value,
			Schedule2Start: g.w.es2s.value(), Schedule2End: g.w.es2e.value(),
			Everyday:       g.w.esEv.selected() == 0,
			Weekdays:       [7]bool{true, true, true, true, true, true, true},
			AutoBattery:    true,
			ChargeLimit:    []int{60, 70, 80, 90, 100}[g.w.ecCL.selected()],
			DischargeLimit: []int{40, 50, 60, 70, 80, 90, 95}[g.w.ecDL.selected()],
		}
		if p.Overlap() {
			g.toast("两个调度重叠(至少间隔 1 小时)")
		} else {
			g.run("节能计划已下发", pgEnergy, actDur, func() error { return g.be.EnergyApply(p) })
		}
	}
	if g.w.enDisable.Clicked(gtx) {
		g.run("已停用(阈值 100/95)", pgEnergy, actDur, func() error {
			return g.be.EnergyApply(energysave.Profile{Name: "off", ChargeLimit: 100, DischargeLimit: 95,
				Everyday: true, Weekdays: [7]bool{true, true, true, true, true, true, true}})
		})
	}

	nowTxt := "-- W"
	if len(hist) > 0 {
		nowTxt = fmt.Sprintf("%.1f W", hist[len(hist)-1])
	}
	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "节能计划", "实时功耗曲线与充放电阈值/调度", func(gtx C) D {
				return pillBadge(gtx, nowTxt, colOk)
			})
		},
		func(gtx C) D {
			return grid2Fill(gtx, 1, 1,
				func(gtx C) D {
					return panel(gtx, "实时功耗", func(gtx C) D {
						return col(gtx,
							func(gtx C) D { return drawWattChart(gtx, hist) },
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, nowTxt, textStyle{Size: fsStat, Col: colText}) },
									hSpacer(14),
									func(gtx C) D {
										return txt(gtx, fmt.Sprintf("电池 %.1f Wh · 适配器/PD %.0f W", meta.BatWh, meta.PD), textStyle{Size: fsBody, Col: colText3})
									})
							})
					})
				},
				func(gtx C) D {
					return panel(gtx, "充放电阈值 / 调度", func(gtx C) D {
						return col(gtx,
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "充电上限 %", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D {
										return g.w.ecCL.layout(gtx)
									},
									hSpacer(14),
									func(gtx C) D { return txt(gtx, "放电下限 %", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(6), func(gtx C) D { return g.w.ecDL.layout(gtx) })
							},
							vSpacer(10),
							func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(func(gtx C) D { return txt(gtx, "调度一", textStyle{Size: fsBody, Col: colText}) }), layout.Rigid(vSpacer(2)))
							},
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return switchBox(gtx, &g.w.es1on) },
									hSpacer(8),
									func(gtx C) D { return numBox(gtx, &g.w.es1s, 64) },
									func(gtx C) D { return txt(gtx, "时起", textStyle{Size: fsBody, Col: colText3}) },
									func(gtx C) D { return numBox(gtx, &g.w.es1e, 64) },
									func(gtx C) D { return txt(gtx, "时止", textStyle{Size: fsBody, Col: colText3}) })
							},
							vSpacer(6),
							func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(func(gtx C) D { return txt(gtx, "调度二", textStyle{Size: fsBody, Col: colText}) }), layout.Rigid(vSpacer(2)))
							},
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return switchBox(gtx, &g.w.es2on) },
									hSpacer(8),
									func(gtx C) D { return numBox(gtx, &g.w.es2s, 64) },
									func(gtx C) D { return txt(gtx, "时起", textStyle{Size: fsBody, Col: colText3}) },
									func(gtx C) D { return numBox(gtx, &g.w.es2e, 64) },
									func(gtx C) D { return txt(gtx, "时止", textStyle{Size: fsBody, Col: colText3}) })
							},
							vSpacer(8),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return txt(gtx, "重复", textStyle{Size: fsBody, Col: colText3}) },
									hSpacer(8), func(gtx C) D { return g.w.esEv.layout(gtx) })
							},
							vSpacer(10),
							func(gtx C) D {
								return row(gtx, func(gtx C) D { return actBtn(gtx, &g.w.enApply, "应用节能计划", btnPri, false) },
									hSpacer(12), func(gtx C) D { return actBtn(gtx, &g.w.enDisable, "停用", btnPlain, false) })
							},
							vSpacer(4),
							func(gtx C) D {
								return txt(gtx, "下发内容:时间同步 → 生效日 → 两个调度 → 使能 → 阈值", textStyle{Size: fsBody2, Col: colText3})
							})
					})
				})
		})
}

// ---------- 偏好 ----------

func (g *GUI) pagePref(gtx C) D {
	s := g.prefSnap.Load()
	if s == nil {
		return panel(gtx, "正在加载", func(gtx C) D { return txt(gtx, "正在读取偏好…", textStyle{Size: fsBig, Col: colText3}) })
	}
	g.w.pfFnLock.Value = s.Keyboard.FnLock
	g.w.pfWin.Value = s.Keyboard.WinKey
	g.w.pfFnWin.Value = s.Keyboard.FnWin
	g.w.pfPg.Value = s.PgUpDn
	g.w.pfBL.syncExt(s.BlueLight)

	if g.w.pfKbApply.Clicked(gtx) {
		fn, win, fnw := g.w.pfFnLock.Value, g.w.pfWin.Value, g.w.pfFnWin.Value
		g.run("键盘设置已下发", pgPref, actDur, func() error { g.be.SetKeyboardPref(fn, win, fnw); return nil })
	}
	if g.w.pfDispApply.Clicked(gtx) {
		bl, pg := g.w.pfBL.value(), g.w.pfPg.Value
		g.run("显示设置已应用", pgPref, actDur, func() error {
			g.be.SetBlueLight(float64(bl))
			g.be.SetPgUpDn(pg)
			return nil
		})
	}
	// GPU 模式下拉:按支持位重建选项
	type gpuOpt struct {
		label string
		val   int
	}
	var gpuOpts []gpuOpt
	if s.GPUSwitch.Support&1 != 0 {
		gpuOpts = append(gpuOpts, gpuOpt{"仅集成显卡", 1})
	}
	if s.GPUSwitch.Support&2 != 0 {
		gpuOpts = append(gpuOpts, gpuOpt{"仅独立显卡", 2})
	}
	if s.GPUSwitch.Support&4 != 0 {
		gpuOpts = append(gpuOpts, gpuOpt{"MS Hybrid", 3})
	}
	if s.GPUSwitch.Support&8 != 0 {
		gpuOpts = append(gpuOpts, gpuOpt{"动态切换", 4})
	}
	g.w.pfGPU.opts = make([]string, len(gpuOpts))
	gpuSel := 0
	for i, o := range gpuOpts {
		g.w.pfGPU.opts[i] = o.label
		if o.val == s.GPUSwitch.Current {
			gpuSel = i
		}
	}
	g.w.pfGPU.sel = gpuSel
	if g.w.pfGPUApply.clicked(gtx) {
		if gpuSel >= 0 && gpuSel < len(gpuOpts) {
			v := gpuOpts[gpuSel].val
			g.run("已下发,即将重启", -1, 0, func() error { g.be.SetGPUSwitch(v); return nil })
		}
	}
	if g.w.pfLangApply.Clicked(gtx) {
		v := g.w.pfLang.selected()
		g.run("语言已切换", pgPref, actDur, func() error { g.be.SetLanguage(v); return nil })
	}

	return col(gtx,
		func(gtx C) D {
			return pageHead(gtx, "偏好", "键盘 Fn 行为 / 显示 / GPU 模式 / 界面语言", func(gtx C) D {
				return pillBadge(gtx, "应用后生效", colText3)
			})
		},
		func(gtx C) D {
			return fillPair(gtx,
				func(gtx C) D {
					return colFill(gtx, pageSlack,
						func(gtx C) D {
							return panel(gtx, "键盘 Fn 键行为", func(gtx C) D {
								return col(gtx,
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return txt(gtx, "FnLock", textStyle{Size: fsBody, Col: colText3}) },
											hSpacer(6), func(gtx C) D { return switchBox(gtx, &g.w.pfFnLock) },
											hSpacer(14), func(gtx C) D { return txt(gtx, "Win 键启用", textStyle{Size: fsBody, Col: colText3}) },
											hSpacer(6), func(gtx C) D { return switchBox(gtx, &g.w.pfWin) },
											hSpacer(14), func(gtx C) D { return txt(gtx, "Win+Fn 切换", textStyle{Size: fsBody, Col: colText3}) },
											hSpacer(6), func(gtx C) D { return switchBox(gtx, &g.w.pfFnWin) })
									},
									vSpacer(10),
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return actBtn(gtx, &g.w.pfKbApply, "应用", btnPri, false) },
											hSpacer(12), func(gtx C) D { return txt(gtx, "应用后立即生效", textStyle{Size: fsBody2, Col: colText3}) })
									})
							})
						},
						func(gtx C) D {
							return panel(gtx, "显示", func(gtx C) D {
								return col(gtx,
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return txt(gtx, "蓝光过滤", textStyle{Size: fsBody, Col: colText3}) },
											hSpacer(8), func(gtx C) D {
												return txt(gtx, fmt.Sprintf("%d/10", g.w.pfBL.value()), textStyle{Size: fsBig, Col: colOk})
											})
									},
									func(gtx C) D { return g.w.pfBL.layout(gtx) },
									vSpacer(8),
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return txt(gtx, "PgUp/PgDn 启用", textStyle{Size: fsBody, Col: colText3}) },
											hSpacer(8), func(gtx C) D { return switchBox(gtx, &g.w.pfPg) },
											hSpacer(14), func(gtx C) D { return actBtn(gtx, &g.w.pfDispApply, "应用", btnPri, false) })
									})
							})
						})
				},
				func(gtx C) D {
					return colFill(gtx, pageSlack,
						func(gtx C) D {
							return panel(gtx, "GPU 模式切换(需重启)", func(gtx C) D {
								return col(gtx,
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return g.w.pfGPU.layout(gtx) },
											hSpacer(14), func(gtx C) D { return g.w.pfGPUApply.layout(gtx, "应用并重启") })
									},
									vSpacer(6),
									func(gtx C) D {
										return txt(gtx, "切换后立即重启,未保存的改动会丢失", textStyle{Size: fsBody2, Col: colText3})
									})
							})
						},
						func(gtx C) D {
							return panel(gtx, "界面语言", func(gtx C) D {
								return col(gtx,
									func(gtx C) D {
										return row(gtx, func(gtx C) D { return g.w.pfLang.layout(gtx) },
											hSpacer(14), func(gtx C) D { return actBtn(gtx, &g.w.pfLangApply, "应用", btnPri, false) })
									},
									vSpacer(6),
									func(gtx C) D {
										return txt(gtx, "应用后立即生效", textStyle{Size: fsBody2, Col: colText3})
									})
							})
						})
				})
		})
}

// mhzOrDash 频率读数:0 表示该通道没取到(无 XTU/BIOS 支持时恒零),
// 显示占位符而不是冒充真实读数。
func mhzOrDash(v int) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d MHz", v)
}

// rangeOrDash 带单位的区间读数:两端都缺失时整体显示占位符。
func rangeOrDash(lo, hi int, unit string) string {
	if lo <= 0 && hi <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d ~ %d %s", lo, hi, unit)
}

// ratioOrDash 倍频区间读数。
func ratioOrDash(lo, hi int) string {
	if lo <= 0 && hi <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dx ~ %dx", lo, hi)
}

// currentOffsetTxt GPU 当前生效偏移:NVAPI 未就绪且读数为空时不显示假 0。
func currentOffsetTxt(gi GPUOCOut) string {
	if !gi.Ready && gi.Current.Core == 0 && gi.Current.Mem == 0 {
		return "—"
	}
	return fmt.Sprintf("核心 %d / 显存 %d MHz", gi.Current.Core, gi.Current.Mem)
}

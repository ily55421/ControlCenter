// widgets — 全部页面的交互控件状态(Gio 要求状态与布局分离,状态跨帧保持)。
package gui

import "gioui.org/widget"

type widgets struct {
	sideToggle, info widget.Clickable
	nav              [pgCount]widget.Clickable

	// 系统监控
	modeTab    [5]widget.Clickable
	fanTab     [5]widget.Clickable // 对应 fanModes 下标 {0,1,6,3,5}
	noiseless  widget.Bool
	tileWin    widget.Clickable // 偏好磁贴
	tileFnWin  widget.Clickable
	tileFnLock widget.Clickable
	tileTurbo  widget.Clickable
	tileDTT    widget.Clickable
	tileLow    widget.Clickable
	fanOffset  *neonSlider
	blSlider   *neonSlider
	dustRun    widget.Clickable
	dustApply  widget.Clickable
	dustDays   numField
	dustHour   numField
	dustResume widget.Bool
	fanSave    widget.Clickable
	fanReset   widget.Clickable
	curveEd    [3][6]numField // 3 风扇 × (T1..T3, D1..D3)
	curveTag   [3]struct{}    // 每风扇曲线编辑器的指针输入 tag
	gpuSwitch  widget.Clickable

	// LED 设备
	kbOn       widget.Bool
	kbOvr      widget.Bool
	kbSleepOff widget.Clickable
	kbSleepOn  widget.Clickable
	slH, slM   numField
	slS        numField
	kbPart     *dropdown
	kbGrid     KBGrid // 逐键点选状态(SelR/SelC=-1 未选)
	kbEff      *dropdown
	kbBr       *neonSlider
	kbApplyEff widget.Clickable
	kbReset    widget.Clickable

	// FlexiKey
	fkMainTab widget.Clickable
	fkStatTab widget.Clickable
	fkMouseTab widget.Clickable
	fkProf    []widget.Clickable // 与快照 profiles 对齐(刷新时重建)
	fkProfN   int
	fkApply   widget.Clickable
	fkDisable widget.Clickable

	// FlexiCharger
	fcMode  [3]widget.Clickable
	fcStart *neonSlider
	fcStop  *neonSlider
	fcApply widget.Clickable

	// CPU 超频
	cpuPL1, cpuPL2, cpuPLT, cpuVR, cpuVO, cpuOV numField
	cpuVRA                                      widget.Bool
	vraDirty                                    bool // 用户改动 VR Auto 后暂不同步快照
	pcoreEd                                     [10]numField
	ecoreEd                                     [16]numField
	cpuApplyB                                   confirmBtn
	cpuDefB                                     confirmBtn

	// GPU 超频
	gc0, gm0, gc1, gm1 *neonSlider
	gpuSaveB           widget.Clickable
	gpuResetB          widget.Clickable

	// 电池
	batStartB confirmBtn
	batStopB  widget.Clickable

	// 节能计划
	ecCL, ecDL   *dropdown
	es1on, es2on widget.Bool
	es1s, es1e   numField
	es2s, es2e   numField
	esEv         *dropdown
	enApply      widget.Clickable
	enDisable    widget.Clickable

	// 偏好
	pfFnLock, pfWin, pfFnWin widget.Bool
	pfKbApply                widget.Clickable
	pfBL                     *neonSlider
	pfPg                     widget.Bool
	pfDispApply              widget.Clickable
	pfGPU                    *dropdown
	pfGPUApply               confirmBtn
	pfLang                   *dropdown
	pfLangApply              widget.Clickable
}

// kbPartNames 分区显示名(与后端 part 值一一对应)。
var kbPartNames = []string{"整键盘", "左区", "中区", "右区", "触摸板", "Logo", "灯带"}
var kbPartVals = []string{"all", "left", "mid", "right", "tp", "logo", "lightbar"}

// kbEffNames 灯效(值 0,2..7,与 Web effects 数组一致)。
var kbEffNames = []string{"随机", "呼吸", "循环", "波浪", "舞蹈", "节拍", "闪烁"}
var kbEffVals = []int{0, 2, 3, 4, 5, 6, 7}

// langNames 语言列表(值 = 下标,与 AppSettings UI language 取值一致)。
var langNames = []string{
	"跟随系统", "English(US)", "English(GB)", "Español", "Deutsch", "Italiano",
	"日本語", "한국어", "简体中文", "繁體中文", "Français", "Français(CA)",
	"Português(BR)", "Português(PT)", "Türkçe", "ไทย", "Tiếng Việt", "Русский", "日本語(EP)",
}

func (g *GUI) initWidgets() {
	w := &g.w
	w.kbGrid.SelR, w.kbGrid.SelC = -1, -1
	w.fanOffset = newSlider(0, 100, false, func(v int) {
		g.run("偏移已下发", -1, 0, func() error { g.be.SetFanOffset(v); return nil })
	})
	w.blSlider = newSlider(0, 10, true, func(v int) {
		g.run("蓝光过滤 "+itoa(v)+" 档", -1, 0, func() error { g.be.SetBlueLight(float64(v)); return nil })
	})
	w.kbPart = newDropdown(kbPartNames, 0, 160)
	w.kbEff = newDropdown(kbEffNames, 0, 160)
	w.kbBr = newSlider(0, 4, true, func(v int) {
		lv := v
		g.run("已下发", -1, 0, func() error { return g.be.KBCmd(KBRequest{Cmd: "brightness", Level: lv}) })
	})
	w.fcStart = newSlider(40, 95, false, nil)
	w.fcStop = newSlider(60, 100, false, nil)
	w.gc0 = newSlider(-200, 400, false, nil)
	w.gm0 = newSlider(-200, 1600, false, nil)
	w.gc1 = newSlider(-200, 400, false, nil)
	w.gm1 = newSlider(-200, 1600, false, nil)
	w.ecCL = newDropdown([]string{"60", "70", "80", "90", "100"}, 4, 90)
	w.ecDL = newDropdown([]string{"40", "50", "60", "70", "80", "90", "95"}, 0, 90)
	w.esEv = newDropdown([]string{"每天", "按星期"}, 0, 110)
	w.pfGPU = newDropdown([]string{"仅集成显卡", "仅独立显卡", "MS Hybrid", "动态切换"}, 2, 200)
	w.pfLang = newDropdown(langNames, 8, 220)

	// 数字输入初值(页面刷新时会被快照同步)
	w.dustDays.init(7)
	w.dustHour.init(3)
	w.slH.init(0)
	w.slM.init(0)
	w.slS.init(30)
	w.cpuPL1.init(45)
	w.cpuPL2.init(65)
	w.cpuPLT.init(28)
	w.cpuVR.init(0)
	w.cpuVO.init(0)
	w.cpuOV.init(0)
	for i := range w.pcoreEd {
		w.pcoreEd[i].init(0)
	}
	for i := range w.ecoreEd {
		w.ecoreEd[i].init(0)
	}
	w.es1s.init(22)
	w.es1e.init(6)
	w.es2s.init(12)
	w.es2e.init(14)
}

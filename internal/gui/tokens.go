// tokens — 设计令牌:配色、度量、圆角、字阶。
//
// 这里是**唯一**允许出现颜色/尺寸常量的地方:页面代码只允许引用这些令牌,
// 不要再裸写 color.NRGBA{...} 或魔法数字大小。改配色只改这一个文件。
//
// 取值原则(与 docs/gio-optimization.md 一致):
//   - 背景不用纯黑(#000),用略偏蓝的 #070A0F:纯黑上的 1px 描边会"发光发脏";
//   - 描边绝不用纯灰实线,#3A3F47 这类实线在 OLED/低亮度屏上会发灰,
//     改用 rgba(148,163,184,.13) 与背景自然融合,圆角处也不会出硬边;
//   - 文本分四级,标题不再与正文同色同重。
package gui

import "image/color"

// ---------- 背景 ----------

var (
	colBg      = color.NRGBA{R: 0x07, G: 0x0A, B: 0x0F, A: 0xFF} // 窗口底
	colSurface = color.NRGBA{R: 0x0F, G: 0x14, B: 0x1C, A: 0xFF} // 卡片底
	colSunken  = color.NRGBA{R: 0x0B, G: 0x0F, B: 0x16, A: 0xFF} // 内嵌区(输入框/图表/键帽底)
	colBar     = color.NRGBA{R: 0x09, G: 0x0D, B: 0x13, A: 0xFF} // 侧栏/标题栏(比窗口底再深一档)
	colHover   = color.NRGBA{R: 0x16, G: 0x1D, B: 0x27, A: 0xFF} // 悬停底
	colActive  = color.NRGBA{R: 0x1B, G: 0x24, B: 0x30, A: 0xFF} // 选中底
)

// ---------- 描边(一律半透明,不用纯灰实线) ----------

var (
	colLine     = color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0x22} // 13% 常规描边
	colLineSoft = color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0x13} // 7.5% 弱分隔
	colLineHi   = color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0x3D} // 24% 强调描边/悬停
	colTopHi    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x0D} // 顶边 1px 内高光
)

// ---------- 文本四级 ----------

var (
	colText  = color.NRGBA{R: 0xE9, G: 0xEE, B: 0xF6, A: 0xFF} // 主文本/标题
	colText2 = color.NRGBA{R: 0xA7, G: 0xB4, B: 0xC7, A: 0xFF} // 次级文本
	colText3 = color.NRGBA{R: 0x6B, G: 0x7C, B: 0x92, A: 0xFF} // 说明/标签
	colText4 = color.NRGBA{R: 0x4D, G: 0x5D, B: 0x72, A: 0xFF} // 最弱(页脚/mono 小标签)
)

// ---------- 语义色 ----------

var (
	colAccent = color.NRGBA{R: 0x22, G: 0xD3, B: 0xEE, A: 0xFF} // 青(主操作)
	colOk     = color.NRGBA{R: 0x34, G: 0xD3, B: 0x99, A: 0xFF} // 绿
	colWarn   = color.NRGBA{R: 0xFB, G: 0xBF, B: 0x24, A: 0xFF} // 黄
	colDanger = color.NRGBA{R: 0xFB, G: 0x71, B: 0x85, A: 0xFF} // 红
	colViolet = color.NRGBA{R: 0xA7, G: 0x8B, B: 0xFA, A: 0xFF} // 紫
)

// ---------- 度量(单位 Dp) ----------

const (
	rCard  = 16 // 卡片圆角
	rCtl   = 10 // 控件圆角(按钮/输入框/下拉)
	rChip  = 9  // 胶囊/徽章圆角
	rSmall = 8  // 小块(图标胶囊/键帽)

	sp4, sp6, sp8, sp10, sp12, sp16, sp20 = 4, 6, 8, 10, 12, 16, 20 // 间距阶梯

	// 2026-11 视觉密度调整:1400dp 窗口下原内边距/字号偏小,页面显得空而碎。
	// 卡片内边距、头部与控件高度整体上调一档,正文 12.8→14sp。
	cardPadX   = 20  // 卡片左右内边距
	cardPadY   = 18  // 卡片上下内边距
	hdrH       = 56  // 卡片头部高
	shellHdrH  = 52  // 标题栏高
	modelineH  = 30  // 型号栏高
	navH       = 44  // 侧栏单行项高
	navW       = 200 // 侧栏展开宽
	navWMin    = 60  // 侧栏收起宽
	ctlH       = 40  // 控件高(按钮/输入框/下拉)
	listRowH   = 36  // 下拉列表单行高
	footH      = 34  // 页脚信息条高
	btnH       = 38  // 按钮高
	switchW    = 48  // 开关宽
	switchH    = 27  // 开关高
	iconChipSz = 34  // 卡片头部图标胶囊边长
	navIconDp  = 19  // 侧栏图标尺寸
	navBarW    = 3   // 选中项左缘强调条宽
	gridGap    = 20  // 双列栅格列间距

	// LED 键盘图几何。原先写死 36/5 **设备像素**,高缩放下整张图不放大、
	// 长标签被键帽裁掉;改为 Dp 后由 Snap 折算。
	kbKeyH   = 36 // 键帽高
	kbKeyGap = 5  // 键帽间距

	// 页面骨架:layoutPage 的 Inset 与默认窗口尺寸计算共用同一组数,
	// 任何一处另写一遍都会在改边距时静默破坏窗口高度(曾致默认开窗即出滚动条)。
	pageTopH   = 18   // 页面上边距
	pageBotH   = 46   // 页面下边距
	pageSideW  = 20   // 页面左右边距
	scrollBarW = 12   // 滚动条右缘占位
	pageBudget = 1180 // 页面自然高度的测试预算

	// 窗口与响应式断点
	winEdgeW  = 8    // 默认窗口与屏幕工作区之间留白
	defWinW   = 1400 // 默认窗口宽
	defWinH   = 900  // 默认窗口高,长页面滚动而不占满屏幕
	minWinW   = 1000 // 窗口最小宽
	minWinH   = 560  // 窗口最小高
	gridWideW = 1320 // 资讯/偏好子栅格开始并排的宽度断点
	gridPairW = 900  // grid2 退化为单列的宽度断点
	halfMinW  = 210  // 半宽卡片可用的最小宽度(不足则单列)

	// 内容区与复杂图形的自适应尺寸。
	// 页面内容宽度上限:超宽窗口下内容左对齐,不把小卡片无限拉宽
	// (与 docs/05-UI设计规范.md 的 1440dp 目标一致)。
	contentMaxW = 1440
	// 键盘图最小可读宽度:再窄就把键帽压到标签放不下(实测 17 列 5 字符标签
	// 需要约 680dp)。不足时保持该宽度并在卡片内局部横向滚动。
	kbMinW = 680
	// 风扇曲线最小列宽;实际按卡片可用宽度等分,最多三列。
	curveColW = 240
	// 超宽卡片里的滑块宽度上限,避免滑块跟随容器无限拉长(比例失衡)。
	sliderMaxW = 360
	// LED 设置区(色环 / 亮度 / 灯效)的紧凑宽度上限:超宽卡片里不把这些控件拉散。
	ledSettingsW = 460
	// LED 色环直径。
	wheelSz = 210

	// 标题栏右侧控件
	winBtnW   = 36 // 最小化/最大化/关闭按钮宽
	infoBtnW  = 32 // 信息按钮宽
	infoBtnH  = 26 // 信息按钮胶囊高
	appIconDp = 22 // 标题栏应用图标边长
	headerGap = 8  // 控件之间的间距
	headerPad = 4  // 右侧留白
)

// ---------- 字阶(Sp) ----------

const (
	fsStat  = 36   // 数字三联的大数字(MHz / 利用率 / 温度)
	fsHero  = 56   // 单页主数字(电池百分比)
	fsTitle = 22   // 页面/卡片标题
	fsBig   = 15   // 卡片标题正文
	fsBody  = 14   // 常规正文
	fsBody2 = 13   // 次级正文/键值
	fsSmall = 12.5 // 说明文字
	fsMicro = 11.5 // mono 小标签
	fsTiny  = 11   // 页脚/极小标注
)

// alpha 返回带透明度的颜色副本。
func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

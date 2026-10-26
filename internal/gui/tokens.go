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

	cardPadX   = 16  // 卡片左右内边距
	cardPadY   = 13  // 卡片上下内边距
	hdrH       = 46  // 卡片头部高
	shellHdrH  = 46  // 标题栏高
	navH       = 38  // 侧栏单行项高
	navW       = 200 // 侧栏展开宽
	navWMin    = 60  // 侧栏收起宽
	ctlH       = 34  // 控件高(按钮/输入框/下拉)
	btnH       = 32  // 按钮高
	switchW    = 42  // 开关宽
	switchH    = 24  // 开关高
	iconChipSz = 28  // 卡片头部图标胶囊边长
	navIconDp  = 17  // 侧栏图标尺寸
)

// ---------- 字阶(Sp) ----------

const (
	fsDisplay = 38   // 大数字(频率/功率)
	fsTitle   = 19   // 页面/卡片标题
	fsBig     = 13.5 // 卡片标题正文
	fsBody    = 12.8 // 常规正文
	fsBody2   = 12   // 次级正文/键值
	fsSmall   = 11   // 说明文字
	fsMicro   = 10.5 // mono 小标签
	fsTiny    = 10   // 页脚/极小标注
)

// alpha 返回带透明度的颜色副本。
func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

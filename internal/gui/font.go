// font — 内嵌字体:Noto Sans SC 子集(SIL OFL 1.1,许可证见 font/OFL.txt)。
//
// 子集只保留 GUI 实际会用到的字符(源字体 8.5MB → 146KB),再生方式:
//
//	go run ./cmd/fontchars ./internal ./cmd > Temp/fontchars.txt
//	python -m fontTools.subset "C:/Windows/Fonts/Noto Sans SC (TrueType).otf" \
//	  --text-file=Temp/fontchars.txt --output-file=internal/gui/font/CCSansSC-Regular.otf \
//	  --drop-tables+=DSIG --name-IDs=2,3,4,6
//
// 子集属于「修改版」,按 OFL 保留字体名条款,运行期族名一律改为 fontFamily。
// 子集未覆盖的字形(▾ ▴ ↵ ⚡ ✓ ✗ 等,Noto 与微软雅黑都没有)由系统字体回退补齐,
// 因此 newTheme 不再使用 text.NoSystemFonts()。
package gui

import (
	_ "embed"

	"gioui.org/font/opentype"
	"gioui.org/text"
)

//go:embed font/CCSansSC-Regular.otf
var ccSansRegular []byte

//go:embed font/CCSansSC-Bold.otf
var ccSansBold []byte

// fontFamily 内嵌子集的统一族名:常规与粗体共用一族,靠 OS/2 权重区分。
const fontFamily = "CC Sans SC"

// embeddedFonts 解析内嵌子集。注册顺序即优先级:常规在前、粗体在后。
func embeddedFonts() []text.FontFace {
	var faces []text.FontFace
	for _, src := range [][]byte{ccSansRegular, ccSansBold} {
		ff, err := opentype.ParseCollection(src)
		if err != nil || len(ff) == 0 {
			continue
		}
		for i := range ff {
			ff[i].Font.Typeface = fontFamily
		}
		faces = append(faces, ff...)
	}
	return faces
}

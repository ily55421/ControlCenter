package gui

import (
	"image"
	"testing"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// 内嵌字体的三条底线:解析得出常规+粗体、中文按真实全角度量渲染、
// 子集缺的字形能被系统回退补上。任一失守都会直接表现为用户看到的
// 「方块/空白/中文与图标错位」。

// testGtx 构造一个 1x 缩放的离屏上下文。
func testGtx(w, h int) layout.Context {
	var ops op.Ops
	return layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(w, h)},
	}
}

// TestEmbeddedFontsParse 内嵌子集必须解出两笔且族名统一。
// 解析失败时 newTheme 会静默退回 material 默认字体(Go Text,无中文),
// 中文立刻变方块 —— 这里兜住这个静默降级。
func TestEmbeddedFontsParse(t *testing.T) {
	faces := embeddedFonts()
	if len(faces) != 2 {
		t.Fatalf("内嵌字体解析出面数 = %d,期望 2(常规 + 粗体)", len(faces))
	}
	weights := map[font.Weight]bool{}
	for _, f := range faces {
		if string(f.Font.Typeface) != fontFamily {
			t.Fatalf("族名未统一为 %q: got %q", fontFamily, f.Font.Typeface)
		}
		weights[f.Font.Weight] = true
	}
	if len(weights) != 2 {
		t.Fatal("两笔字重相同,粗体标签将无法区分")
	}
}

// TestCJKUsesEmbeddedFace 中文必须走内嵌面:四个全角汉字的Advance 应接近 4em。
// 若退化成无中文字形的面,宽度会显著偏小或直接为 0。
func TestCJKUsesEmbeddedFace(t *testing.T) {
	g = &GUI{th: newTheme()}
	gtx := testGtx(2000, 200)
	const size = 20 // Sp,PxPerSp=1 → em=20px
	d := txt(gtx, "系统监控", textStyle{Size: size, Col: colText})
	if d.Size.X == 0 || d.Size.Y == 0 {
		t.Fatalf("中文测量为 0,字体未生效: %+v", d.Size)
	}
	want := 4 * size
	if d.Size.X < want*9/10 || d.Size.X > want*11/10 {
		t.Fatalf("四字全角宽度 = %dpx,期望 %d±10%%(字体面不对或字形缺失)", d.Size.X, want)
	}
	if d.Size.Y < size {
		t.Fatalf("行高 = %dpx,小于 em %dpx,度量异常", d.Size.Y, size)
	}
}

// TestSubsetGapsCoveredByFallback 子集不含 ▾ ▴ ↵ ⚡ ✓ ✗(Noto 与微软雅黑都没有),
// 必须由系统字体回退补上。有人重新加上 text.NoSystemFonts() 时这里会失败:
// 那正是「下拉箭头渲染成空白」的根因。
func TestSubsetGapsCoveredByFallback(t *testing.T) {
	g = &GUI{th: newTheme()}
	gtx := testGtx(2000, 200)
	for _, s := range []string{"▾", "▴", "↵", "✓", "✗"} {
		withGap := txt(gtx, "A"+s+"A", textStyle{Size: 20, Col: colText}).Size.X
		bare := txt(gtx, "AA", textStyle{Size: 20, Col: colText}).Size.X
		if withGap <= bare {
			t.Errorf("%q 没有可用字形(宽度 %d vs 基线 %d):系统字体回退被关掉了?", s, withGap, bare)
		}
	}
}

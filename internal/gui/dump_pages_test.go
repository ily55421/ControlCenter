package gui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestDumpPageScreens 离屏把九个页面整窗渲染成 PNG(视觉核对用,不做像素断言)。
// 默认跳过,CC_DUMP=1 时输出到 Temp/shots/headless/。
func TestDumpPageScreens(t *testing.T) {
	if os.Getenv("CC_DUMP") == "" {
		t.Skip("设置 CC_DUMP=1 才生成页面截图")
	}
	fakeSnaps()
	outDir := filepath.Join("..", "..", "Temp", "shots", "headless")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	size := image.Pt(defWinW, windowHeightFor(pageBudget))
	pages := []struct {
		name string
		id   int
	}{
		{"system", pgSystem}, {"kb", pgKB}, {"fk", pgFlexikey}, {"ch", pgCharger},
		{"cpu", pgCPU}, {"gpu", pgGPU}, {"bat", pgBat}, {"en", pgEnergy}, {"pref", pgPref},
	}
	for _, p := range pages {
		win, err := headless.NewWindow(size.X, size.Y)
		if err != nil {
			t.Skipf("headless 不可用: %v", err)
		}
		g.page = p.id
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(size),
		}
		// 多跑两帧:第一帧让 spreadCol/grid2Fill 拿到自然高度,后续帧才是最终分布。
		g.layoutRoot(gtx)
		ops.Reset()
		g.layoutRoot(gtx)
		ops.Reset()
		g.layoutRoot(gtx)
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("%s 帧提交失败: %v", p.name, err)
		}
		img := image.NewRGBA(image.Rectangle{Max: size})
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("%s 截图失败: %v", p.name, err)
		}
		path := filepath.Join(outDir, fmt.Sprintf("%s.png", p.name))
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("写文件失败: %v", err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatalf("编码失败: %v", err)
		}
		f.Close()
		win.Release()
		t.Logf("%s -> %s", p.name, path)
	}
}

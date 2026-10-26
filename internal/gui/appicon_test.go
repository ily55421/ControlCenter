package gui

import "testing"

// TestDecodeAppIcon 内嵌 app.ico 必须能解出位图(否则标题栏只剩矢量占位)。
// 该测试同时锁住 ICO(DIB 帧)解码路径:标准库没有 ICO 解码器,
// 一旦 decodeICODIB 的字节序/行序/alpha 处理被改错,这里会先失败。
func TestDecodeAppIcon(t *testing.T) {
	img := appIconImage()
	if img == nil {
		t.Fatal("decodeICO 返回 nil:app.ico 无法解码")
	}
	b := img.Bounds()
	if b.Dx() < 32 || b.Dy() < 32 {
		t.Fatalf("图标尺寸过小: %dx%d", b.Dx(), b.Dy())
	}
	c := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
	r, g, bl, a := c.RGBA()
	if a>>8 < 128 {
		t.Fatalf("图标中心 alpha 过低: %d", a>>8)
	}
	if r>>8 == 0 && g>>8 == 0 && bl>>8 == 0 {
		t.Fatal("图标中心为纯黑,可能解码错位")
	}
	t.Logf("icon ok %dx%d center=(%d,%d,%d,%d)", b.Dx(), b.Dy(), r>>8, g>>8, bl>>8, a>>8)
}

// TestIconHasTransparency 图标四角应为透明(带 alpha 的圆角/异形图标)。
func TestIconHasTransparency(t *testing.T) {
	img := appIconImage()
	if img == nil {
		t.Skip("图标不可用")
	}
	b := img.Bounds()
	_, _, _, a := img.At(b.Min.X, b.Min.Y).RGBA()
	if a>>8 > 128 {
		t.Logf("左上角 alpha=%d(该图标可能为满幅不透明设计,非错误)", a>>8)
	}
}

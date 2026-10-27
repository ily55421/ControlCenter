// appicon.go — 自绘标题栏左侧的应用图标、ICO 解码与细线绘制辅助。
package gui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

var (
	icoOnce   sync.Once
	icoImg    image.Image
	icoOp     paint.ImageOp
	icoOpInit bool
)

// icoFrame 从 ICO 目录里挑最大的一帧,返回其数据段(可能是 PNG,也可能是 BMP)。
func icoFrame(data []byte) []byte {
	if len(data) < 6+16 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	bestSize, bestOff := 0, -1
	for i := 0; i < count; i++ {
		p := 6 + i*16
		if p+16 > len(data) {
			break
		}
		sz := int(binary.LittleEndian.Uint32(data[p+8:]))
		off := int(binary.LittleEndian.Uint32(data[p+12:]))
		if off <= 0 || sz <= 0 || off+sz > len(data) {
			continue
		}
		if sz > bestSize {
			bestSize, bestOff = sz, off
		}
	}
	if bestOff < 0 {
		return nil
	}
	return data[bestOff : bestOff+bestSize]
}

// decodeICO 解出 ICO 中最大的一帧。
// 标准库没有 ICO 解码器(DIB 帧无法直接丢给 image.Decode),
// 因此:PNG 帧交给 image.Decode;BMP/DIB 帧按 header 自行还原为 RGBA。
func decodeICO(data []byte) image.Image {
	frame := icoFrame(data)
	if frame == nil {
		return nil
	}
	// PNG 帧(0x89 'P' 'N' 'G')
	if len(frame) > 8 && frame[0] == 0x89 && frame[1] == 'P' && frame[2] == 'N' && frame[3] == 'G' {
		if img, _, err := image.Decode(bytes.NewReader(frame)); err == nil {
			return img
		}
		return nil
	}
	return decodeICODIB(frame)
}

// decodeICODIB 解 ICO 的 BMP 帧:头 40 字节 BITMAPINFOHEADER,
// 其后是 自下而上 的 BGRA 像素 + 1bpp AND 掩码(现代 32bpp 图标通常直接用 alpha)。
func decodeICODIB(b []byte) image.Image {
	if len(b) < 40 {
		return nil
	}
	hdrSize := int(binary.LittleEndian.Uint32(b[0:]))
	if hdrSize < 40 || hdrSize > len(b) {
		return nil
	}
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	// ICO 里的高度字段是 XOR+AND 两张位图之和,真实高度需减半
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	compression := binary.LittleEndian.Uint32(b[16:])
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 || compression != 0 {
		return nil
	}
	if bpp != 32 && bpp != 24 {
		return nil // 仅支持常见真彩帧
	}
	off := hdrSize
	rowBytes := ((w*bpp + 31) / 32) * 4
	if off+rowBytes*h > len(b) {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		src := off + (h-1-y)*rowBytes // 位图自下而上存储
		for x := 0; x < w; x++ {
			i := src + x*(bpp/8)
			bch, g, r := b[i], b[i+1], b[i+2]
			a := uint8(0xFF)
			if bpp == 32 {
				a = b[i+3]
			}
			// 不做"alpha=0 视为不透明"的兜底:该图标自带抗锯齿 alpha,
			// 强制不透明会让圆角边缘出现黑边。
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: bch, A: a})
		}
	}
	return img
}

// appIconImage 解析嵌入图标,失败时使用矢量图标。
func appIconImage() image.Image {
	icoOnce.Do(func() {
		img := decodeICO(appIconICO)
		if img == nil {
			return
		}
		icoImg = img
		icoOp = paint.NewImageOp(img)
		icoOpInit = true
	})
	return icoImg
}

// drawAppIcon 在 (x,y) 处绘制 size×size 的应用图标;无可用位图时画矢量占位。
func drawAppIcon(gtx C, x, y, size int) {
	if appIconImage() == nil || !icoOpInit {
		cx, cy := float32(x+size/2), float32(y+size/2)
		r := float32(size) / 2
		fillCircle(gtx, cx, cy, r, pal.Lime)
		fillCircle(gtx, cx, cy, r*0.55, pal.BG)
		fillCircle(gtx, cx, cy, r*0.22, pal.Lime)
		return
	}
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rect(0, 0, size, size)).Push(gtx.Ops).Pop()
	bounds := icoImg.Bounds()
	sx, sy := float32(size)/float32(bounds.Dx()), float32(size)/float32(bounds.Dy())
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sx, sy))).Push(gtx.Ops).Pop()
	icoOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// appIconWidget 应用图标的容器形态:尺寸按 Dp 取,位置交给外层 Flex/Stack,
// 不再由调用方传绝对坐标(那样每个调用点都得自己算垂直居中)。
func appIconWidget(gtx C, size unit.Dp) D {
	s := Snap(gtx, size)
	drawAppIcon(gtx, 0, 0, s)
	return D{Size: image.Pt(s, s)}
}

// fillLine 画任意角度细线(标题栏关闭按钮的叉):
// 以 (x0,y0) 为原点旋转后画一条 length×thick 横条。
func fillLine(gtx C, x0, y0, x1, y1, thick int, c color.NRGBA) {
	dx, dy := float32(x1-x0), float32(y1-y0)
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length < 0.5 {
		return
	}
	ang := math.Atan2(float64(dy), float64(dx))
	t := float32(thick)
	cos, sin := float32(math.Cos(ang)), float32(math.Sin(ang))
	tr := op.Affine(f32.NewAffine2D(
		cos, -sin, float32(x0),
		sin, cos, float32(y0),
	)).Push(gtx.Ops)
	fillArea(gtx, image.Rect(0, int(-t/2), int(length), int(t/2)+1), c)
	tr.Pop()
}

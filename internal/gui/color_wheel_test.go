package gui

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestWheelHitMatchesPaintedHue(t *testing.T) {
	regressionTheme(t)
	const size = 200
	img := regressionImage(t, image.Pt(size, size), func(gtx C) D {
		return colorWheel(gtx, size, "", "", nil)
	})
	for _, p := range []image.Point{{180, 100}, {100, 180}, {20, 100}, {100, 20}, {155, 155}} {
		angle, ok := wheelAngle(size, p.X, p.Y)
		if !ok {
			t.Fatalf("ring point %v was not selectable", p)
		}
		r, g, b := hsl2rgb(angle/(2*math.Pi), 1, 0.5)
		got := img.RGBAAt(p.X, p.Y)
		peak := max(int(got.R), int(got.G), int(got.B))
		if peak < 150 {
			t.Fatalf("ring point %v did not render: %v", p, got)
		}
		for i, channel := range []int{int(got.R), int(got.G), int(got.B)} {
			want := []int{int(r), int(g), int(b)}[i]
			if math.Abs(float64(channel*255/peak-want)) > 10 {
				t.Errorf("point %v: painted %v, selected rgb(%d,%d,%d)", p, got, r, g, b)
				break
			}
		}
	}
	for _, p := range []image.Point{{100, 100}, {0, 0}, {199, 199}} {
		if _, ok := wheelAngle(size, p.X, p.Y); ok {
			t.Errorf("point outside ring %v was selectable", p)
		}
	}
}

func TestWheelStaysInsideAllocation(t *testing.T) {
	regressionTheme(t)
	const size = 160
	img := regressionImage(t, image.Pt(210, 210), func(gtx C) D {
		return colorWheel(gtx, size, "", "", nil)
	})
	for y := size; y < 210; y++ {
		for x := 0; x < 210; x++ {
			if got := img.RGBAAt(x, y); got != (color.RGBA{A: 255}) {
				t.Fatalf("wheel painted beyond allocated height at (%d,%d): %v", x, y, got)
			}
		}
	}
}

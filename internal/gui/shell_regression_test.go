package gui

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
)

func TestShellRegressionTextConstraints(t *testing.T) {
	regressionTheme(t)
	var ops op.Ops
	var router input.Router
	gtx := regressionContext(&ops, &router, layout.Exact(image.Pt(320, 140)))
	st := textStyle{Size: 18, Col: color.NRGBA{R: 255, G: 255, B: 255, A: 255}}
	const text = "Control Center"
	loose := gtx
	loose.Constraints.Min = image.Point{}
	want := txt(loose, text, st)
	if want.Size.X <= 0 || want.Size.Y <= 0 || want.Size.Y >= 140 {
		t.Fatalf("invalid natural text dimensions: %+v", want)
	}
	if got := txt(gtx, text, st); got.Size != image.Pt(320, want.Size.Y) {
		t.Fatalf("txt retained Exact height or lost allocated width: %+v", got)
	}
	for _, constraints := range []layout.Constraints{gtx.Constraints, {Max: image.Pt(1, 1)}} {
		gtx.Constraints = constraints
		if got := measureText(gtx, text, st); got != want {
			t.Errorf("measureText(%v) = %+v, want %+v", constraints, got, want)
		}
	}
}

func TestShellRegressionDrawTextAtExact(t *testing.T) {
	regressionTheme(t)
	st := textStyle{Size: 18, Col: color.NRGBA{R: 255, G: 255, B: 255, A: 255}}
	size := image.Pt(300, 120)
	want := regressionImage(t, size, func(gtx C) D {
		defer op.Offset(image.Pt(17, 23)).Push(gtx.Ops).Pop()
		gtx.Constraints.Min = image.Point{}
		return txt(gtx, "Control Center", st)
	})
	got := regressionImage(t, size, func(gtx C) D {
		drawTextAt(gtx, 17, 23, "Control Center", st)
		return D{}
	})
	if countBright(want, image.Rect(17, 23, 250, 70)) < 20 {
		t.Fatal("reference text did not render at the requested offset")
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("drawTextAt under Exact differs from naturally sized, offset text")
	}
}

func TestShellRegressionStrokePreservesInterior(t *testing.T) {
	img := regressionImage(t, image.Pt(80, 80), func(gtx C) D {
		fillRect(gtx, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		strokeRR(gtx, image.Rect(8, 8, 72, 72), 12, 4,
			color.NRGBA{R: 240, G: 240, B: 240, A: 128})
		return D{}
	})
	bg := img.RGBAAt(0, 0)
	for _, p := range []image.Point{image.Pt(20, 20), image.Pt(40, 40), image.Pt(59, 59)} {
		if got := img.RGBAAt(p.X, p.Y); got != bg {
			t.Errorf("semi-transparent stroke filled interior at %v: %v, background %v", p, got, bg)
		}
	}
	if edge := img.RGBAAt(40, 9); int(edge.G) <= int(bg.G)+20 {
		t.Fatalf("stroke edge was not drawn: %v", edge)
	}
}

func TestShellRegressionAppIconScaledTo22(t *testing.T) {
	if appIconImage() == nil {
		t.Fatal("embedded control-center-v2.ico did not decode")
	}
	const x0, y0, size = 7, 5, 22
	img := regressionImage(t, image.Pt(36, 34), func(gtx C) D {
		drawAppIcon(gtx, x0, y0, size)
		return D{}
	})
	cyan, green, far := 0, 0, 0
	bounds := image.Rect(x0, y0, x0+size, y0+size)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := img.RGBAAt(x, y)
			if !image.Pt(x, y).In(bounds) {
				if c != (color.RGBA{A: 255}) {
					t.Fatalf("icon painted outside 22px allocation at (%d,%d): %v", x, y, c)
				}
				continue
			}
			isCyan := c.G > 90 && c.B > 90 && int(c.G) > int(c.R)+30 && int(c.B) > int(c.R)+30
			isGreen := c.G > 90 && int(c.G) > int(c.R)+30 && int(c.G) > int(c.B)+20
			if isCyan {
				cyan++
			}
			if isGreen {
				green++
			}
			if (isCyan || isGreen) && x >= x0+size/2 && y >= y0+size/2 {
				far++
			}
		}
	}
	if cyan < 3 || green < 3 || far < 2 {
		t.Fatalf("22px icon lost full artwork: cyan=%d green=%d bottom-right=%d", cyan, green, far)
	}
}

func TestShellRegressionWindowWorkArea(t *testing.T) {
	for _, tc := range []struct{ area, want image.Point }{
		{image.Pt(1920, 1040), image.Pt(1400, 900)},
		{image.Pt(1408, 908), image.Pt(1400, 900)},
		{image.Pt(1400, 900), image.Pt(1392, 892)},
		{image.Pt(1366, 728), image.Pt(1358, 720)},
		{image.Pt(800, 500), image.Pt(792, 492)},
		{image.Pt(1920, 600), image.Pt(1400, 592)},
		{image.Pt(900, 1080), image.Pt(892, 900)},
		{image.Point{}, image.Pt(1400, 900)},
	} {
		w, h := windowSizeForWorkArea(tc.area.X, tc.area.Y)
		if got := image.Pt(w, h); got != tc.want {
			t.Errorf("work area %v: size=%v, want %v", tc.area, got, tc.want)
		}
	}
}

func TestShellRegressionAnimationStartsAtTarget(t *testing.T) {
	regressionTheme(t)
	g.win = new(app.Window) // No Event call: no native window or hardware backend.
	for _, target := range []float32{0, 43, 100} {
		var a animVal
		if got := a.tick(target); got != target || !a.started {
			t.Fatalf("first tick(%v)=%v, started=%v", target, got, a.started)
		}
		if got := a.tick(target); got != target {
			t.Fatalf("unchanged target drifted: %v", got)
		}
		next := target + 20
		if got := a.tick(next); got <= target || got >= next {
			t.Fatalf("subsequent change did not animate: %v -> %v (target %v)", target, got, next)
		}
		for range 64 {
			a.tick(next)
		}
		if a.cur != next {
			t.Fatalf("animation did not converge: %v, want %v", a.cur, next)
		}
	}
}

func TestShellRegressionScrollRange(t *testing.T) {
	for _, tc := range []struct {
		offset, max float32
		want        pointer.ScrollRange
	}{
		{0, 0, pointer.ScrollRange{}},
		{0, 100, pointer.ScrollRange{Min: 0, Max: 100}},
		{100, 100, pointer.ScrollRange{Min: -100, Max: 0}},
		{1.25, 10.5, pointer.ScrollRange{Min: -2, Max: 10}},
		{9.75, 10.5, pointer.ScrollRange{Min: -10, Max: 1}},
		{0.01, 0.02, pointer.ScrollRange{Min: -1, Max: 1}},
	} {
		if got := scrollRange(tc.offset, tc.max); got != tc.want {
			t.Errorf("scrollRange(%v,%v)=%v, want %v", tc.offset, tc.max, got, tc.want)
		}
	}
}

func TestShellRegressionPageWheelRouter(t *testing.T) {
	for _, name := range []string{"fixed-content", "snapshot-page"} {
		t.Run(name, func(t *testing.T) {
			regressionTheme(t)
			oldScroll, oldDragging, oldDragY := pageScroll, scrollDragging, scrollDragY
			pageScroll, scrollDragging, scrollDragY = [pgCount]scrollState{}, [pgCount]bool{}, [pgCount]int{}
			t.Cleanup(func() { pageScroll, scrollDragging, scrollDragY = oldScroll, oldDragging, oldDragY })
			g.win = new(app.Window)
			size := image.Pt(1100, 240)
			content := layout.Widget(func(gtx C) D {
				if gtx.Constraints.Min.Y != 0 || gtx.Constraints.Max.Y <= 640 {
					t.Fatalf("content retained viewport height constraints: %v", gtx.Constraints)
				}
				return D{Size: image.Pt(gtx.Constraints.Max.X, 640)}
			})
			if name == "snapshot-page" {
				fakeSnaps()
				content = g.pageSystem
			}
			var ops op.Ops
			var router input.Router
			render := func() {
				ops.Reset()
				gtx := regressionContext(&ops, &router, layout.Exact(size))
				if got := g.scrolledPage(gtx, pgSystem, content).Size; got != size {
					t.Fatalf("viewport size=%v, want %v", got, size)
				}
				router.Frame(&ops)
			}
			render()
			render() // Register the range after the first natural-height measurement.
			maxOffset := float32(pageScroll[pgSystem].contentH - size.Y)
			if maxOffset <= 120 || pageScroll[pgSystem].viewH != size.Y {
				t.Fatalf("missing overflowing viewport: %+v", pageScroll[pgSystem])
			}
			wheel := func(delta, want float32) {
				t.Helper()
				router.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse,
					Position: f32.Pt(10, 10), Scroll: f32.Pt(0, delta)})
				render()
				if got := pageScroll[pgSystem].offset; got != want {
					t.Fatalf("wheel %v: offset=%v, want %v", delta, got, want)
				}
				render()
				if got := pageScroll[pgSystem].offset; got != want {
					t.Fatalf("idle frame changed offset: %v, want %v", got, want)
				}
			}
			wheel(120, 120) // Full wheel delta, not half-speed consumption.
			wheel(0.5, 120.5)
			wheel(maxOffset*2, maxOffset)
			wheel(120, maxOffset)
			wheel(-60, maxOffset-60)
			wheel(-maxOffset*2, 0)
			wheel(-120, 0)
			wheel(60, 60)
			if pageScroll[pgKB] != (scrollState{}) {
				t.Fatal("wheel changed an unrelated page")
			}
		})
	}
}

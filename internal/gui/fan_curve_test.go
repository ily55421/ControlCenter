package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestFanCurveCoordinatesAndInterpolation(t *testing.T) {
	pts := [3][2]int{{30, 30}, {60, 60}, {90, 80}}
	for _, scale := range []float32{1, 1.5, 2} {
		gtx := C{Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
		for _, width := range []unit.Dp{240, 333, 900} {
			w, h := gtx.Dp(width), gtx.Dp(220)
			r := curveBounds(gtx, w, h)
			if r.Dx() < gtx.Dp(180) || r.Dy() < gtx.Dp(150) {
				t.Fatalf("plot too small: %v @%g", r, scale)
			}
			for _, temp := range []int{0, 30, 35, 60, 90, 100} {
				for _, duty := range []int{0, 30, 50, 100} {
					p := curveXY(gtx, w, h, temp, duty)
					tt, dd := curveTD(gtx, w, h, p.X, p.Y)
					if tt != temp || dd != duty {
						t.Fatalf("round trip (%d,%d) => (%d,%d)", temp, duty, tt, dd)
					}
				}
			}
			if curveXY(gtx, w, h, 30, 30).X >= curveXY(gtx, w, h, 35, 30).X {
				t.Fatal("30℃ was clamped to 35℃")
			}
			path := curvePath(gtx, w, h, pts)
			if got := path[len(path)-1]; got != f32.Pt(float32(r.Max.X), float32(r.Min.Y)) {
				t.Fatalf("100℃/100%% endpoint=%v", got)
			}
			for _, temp := range []int{10, 30, 43, 60, 77, 90, 95, 100} {
				wp := curveXY(gtx, w, h, temp, 0)
				wp.Y -= curveDuty(pts, temp) / 100 * float32(r.Dy())
				for j := 1; j < len(path); j++ {
					a, b := path[j-1], path[j]
					if wp.X < a.X || wp.X > b.X {
						continue
					}
					wantY := a.Y + (b.Y-a.Y)*(wp.X-a.X)/(b.X-a.X)
					if math.Abs(float64(wp.Y-wantY)) > 0.001 {
						t.Fatalf("working point at %d℃ is off curve: y=%g want %g", temp, wp.Y, wantY)
					}
					break
				}
			}
			tt, dd := curveTD(gtx, w, h, -100, -100)
			if tt != 0 || dd != 100 {
				t.Fatalf("outside top left => %d,%d", tt, dd)
			}
		}
	}
	if curveDuty(pts, 95) != 90 || curveDuty(pts, 100) != 100 {
		t.Fatal("last segment must interpolate to 100℃/100%")
	}
	// Degenerate dimensions must never cause division by zero or a NaN.
	for _, size := range []image.Point{{0, 0}, {1, 1}, {20, 20}} {
		gtx := C{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		p := curveXY(gtx, size.X, size.Y, 30, 30)
		tt, dd := curveTD(gtx, size.X, size.Y, p.X, p.Y)
		if tt != 30 || dd != 30 {
			t.Fatalf("tiny plot round trip %v => %d,%d", size, tt, dd)
		}
	}
}

func TestFanCurveColumns(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		gtx := C{Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
		for _, tc := range []struct{ width, cols int }{{240, 1}, {489, 1}, {490, 2}, {620, 2}, {739, 2}, {740, 3}, {1000, 3}} {
			avail := gtx.Dp(unit.Dp(tc.width))
			cols := curveColumns(gtx, avail)
			if cols != tc.cols {
				t.Fatalf("width=%d @%g: columns=%d want %d", tc.width, scale, cols, tc.cols)
			}
			usable := avail - (cols-1)*gtx.Dp(sp10)
			total, lo, hi := (cols-1)*gtx.Dp(sp10), avail, 0
			for j := 0; j < cols; j++ {
				w := usable*(j+1)/cols - usable*j/cols
				total += w
				lo, hi = min(lo, w), max(hi, w)
			}
			if lo < gtx.Dp(curveColW) || hi-lo > 1 || total != avail {
				t.Fatalf("unequal/unfilled columns: min=%d max=%d total=%d avail=%d", lo, hi, total, avail)
			}
		}
	}
}

func TestFanCurveMoveMonotonic(t *testing.T) {
	pts := [3][2]int{{30, 30}, {60, 60}, {90, 80}}
	for p := range pts {
		for _, temp := range []int{-100, 0, 30, 99, 100, 200} {
			for _, duty := range []int{-100, 0, 100, 200} {
				got := curveMovePoint(pts, p, temp, duty)
				if got[0][0] < 0 || got[0][0] >= got[1][0] || got[1][0] >= got[2][0] || got[2][0] > 100 ||
					got[0][1] < 0 || got[0][1] > got[1][1] || got[1][1] > got[2][1] || got[2][1] > 100 {
					t.Fatalf("nonmonotonic drag p=%d => %v", p, got)
				}
				if !validFanTable(FanTableOut{T1: got[0][0], T2: got[1][0], T3: got[2][0], D1: got[0][1], D2: got[1][1], D3: got[2][1]}) {
					t.Fatalf("drag produced an unsavable table: %v", got)
				}
			}
		}
	}
}

func TestFanCurveDragLifecycle(t *testing.T) {
	for _, fan := range []int{0, 1, 2} {
		for _, source := range []pointer.Source{pointer.Mouse, pointer.Touch} {
			t.Run(fmt.Sprintf("fan%d-source%d", fan, source), func(t *testing.T) {
				regressionTheme(t)
				fakeSnaps()
				gui, snap := g, g.sys.Load()
				gui.win = nil // Tests never create a native window or dispatch hardware writes.
				var ops op.Ops
				var router input.Router
				render := func() C {
					ops.Reset()
					gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(320, 2000)})
					gui.curveEditor(gtx, fan, snap, 72)
					router.Frame(&ops)
					return gtx
				}
				queue := func(kind pointer.Kind, pos f32.Point, id pointer.ID) {
					buttons := pointer.ButtonPrimary
					if kind == pointer.Release || kind == pointer.Cancel {
						buttons = 0
					}
					router.Queue(pointer.Event{Kind: kind, Source: source, Position: pos, Buttons: buttons, PointerID: id})
				}
				gtx := render()
				queue(pointer.Press, curveXY(gtx, 320, 220, 60, 60), 7)
				render()
				if gui.curveDragFan != fan || gui.curveDragPt != 1 {
					t.Fatalf("press did not select fan/point: %d/%d", gui.curveDragFan, gui.curveDragPt)
				}
				queue(pointer.Move, curveXY(gtx, 320, 220, 43, 45), 7)
				render()
				if got := gui.w.curveEd[fan][1].value(); got != 43 {
					t.Fatalf("drag T2=%d, want 43", got)
				}
				if got := gui.w.curveEd[fan][4].value(); got != 45 {
					t.Fatalf("drag D2=%d, want 45", got)
				}
				// A second touch must not change or release the captured first touch.
				if source == pointer.Touch {
					queue(pointer.Press, curveXY(gtx, 320, 220, 30, 30), 8)
					render()
					queue(pointer.Release, curveXY(gtx, 320, 220, 30, 30), 8)
					render()
					if gui.curveDragFan != fan || gui.curveDragPt != 1 {
						t.Fatal("second touch released the captured pointer")
					}
				}
				queue(pointer.Release, f32.Pt(-100, -100), 7)
				render()
				if gui.curveDragFan != -1 || gui.curveDragPt != -1 || len(curvePointerIDs) != 0 {
					t.Fatal("release outside plot left a sticky grab")
				}
				if gui.w.curveEd[fan][1].value() != 31 || gui.w.curveEd[fan][4].value() != 100 {
					t.Fatal("release position was not clamped monotonically")
				}
				for range 3 {
					render()
				}
				if gui.w.curveEd[fan][1].value() != 31 {
					t.Fatal("old snapshot reverted the draft")
				}
				queue(pointer.Press, curveXY(gtx, 320, 220, 31, 100), 7)
				render()
				queue(pointer.Cancel, f32.Point{}, 7)
				render()
				if gui.curveDragFan != -1 || len(curvePointerIDs) != 0 {
					t.Fatal("cancel left an active drag")
				}
				// Loss of a hardware table while captured removes the input target.
				queue(pointer.Press, curveXY(gtx, 320, 220, 31, 100), 7)
				render()
				unavailable := *snap
				unavailable.tables[fan] = FanTableOut{}
				snap = &unavailable
				render()
				queue(pointer.Release, f32.Pt(-100, -100), 7)
				render()
				queue(pointer.Press, curveXY(gtx, 320, 220, 0, 0), 7)
				render()
				if gui.curveDragFan != -1 || len(curvePointerIDs) != 0 {
					t.Fatal("unavailable curve allowed dragging")
				}
			})
		}
	}
}

func TestFanCurveCardSliderFullWidth(t *testing.T) {
	regressionTheme(t)
	fakeSnaps()
	g.win = nil
	snap := *g.sys.Load()
	snap.tables[2] = FanTableOut{} // Typical two-fan layout, without invented GPU2 data.
	g.w.fanOffset.onChange = nil
	g.w.fanOffset.syncExt(100)
	for _, width := range []int{300, 620, 1000} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var ops op.Ops
			var router input.Router
			gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(width, 4000)})
			dims := g.systemLeft(gtx, &snap, nil)
			img := regressionImage(t, dims.Size, func(gtx C) D { return g.systemLeft(gtx, &snap, nil) })
			maxGreenX := -1
			for y := 0; y < img.Bounds().Max.Y; y++ {
				for x := 0; x < width; x++ {
					c := img.RGBAAt(x, y)
					if c.G > 130 && c.R < 100 && c.B < 180 {
						maxGreenX = max(maxGreenX, x)
					}
				}
			}
			if maxGreenX < width-gtx.Dp(cardPadX)-gtx.Dp(12) {
				t.Fatalf("slider did not use its card width: last green x=%d, width=%d", maxGreenX, width)
			}
		})
	}
}

func fanCurveCyanPixels(img *image.RGBA, area image.Rectangle) int {
	n := 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 100 && c.B > 180 && c.R < 90 {
				n++
			}
		}
	}
	return n
}

func TestFanCurveHeadless(t *testing.T) {
	regressionTheme(t)
	fakeSnaps()
	g.win = nil
	original := *g.sys.Load()
	original.tables[2] = FanTableOut{T1: 30, T2: 60, T3: 90, D1: 30, D2: 60, D3: 80}
	for _, width := range []int{280, 620, 1000} {
		for _, unavailable := range []bool{false, true} {
			t.Run(fmt.Sprintf("w%d-empty%v", width, unavailable), func(t *testing.T) {
				snap := original
				if unavailable {
					snap.tables = [3]FanTableOut{}
				}
				var ops op.Ops
				var router input.Router
				gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(width, 4000)})
				dims := g.curveRow(gtx, &snap, [3]int{30, 72, 95})
				if dims.Size.X != width || dims.Size.Y <= 0 || dims.Size.Y > 1600 {
					t.Fatalf("bad curve row dimensions: %v", dims.Size)
				}
				win, err := headless.NewWindow(width, dims.Size.Y)
				if err != nil {
					t.Skipf("headless unavailable: %v", err)
				}
				defer win.Release()
				ops.Reset()
				fillRect(gtx, color.NRGBA{A: 255})
				g.curveRow(gtx, &snap, [3]int{30, 72, 95})
				if err := win.Frame(&ops); err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(image.Rect(0, 0, width, dims.Size.Y))
				if err := win.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				cyan := fanCurveCyanPixels(img, img.Bounds())
				if unavailable && cyan != 0 {
					t.Fatalf("unavailable tables rendered a fake curve: %d cyan pixels", cyan)
				}
				if !unavailable {
					cols := curveColumns(gtx, width)
					usable := width - (cols-1)*gtx.Dp(sp10)
					for j := 0; j < cols; j++ {
						x0 := usable*j/cols + j*gtx.Dp(sp10)
						x1 := usable*(j+1)/cols + j*gtx.Dp(sp10)
						area := image.Rect(x0, 30, x1, 220)
						if n := fanCurveCyanPixels(img, area); n < 100 {
							t.Fatalf("column %d has no readable plot: cyan pixels=%d", j, n)
						}
					}
				}
				// Optional curve-only evidence; no native app or backend calls.
				if dir := os.Getenv("CC_CURVE_SHOTS"); dir != "" {
					path := filepath.Join(dir, fmt.Sprintf("fan-curve-%d-empty%v.png", width, unavailable))
					f, err := os.Create(path)
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, img)
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
					t.Log(path)
				}
			})
		}
	}
}

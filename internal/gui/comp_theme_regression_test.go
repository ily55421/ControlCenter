package gui

import (
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

func regressionTheme(t *testing.T) {
	t.Helper()
	previous := g
	g = &GUI{th: newTheme()}
	t.Cleanup(func() { g = previous })
}

func regressionContext(ops *op.Ops, router *input.Router, constraints layout.Constraints) C {
	return C{
		Ops: ops, Source: router.Source(), Now: time.Now(),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: constraints,
	}
}

func regressionPointer(router *input.Router, kind pointer.Kind, pos image.Point) {
	buttons := pointer.ButtonPrimary
	if kind == pointer.Release {
		buttons = 0
	}
	if kind == pointer.Drag {
		kind = pointer.Move
	}
	router.Queue(pointer.Event{
		Kind: kind, Source: pointer.Mouse, Buttons: buttons,
		Position: f32.Pt(float32(pos.X), float32(pos.Y)),
	})
}

func regressionTap(router *input.Router, pos image.Point) {
	regressionPointer(router, pointer.Press, pos)
	regressionPointer(router, pointer.Release, pos)
}

func regressionImage(t *testing.T, size image.Point, w layout.Widget) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless unavailable: %v", err)
	}
	defer win.Release()
	var ops op.Ops
	var router input.Router
	gtx := regressionContext(&ops, &router, layout.Exact(size))
	fillRect(gtx, color.NRGBA{A: 255})
	w(gtx)
	if err := win.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

func TestRegressionSliderReleaseCommitsOnce(t *testing.T) {
	for _, pressX := range []int{40, 150} {
		t.Run(fmt.Sprintf("press%d", pressX), func(t *testing.T) {
			var commits []int
			s := newSlider(0, 100, true, func(v int) { commits = append(commits, v) })
			s.syncExt(0)
			var ops op.Ops
			var router input.Router
			render := func() {
				ops.Reset()
				gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(200, 200)})
				if got := s.layout(gtx).Size; got != image.Pt(200, ctlH) {
					t.Fatalf("slider size = %v", got)
				}
				router.Frame(&ops)
			}
			render()
			regressionPointer(&router, pointer.Press, image.Pt(pressX, ctlH/2))
			render()
			regressionPointer(&router, pointer.Drag, image.Pt(150, ctlH/2))
			render()
			if !s.f.Dragging() || s.value() != 75 || len(commits) != 0 {
				t.Fatalf("dragging=%v value=%d commits=%v", s.f.Dragging(), s.value(), commits)
			}
			s.syncExt(0)
			regressionPointer(&router, pointer.Release, image.Pt(150, ctlH/2))
			render()
			if s.f.Dragging() || len(commits) != 1 || commits[0] != 75 {
				t.Fatalf("release: dragging=%v commits=%v", s.f.Dragging(), commits)
			}
			for range 3 {
				s.syncExt(0)
				render()
			}
			if len(commits) != 1 || s.value() != 75 {
				t.Fatalf("old snapshot or repeated frame changed commit: value=%d commits=%v", s.value(), commits)
			}
			s.syncExt(80)
			render()
			if len(commits) != 1 || s.value() != 80 {
				t.Fatalf("new snapshot: value=%d commits=%v", s.value(), commits)
			}
		})
	}
}

func TestRegressionSliderAllocatedWidth(t *testing.T) {
	for _, tc := range []struct {
		min, max, want int
	}{{0, 1000, sliderMaxW}, {0, 200, 200}, {120, 1000, 120}, {480, 1000, 480}, {400, 300, 300}} {
		var ops op.Ops
		var router input.Router
		gtx := regressionContext(&ops, &router, layout.Constraints{
			Min: image.Pt(tc.min, 0), Max: image.Pt(tc.max, 100),
		})
		s := newSlider(0, 100, true, nil)
		if got := s.layout(gtx).Size; got != image.Pt(tc.want, ctlH) {
			t.Errorf("constraints %v: size=%v, want=%v", gtx.Constraints, got, image.Pt(tc.want, ctlH))
		}
	}
}

func TestRegressionNumFieldSnapshotDedup(t *testing.T) {
	var n numField
	var ops op.Ops
	var router input.Router
	gtx := regressionContext(&ops, &router, layout.Exact(image.Pt(120, ctlH)))
	n.init(10)
	n.setText(99)
	n.sync(gtx, 10)
	if n.value() != 10 || !n.synced {
		t.Fatal("initial value incorrectly counted as an external snapshot")
	}
	n.ed.SetText("42")
	n.sync(gtx, 10)
	n.sync(gtx, 10)
	if n.value() != 42 {
		t.Fatal("unchanged snapshot overwrote editing after blur")
	}
	n.sync(gtx, 20)
	if n.value() != 20 || n.lastSynced != 20 {
		t.Fatal("new external value was not applied")
	}
	n.setText(55)
	n.sync(gtx, 20)
	if n.value() != 55 || n.lastSynced != 20 {
		t.Fatal("manual setText changed snapshot identity")
	}
}

func TestRegressionDropdownViewportAndPosition(t *testing.T) {
	regressionTheme(t)
	opts := make([]string, 19)
	for i := range opts {
		opts[i] = fmt.Sprintf("Language %02d", i)
	}
	d := newDropdown(opts, 0, 180)
	d.open = true
	var ops op.Ops
	var router input.Router
	render := func() D {
		ops.Reset()
		gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(300, 2000)})
		dims := d.layout(gtx)
		router.Frame(&ops)
		return dims
	}
	if got := render().Size; got != image.Pt(180, ctlH+8*listRowH) {
		t.Fatalf("dropdown size=%v", got)
	}
	if d.list.Position.Count != 8 || d.list.Position.Length != 19*listRowH {
		t.Fatalf("list viewport=%+v", d.list.Position)
	}
	regressionTap(&router, image.Pt(60, ctlH+8*listRowH+listRowH/2))
	render()
	if !d.open || d.sel != 0 {
		t.Fatal("invisible option received a click outside the list")
	}
	router.Queue(pointer.Event{
		Kind: pointer.Scroll, Source: pointer.Mouse,
		Position: f32.Pt(80, ctlH+listRowH), Scroll: f32.Pt(0, 3*listRowH),
	})
	render()
	position := d.list.Position
	if position.First == 0 {
		t.Fatalf("list did not scroll: %+v", position)
	}
	render()
	if d.list.Position.First != position.First || d.list.Position.Offset != position.Offset {
		t.Fatalf("scroll reset across frames: %+v -> %+v", position, d.list.Position)
	}
	regressionTap(&router, image.Pt(60, ctlH+listRowH/2))
	render()
	if d.open || d.sel != position.First {
		t.Fatalf("option click: open=%v sel=%d want=%d", d.open, d.sel, position.First)
	}
	render()
	regressionTap(&router, image.Pt(60, ctlH/2))
	render()
	if !d.open || d.list.Position.First != position.First || d.list.Position.Offset != position.Offset {
		t.Fatalf("reopening reset list position: %+v -> %+v", position, d.list.Position)
	}
}

func TestRegressionDisabledButtonNoClick(t *testing.T) {
	regressionTheme(t)
	var clk widget.Clickable
	var ops op.Ops
	var router input.Router
	disabled, clicks := true, 0
	render := func() {
		ops.Reset()
		gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(160, 100)})
		if clk.Clicked(gtx) {
			clicks++
		}
		button(gtx, &clk, "Apply", btnPrimary, disabled)
		router.Frame(&ops)
	}
	render()
	regressionTap(&router, image.Pt(30, btnH/2))
	render()
	if clicks != 0 {
		t.Fatal("disabled button responded to a pointer click")
	}
	disabled = false
	render()
	regressionTap(&router, image.Pt(30, btnH/2))
	render()
	if clicks != 1 {
		t.Fatalf("enabled button clicks=%d", clicks)
	}
}

func TestRegressionSegmentEqualHitAreas(t *testing.T) {
	regressionTheme(t)
	labels := []string{"I", "A very long label", "中"}
	var s segmented
	var ops op.Ops
	var router input.Router
	picked := -1
	render := func() {
		ops.Reset()
		gtx := regressionContext(&ops, &router, layout.Exact(image.Pt(300, 80)))
		dims, click := s.Layout(gtx, labels)
		picked = click
		if dims.Size != image.Pt(300, ctlH) {
			t.Fatalf("segment size=%v", dims.Size)
		}
		router.Frame(&ops)
	}
	render()
	for i := range labels {
		for _, x := range []int{i*100 + 2, (i+1)*100 - 2} {
			regressionTap(&router, image.Pt(x, ctlH/2))
			render()
			if picked != i || s.sel != i {
				t.Fatalf("x=%d picked=%d sel=%d want=%d", x, picked, s.sel, i)
			}
		}
	}
}

func TestRegressionSegmentHighlightFillsAllocation(t *testing.T) {
	regressionTheme(t)
	labels := []string{"I", "Long label", "中"}
	for sel := range labels {
		s := segmented{sel: sel}
		img := regressionImage(t, image.Pt(300, ctlH), func(gtx C) D {
			dims, _ := s.Layout(gtx, labels)
			return dims
		})
		inactive := img.RGBAAt(((sel+1)%3)*100+12, ctlH-8)
		for _, x := range []int{sel*100 + 12, sel*100 + 50, sel*100 + 87} {
			selected := img.RGBAAt(x, ctlH-8)
			if selected.G <= inactive.G || selected.B <= inactive.B {
				t.Errorf("segment=%d x=%d highlight=%v inactive=%v", sel, x, selected, inactive)
			}
		}
	}
}

func TestRegressionDropdownPixelsStayInsideViewport(t *testing.T) {
	regressionTheme(t)
	opts := make([]string, 19)
	for i := range opts {
		opts[i] = fmt.Sprintf("Language %02d", i)
	}
	d := newDropdown(opts, 0, 180)
	d.open = true
	d.list.Position = layout.Position{First: 3, Offset: 9}
	bottom := ctlH + 8*listRowH
	img := regressionImage(t, image.Pt(200, bottom+listRowH), func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return d.layout(gtx)
	})
	for y := bottom; y < bottom+listRowH; y++ {
		for x := 0; x < 180; x++ {
			if got := img.RGBAAt(x, y); got != (color.RGBA{A: 255}) {
				t.Fatalf("list painted below viewport at (%d,%d): %v", x, y, got)
			}
		}
	}
}

func TestRegressionDropdownLongHeaderKeepsCenteredChevron(t *testing.T) {
	regressionTheme(t)
	d := newDropdown([]string{"A language name much longer than the dropdown"}, 0, 160)
	img := regressionImage(t, image.Pt(160, ctlH), d.layout)
	minY, maxY, pixels := ctlH, -1, 0
	for y := 2; y < ctlH-2; y++ {
		for x := 143; x < 153; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 60 {
				minY, maxY = min(minY, y), max(maxY, y)
				pixels++
			}
		}
	}
	if pixels < 4 || minY < ctlH/2-5 || maxY > ctlH/2+5 {
		t.Fatalf("chevron pixels=%d y=[%d,%d]", pixels, minY, maxY)
	}
}

func TestRegressionNarrowKVRowLeavesValueRoom(t *testing.T) {
	regressionTheme(t)
	var ops op.Ops
	var router input.Router
	gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(180, 1000)})
	value := "中央处理器型号与频率"
	valueContext := gtx
	valueContext.Constraints.Min.X = 120
	valueContext.Constraints.Max.X = 120
	want := txt(valueContext, value, textStyle{Size: fsBody, Col: colText}).Size.Y
	ops.Reset()
	if got := kvRow(gtx, "CPU", value); got.Size.X != 180 || got.Size.Y != want || got.Baseline == 0 {
		t.Fatalf("narrow KV dimensions=%+v, want height=%d", got, want)
	}
}

func TestRegressionCardTitleAndPageActionPlacement(t *testing.T) {
	regressionTheme(t)
	img := regressionImage(t, image.Pt(300, hdrH), func(gtx C) D {
		return cardHead(gtx, cardOpt{title: "Title"})
	})
	minY, maxY := hdrH, -1
	for y := 0; y < hdrH; y++ {
		for x := cardPadX; x < 140; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 150 && c.G > 150 && c.B > 150 {
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxY < minY || minY < hdrH/4 || maxY > 3*hdrH/4 {
		t.Fatalf("card title vertical bounds=[%d,%d]", minY, maxY)
	}
	var dims D
	img = regressionImage(t, image.Pt(400, 120), func(gtx C) D {
		dims = pageHead(gtx, "Title", "", func(gtx C) D {
			fillArea(gtx, image.Rect(0, 0, 20, 20), colDanger)
			return D{Size: image.Pt(20, 20)}
		})
		return dims
	})
	if c := img.RGBAAt(395, (dims.Size.Y-sp16)/2); c.R < 200 || c.G > 150 {
		t.Fatalf("page action not placed at right edge: %v", c)
	}
}

func TestRegressionBackdropOnlyVerticalBands(t *testing.T) {
	regressionTheme(t)
	img := regressionImage(t, image.Pt(256, 240), func(gtx C) D {
		drawBackdrop(gtx)
		return D{Size: gtx.Constraints.Max}
	})
	top := color.NRGBA{R: 0x0B, G: 0x10, B: 0x18, A: 255}
	for band := range 12 {
		want := mixNRGBA(top, colBg, float32(band)/11)
		for _, x := range []int{1, 64, 127, 192, 254} {
			got := img.RGBAAt(x, band*20+10)
			if got != (color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}) {
				t.Fatalf("backdrop band=%d x=%d color=%v want=%v", band, x, got, want)
			}
		}
	}
}

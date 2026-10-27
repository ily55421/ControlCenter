package gui

import (
	"encoding/binary"
	"errors"
	"image"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/fan"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/smbios"
	"controlcenter/internal/wmi"
)

var errInjectedIO = errors.New("injected ioctl failure")

type recordedWMI struct {
	command int
	data    []byte
}

type recordingTransport struct {
	mu       sync.Mutex
	settings [4096]byte
	calls    []recordedWMI
	failCode uint32
	info     [256]byte
}

func (r *recordingTransport) call(code uint32, in, out []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if code == r.failCode {
		return errInjectedIO
	}
	if code == 0x32240c {
		if binary.LittleEndian.Uint32(in) == 0 {
			copy(out, r.settings[:])
		} else {
			offset := int(binary.LittleEndian.Uint16(in[4:]))
			length := int(binary.LittleEndian.Uint16(in[6:]))
			copy(r.settings[offset:offset+length], in[8:8+length])
		}
		return nil
	}
	command := int(binary.LittleEndian.Uint32(in[0x18:]))
	data := append([]byte(nil), in[0x22:0x122]...)
	r.calls = append(r.calls, recordedWMI{command: command, data: data})
	binary.LittleEndian.PutUint32(out[8:], 1)
	binary.LittleEndian.PutUint16(out[12:], 2)
	binary.LittleEndian.PutUint16(out[14:], 256)
	if command == 13 {
		copy(out[16:], r.info[:])
	}
	return nil
}

func (r *recordingTransport) contains(command, sub, data int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if call.command == command && (sub < 0 || int(call.data[3]) == sub) && int(call.data[0]) == data {
			return true
		}
	}
	return false
}

type recordingMode struct {
	mode    appsettings.ModeType
	entered chan struct{}
	release chan struct{}
	err     error
	apply   func(appsettings.ModeType) error
}

func (m *recordingMode) Change(mode appsettings.ModeType) error {
	m.mode = mode
	if m.entered != nil {
		close(m.entered)
	}
	if m.release != nil {
		<-m.release
	}
	if m.apply != nil {
		return m.apply(mode)
	}
	return m.err
}

func pipelineGUI(t *testing.T) (*GUI, *recordingTransport) {
	t.Helper()
	regressionTheme(t)
	fakeSnaps()
	gui := g
	transport := new(recordingTransport)
	bridge := acpibridge.NewWithTransport(transport.call)
	settings := appsettings.New(bridge)
	client := wmi.New(bridge, settings)
	gui.be = &Backend{
		w: client, as: settings, fc: fan.New(client, settings), pm: new(recordingMode),
		kb: keyboard.New(client, settings), plat: &platform{tbl: new(smbios.Table), kbType: keyboard.RGBZone},
	}
	gui.msgCh = make(chan func(), 32)
	gui.done = make(chan struct{})
	t.Cleanup(func() {
		gui.dying.Store(true)
		close(gui.done)
	})
	return gui, transport
}

func waitPipeline(t *testing.T, gui *GUI) {
	t.Helper()
	select {
	case fn := <-gui.msgCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("production page did not complete an action")
	}
}

func TestRequestModeWaitsAndReturnsFailure(t *testing.T) {
	mode := &recordingMode{entered: make(chan struct{}), release: make(chan struct{}), err: errInjectedIO}
	be := &Backend{pm: mode}
	returned := make(chan error, 1)
	go func() { returned <- be.RequestMode(1) }()
	<-mode.entered
	select {
	case <-returned:
		t.Fatal("mode request returned before the engine completed")
	default:
	}
	close(mode.release)
	if err := <-returned; !errors.Is(err, errInjectedIO) || mode.mode != appsettings.PowerSaving {
		t.Fatalf("mode=%d error=%v", mode.mode, err)
	}
}

func TestProductionSystemActionsReachTransport(t *testing.T) {
	cases := []struct {
		name         string
		click        func(*GUI)
		command, sub int
		data         int
	}{
		{"fan maximum", func(gui *GUI) { gui.w.fanSeg.clks[1].Click() }, 121, 1, 1},
		{"fan offset", func(gui *GUI) { gui.w.fanOffset.f.Value = 0.4 }, 121, 14, 102},
		{"anti dust", func(gui *GUI) { gui.w.dustRun.Click() }, 121, 41, 1},
		{"anti dust schedule", func(gui *GUI) { gui.w.dustApply.Click() }, 121, 40, 7},
		{"restore curve", func(gui *GUI) { gui.w.fanReset.Click() }, 121, 34, 1},
		{"save curve", func(gui *GUI) { gui.w.fanSave.Click() }, 14, -1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gui, transport := pipelineGUI(t)
			var ops op.Ops
			var router input.Router
			render := func() {
				ops.Reset()
				gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(1148, 100000)})
				gui.pageSystem(gtx)
				router.Frame(&ops)
			}
			render()
			tc.click(gui)
			render()
			waitPipeline(t, gui)
			if !transport.contains(tc.command, tc.sub, tc.data) {
				t.Fatalf("action did not reach WMI command=%d sub=%d data=%d: %+v", tc.command, tc.sub, tc.data, transport.calls)
			}
		})
	}
}

func TestProductionModeReportsEngineFailure(t *testing.T) {
	gui, _ := pipelineGUI(t)
	gui.be.pm = &recordingMode{err: errInjectedIO}
	var ops op.Ops
	var router input.Router
	gtx := regressionContext(&ops, &router, layout.Constraints{Max: image.Pt(1148, 100000)})
	gui.w.modeTab[1].Click()
	gui.pageSystem(gtx)
	waitPipeline(t, gui)
	if !strings.Contains(gui.toastS.msg, errInjectedIO.Error()) {
		t.Fatalf("mode failure was reported as success: %+v", gui.toastS)
	}
}

func TestBackendWriteFailuresReachCaller(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Backend) error
	}{
		{"fan mode", func(be *Backend) error { return be.SetFanMode(1) }},
		{"fan offset", func(be *Backend) error { return be.SetFanOffset(40) }},
		{"noiseless", func(be *Backend) error { return be.SetModeOption("noiseless", true) }},
		{"keyboard preference", func(be *Backend) error { return be.SetKeyboardPref(true, true, false) }},
		{"gpu mode", func(be *Backend) error { return be.SetGPUSwitch(2) }},
		{"charging", func(be *Backend) error { return be.SetFlexiCharger(2, 50, 80) }},
		{"anti dust", func(be *Backend) error { return be.RunAntiDust() }},
		{"restore curve", func(be *Backend) error { return be.ResetFanTables() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gui, transport := pipelineGUI(t)
			transport.failCode = 0x322400
			if err := tc.call(gui.be); !errors.Is(err, errInjectedIO) {
				t.Fatalf("error swallowed: %v", err)
			}
		})
	}
}

func TestFanTablesUseRealDefaultsAndKeepMissingUnavailable(t *testing.T) {
	gui, transport := pipelineGUI(t)
	transport.info[12] = 1
	copy(transport.info[16:22], []byte{30, 76, 60, 153, 90, 230})
	tables := gui.be.FanTables()
	if !validFanTable(tables[0]) || tables[0].T1 != 30 || tables[0].D1 != 30 {
		t.Fatalf("EC defaults not used: %+v", tables[0])
	}
	if validFanTable(tables[1]) || validFanTable(tables[2]) {
		t.Fatalf("missing fan data became a fabricated curve: %+v", tables)
	}
	if err := gui.be.SaveFanTables([3]FanTableOut{}); err == nil {
		t.Fatal("empty curve enabled custom fan mode")
	}
}

func TestInputTargetsHaveDistinctAddresses(t *testing.T) {
	gui := new(GUI)
	seen := map[*byte]bool{}
	for _, tag := range []*byte{&gui.wheelTag, &gui.keyTag} {
		seen[tag] = true
	}
	if len(seen) != 2 {
		t.Fatal("keyboard and wheel input targets overlap")
	}
	for i := 0; i < pgCount; i++ {
		for _, tag := range []*byte{&scrollTags[i], &scrollDrag[i]} {
			if seen[tag] {
				t.Fatal("page scroll input targets overlap")
			}
			seen[tag] = true
		}
	}
}

func TestRootDisplaysBoundedFailureToast(t *testing.T) {
	regressionTheme(t)
	fakeSnaps()
	g.toast("失败: " + strings.Repeat("driver operation failed; ", 40))
	if time.Until(g.toastS.until) < 4*time.Second {
		t.Fatal("hardware failure disappeared too quickly")
	}
	for _, size := range []image.Point{{1000, 560}, {1400, 900}} {
		img := regressionImage(t, size, g.layoutRoot)
		red := 0
		for y := size.Y - 220; y < size.Y; y++ {
			for x := size.X - 550; x < size.X; x++ {
				c := img.RGBAAt(x, y)
				if c.R > 180 && c.G < 140 && c.B < 160 {
					red++
				}
			}
		}
		if red < 100 {
			t.Fatalf("failure toast was not visible in the production root at %v: red=%d", size, red)
		}
	}
}

func TestNativeProductionActionPipeline(t *testing.T) {
	if os.Getenv("CC_PIPELINE_NATIVE") != "1" {
		t.Skip("CC_PIPELINE_NATIVE=1")
	}
	gui, transport := pipelineGUI(t)
	copy(transport.info[16:22], []byte{30, 76, 60, 153, 90, 230})
	copy(transport.info[24:30], []byte{35, 76, 65, 153, 90, 230})
	transport.info[12] = 2
	gui.be.pm = &recordingMode{apply: func(mode appsettings.ModeType) error {
		if mode == appsettings.PowerSaving {
			return errInjectedIO
		}
		if err := gui.be.w.SetWMI(121, 25, uint32(mode)); err != nil {
			return err
		}
		return gui.be.as.SetPowerModeOnly(mode)
	}}
	gui.win = new(app.Window)
	gui.win.Option(app.Title("Control Center Pipeline Probe"), app.Decorated(false), app.Size(unit.Dp(1400), unit.Dp(1000)))
	pageScroll = [pgCount]scrollState{}
	sawFailure := false
	for {
		switch e := gui.win.Event().(type) {
		case app.FrameEvent:
			gui.drainMsgs()
			sawFailure = sawFailure || strings.Contains(gui.toastS.msg, errInjectedIO.Error())
			gtx := app.NewContext(&gui.ops, e)
			gui.layoutRoot(gtx)
			e.Frame(gtx.Ops)
		case app.DestroyEvent:
			if e.Err != nil {
				t.Fatal(e.Err)
			}
			if !transport.contains(121, 25, 0) || !transport.contains(121, 1, 1) || !sawFailure {
				t.Fatalf("production native events did not reach mode/fan/error paths: sawFailure=%v", sawFailure)
			}
			t.Log("production window mode/fan actions reached recorded IOCTLs; engine failure reached UI; no hardware writes")
			return
		}
	}
}

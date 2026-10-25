// Package ui — 内嵌 Web UI(全部功能面板的 JSON API + 单页应用)。
package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/battery"
	"controlcenter/internal/bluelight"
	"controlcenter/internal/cpuoc"
	"controlcenter/internal/energysave"
	"controlcenter/internal/fan"
	"controlcenter/internal/flexikey"
	"controlcenter/internal/gpu"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/smbios"
	"controlcenter/internal/wmi"
)

// Server Web UI 服务。
type Server struct {
	w  *wmi.Client
	as *appsettings.Store
	kb *keyboard.Controller
	fc *fan.Controller
	pm *powermode.Engine

	mu       sync.Mutex
	sampler  *energysave.Sampler
	batState *batteryState

	platOnce sync.Mutex
	plat     *platform // 进程内不变的平台信息,首次使用时取

	fanCache fanLiveCache // 风扇实时数据:EC 包读慢,改为缓存 + 异步刷新

	bootStamp string // 本进程启动标记:页面据此在服务重启后自动刷新

	GPUOCBase string // ControlGPU.exe 所在目录
}

// fanLiveTTL 风扇实时数据有效期。页面每 2 秒轮询一次,1 秒内的缓存直接返回。
const fanLiveTTL = time.Second

// fanLiveCache 缓存最近一次 GetPackage(12) 的结果。
// EC 的 256 字节包读实测约 80ms,且会独占整条 ACPI 桥通道(所有 IOCTL 串行);
// 若每个轮询请求都同步等它,面板卡住不说,托盘热键/其他面板请求也得排队。
type fanLiveCache struct {
	mu         sync.Mutex
	live       fan.LiveSample
	err        error
	at         time.Time
	sampled    bool
	refreshing bool
}

// platform 运行期不会变的平台信息。这几项每项都要付一次 256 字节 WMI 包读
// (实测真机 ~80ms/次),而它们只在开机时由 BIOS 决定,故取一次缓存。
type platform struct {
	tbl        *smbios.Table
	customerID int
	sup        powermode.SupportBits
	kbType     keyboard.KBType
}

// platformInfo 惰性读取平台信息(含键盘类型向 AppSettings(2,4) 的一次镜像)。
func (s *Server) platformInfo() *platform {
	s.platOnce.Lock()
	defer s.platOnce.Unlock()
	if s.plat == nil {
		s.plat = &platform{
			tbl:        smbios.Get(),
			customerID: int(s.w.GetPackage(17)[2]),
			sup:        powermode.Detect(s.w, s.as),
			kbType:     s.kb.Type(),
		}
	}
	return s.plat
}

// customID 平台定制商 ID(WMI17[2],决定 XTU 参数分支)。
func (s *Server) customID() int { return s.platformInfo().customerID }

type batteryState struct {
	Ongoing battery.Ongoing
	Started time.Time
}

// New 创建服务。
func New(w *wmi.Client, as *appsettings.Store, kb *keyboard.Controller, fc *fan.Controller, pm *powermode.Engine) *Server {
	return &Server{
		w: w, as: as, kb: kb, fc: fc, pm: pm,
		bootStamp: strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

// maxBodyBytes 请求体上限(面板配置 JSON 远小于此,用于挡住意外的大请求)。
const maxBodyBytes = 1 << 20

// newHTTPServer 带超时的服务器:即使是回环地址也要防慢连接长期占用。
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}

// Start 启动 HTTP 服务(随机本地端口)并返回地址。
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go newHTTPServer(s.Handler()).Serve(ln)
	return fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port), nil
}

// ServeOn 在指定端口启动(被占用时返回错误)。
func (s *Server) ServeOn(port int) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, err
	}
	go newHTTPServer(s.Handler()).Serve(ln)
	return port, nil
}

// Handler 返回全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.apiStatus)
	mux.HandleFunc("/api/mode", s.apiMode)
	mux.HandleFunc("/api/mode/options", s.apiModeOptions)
	mux.HandleFunc("/api/preference", s.apiPreference)
	mux.HandleFunc("/api/fan/live", s.apiFanLive)
	mux.HandleFunc("/api/fan/config", s.apiFanConfig)
	mux.HandleFunc("/api/fan/table", s.apiFanTable)
	mux.HandleFunc("/api/fan/antidust", s.apiAntiDust)
	mux.HandleFunc("/api/kb/state", s.apiKBState)
	mux.HandleFunc("/api/kb/cmd", s.apiKBCmd)
	mux.HandleFunc("/api/gpu/oc", s.apiGPUOC)
	mux.HandleFunc("/api/cpu/info", s.apiCPUInfo)
	mux.HandleFunc("/api/cpu/apply", s.apiCPUApply)
	mux.HandleFunc("/api/cpu/default", s.apiCPUDefault)
	mux.HandleFunc("/api/battery", s.apiBattery)
	mux.HandleFunc("/api/battery/refresh", s.apiBatteryRefresh)
	mux.HandleFunc("/api/energy/sample", s.apiEnergySample)
	mux.HandleFunc("/api/energy/apply", s.apiEnergyApply)
	mux.HandleFunc("/api/flexikey/state", s.apiFlexikeyState)
	mux.HandleFunc("/api/flexikey/apply", s.apiFlexikeyApply)
	return limitBody(mux)
}

// limitBody 限制请求体大小:超限后读取报错,handler 因此拿不到被撑爆的输入。
func limitBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		h.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, err error) {
	writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
}

// apiStatus 总状态(主页磁贴支持位)。
// 页面每 15 秒轮询它做"服务是否重启"探测,所以这里只保留真正会变的量走硬件:
// wmi16/mode/language/clevoID;平台信息(SMBIOS、WMI17、能力位、键盘类型)取一次缓存。
func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	w16 := s.w.GetInt(16)
	p := s.platformInfo()
	writeJSON(w, map[string]any{
		"ok":          true,
		"boot":        s.bootStamp,
		"wmi16":       w16,
		"kbType":      p.kbType,
		"mode":        s.as.GetPowerMode(),
		"language":    s.as.GetUILanguage(),
		"customerID":  p.customerID,
		"clevoID":     s.as.GetClevoID(),
		"manufacturer": p.tbl.System.ManufacturerName,
		"family":      p.tbl.System.Family,
		"product":     p.tbl.System.ProductName,
		"bios":        p.tbl.BIOS.Version,
		"cpu":         p.tbl.CPU.Version,
		"cores":       p.tbl.CPU.CoreCount,
		"support": map[string]bool{
			"powerMode": p.sup.PowerMode, "flexikey": w16&2 == 2 && p.sup.FeatureListVer >= 0,
			"gpuoc": w16&0x20 != 0 || p.sup.SimpleGPUOC, "cpuoc": p.sup.CPUOC,
			"fan": w16&0x80 != 0, "energy": w16&0x100 != 0, "battery": w16&0x200 != 0,
			"keyboard": w16&0x18 != 0, "turboFan": p.sup.TurboFan, "slowFan": p.sup.SlowFan,
			"msHybrid": p.sup.MSHybridSwitch, "amdRyzen": p.sup.AMDRyzen, "dtt": p.sup.DTT,
			"quietPlus": p.sup.QuietPlusOnly || p.sup.NVIDIAPowerOff,
		},
	})
}

// apiMode GET 当前模式;POST 切换 {"mode":0..4}。
func (s *Server) apiMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct{ Mode int }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		go s.pm.Change(appsettings.ModeType(req.Mode))
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "mode": s.as.GetPowerMode()})
}

// apiModeOptions 模式附属开关(GET/POST)。
func (s *Server) apiModeOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Key   string `json:"key"`
			Value bool   `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		switch req.Key {
		case "turboFan":
			_ = s.fc.SetTurbo(req.Value)
		case "noiseless":
			_ = s.fc.SetNoiseless(req.Value)
		case "dtt":
			v := 0
			if req.Value {
				v = 1
			}
			_ = s.as.WriteU8(appsettings.AddrDTT, byte(v))
			if req.Value {
				_ = s.w.SetWMI(121, 25, 130)
			} else {
				_ = s.w.SetWMI(121, 25, 2)
			}
		case "lowRefresh":
			v := 0
			if req.Value {
				v = 1
			}
			_ = s.as.WriteU8(appsettings.AddrLowRefreshOn, byte(v))
		case "blueLight":
			v := 0
			if req.Value {
				v = 10
			}
			_ = s.as.WriteU8(appsettings.AddrBlueLight, byte(v))
			if v > 0 {
				bluelight.SetBlueLightRamp(float64(v)/100*10 + 1)
			} else {
				bluelight.ResetDeviceGammaRamp()
			}
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"turboFan":  s.as.ReadU8(appsettings.AddrTurboFan) == 1,
		"noiseless": s.as.ReadU8(appsettings.AddrNoiselessFan) == 1,
		"dtt":       s.as.ReadU8(appsettings.AddrDTT) == 1,
		"lowRefresh": s.as.ReadU8(appsettings.AddrLowRefreshOn) == 1,
		"lowRefreshSupport": s.as.ReadU8(appsettings.AddrLowRefreshSup) == 1,
		"blueLight": s.as.ReadU8(appsettings.AddrBlueLight),
	})
}

// apiPreference 偏好设置(GET 汇总 / POST 各项)。
func (s *Server) apiPreference(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		if v, ok := req["flexiCharger"].(map[string]any); ok {
			en, _ := v["enabled"].(bool)
			st, _ := v["start"].(float64)
			sp, _ := v["stop"].(float64)
			mode := battery.ChargerModeOf(en, byte(int(st)), byte(int(sp)))
			if m, ok := v["mode"].(float64); ok && m >= 0 && m <= 2 {
				mode = int(m) // 前端显式给出时以它为准(自定义 70/80 不应被降级成"推荐")
			}
			s.w.SetFlexiCharger(en, byte(int(st)), byte(int(sp)))
			// 302 存模式号(与原版取值 0/1/2 一致);阈值另存用户目录,
			// 供 ccapp/ccfnkey 启动时回写 EC(冷启动后 EC 的充电控制会被 BIOS 复位)。
			if err := s.as.WriteU8(appsettings.AddrFlexiCharger, byte(mode)); err != nil {
				errJSON(w, err)
				return
			}
			if err := battery.SaveChargerPref(battery.ChargerPref{
				Mode: mode, Start: int(st), Stop: int(sp)}); err != nil {
				errJSON(w, fmt.Errorf("充电模式已下发但未能记住,重启后会回到原状态: %w", err))
				return
			}
		}
		if v, ok := req["keyboard"].(map[string]any); ok {
			fn := byte(1)
			win := byte(1)
			fnw := byte(1)
			if b, ok := v["fnLock"].(bool); ok && !b {
				fn = 2
			}
			if b, ok := v["winKey"].(bool); ok && !b {
				win = 2
			}
			if b, ok := v["fnWin"].(bool); ok && !b {
				fnw = 2
			}
			s.w.SetKeyboardSetting(fn, win, fnw)
			_ = s.as.Write(appsettings.AddrFnLock, 3, []byte{fn, win, fnw})
		}
		if v, ok := req["gpuSwitch"].(float64); ok {
			s.w.SetGPUSwitch(int(v))
		}
		if v, ok := req["pgUpDn"].(bool); ok {
			_ = s.as.WriteU8(appsettings.AddrPgUpDn, b8(!v))
			s.w.SetPgUpDn(!v)
		}
		if v, ok := req["blueLight"].(float64); ok {
			_ = s.as.WriteU8(appsettings.AddrBlueLight, byte(int(v)))
			if v > 0 {
				_ = bluelight.SetBlueLightRamp(v/100*10 + 1)
			} else {
				bluelight.ResetDeviceGammaRamp()
			}
		}
		if v, ok := req["language"].(float64); ok {
			_ = s.as.WriteU8(appsettings.AddrUILanguage, byte(int(v)))
			_ = writeOutSafe("clevofnkey^102")
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	en, st, sp, stt, spt := s.w.QueryFlexiCharger()
	mode := battery.ChargerModeOf(en, st, sp)
	if p, ok := battery.LoadChargerPref(); ok {
		mode = p.Mode
	}
	fn, win, fnw := s.w.QueryKeyboard()
	gpuCur, gpuSup := s.w.QueryGPUSwitch()
	writeJSON(w, map[string]any{
		"ok": true,
		"flexiCharger": map[string]any{
			"enabled": en, "start": st, "stop": sp, "mode": mode,
			"startTable": nonZero(stt), "stopTable": nonZero(spt),
		},
		"keyboard": map[string]any{
			"fnLock": fn == 1, "winKey": win == 1, "fnWin": fnw == 1,
			"fnLockVisible": fn != 0, "winKeyVisible": win != 0,
		},
		"gpuSwitch":   map[string]any{"current": gpuCur, "support": gpuSup},
		"pgUpDn":      s.as.ReadU8(appsettings.AddrPgUpDn) == 0,
		"blueLight":   s.as.ReadU8(appsettings.AddrBlueLight),
		"language":    s.as.GetUILanguage(),
	})
}

func b8(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func nonZero(b []byte) []int {
	var out []int
	for _, v := range b {
		if v != 0 {
			out = append(out, int(v))
		}
	}
	return out
}

func writeOutSafe(msg string) error { return writeOutImpl(msg) }

// fanLiveSnapshot 返回最近一次风扇采样:首次同步采(页面第一帧即真值),
// 之后过期只触发一次后台刷新,请求本身立即返回上一份数据。
func (s *Server) fanLiveSnapshot() (fan.LiveSample, error) {
	c := &s.fanCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sampled {
		c.live, c.err = s.fc.ReadLive()
		c.sampled, c.at = true, time.Now()
		return c.live, c.err
	}
	if time.Since(c.at) > fanLiveTTL && !c.refreshing {
		c.refreshing = true
		go func() {
			live, err := s.fc.ReadLive()
			c.mu.Lock()
			c.live, c.err, c.at = live, err, time.Now()
			c.refreshing = false
			c.mu.Unlock()
		}()
	}
	return c.live, c.err
}

// apiFanLive 风扇实时数据(页面 2 秒轮询;见 fanLiveCache 注释)。
func (s *Server) apiFanLive(w http.ResponseWriter, r *http.Request) {
	live, err := s.fanLiveSnapshot()
	if err != nil {
		errJSON(w, err)
		return
	}
	mode := s.as.ReadU8(appsettings.AddrFanMode)
	writeJSON(w, map[string]any{
		"ok": true,
		"cpu": map[string]any{"rpm": live.Fan.RPM, "duty": live.Fan.Duty * 100 / 255, "temp": live.Fan.Temp},
		"gpu1": map[string]any{"rpm": live.GPU1.RPM, "duty": live.GPU1.Duty * 100 / 255, "temp": live.GPU1.Temp},
		"gpu2": map[string]any{"rpm": live.GPU2.RPM, "duty": live.GPU2.Duty * 100 / 255, "temp": live.GPU2.Temp},
		"sys":  map[string]any{"rpm": live.SysFan.RPM, "duty": live.SysFan.Duty * 100 / 255, "temp": live.SysFan.Temp},
		"mode": mode,
	})
}

// apiFanConfig 风扇模式/偏移(GET/POST)。
func (s *Server) apiFanConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Mode   *int `json:"mode"`
			Offset *int `json:"offset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		if req.Mode != nil {
			_ = s.fc.SetMode(fan.FanMode(*req.Mode))
		}
		if req.Offset != nil {
			_ = s.fc.SetOffsetPercent(*req.Offset)
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	info := s.fc.ReadInfo()
	writeJSON(w, map[string]any{
		"ok": true,
		"mode": s.as.ReadU8(appsettings.AddrFanMode),
		"offset": s.as.ReadU8(appsettings.AddrFanOffset),
		"fanCount": info.FanCount,
		"initFanMode": info.InitFanMode,
		"bitsExist": info.BitsExist,
	})
}

// apiFanTable 自定义曲线(GET/POST {"tables":[{t1,t2,t3,d1,d2,d3}×3]})。
func (s *Server) apiFanTable(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Tables [3]struct{ T1, T2, T3, D1, D2, D3 int }
			Reset  bool `json:"reset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		if req.Reset {
			_ = s.fc.LoadDefaultTable()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		var tables [3]fan.FanTable
		for i, t := range req.Tables {
			tables[i] = fan.FanTable{
				T1: t.T1, T2: t.T2, T3: t.T3, T4: 100,
				D1: t.D1, D2: t.D2, D3: t.D3, D4: 100,
				T2Def: t.T2, T3Def: t.T3, D2Def: t.D2, D3Def: t.D3,
			}
		}
		if err := s.fc.WriteCustomTable(tables); err != nil {
			errJSON(w, err)
			return
		}
		_ = s.fc.SetMode(fan.Custom)
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	tables := s.fc.ReadCustomTable()
	out := make([]map[string]int, 3)
	for i, t := range tables {
		out[i] = map[string]int{"t1": t.T1, "t2": t.T2, "t3": t.T3, "d1": t.D1, "d2": t.D2, "d3": t.D3}
	}
	writeJSON(w, map[string]any{"ok": true, "tables": out})
}

// apiAntiDust 防尘(GET/POST)。
func (s *Server) apiAntiDust(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"` // run | schedule
			Days   int    `json:"days"`
			Resume bool   `json:"resume"`
			Hour   int    `json:"hour"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		if req.Action == "run" {
			_ = s.fc.RunAntiDust()
		} else {
			_ = s.fc.SetAntiDustScheduler(req.Days, req.Resume, req.Hour)
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	en, days, resume, hour := s.fc.ReadAntiDustScheduler()
	writeJSON(w, map[string]any{
		"ok": true, "enabled": en, "days": days, "resume": resume, "hour": hour,
		"running": s.fc.AntiDustStatus(),
	})
}

// apiKBState 键盘灯状态(GET)。全部字段落在 page2,取一次页快照即可(原为 14 次 4KB 读)。
func (s *Server) apiKBState(w http.ResponseWriter, r *http.Request) {
	pg := s.as.Page(2)
	writeJSON(w, map[string]any{
		"ok": true, "type": s.platformInfo().kbType,
		"on": appsettings.U8(pg, 80) == 1, "brightness": appsettings.U8(pg, 35),
		"mode": appsettings.U8(pg, 32), "speed": appsettings.U8(pg, 34),
		"allColor":  rgbAt(pg, 81), "leftColor": rgbAt(pg, 85),
		"midColor": rgbAt(pg, 89), "rightColor": rgbAt(pg, 93),
		"tpColor": rgbAt(pg, 97), "logoColor": rgbAt(pg, 101), "lightBarColor": rgbAt(pg, 105),
		"sleepTrigger": appsettings.U8(pg, 36) == 1,
		"sleepSeconds": appsettings.U16(pg, 37),
		"overrideBoot": appsettings.U8(pg, 7),
	})
}

// rgbAt 页内 3 字节 RGB。
func rgbAt(pg []byte, off int) map[string]int {
	if off < 0 || off+3 > len(pg) {
		return map[string]int{"r": 0, "g": 0, "b": 0}
	}
	return map[string]int{"r": int(pg[off]), "g": int(pg[off+1]), "b": int(pg[off+2])}
}

// apiKBCmd 键盘灯命令(POST {"cmd":...})。
func (s *Server) apiKBCmd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, fmt.Errorf("POST only"))
		return
	}
	var req struct {
		Cmd       string `json:"cmd"`
		Part      string `json:"part"`
		R         int    `json:"r"`
		G         int    `json:"g"`
		B         int    `json:"b"`
		Level     int    `json:"level"`
		Effect    int    `json:"effect"`
		Mode      int    `json:"mode"`
		Seconds   int    `json:"seconds"`
		Override  *int   `json:"override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err)
		return
	}
	t := s.platformInfo().kbType
	col := keyboard.Color{R: req.R, G: req.G, B: req.B}
	var err error
	switch req.Cmd {
	case "onoff":
		if req.Mode == 1 {
			err = s.kb.SetOnOff(keyboard.OnOffBits{Main: true, Left: true, Mid: true, Right: true})
		} else {
			err = s.kb.SetOnOff(keyboard.OnOffBits{})
		}
	case "brightness":
		if t == keyboard.White {
			err = s.kb.SetWhiteBrightness(req.Level)
		} else {
			err = s.kb.SetBrightnessRGB(req.Level)
		}
	case "color":
		var part byte
		switch req.Part {
		case "left":
			part = 0xF0
		case "mid":
			part = 0xF1
		case "right":
			part = 0xF2
		case "lightbar":
			part = 0xF3
		case "logo":
			part = 0xF6
		default:
			err = s.kb.SetAllColor(t, col)
		}
		if part != 0 {
			err = s.kb.SetColorRGB(t, part, col)
		}
		if req.Part == "all" || req.Part == "" {
			_ = s.as.Write(593, 3, []byte{byte(req.R), byte(req.G), byte(req.B)})
		} else {
			var base int
			switch req.Part {
			case "left":
				base = 597
			case "mid":
				base = 601
			case "right":
				base = 605
			case "tp":
				base = 609
			case "logo":
				base = 613
			case "lightbar":
				base = 617
			}
			if base > 0 {
				_ = s.as.Write(base, 3, []byte{byte(req.R), byte(req.G), byte(req.B)})
			}
		}
	case "effect":
		err = s.kb.SetEffectRGB(keyboard.Effect(req.Effect))
		// 单色特效(Breath 等)同时设三区色
		if req.Effect == int(keyboard.EffBreath) {
			_ = s.kb.SetAllColor(t, col)
		}
	case "sleep":
		on := req.Mode == 1
		err = s.kb.SetSleepTimerTrigger(on)
		if on && req.Seconds > 0 {
			err = s.kb.SetSleepTimerMinutes(req.Seconds / 60)
		}
	case "override":
		if req.Override != nil {
			err = s.kb.SetOverrideBootEffect(*req.Override)
		}
	case "reset":
		err = s.kb.ResetToDefault()
	}
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// apiGPUOC GPU 超频(GET/POST)。
func (s *Server) apiGPUOC(w http.ResponseWriter, r *http.Request) {
	store := gpu.New(s.as, s.w)
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"` // save | reset
			Core0  int    `json:"core0"`
			Mem0   int    `json:"mem0"`
			Core1  int    `json:"core1"`
			Mem1   int    `json:"mem1"`
			Dual   bool   `json:"dual"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, err)
			return
		}
		path := gpu.ControlGPUPath(s.GPUOCBase)
		var err error
		switch req.Action {
		case "save":
			err = store.Save(path, gpu.OffsetPair{Core: int16(req.Core0), Mem: int16(req.Mem0)},
				gpu.OffsetPair{Core: int16(req.Core1), Mem: int16(req.Mem1)}, req.Dual)
		case "reset":
			err = store.Reset(path, req.Dual)
		}
		if err != nil {
			errJSON(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	g0, g1 := store.Saved()
	cur := store.Current()
	count, base, ready, ver := store.Info()
	writeJSON(w, map[string]any{
		"ok": true,
		"saved0": g0, "saved1": g1, "current": cur,
		"count": count, "baseClock": base, "ready": ready, "driver": ver,
	})
}

// apiCPUInfo CPU 超频信息(GET)。page6 一次页快照(原为 81 次 4KB 读)。
func (s *Server) apiCPUInfo(w http.ResponseWriter, r *http.Request) {
	pg := s.as.Page(6)
	writeJSON(w, map[string]any{
		"ok":       true,
		"info":     cpuoc.ReadInfoFrom(pg),
		"params":   cpuoc.ReadParamsFrom(pg),
		"defaults": cpuoc.ReadDefaultsFrom(pg),
	})
}

// apiCPUApply 应用参数(POST {"params":{...}})。
func (s *Server) apiCPUApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Params cpuoc.Params
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err)
		return
	}
	customID := s.customID()
	if err := cpuoc.Apply(s.as, s.w, req.Params, customID); err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// apiCPUDefault 恢复默认。
func (s *Server) apiCPUDefault(w http.ResponseWriter, r *http.Request) {
	customID := s.customID()
	if err := cpuoc.RestoreDefaults(s.as, s.w, customID); err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// apiBattery 电池信息(GET)。
func (s *Server) apiBattery(w http.ResponseWriter, r *http.Request) {
	d := battery.Read(s.w)
	health := battery.Evaluate(d)
	s.mu.Lock()
	ongoing := battery.Ongoing(0)
	if s.batState != nil {
		ongoing = s.batState.Ongoing
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"ok": true,
		"data": map[string]any{
			"cycleCount": d.CycleCount, "fullCharge": d.FullChargeCapacity,
			"design": d.DesignCapacity, "capacityPct": battery.CapacityPct(d),
			"stopChargeThreshold": d.StopChargeThresh,
			"batteryStatus": d.BatteryStatus, "manufacturerDate": d.ManufacturerDate,
			"ageYears": battery.AgeYears(d),
		},
		"health":     health.String(),
		"healthCode": int(health),
		"ongoing":    int(ongoing),
	})
}

// apiBatteryRefresh 电池校准控制(POST {"action":"start|stop"})。
func (s *Server) apiBatteryRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct{ Action string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err)
		return
	}
	s.mu.Lock()
	if s.batState == nil {
		s.batState = &batteryState{}
	}
	st := s.batState
	switch req.Action {
	case "start":
		battery.RefreshCommand(s.w, 1, 0, 1) // 先恢复
		battery.RefreshCommand(s.w, 0, 1, 0) // 开始放电
		st.Ongoing = battery.Discharge1
		st.Started = time.Now()
		_ = writeOutSafe("clevobattery^207^1")
	case "stop":
		battery.RefreshCommand(s.w, 1, 0, 1)
		st.Ongoing = battery.Stop
		_ = writeOutSafe("clevobattery^207^0")
	}
	ongoing := st.Ongoing
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "ongoing": int(ongoing)})
}

// apiEnergySample 功耗采样(GET)。
func (s *Server) apiEnergySample(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.sampler == nil {
		s.sampler = energysave.NewSampler(s.w)
	}
	sampler := s.sampler
	s.mu.Unlock()
	watt := sampler.SampleAC(false)
	charge, discharge := energysave.AutoChargeLimits(s.w)
	writeJSON(w, map[string]any{
		"ok": true, "watt": watt, "batWh": sampler.BatWh, "pdWatt": sampler.PD,
		"chargeLimit": charge, "dischargeLimit": discharge,
	})
}

// apiEnergyApply 节能调度应用(POST profile)。
func (s *Server) apiEnergyApply(w http.ResponseWriter, r *http.Request) {
	var p energysave.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		errJSON(w, err)
		return
	}
	now := time.Now()
	if err := energysave.Apply(s.w, p, now.Hour(), now.Minute(), now.Second(), int(now.Weekday())); err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// apiStatus 辅助:事件总线写入(避免循环导入,由 server 注入)。
var writeOutImpl = func(string) error { return nil }

// SetEventWriter 注入事件总线写入函数。
func SetEventWriter(fn func(string) error) { writeOutImpl = fn }


// apiFlexikeyState Flexikey 状态(GET):profile 列表/选中项/开关。
func (s *Server) apiFlexikeyState(w http.ResponseWriter, r *http.Request) {
	path := flexikey.DefaultPath()
	store, err := flexikey.Load(path)
	if err != nil {
		errJSON(w, err)
		return
	}
	_, statErr := os.Stat(path)
	type profInfo struct {
		Name  string `json:"name"`
		Sel   bool   `json:"selected"`
		KB    bool   `json:"kb"`
		Mouse bool   `json:"mouse"`
		Slots int    `json:"slots"`
	}
	list := make([]profInfo, 0, len(store.Profiles))
	for _, p := range store.Profiles {
		list = append(list, profInfo{Name: p.Name, Sel: p.IsSelected,
			KB: p.IsKbEnable, Mouse: p.IsMouseEnable, Slots: len(p.Keys)})
	}
	writeJSON(w, map[string]any{
		"ok": true, "path": path, "exists": statErr == nil, "profiles": list,
		"kbEnabled":    s.as.ReadU8(appsettings.AddrFlexikeyKB) == 1,
		"mouseEnabled": s.as.ReadU8(appsettings.AddrFlexikeyMouse) == 1,
	})
}

// apiFlexikeyApply Flexikey 操作(POST {"action":"select|apply|disable","name":...})。
func (s *Server) apiFlexikeyApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err)
		return
	}
	store, err := flexikey.Load(flexikey.DefaultPath())
	if err != nil {
		errJSON(w, err)
		return
	}
	ctl := flexikey.NewController(flexikey.NewDriver(), s.as)
	switch req.Action {
	case "select":
		found := false
		for _, p := range store.Profiles {
			p.IsSelected = p.Name == req.Name
			if p.IsSelected {
				found = true
			}
		}
		if !found {
			errJSON(w, fmt.Errorf("配置 %q 不存在", req.Name))
			return
		}
		if err := store.Save(); err != nil {
			errJSON(w, err)
			return
		}
	case "apply":
		if err := ctl.Apply(store.Selected()); err != nil {
			errJSON(w, err)
			return
		}
		_ = writeOutSafe("clevofnkey^205")
	case "disable":
		if err := ctl.DisableAll(); err != nil {
			errJSON(w, err)
			return
		}
		_ = writeOutSafe("clevofnkey^205")
	default:
		errJSON(w, fmt.Errorf("未知 action %q", req.Action))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

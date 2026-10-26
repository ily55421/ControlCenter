// ccapp — ControlCenter30.exe 的 Go 等价实现(主界面):
// Windows 原生 GUI(Gio/D3D11 硬件渲染)+ 直连引擎层(无 HTTP/JSON),
// 磁贴/电源模式/偏好设置,事件日志 IPC 与原版完全兼容。
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/battery"
	"controlcenter/internal/cpuoc"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/gui"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/launcher"
	"controlcenter/internal/powermode"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

// instanceName 单实例互斥量名;默认与正式版一致,冒烟测试用 ldflags 覆盖。
var instanceName = "ccapp"

// initLogging GUI 子系统下没有可用的标准输出句柄:
// 默认丢弃日志;CC_LOG=1 时追加写入 %LOCALAPPDATA%\ControlCenter\ccapp.log 便于排查。
func initLogging() {
	log.SetFlags(log.Ltime)
	if os.Getenv("CC_LOG") != "1" {
		log.SetOutput(discard{})
		return
	}
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "ControlCenter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.SetOutput(discard{})
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "ccapp.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.SetOutput(discard{})
		return
	}
	log.SetOutput(f)
}

// discard 吞掉全部日志写入(无控制台时的默认去向)。
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func main() {
	// GUI 子系统程序没有控制台(stdout/stderr 句柄为无效值):
	// 显式丢弃日志输出,避免 log 包写标准错误失败刷屏;
	// 需要排查时设置 CC_LOG=1 可把日志追加写入 %LOCALAPPDATA%\ControlCenter\ccapp.log。
	initLogging()

	// 深链页参数(-page=fan 等,兼容面板启动器/托盘/旧版协议页名)
	page := ""
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "-page="); ok {
			page = launcher.NormalizePage(v)
		}
	}

	// 单实例:已有实例则导航到目标页并前置其窗口(而不是静默退出)
	// instanceName 可经 -ldflags "-X main.instanceName=..." 覆盖(冒烟测试用独立名字)。
	if dup, err := win32.SingleInstanceGuard(instanceName); err == nil && dup {
		log.Println("Control Center 已在运行,激活窗口")
		if !win32.NavNotify(page) {
			// 旧版实例没有导航窗口:仅前置
			win32.FocusWindow(launcher.MainWindowTitle)
		}
		return
	}

	w := wmi.Default
	as := appsettings.Default
	kb := keyboard.Default
	fc := fan.Default
	gpuStore := gpu.New(as, w)
	pm := powermode.New(w, as, fc, gpuStore)
	exe, _ := os.Executable()
	pm.ControlGPUPath = gpu.FindControlGPU(filepath.Dir(exe))

	// 启动序列(对应 App_Startup):
	// 版本写入 AppSettings(0,4..7)
	_ = as.WriteU8(4, 1)
	_ = as.WriteU8(5, 7)
	_ = as.WriteU8(6, 0)
	_ = as.WriteU8(7, 0)
	// CC30 运行标志
	_ = as.WriteU8(appsettings.AddrCC30Launch, 1)
	// XTU 实时区读取标志:置 1 后 FnKey/驱动持续刷新 page6 实时数据(频率/温度/利用率);
	// 退出时清 0(见 OutLog 监听与 cleanupExit)。原版由 App_Startup 设置。
	_ = as.WriteU8(appsettings.AddrXTUInfoRead, 1)

	// 开机回写充电模式:EC 的充电控制寄存器冷启动会被 BIOS 复位,按记忆重新下发。
	if applied, err := battery.RestoreCharger(w); err != nil {
		log.Printf("FlexiCharger 回写失败: %v", err)
	} else if applied {
		log.Printf("FlexiCharger 已按记忆回写 EC")
	}

	be := gui.NewBackend(w, as, kb, fc, pm)
	if pm.ControlGPUPath != "" {
		be.GPUOCBase = filepath.Dir(pm.ControlGPUPath)
	}

	// 跨进程导航:message-only 窗口(WM_COPYDATA 页名)→ GUI 深链通道
	nav := make(chan string, 8)
	win32.ServeNavWindow(func(p string) {
		select {
		case nav <- launcher.NormalizePage(p):
		default:
		}
	})

	// OutLog 监听(托盘/热键的关闭与语言切换命令)
	go func() {
		for {
			sub, err := eventbus.Subscribe("OutLog")
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			for {
				entries, err := sub.Wait()
				if err != nil {
					break
				}
				for _, e := range entries {
					msg := e.Message
					if contains(msg, "clevofnkey^204") {
						// 关闭 CC:清标志并退出
						log.Println("收到关闭命令,退出")
						_ = as.WriteU8(appsettings.AddrXTUInfoRead, 0)
						_ = as.WriteU8(appsettings.AddrCC30Launch, 0)
						time.Sleep(300 * time.Millisecond)
						gui.RequestClose()
					}
					if contains(msg, "clevofnkey^102") {
						log.Println("语言已切换")
					}
				}
			}
			sub.Close()
			time.Sleep(2 * time.Second)
		}
	}()

	log.Println("Control Center (Go) 原生 GUI 已启动")

	// Gio 要求平台主循环占用 main goroutine;GUI 事件循环放独立 goroutine,
	// Run 返回(窗口关闭/托盘关闭命令)即清理退出。
	go func() {
		err := gui.Run(be, page, nav, nil)
		if err != nil {
			log.Printf("GUI 退出: %v", err)
		}
		cleanupExit(as)
	}()
	app.Main()
}

func cleanupExit(as *appsettings.Store) {
	_ = as.WriteU8(appsettings.AddrXTUInfoRead, 0)
	_ = as.WriteU8(appsettings.AddrCC30Launch, 0)
	time.Sleep(200 * time.Millisecond)
	exitProcess()
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func exitProcess() {
	// cpuoc 包引用仅为保持与原版关闭序列一致的语义
	_ = cpuoc.WaitXTUIdle
	log.Fatal("退出")
}

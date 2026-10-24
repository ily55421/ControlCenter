// ccapp — ControlCenter30.exe 的 Go 等价实现(主界面启动器):
// 内嵌 Web UI(全部功能面板)+ 磁贴/电源模式/偏好设置,事件日志 IPC 完全兼容。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/launcher"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/cpuoc"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/ui"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

func main() {
	log.SetFlags(log.Ltime)

	// 单实例:已有实例则激活其 GUI 窗口(而不是静默退出)
	if win32.ProcessExistsOther("ccapp.exe") {
		fmt.Println("Control Center 已在运行,激活窗口")
		launcher.Open("system", "Control Center")
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

	ui.SetEventWriter(eventbus.WriteOut)

	srv := ui.New(w, as, kb, fc, pm)
	// GPU OC 应用通道:探测 ControlGPU.exe(与 ccfnkey 同约定)
	if pm.ControlGPUPath != "" {
		srv.GPUOCBase = filepath.Dir(pm.ControlGPUPath)
	}
	// 固定端口 8765 优先(与启动器/托盘约定一致),被占用则回退随机端口
	addr, uiPort := "", 0
	if port, err := srv.ServeOn(8765); err == nil {
		addr, uiPort = fmt.Sprintf("http://127.0.0.1:%d/", port), port
	} else if a, err2 := srv.Start(); err2 == nil {
		addr = a
		uiPort = portOf(a)
	} else {
		log.Fatalf("UI 服务启动失败: %v", err2)
	}
	// 记录端口,使托盘/面板启动器复用本进程的 UI 服务
	if uiPort > 0 {
		launcher.WritePort(uiPort)
	}
	fmt.Println("Control Center (Go) 已启动:", addr)

	// OutLog 监听(关闭命令等)
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
						_ = as.WriteU8(appsettings.AddrXTUInfoRead, 0)
						_ = as.WriteU8(appsettings.AddrCC30Launch, 0)
						win32.FocusWindow("")
						log.Println("收到关闭命令,退出")
						time.Sleep(300 * time.Millisecond)
						cleanupExit(as)
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

	// 以独立 GUI 应用窗口打开(Edge/Chrome app 模式,回退默认浏览器)
	win32.OpenAppWindow(addr)
	select {}
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

// portOf 从 "http://127.0.0.1:PORT/" 解析端口。
func portOf(addr string) int {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return 0
	}
	var p int
	fmt.Sscanf(strings.Trim(addr[i+1:], "/"), "%d", &p)
	return p
}

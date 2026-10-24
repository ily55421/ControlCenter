// ccservice — DCHUService.exe 的 Go 等价实现:
// Windows 服务,承担 (1) 热键事件源(Arg001 广播)(2) w_arg253/254/255 进程执行通道
// (3) 会话变更时拉起托盘程序 (4) 事件日志基础设施初始化。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/win32"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const svcName = "CCDCHUService"
const svcDisplayName = "Control Center Hotkey Service"

func main() {
	install := flag.Bool("install", false, "安装服务")
	remove := flag.Bool("remove", false, "卸载服务")
	debug := flag.Bool("debug", false, "以控制台模式运行(调试)")
	flag.Parse()

	exe, _ := os.Executable()
	switch {
	case *install:
		if err := eventbus.EnsureLogs(); err != nil {
			log.Printf("创建事件日志失败(可忽略,可能已存在): %v", err)
		}
		if err := win32.InstallService(svcName, svcDisplayName, exe); err != nil {
			log.Fatal(err)
		}
		if err := win32.StartService(svcName, 30*time.Second); err != nil {
			log.Printf("启动服务失败: %v", err)
		}
		fmt.Println("服务已安装并启动")
	case *remove:
		if err := win32.RemoveService(svcName); err != nil {
			log.Fatal(err)
		}
		fmt.Println("服务已卸载")
	case *debug:
		runService(true)
	default:
		isSvc, err := svc.IsWindowsService()
		if err == nil && isSvc {
			runService(false)
		} else {
			runService(true)
		}
	}
}

type ccservice struct{}

func runService(interactive bool) {
	if interactive {
		log.SetFlags(log.Ltime)
		run(&ccservice{})
		return
	}
	elog, err := eventlog.Open(svcName)
	if err != nil {
		log.Fatal(err)
	}
	defer elog.Close()
	log.SetOutput(&eventLogWriter{elog})
	run(&ccservice{})
}

type eventLogWriter struct{ el *eventlog.Log }

func (w *eventLogWriter) Write(p []byte) (int, error) {
	_ = w.el.Info(1, string(p))
	return len(p), nil
}

func run(s svc.Handler) {
	_ = svc.Run(svcName, s)
}

// Execute 服务主入口。
func (s *ccservice) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptPauseAndContinue | svc.AcceptShutdown | svc.AcceptSessionChange
	status <- svc.Status{State: svc.StartPending}

	// 1. 事件日志基础设施(原版在构造函数里创建 OutLog/PowerBiosServerLog)
	if err := eventbus.EnsureLogs(); err != nil {
		log.Printf("EnsureLogs: %v", err)
	}

	// 2. AppSettings:标记 Arg001 由服务发送
	as := appsettings.Default
	_ = as.WriteU8(appsettings.AddrArg001ByService, 1)

	// 3. 等待桌面就绪(原版 CheckExplorerExist)
	waitExplorer()

	// 4. 启动热键事件源(独立连接,事件值 → Arg001^N 广播)
	go eventSource()

	// 5. 命令通道监听(w_arg253/254/255 → 进程执行)
	go commandListener()

	// 6. 拉起托盘程序(ccfnkey)
	launchTray()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	log.Printf("ccservice 已启动")

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				_ = as.WriteU8(appsettings.AddrArg001ByService, 0)
				log.Printf("ccservice 已停止")
				return false, 0
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: accepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: accepted}
			case svc.SessionChange:
				handleSessionChange(c)
			}
		}
	}
}

func waitExplorer() {
	for i := 0; i < 120; i++ {
		if win32.ProcessExistsByName("explorer.exe") {
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func handleSessionChange(c svc.ChangeRequest) {
	switch c.EventType {
	case 5: // SessionLogon
		log.Printf("会话登录 SessionId=%d", c.EventData)
		launchTray()
	case 8: // SessionUnlock
		launchTray()
	}
}

// launchTray 启动托盘程序(若未运行)。
func launchTray() {
	exe, _ := os.Executable()
	tray := filepath.Join(filepath.Dir(exe), "ccfnkey.exe")
	if _, err := os.Stat(tray); err != nil {
		return
	}
	if win32.ProcessExistsByName("ccfnkey.exe") {
		return
	}
	if err := win32.StartAsActiveUser(`"` + tray + `"`); err != nil {
		log.Printf("启动托盘失败: %v", err)
	}
}

// eventSource 热键事件源:WAIT_EVENT 循环 → Arg001^<value>。
func eventSource() {
	for {
		loop, err := acpibridge.NewEventLoop()
		if err != nil {
			log.Printf("事件源连接失败: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		err = loop.Run(func(v uint64) bool {
			_ = eventbus.WriteOut(fmt.Sprintf("Arg001^%d", v))
			return true
		}, nil)
		loop.Stop()
		if err != nil {
			log.Printf("事件循环退出: %v(5 秒后重连)", err)
		}
		time.Sleep(5 * time.Second)
	}
}

// commandListener 命令通道:PowerBiosServerLog 的 w_arg253/254/255。
func commandListener() {
	for {
		sub, err := eventbus.Subscribe("PowerBiosServerLog")
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		for {
			entries, err := sub.Wait()
			if err != nil {
				break
			}
			for _, e := range entries {
				handleCommand(e.Message)
			}
		}
		sub.Close()
		time.Sleep(2 * time.Second)
	}
}

func handleCommand(msg string) {
	msg = strings.TrimSpace(msg)
	switch {
	case strings.HasPrefix(msg, "w_arg253^launchapp^"):
		// w_arg253^launchapp^<PFN>^<yes|no>[^<进程名>]
		parts := strings.Split(msg, "^")
		if len(parts) >= 4 {
			pfn := parts[2]
			kill := parts[3] == "yes"
			proc := ""
			if len(parts) >= 5 {
				proc = parts[4]
			}
			if kill && proc != "" {
				win32.KillOtherSessions(proc)
			}
			cmd := `explorer.exe shell:appsFolder\` + pfn + "!App"
			if err := win32.StartAsActiveUser(cmd); err != nil {
				log.Printf("launchapp %s: %v", pfn, err)
			}
		}
	case strings.HasPrefix(msg, "w_arg253^"):
		rest := strings.TrimPrefix(msg, "w_arg253^")
		if err := win32.StartAsActiveUser(rest); err != nil {
			log.Printf("启动 %s: %v", rest, err)
		}
	case strings.HasPrefix(msg, "w_arg254^"):
		rest := strings.TrimPrefix(msg, "w_arg254^")
		if err := win32.StartAsActiveUser(rest); err != nil {
			log.Printf("执行 %s: %v", rest, err)
		}
	case strings.HasPrefix(msg, "w_arg255^"):
		rest := strings.TrimPrefix(msg, "w_arg255^")
		parts := strings.SplitN(rest, "^", 2)
		exe := parts[0]
		args := ""
		if len(parts) > 1 {
			args = parts[1]
		}
		full := `"` + exe + `"`
		if args != "" {
			full += " " + args
		}
		if err := win32.StartAsActiveUser(full); err != nil {
			log.Printf("执行 %s: %v", full, err)
		}
	case strings.Contains(msg, "clevofnkey^1ff"):
		// Fn+F2 触摸板 hook:原版转给 WakePanelUp_Hook.exe
		exe, _ := os.Executable()
		hook := filepath.Join(filepath.Dir(exe), "WakePanelUp_Hook.exe")
		if _, err := os.Stat(hook); err == nil {
			_ = win32.StartAsActiveUser(`"` + hook + `"`)
		}
	case strings.HasPrefix(msg, "cpuoc_bg_"):
		// XTU 后台命令:转给 CPUOC\CC30_BG.exe(若存在)
		exe, _ := os.Executable()
		bg := filepath.Join(filepath.Dir(exe), `CPUOC\CC30_BG.exe`)
		if _, err := os.Stat(bg); err == nil {
			_ = win32.StartAsActiveUser(`"` + bg + `" ` + msg)
		}
	}
}

var _ = json.Marshal
var _ = mgr.Connect

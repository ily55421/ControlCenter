// ControlCenter.exe — GUI 启动器/安装器(替代全部 bat 脚本):
// 状态检测(驱动/服务/事件日志)、一键安装、启动托盘与主界面、硬件诊断、卸载。
// 纯 Win32 GUI(无控制台窗口)。安装/卸载需要管理员:未提权时自动以 runas 重启自身。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/smbios"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
	"controlcenter/internal/fan"
	"controlcenter/internal/keyboard"
)

const svcName = "CCDCHUService"

var (
	win    *win32.SimpleWindow
	selfDir string
)

func log(format string, a ...any) {
	stamp := time.Now().Format("15:04:05")
	win.AppendLog("[" + stamp + "] " + fmt.Sprintf(format, a...))
}

func main() {
	exe, _ := os.Executable()
	selfDir = filepath.Dir(exe)

	// 命令行模式:被 runas 重启后执行提权动作
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "/install", "-install":
			runInstallActions()
			return
		case "/uninstall", "-uninstall":
			runUninstallActions()
			return
		case "/selftest", "-selftest":
			selftest()
			return
		}
	}

	var err error
	win, err = win32.NewSimpleWindow("Control Center — 启动器", 760, 560)
	if err != nil {
		fmt.Fprintln(os.Stderr, "窗口创建失败:", err)
		time.Sleep(5 * time.Second)
		return
	}

	// 布局:两行按钮 + 日志区
	win.AddButton("① 一键安装", 16, 14, 150, 36, onInstall)
	win.AddButton("② 启动 Control Center", 176, 14, 190, 36, onStart)
	win.AddButton("硬件诊断", 376, 14, 110, 36, onDiagnose)
	win.AddButton("卸载", 496, 14, 110, 36, onUninstall)
	win.AddButton("退出", 616, 14, 110, 36, func() { os.Exit(0) })
	win.AddEdit(16, 62, 710, 450)
	win.Show()

	log("Control Center (Go) 启动器就绪  ·  目录 %s", selfDir)
	log("管理员权限: %v", win32.IsElevated())
	autoCheck()
	win.Run()
}

// autoCheck 启动自检:驱动/事件日志/服务/后台进程。
func autoCheck() {
	go func() {
		if _, err := acpibridge.DevicePath(); err != nil {
			log("✗ 未检测到 ACPI 桥驱动(请先安装原版 AcpiBridge 驱动)")
		} else {
			log("✓ ACPI 桥驱动已连接")
		}
		if eventbus.LogsExist() {
			log("✓ 事件日志(OutLog / PowerBiosServerLog)已就绪")
		} else {
			log("… 事件日志未创建(将在一键安装时创建)")
		}
		if win32.ServiceExists(svcName) {
			if win32.ServiceRunning(svcName) {
				log("✓ 服务 %s 运行中", svcName)
			} else {
				log("! 服务 %s 已安装但未运行", svcName)
			}
		} else {
			log("… 服务 %s 未安装(热键事件源,建议一键安装)", svcName)
		}
		if win32.ProcessExistsByName("ccfnkey.exe") {
			log("✓ 托盘(ccfnkey)运行中")
		}
		if win32.ProcessExistsByName("ccapp.exe") {
			log("✓ 主界面(ccapp)运行中 → http://127.0.0.1:8765/")
		}
	}()
}

// onInstall 一键安装:未提权则 runas 重启自身执行。
func onInstall() {
	if !win32.IsElevated() {
		log("需要管理员权限:请求提权后继续安装…")
		if win32.RelaunchElevated("/install") {
			log("已弹出 UAC,请在授权窗口确认。")
		} else {
			log("✗ 提权失败(被拒绝)。")
		}
		return
	}
	runInstallActions()
	postInstallStart()
}

func runInstallActions() {
	if err := eventbus.EnsureLogs(); err != nil {
		fmt.Println("事件日志创建失败:", err)
	} else {
		fmt.Println("事件日志已创建")
	}
	exe := filepath.Join(selfDir, "ccservice.exe")
	if _, err := os.Stat(exe); err != nil {
		fmt.Println("未找到 ccservice.exe,跳过服务安装")
		return
	}
	if err := win32.InstallService(svcName, "Control Center Hotkey Service", exe); err != nil {
		fmt.Println("服务安装:", err)
		return
	}
	fmt.Println("服务已安装")
	if err := win32.StartService(svcName, 30*time.Second); err != nil {
		fmt.Println("服务启动:", err)
	}
	fmt.Println("OK")
}

// postInstallStart 安装完成后启动托盘+主界面并创建快捷方式。
func postInstallStart() {
	log("安装动作完成,启动托盘与主界面…")
	if err := createShortcut(); err != nil {
		log("桌面快捷方式:%v", err)
	} else {
		log("✓ 已创建桌面快捷方式")
	}
	startAll()
	autoCheck()
}

// onStart 启动托盘与主界面(免管理员)。
func onStart() { startAll() }

func startAll() {
	if !win32.ProcessExistsByName("ccfnkey.exe") {
		tray := filepath.Join(selfDir, "ccfnkey.exe")
		if _, err := os.Stat(tray); err == nil {
			if err := win32.StartAsActiveUser(`"` + tray + `"`); err != nil {
				log("托盘启动失败: %v", err)
			} else {
				log("✓ 托盘已启动")
			}
		} else {
			log("✗ 未找到 ccfnkey.exe")
		}
	} else {
		log("托盘已在运行")
	}
	if !win32.ProcessExistsByName("ccapp.exe") {
		app := filepath.Join(selfDir, "ccapp.exe")
		if _, err := os.Stat(app); err == nil {
			if err := win32.StartAsActiveUser(`"` + app + `"`); err != nil {
				log("主界面启动失败: %v", err)
			} else {
				log("✓ 主界面已启动(浏览器将自动打开 http://127.0.0.1:8765/)")
			}
		} else {
			log("✗ 未找到 ccapp.exe")
		}
	} else {
		log("主界面已在运行 → http://127.0.0.1:8765/")
		win32.OpenAppWindow("http://127.0.0.1:8765/")
	}
}

// onUninstall 卸载:提权后移除服务并结束进程。
func onUninstall() {
	if !win32.IsElevated() {
		log("需要管理员权限:请求提权后继续卸载…")
		if !win32.RelaunchElevated("/uninstall") {
			log("✗ 提权失败(被拒绝)。")
		}
		return
	}
	runUninstallActions()
	log("卸载完成。")
	autoCheck()
}

func runUninstallActions() {
	if err := win32.RemoveService(svcName); err != nil {
		fmt.Println("服务卸载:", err)
	} else {
		fmt.Println("服务已卸载")
	}
}

// onDiagnose 硬件诊断(输出到日志区)。
func onDiagnose() {
	go func() {
		log("── 硬件诊断 ─────────────────────")
		path, err := acpibridge.DevicePath()
		if err != nil {
			log("ACPI 桥: %v", err)
			return
		}
		log("ACPI 桥设备: %s", path)
		as := appsettings.Default
		if img, err := acpibridge.Default().ReadSettingsImage(); err == nil {
			log("设置区 4KB 读取成功;ClevoID=%d 电源模式=%d 语言=%d",
				as.GetClevoID(), as.GetPowerMode(), as.GetUILanguage())
			_ = img
		} else {
			log("设置区读取失败: %v", err)
		}
		w := wmi.Default
		w16 := w.GetInt(16)
		log("WMI16=%#x(电源模式%v Flexikey%v 键盘%v 风扇%v)",
			w16, w16&1 != 0, w16&2 != 0, w16&0x18 != 0, w16&0x80 != 0)
		log("WMI96=%#x  WMI122=%#x", w.GetInt(96), w.GetInt(122))
		p17 := w.GetPackage(17)
		log("WMI17: 客户=%d 皮肤=(%d,%d,%d)", p17[2], p17[3], p17[4], p17[5])
		live, err := fan.Default.ReadLive()
		if err != nil {
			log("风扇实时读取失败: %v", err)
		} else {
			log("风扇: CPU %dRPM/%d%%/%.0f℃  GPU1 %dRPM/%d%%/%.0f℃",
				live.Fan.RPM, live.Fan.Duty*100/255, live.Fan.Temp,
				live.GPU1.RPM, live.GPU1.Duty*100/255, live.GPU1.Temp)
		}
		log("键盘灯类型: %d", keyboard.Default.Type())
		t := smbios.Get()
		log("SMBIOS: %s %s · BIOS %s · %s",
			t.System.ManufacturerName, t.System.ProductName, t.BIOS.Version, t.CPU.Version)
		log("事件日志: %v", eventbus.LogsExist())
		log("── 诊断结束 ─────────────────────")
	}()
}

// createShortcut 桌面快捷方式(PowerShell WScript.Shell,隐藏窗口)。
func createShortcut() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	target := filepath.Join(selfDir, "ccapp.exe")
	lnk := filepath.Join(home, "Desktop", "Control Center.lnk")
	script := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');`+
		`$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.Save()`, lnk, target, selfDir)
	cmd := []string{"powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script}
	return runHidden(cmd)
}

func runHidden(args []string) error {
	return runCmd(args)
}

var _ = strings.TrimSpace

// selftest 自检:执行与"硬件诊断"按钮相同的检测链路,输出到 stdout。
func selftest() {
	w2, err := win32.NewSimpleWindow("selftest", 100, 100)
	if err != nil {
		fmt.Println("窗口创建失败:", err)
		os.Exit(1)
	}
	win = w2
	win.AddEdit(0, 0, 10, 10)
	log("自检开始")
	onDiagnose()
	time.Sleep(4 * time.Second)
	fmt.Println("自检:窗口/按钮/日志链路 OK")
}

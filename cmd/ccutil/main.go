// ccutil — 原套件工具程序的合集(CClnk/RunAutoInstall/RemoveAPP/AddFn/clean/WakePanelUp_Hook)。
//
// 用法:ccutil <子命令> [参数]
//
//	shortcut <目标exe> [快捷方式名]     创建桌面/开始菜单快捷方式(CClnk.exe)
//	provision <appxbundle> <license>    DISM 预配安装 Appx(RunAutoInstall.exe)
//	provision-all <目录>                预配目录下全部 appxbundle
//	removeapp <identity>                移除 UWP 应用(RemoveAPP.exe)
//	clean                               清理注册表残留(clean.exe)
//	wakepanel                           面板唤醒钩子(WakePanelUp_Hook.exe)
//	service install|remove              安装/卸载 ccservice
//	logs                                创建事件日志(OutLog/PowerBiosServerLog)
//	info                                打印 ACPI 桥/设置区诊断信息
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/uwp"
	"controlcenter/internal/win32"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "shortcut":
		cmdShortcut(os.Args[2:])
	case "provision":
		if err := uwp.ProvisionAppx(os.Args[2], os.Args[3]); err != nil {
			fatal(err)
		}
		fmt.Println("预配完成:", os.Args[2])
	case "provision-all":
		cmdProvisionAll(os.Args[2])
	case "removeapp":
		if err := uwp.RemoveAppx(os.Args[2]); err != nil {
			fatal(err)
		}
		fmt.Println("已移除:", os.Args[2])
	case "clean":
		cmdClean()
	case "wakepanel":
		cmdWakePanel()
	case "service":
		cmdService(os.Args[2:])
	case "logs":
		if err := eventbus.EnsureLogs(); err != nil {
			fatal(err)
		}
		fmt.Println("事件日志已创建")
	case "info":
		cmdInfo()
	default:
		usage()
	}
}

func usage() {
	fmt.Print(`ccutil — Control Center 工具集
用法:
  ccutil shortcut <exe> [名称]      创建桌面快捷方式
  ccutil provision <appx> <lic>     预配安装 Appx
  ccutil provision-all <目录>       预配目录下全部 Appx
  ccutil removeapp <identity>       移除 UWP 应用
  ccutil clean                      清理注册表残留
  ccutil wakepanel                  面板唤醒钩子(常驻)
  ccutil service install|remove     安装/卸载 ccservice
  ccutil logs                       创建事件日志
  ccutil info                       ACPI 桥诊断
`)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "错误:", err); os.Exit(1) }

// cmdShortcut CClnk.exe 等价:创建快捷方式(经 PowerShell WScript.Shell)。
func cmdShortcut(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: ccutil shortcut <exe> [名称]")
		return
	}
	target, _ := filepath.Abs(args[0])
	name := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	if len(args) >= 2 {
		name = args[1]
	}
	desktop := desktopDir()
	lnk := filepath.Join(desktop, name+".lnk")
	script := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');`+
		`$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s';$s.Save()`,
		lnk, target, filepath.Dir(target), target)
	out, err := powershell(script)
	if err != nil {
		fatal(fmt.Errorf("创建快捷方式失败: %v (%s)", err, out))
	}
	fmt.Println("快捷方式已创建:", lnk)
}

func desktopDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Desktop")
	}
	return "."
}

func powershell(script string) (string, error) {
	cmd := []string{"powershell", "-NoProfile", "-Command", script}
	return run(cmd...)
}

// cmdProvisionAll RunAutoInstall.exe 等价。
func cmdProvisionAll(dir string) {
	if dir == "" {
		dir = `C:\Program Files (x86)\ControlCenter\AppInstall`
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		appx := findFile(sub, ".appxbundle")
		lic := findFile(sub, "_License1.xml")
		if appx == "" || lic == "" {
			continue
		}
		fmt.Printf("预配 %s … ", e.Name())
		if err := uwp.ProvisionAppx(appx, lic); err != nil {
			fmt.Println("失败:", err)
		} else {
			fmt.Println("完成")
		}
	}
}

func findFile(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ext) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// cmdClean clean.exe 等价:清理 ControlCenter 注册表残留。
func cmdClean() {
	cleanKeys := []string{
		`Software\ControlCenter2.0`,
		`Software\ControlCenter3.0`,
		`Software\ControlCenterAP`,
		`Software\hotkey`,
		`Software\ComboKey`,
	}
	for _, k := range cleanKeys {
		if err := deleteRegTree("HKCU", k); err == nil {
			fmt.Println("已清理:", k)
		}
	}
	fmt.Println("清理完成")
}

// cmdWakePanel WakePanelUp_Hook.exe 等价:监听用户输入唤醒面板(简化实现:
// 阻塞等待,由调用方按需运行;原生版本使用低级钩子)。
func cmdWakePanel() {
	fmt.Println("wakepanel 钩子运行中(Ctrl+C 退出)")
	for {
		win32.SendWinCtrlF24(false)
		win32.SendWinCtrlF24(true)
		sleepHours(24)
	}
}

func cmdService(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: ccutil service install|remove")
		return
	}
	switch args[0] {
	case "install":
		if err := eventbus.EnsureLogs(); err != nil {
			fmt.Println("创建事件日志(可能已存在):", err)
		}
		exe, _ := os.Executable()
		svcExe := filepath.Join(filepath.Dir(exe), "ccservice.exe")
		if err := win32.InstallService("CCDCHUService", "Control Center Hotkey Service", svcExe); err != nil {
			fatal(err)
		}
		fmt.Println("服务已安装")
	case "remove":
		if err := win32.RemoveService("CCDCHUService"); err != nil {
			fatal(err)
		}
		fmt.Println("服务已卸载")
	}
}

// cmdInfo 诊断:ACPI 桥连接 + 设置区概要。
func cmdInfo() {
	path, err := acpibridge.DevicePath()
	if err != nil {
		fmt.Println("ACPI 桥设备:", err)
		return
	}
	fmt.Println("ACPI 桥设备:", path)
	as := appsettings.Default
	img, err := acpibridge.Default().ReadSettingsImage()
	if err != nil {
		fmt.Println("设置区读取失败:", err)
		return
	}
	fmt.Printf("设置区 4KB 读取成功(前 64 字节):\n")
	for i := 0; i < 64; i += 16 {
		fmt.Printf("  %04x  % x\n", i, img[i:i+16])
	}
	fmt.Println("ClevoID:", as.GetClevoID())
	fmt.Println("电源模式:", as.GetPowerMode())
	fmt.Println("UI 语言:", as.GetUILanguage())
	fmt.Println("事件日志:", eventbus.LogsExist())
}

func sleepHours(h int) { sleepMs(h * 3600 * 1000) }

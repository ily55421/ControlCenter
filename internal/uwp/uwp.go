// Package uwp — UWP 应用检测/启动(PowerShell Get-AppxPackage 与 URI 协议激活)。
package uwp

import (
	"os/exec"
	"strings"

	"controlcenter/internal/eventbus"
	"controlcenter/internal/win32"
)

// 包族名(与原套件一致)。
const (
	PFNControlCenter = "CLEVOCO.ControlCenter3.0_6h6z29zh29qx0"
	PFNFlexikey      = "CLEVOCO.Flexikey_6h6z29zh29qx0"
	PFNFlexiAccess   = "CLEVOCO.FlexiAccess_6h6z29zh29qx0"
	PFNLedKeyboard   = "CLEVOCO.LedKeyboardSetting_6h6z29zh29qx0"
	PFNFanSpeed      = "CLEVOCO.504814C03D814_6h6z29zh29qx0"
	PFNCPUOC         = "CLEVOCO.CPUOverclocking_6h6z29zh29qx0"
	PFNGPUOC         = "CLEVOCO.GPUOverclocking_6h6z29zh29qx0"
	PFNEnergySave    = "CLEVOCO.EnergySaveUtility_6h6z29zh29qx0"
	PFNBattery       = "CLEVOCO.BatteryPackUtility_6h6z29zh29qx0"
	PFNFnHotkeys     = "CLEVOCO.FnhotkeysandOSD_6h6z29zh29qx0"
)

// App 定义(磁贴表)。
type App struct {
	URI        string // 协议激活
	PFN        string // 包族名(w_arg253 launchapp)
	Process    string // Win32 进程检测名
	WindowName string
}

// Apps 全部功能应用。
var Apps = map[string]App{
	"power":     {URI: "clevofnkey:", PFN: "", Process: "", WindowName: ""},
	"flexikey":  {URI: "clevoflexikey:", PFN: PFNFlexikey, Process: "cc.exe", WindowName: "ControlCenter"},
	"flexiacc":  {URI: "clevofa:", PFN: PFNFlexiAccess, Process: "fa30.exe", WindowName: "FlexiAccess"},
	"keyboard":  {URI: "clevokeyboard:", PFN: PFNLedKeyboard, Process: "ledkeyboardsetting.exe", WindowName: "LED Keyboard Setting"},
	"perkey":    {URI: "clevoperkey:", PFN: PFNLedKeyboard, Process: "ledkeyboardsetting.exe", WindowName: "LED Keyboard Setting"},
	"gpu":       {URI: "clevogpu:", PFN: PFNGPUOC, Process: "gpuoverclocking.exe", WindowName: "GPU Overclocking"},
	"cpu":       {URI: "clevocpu:", PFN: PFNCPUOC, Process: "cpuoc2.0.exe", WindowName: "CPU OverClocking"},
	"fan":       {URI: "clevofan:", PFN: PFNFanSpeed, Process: "fanspeedsetting.exe", WindowName: "Fan Speed Setting"},
	"energy":    {URI: "clevoenergy:", PFN: PFNEnergySave, Process: "desktopbridge_energysave.exe", WindowName: "Energy Save"},
	"battery":   {URI: "clevobattery:", PFN: PFNBattery, Process: "batterypackutility.exe", WindowName: "Battery Utility"},
}

// Installed 检测 UWP 是否已装(PowerShell Get-AppxPackage)。
func Installed(identity string) bool {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-AppxPackage "+identity+" | Select -Expand Name")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// Launch 启动应用:已装走 w_arg253 launchapp(服务拉起),否则协议激活。
func Launch(key string) {
	app, ok := Apps[key]
	if !ok {
		return
	}
	if app.PFN != "" && Installed(strings.Split(app.PFN, "_")[0]) {
		_ = eventbus.LaunchApp(app.PFN)
		go func() {
			if !win32.ProcessExistsByName(app.Process) {
				_ = win32.ShellExecuteOpen(app.URI)
			}
		}()
		return
	}
	if app.URI != "" {
		_ = win32.ShellExecuteOpen(app.URI)
	}
}

// LaunchFnKey 启动 FnHotkeys UWP(explorer shell:appsFolder)。
func LaunchFnKey() {
	_ = win32.StartAsExplorer(`explorer.exe shell:appsFolder\` + PFNFnHotkeys + "!App")
}

// ProvisionAppx DISM 预配安装(RunAutoInstall 等价)。
func ProvisionAppx(appxPath, licensePath string) error {
	cmd := exec.Command("dism", "/Online", "/Add-ProvisionedAppxPackage",
		"/PackagePath:"+appxPath, "/LicensePath:"+licensePath, `/Region="all"`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	_ = out
	return nil
}

// RemoveAppx 移除 UWP 包。
func RemoveAppx(identity string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Remove-AppxPackage (Get-AppxPackage "+identity+").PackageFullName")
	return cmd.Run()
}

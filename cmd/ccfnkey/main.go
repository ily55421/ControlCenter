// ccfnkey — FnKey.exe 的 Go 等价实现(用户会话托盘程序):
// 托盘图标+菜单、电源模式引擎、热键事件分发(OSD)、OutLog 事件总线监听、睡眠/恢复联动。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
	"controlcenter/internal/battery"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/hotkeys"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/launcher"
	"controlcenter/internal/osdnotifier"
	"controlcenter/internal/powermode"
	"controlcenter/internal/win32"
	"controlcenter/internal/wmi"
)

const appName = "Control Center"

func main() {
	log.SetFlags(log.Ltime | log.Ldate)

	// 单实例
	if dup, err := win32.SingleInstanceGuard("ccfnkey"); err == nil && dup {
		return
	}

	// 等待事件日志基础设施(由 ccservice 创建)
	waitLogs()

	w := wmi.Default
	as := appsettings.Default
	kb := keyboard.Default
	fc := fan.Default
	gpuStore := gpu.Default
	pm := powermode.New(w, as, fc, gpuStore)

	exe, _ := os.Executable()
	pm.ControlGPUPath = findControlGPU(filepath.Dir(exe))

	// 开机回写充电模式(ccapp 若已先跑过则此处为幂等空操作)
	if applied, err := battery.RestoreCharger(w); err != nil {
		log.Printf("FlexiCharger 回写失败: %v", err)
	} else if applied {
		log.Printf("FlexiCharger 已按记忆回写 EC")
	}

	// OSD(独立线程的消息循环)
	osdSvc := osdnotifier.New()
	go osdSvc.Run()

	disp := hotkeys.New(w, as, kb, fc, pm, osdSvc)

	// OutLog 事件总线监听(clevocc30^101 等命令 + Arg001 热键)
	go busListener(pm, disp)

	// 启动时按 DC/AC 策略恢复模式(原版 GetDCModePowerMode 逻辑)
	initialMode(pm, as)

	// 托盘
	tray, err := win32.NewTray(appName+" (Go)", nil, 0, 0)
	if err != nil {
		log.Printf("托盘创建失败: %v", err)
		select {} // 仍保持事件处理
	}
	setupTray(tray, pm, as)
	tray.RunMessageLoop()
}

func waitLogs() {
	// 服务启动时秒级创建日志:200ms 一轮即可,原来 1s 一轮让托盘平白晚起。
	for i := 0; i < 150; i++ {
		if eventbus.LogsExist() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	log.Printf("事件日志不存在,由本进程尝试创建")
	if err := eventbus.EnsureLogs(); err != nil {
		log.Printf("EnsureLogs: %v(可能权限不足,事件将无法广播)", err)
	}
}

func findControlGPU(dir string) string { return gpu.FindControlGPU(dir) }

func initialMode(pm *powermode.Engine, as *appsettings.Store) {
	if !acpibridgeAvailable() {
		return
	}
	if win32.ACOnline() {
		m := appsettings.ModeType(as.ReadU8(appsettings.AddrACModePowerMode))
		go pm.Change(m)
	} else {
		// DC:quiet_plus(仅当无 PowerSaving)否则 quiet/pwrsaving
		if as.ReadU8(appsettings.AddrQuietNVPwrOffDis) == 0 {
			go pm.Change(appsettings.QuietPlus)
		} else {
			go pm.Change(appsettings.Quiet)
		}
	}
}

func acpibridgeAvailable() bool {
	_, err := acpibridge.DevicePath()
	return err == nil
}

// busListener OutLog 监听:电源模式命令 + 热键事件 + 组件事件。
func busListener(pm *powermode.Engine, disp *hotkeys.Dispatcher) {
	for {
		sub, err := eventbus.Subscribe("OutLog")
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
				handleBusMessage(e.Message, pm, disp)
			}
		}
		sub.Close()
		time.Sleep(2 * time.Second)
	}
}

func handleBusMessage(msg string, pm *powermode.Engine, disp *hotkeys.Dispatcher) {
	m := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(m, "clevocc30^101^"):
		// 解析模式号
		parts := strings.Split(msg, "^")
		if len(parts) >= 3 {
			var mode int
			fmt.Sscanf(parts[len(parts)-1], "%d", &mode)
			go pm.Change(appsettings.ModeType(mode))
		}
	case strings.HasPrefix(m, "arg001^"):
		var v uint64
		fmt.Sscanf(msg[7:], "%d", &v)
		go disp.Dispatch(v)
	case strings.Contains(m, "clevocc30^112^1"):
		// Turbo Fan 开(风扇 UI 联动)
		_ = eventbus.WriteOut("clevofan^113^0")
	case strings.Contains(m, "clevocc30^10f^1"):
		log.Printf("AMD 手动模式进入")
	}
}

// setupTray 托盘菜单。
func setupTray(tray *win32.Tray, pm *powermode.Engine, as *appsettings.Store) {
	const (
		idOpen = 1001
		idQuiet = 1010
		idPwrSave = 1011
		idPerf = 1012
		idEnt = 1013
		idQuietPlus = 1014
		idLangBase = 1100
		idExit = 1900
	)
	update := func() {
		cur := as.GetPowerMode()
		tray.SetMenu([]win32.MenuItem{
			{ID: idOpen, Text: "打开 Control Center"},
			{Separator: true},
			{ID: idQuiet, Text: "安静模式", Checked: cur == appsettings.Quiet},
			{ID: idPwrSave, Text: "节能模式", Checked: cur == appsettings.PowerSaving},
			{ID: idPerf, Text: "性能模式", Checked: cur == appsettings.Performance},
			{ID: idEnt, Text: "娱乐模式", Checked: cur == appsettings.Entertainment},
			{ID: idQuietPlus, Text: "安静增强(独显断电)", Checked: cur == appsettings.QuietPlus},
			{Separator: true},
			{ID: idExit, Text: "退出"},
		})
		tray.SetTip(appName + " - " + []string{"安静", "节能", "性能", "娱乐", "安静增强"}[cur])
	}
	update()

	tray.OnCommand(idOpen, func() {
		launcher.Open("system", launcher.MainWindowTitle)
		_ = eventbus.WriteOut("clevofnkey^149")
	})
	modeCmd := func(m appsettings.ModeType) func() {
		return func() { go pm.Change(m); time.Sleep(500 * time.Millisecond); update() }
	}
	tray.OnCommand(idQuiet, modeCmd(appsettings.Quiet))
	tray.OnCommand(idPwrSave, modeCmd(appsettings.PowerSaving))
	tray.OnCommand(idPerf, modeCmd(appsettings.Performance))
	tray.OnCommand(idEnt, modeCmd(appsettings.Entertainment))
	tray.OnCommand(idQuietPlus, modeCmd(appsettings.QuietPlus))
	tray.OnCommand(idExit, func() {
		tray.Remove()
		os.Exit(0)
	})
	tray.SetOnClick(func() {
		launcher.Open("system", launcher.MainWindowTitle)
	})

	pm.OnChanged(func(from, to appsettings.ModeType) { update() })
}

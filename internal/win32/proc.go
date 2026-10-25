// Package win32 — 进程 / 会话 / 输入合成 / 系统状态。
package win32

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// --- 进程枚举(Toolhelp32)---

// ProcessEntry 进程摘要。
type ProcessEntry struct {
	Name      string
	SessionID uint32
	PID       uint32
}

// ListProcesses 枚举全部进程。
func ListProcesses() []ProcessEntry {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []ProcessEntry
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err == nil {
		for {
			name := windows.UTF16ToString(pe.ExeFile[:])
			out = append(out, ProcessEntry{
				Name: strings.ToLower(name),
				PID:  pe.ProcessID,
			})
			pe.Size = uint32(unsafe.Sizeof(pe))
			if err := windows.Process32Next(snap, &pe); err != nil {
				break
			}
		}
	}
	// 补 SessionID(仅对感兴趣的进程查询,避免句柄开销:这里通过 WTS 枚举)
	if sess, err := WTSEnumerateProcesses(); err == nil {
		m := map[uint32]uint32{}
		for _, p := range sess {
			m[p.PID] = p.SessionID
		}
		for i := range out {
			out[i].SessionID = m[out[i].PID]
		}
	}
	return out
}

// ProcessExistsByName 进程名(小写)是否存在(含调用者自身)。
func ProcessExistsByName(name string) bool {
	name = strings.ToLower(name)
	for _, p := range ListProcesses() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// SingleInstanceGuard 用会话内命名互斥量做单实例检测。
// 比"全量进程快照 + 按 exe 名比对"(实测 ~50ms,且改名副本/无关同名进程都会误判)
// 快三个数量级,也不会误伤。句柄在进程存续期间持有,退出时由系统释放。
func SingleInstanceGuard(name string) (alreadyRunning bool, err error) {
	p, err := windows.UTF16PtrFromString(`Local\ControlCenter-Go-` + name)
	if err != nil {
		return false, err
	}
	// CreateMutexW 在"互斥量已存在"时同样返回有效句柄,并把 ERROR_ALREADY_EXISTS
	// 写进 GetLastError —— x/sys 会把它作为 err 返回,所以它不是失败信号。
	// 只有句柄为 0 才是真正的错误。
	h, err := windows.CreateMutex(nil, false, p)
	if h == 0 {
		return false, err
	}
	_ = h // 故意不 Close:句柄存活期即"本实例占用期"
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return true, nil
	}
	return false, nil
}

// ProcessesExist 一次快照判断多个进程名。
// 每次快照实测约 50ms,逐个调 ProcessExistsByName 会成倍付这份开销。
func ProcessesExist(names ...string) map[string]bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[strings.ToLower(n)] = false
	}
	for _, p := range ListProcesses() {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		}
	}
	return want
}

// ProcessExistsOther 是否存在同名进程,且不是当前进程(用于单实例检测)。
// 注意:直接用 ProcessExistsByName 检测自身同名进程会恒为真,导致进程启动即退出。
func ProcessExistsOther(name string) bool {
	name = strings.ToLower(name)
	self := uint32(os.Getpid())
	for _, p := range ListProcesses() {
		if p.Name == name && p.PID != self {
			return true
		}
	}
	return false
}

// KillOtherSessions 杀掉不在当前活动会话的进程(原 KillOtherSession)。
func KillOtherSessions(processName string) {
	active := WTSSessionID()
	name := strings.ToLower(processName)
	for _, p := range ListProcesses() {
		if p.Name == name && p.SessionID != active {
			h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, p.PID)
			if err == nil {
				windows.TerminateProcess(h, 1)
				windows.CloseHandle(h)
			}
		}
	}
}

// --- WTS 会话 ---

var (
	modwtsapi32            = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSGetActiveSessID = modwtsapi32.NewProc("WTSGetActiveConsoleSessionId")
	procWTSEnumProcs       = modwtsapi32.NewProc("WTSEnumerateProcessesW")
	procWTSFreeMem         = modwtsapi32.NewProc("WTSFreeMemory")
)

// wtsProc WTS_PROCESS_INFOW:DWORD,DWORD,LPWSTR(+8),PSID(+16)。
type wtsProc struct {
	SessionID uint32
	PID       uint32
	NamePtr   *uint16
	SID       uintptr
}

// WTSSessionID 当前活动控制台会话。
func WTSSessionID() uint32 {
	r, _, _ := procWTSGetActiveSessID.Call()
	return uint32(r)
}

// WTSEnumerateProcesses 枚举进程(带会话 ID)。
func WTSEnumerateProcesses() ([]ProcessEntry, error) {
	var buf *byte
	var count uint32
	r, _, err := procWTSEnumProcs.Call(0, 0, 1,
		uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		return nil, fmt.Errorf("WTSEnumerateProcesses: %w", err)
	}
	defer procWTSFreeMem.Call(uintptr(unsafe.Pointer(buf)))
	procs := unsafe.Slice((*wtsProc)(unsafe.Pointer(buf)), count)
	out := make([]ProcessEntry, 0, count)
	for _, p := range procs {
		name := ""
		if p.NamePtr != nil {
			name = windows.UTF16ToString(unsafe.Slice(p.NamePtr, 260))
		}
		out = append(out, ProcessEntry{Name: strings.ToLower(name), SessionID: p.SessionID, PID: p.PID})
	}
	return out, nil
}

// --- 以活动用户身份启动进程(ApplicationLoader 等价:复制 explorer 令牌 + CreateProcessAsUser)---

var (
	modadvapi32            = windows.NewLazySystemDLL("advapi32.dll")
	moduser32              = windows.NewLazySystemDLL("user32.dll")
	modkernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procWTSQueryUserToken  = modwtsapi32.NewProc("WTSQueryUserToken")
	procOpenProcess        = modkernel32.NewProc("OpenProcess")
	procOpenProcessToken   = modadvapi32.NewProc("OpenProcessToken")
	procDuplicateTokenEx   = modadvapi32.NewProc("DuplicateTokenEx")
	procCreateProcessAsUserW = modadvapi32.NewProc("CreateProcessAsUserW")
	procFindWindowW        = moduser32.NewProc("FindWindowW")
	procSetForegroundWnd   = moduser32.NewProc("SetForegroundWindow")
	procSendInput          = moduser32.NewProc("SendInput")
	procKeybdEvent         = moduser32.NewProc("keybd_event")
)

const (
	tokenDuplicate = 0x0002
	tokenAssignPrimary = 0x0001
	tokenQuery     = 0x0008
	maximumAllowed = 0x02000000
)

type startupInfoW struct {
	cb            uint32
	reserved      *uint16
	desktop       *uint16
	title         *uint16
	x, y          uint32
	xSize, ySize  uint32
	xCountChars   uint32
	yCountChars   uint32
	fillAttribute uint32
	flags         uint32
	showWindow    uint16
	cbReserved2   uint16
	lpReserved2   *byte
	stdInput      uintptr
	stdOutput     uintptr
	stdError      uintptr
}

type processInformation struct {
	process   windows.Handle
	thread    windows.Handle
	processID uint32
	threadID  uint32
}

// StartAsActiveUser 以活动控制台用户身份启动进程(服务中绕过 UAC,等价 ApplicationLoader)。
func StartAsActiveUser(cmdline string) error {
	sessionID := WTSSessionID()
	var userToken windows.Handle
	r, _, err := procWTSQueryUserToken.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&userToken)))
	if r == 0 {
		return fmt.Errorf("WTSQueryUserToken: %w", err)
	}
	defer windows.CloseHandle(userToken)

	var primary windows.Handle
	r, _, err = procDuplicateTokenEx.Call(
		uintptr(userToken), maximumAllowed, 0,
		1, // TokenPrimary
		uintptr(unsafe.Pointer(&primary)))
	if r == 0 {
		return fmt.Errorf("DuplicateTokenEx: %w", err)
	}
	defer windows.CloseHandle(primary)

	cl16, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	var si startupInfoW
	si.cb = uint32(unsafe.Sizeof(si))
	var pi processInformation
	r, _, err = procCreateProcessAsUserW.Call(
		uintptr(primary),
		0,
		uintptr(unsafe.Pointer(cl16)),
		0, 0, 0,
		0, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return fmt.Errorf("CreateProcessAsUserW(%s): %w", cmdline, err)
	}
	windows.CloseHandle(pi.process)
	windows.CloseHandle(pi.thread)
	return nil
}

// StartAsExplorer 等价 ApplicationLoader.StartProcessAndBypassUAC_AsExplorer:
// 以 explorer.exe 进程的令牌复制后 CreateProcessAsUser。
func StartAsExplorer(application string) error {
	// 找 explorer PID
	var explorerPID uint32
	active := WTSSessionID()
	if procs, err := WTSEnumerateProcesses(); err == nil {
		for _, p := range procs {
			if p.Name == "explorer.exe" && p.SessionID == active {
				explorerPID = p.PID
				break
			}
		}
	}
	if explorerPID == 0 {
		// explorer 未运行:直接以当前用户
		return StartAsActiveUser(application)
	}
	h, _, err := procOpenProcess.Call(maximumAllowed, 0, uintptr(explorerPID))
	if h == 0 {
		return fmt.Errorf("OpenProcess(explorer): %w", err)
	}
	defer windows.CloseHandle(windows.Handle(h))

	var token windows.Handle
	r, _, err := procOpenProcessToken.Call(h, tokenDuplicate|tokenQuery, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return fmt.Errorf("OpenProcessToken(explorer): %w", err)
	}
	defer windows.CloseHandle(token)

	var primary windows.Handle
	r, _, err = procDuplicateTokenEx.Call(
		uintptr(token), maximumAllowed, 0, 1, uintptr(unsafe.Pointer(&primary)))
	if r == 0 {
		return fmt.Errorf("DuplicateTokenEx(explorer): %w", err)
	}
	defer windows.CloseHandle(primary)

	cl16, err := windows.UTF16PtrFromString(application)
	if err != nil {
		return err
	}
	var si startupInfoW
	si.cb = uint32(unsafe.Sizeof(si))
	var pi processInformation
	r, _, err = procCreateProcessAsUserW.Call(
		uintptr(primary), 0, uintptr(unsafe.Pointer(cl16)),
		0, 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return fmt.Errorf("CreateProcessAsUserW(explorer,%s): %w", application, err)
	}
	windows.CloseHandle(pi.process)
	windows.CloseHandle(pi.thread)
	return nil
}

// FocusWindow 按窗口标题查找并前置。
func FocusWindow(title string) bool {
	return FocusWindowHandle(WindowByTitle(title))
}

// WindowByTitle 返回标题匹配的顶层窗口句柄(FindWindowW 按 Z 序自顶向下,
// 所以拿到的是最靠前的那个;0 表示不存在)。
func WindowByTitle(title string) uintptr {
	t, _ := windows.UTF16PtrFromString(title)
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	return h
}

// FocusWindowHandle 前置指定句柄的窗口。
// FocusWindowHandle 前置指定句柄的窗口(最小化状态先恢复 —— 导航兜底:
// 主窗口若被系统最小化,任何面板/托盘导航都应将其还原并前置)。
func FocusWindowHandle(h uintptr) bool {
	if h == 0 {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	}
	procSetForegroundWnd.Call(h)
	return true
}

// --- 输入合成 ---

// SendWinCtrlF24 发送 Win+Ctrl+F24 组合(Precision TouchPad 热键,原 Send_Win_Ctrl_F24)。
func SendWinCtrlF24(down bool) {
	const (
		vkControl = 0x11
		vkLWin    = 0x5B
		vkF24     = 0x87
		keyUp     = 0x0002
	)
	if down {
		keybdEvent(vkLWin, 0)
		keybdEvent(vkControl, 0)
		keybdEvent(vkF24, 0)
	} else {
		keybdEvent(vkF24, keyUp)
		keybdEvent(vkControl, keyUp)
		keybdEvent(vkLWin, keyUp)
	}
}

func keybdEvent(vk byte, flags uint) {
	procKeybdEvent.Call(uintptr(vk), 0, uintptr(flags), 0)
}

// --- 系统电源状态 ---

// SystemPowerStatus kernel32 GetSystemPowerStatus。
type SystemPowerStatus struct {
	ACLineStatus        byte // 0 离线 1 在线 255 未知
	BatteryFlag         byte
	BatteryLifePercent  byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// GetSystemPowerStatus 读系统电源状态。
func GetSystemPowerStatus() SystemPowerStatus {
	var s SystemPowerStatus
	mod := windows.NewLazySystemDLL("kernel32.dll")
	p := mod.NewProc("GetSystemPowerStatus")
	p.Call(uintptr(unsafe.Pointer(&s)))
	return s
}

// ACOnline 交流适配器在线。
func ACOnline() bool { return GetSystemPowerStatus().ACLineStatus == 1 }

// --- CPU 信息 ---

// CPUName 读注册表 CPU 型号名。
func CPUName() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("ProcessorNameString")
	return v
}

// IsAMDCPU CPU 是否 AMD。
func IsAMDCPU() bool { return strings.Contains(strings.ToUpper(CPUName()), "AMD") }

// --- ShellExecute 协议激活(UWP URI 等)---

// ShellExecuteOpen 以 "open" 打开(如 clevofan: 协议激活)。
func ShellExecuteOpen(target string) error {
	modshell32 := windows.NewLazySystemDLL("shell32.dll")
	p := modshell32.NewProc("ShellExecuteW")
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, err2 := p.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 5 /*SW_SHOW*/)
	if r <= 32 {
		return fmt.Errorf("ShellExecute(%s): %w", target, err2)
	}
	return nil
}

var _ = syscall.SyscallN
var _ = os.Getenv

var procIsIconic = moduser32.NewProc("IsIconic")

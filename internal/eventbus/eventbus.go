// Package eventbus 实现原套件的 Windows 事件日志 IPC:
//
//	OutLog(Source=PowerBIOSServer_Out)          —— 组件间广播事件总线
//	PowerBiosServerLog(Source=PowerBiosServerSource) —— 应用 → 服务的命令通道
//
// 写入用 ReportEventW(消息作为插入字符串,与 .NET EventLog.WriteEntry 兼容);
// 订阅用 OpenEventLog + NotifyChangeEventLog + ReadEventW 轮询。
package eventbus

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	logOut   = "OutLog"
	logIn    = "PowerBiosServerLog"
	srcOut   = "PowerBIOSServer_Out"
	srcIn    = "PowerBiosServerSource"
	eventDLL = `%SystemRoot%\Microsoft.NET\Framework64\v4.0.30319\EventLogMessages.dll`
)

// LogsExist 返回两个日志是否均已创建(与原版 DebugLog 前提检查一致)。
func LogsExist() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\EventLog\`+logOut, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	k2, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\EventLog\`+logIn, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k2.Close()
	return true
}

// EnsureLogs 创建两个自定义日志与事件源(需管理员/SYSTEM 权限,幂等)。
func EnsureLogs() error {
	for _, log := range []string{logOut, logIn} {
		base := `SYSTEM\CurrentControlSet\Services\EventLog\` + log
		k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, base, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("eventbus: 创建日志 %s: %w", log, err)
		}
		k.Close()
	}
	srcs := []struct{ log, src string }{
		{logOut, srcOut}, {logIn, srcIn},
	}
	for _, s := range srcs {
		path := `SYSTEM\CurrentControlSet\Services\EventLog\` + s.log + `\` + s.src
		k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, path, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("eventbus: 创建事件源 %s: %w", s.src, err)
		}
		if err := k.SetStringValue("EventMessageFile", eventDLL); err != nil {
			k.Close()
			return err
		}
		if err := k.SetDWordValue("TypesSupported", 7); err != nil {
			k.Close()
			return err
		}
		k.Close()
	}
	return nil
}

var (
	modadvapi32          = windows.NewLazySystemDLL("advapi32.dll")
	procRegisterEventSrc = modadvapi32.NewProc("RegisterEventSourceW")
	procReportEvent      = modadvapi32.NewProc("ReportEventW")
	procDeregisterSource = modadvapi32.NewProc("DeregisterEventSource")
	procOpenEventLog     = modadvapi32.NewProc("OpenEventLogW")
	procCloseEventLog    = modadvapi32.NewProc("CloseEventLog")
	procReadEventLog     = modadvapi32.NewProc("ReadEventLogW")
	procNotifyChange     = modadvapi32.NewProc("NotifyChangeEventLog")
	procGetNumRecords    = modadvapi32.NewProc("GetNumberOfEventLogRecords")
	procGetOldest        = modadvapi32.NewProc("GetOldestEventLogRecord")
)

const (
	eventLogSuccess     = 0x0000
	eventLogInformation = 0x0004
	seekRead  = 0x0001
	seekFwd   = 0x0002
	seekBack  = 0x0004
	seekEnd   = 0x0008
)

// writeRaw 通过 ReportEventW 写入消息(插入字符串)。
func writeRaw(sourceLog, sourceName, msg string) error {
	srcPtr, err := windows.UTF16PtrFromString(sourceName)
	if err != nil {
		return err
	}
	h, _, err := procRegisterEventSrc.Call(0, uintptr(unsafe.Pointer(srcPtr)))
	if h == 0 {
		return fmt.Errorf("eventbus: RegisterEventSource(%s): %w", sourceName, err)
	}
	defer procDeregisterSource.Call(h)

	s, err := windows.UTF16PtrFromString(msg)
	if err != nil {
		return err
	}
	strs := []uintptr{uintptr(unsafe.Pointer(s))} //nolint:govet // ReportEvent API 布局
	const eventID = 1000
	r1, _, err := procReportEvent.Call(
		h,
		eventLogInformation,
		0, // category
		uintptr(eventID),
		0, // user sid
		1, // string count
		0, // raw data size
		uintptr(unsafe.Pointer(&strs[0])),
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("eventbus: ReportEvent(%s): %w", sourceName, err)
	}
	return nil
}

// WriteOut 向 OutLog 广播事件(PowerBIOSServer_Out)。
func WriteOut(msg string) error {
	if !LogsExist() {
		return nil // 原版行为:日志不存在时静默丢弃
	}
	return writeRaw(logOut, srcOut, msg)
}

// WriteIn 向命令通道写入(PowerBiosServerSource)。
func WriteIn(msg string) error {
	if !LogsExist() {
		return nil
	}
	return writeRaw(logIn, srcIn, msg)
}

// Entry 是订阅到的一条日志。
type Entry struct {
	Message string
}

// Subscriber 订阅一个日志的后续消息。
type Subscriber struct {
	h   windows.Handle
	evt windows.Handle
	mu  sync.Mutex
}

// Subscribe 打开日志并定位到最新记录(忽略历史),返回订阅者。
func Subscribe(logName string) (*Subscriber, error) {
	lpUNC, _ := windows.UTF16PtrFromString("")
	lpn, _ := windows.UTF16PtrFromString(logName)
	h, _, err := procOpenEventLog.Call(
		uintptr(unsafe.Pointer(lpUNC)), uintptr(unsafe.Pointer(lpn)))
	if h == 0 {
		return nil, fmt.Errorf("eventbus: OpenEventLog(%s): %w", logName, err)
	}
	// 跳到末尾:SEEK_END|SEEK_READ 读取 0 字节即定位
	var dummy [1]byte
	procReadEventLog.Call(h, seekEnd|seekRead, 0,
		uintptr(unsafe.Pointer(&dummy[0])), 0, 0, 0)
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		procCloseEventLog.Call(h)
		return nil, err
	}
	if r, _, err2 := procNotifyChange.Call(h, uintptr(ev)); r == 0 {
		windows.CloseHandle(ev)
		procCloseEventLog.Call(h)
		return nil, fmt.Errorf("eventbus: NotifyChangeEventLog: %w", err2)
	}
	return &Subscriber{h: windows.Handle(h), evt: ev}, nil
}

// Wait 阻塞直到日志有新记录,返回全部新消息。
func (s *Subscriber) Wait() ([]Entry, error) {
	if _, err := windows.WaitForSingleObject(s.evt, windows.INFINITE); err != nil {
		return nil, err
	}
	windows.ResetEvent(s.evt)
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	buf := make([]byte, 1<<16)
	for {
		var read, need uint32
		r, _, err := procReadEventLog.Call(
			uintptr(s.h), seekRead|seekFwd, 0,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			uintptr(unsafe.Pointer(&read)), uintptr(unsafe.Pointer(&need)))
		if r == 0 {
			// ERROR_HANDLE_EOF(38): 读尽
			if winErr, ok := err.(windows.Errno); ok && winErr == windows.ERROR_HANDLE_EOF {
				return out, nil
			}
			// ERROR_INSUFFICIENT_BUFFER(122): 缓冲不足
			if winErr, ok := err.(windows.Errno); ok && winErr == windows.ERROR_INSUFFICIENT_BUFFER {
				buf = make([]byte, need)
				continue
			}
			return out, fmt.Errorf("eventbus: ReadEventLog: %w", err)
		}
		out = append(out, parseRecords(buf[:read])...)
	}
}

// Close 关闭订阅。
func (s *Subscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 {
		procCloseEventLog.Call(uintptr(s.h))
		s.h = 0
	}
	if s.evt != 0 {
		windows.CloseHandle(s.evt)
		s.evt = 0
	}
}

// parseRecords 解析 ReadEventLog 输出的 EVENTLOGRECORD 数组,提取插入字符串。
func parseRecords(b []byte) []Entry {
	var out []Entry
	for len(b) >= 4 {
		recLen := binary.LittleEndian.Uint32(b[0:4])
		if recLen < 56 || int(recLen) > len(b) {
			break
		}
		numStrings := binary.LittleEndian.Uint16(b[26:28])
		strOff := binary.LittleEndian.Uint32(b[36:40])
		if int(strOff) < int(recLen) && numStrings > 0 {
			// 字符串区:UTF-16 双 NUL 结尾,多串 NUL 分隔
			region := b[strOff:recLen]
			var sb strings.Builder
			for i := 0; i+1 < len(region) && numStrings > 0; i += 2 {
				ch := binary.LittleEndian.Uint16(region[i : i+2])
				if ch == 0 {
					numStrings--
					if numStrings == 0 {
						break
					}
					sb.WriteByte('\n')
					continue
				}
				sb.WriteRune(rune(ch))
			}
			out = append(out, Entry{Message: sb.String()})
		}
		b = b[recLen:]
	}
	return out
}

// LaunchApp 向服务发送 UWP 启动命令(w_arg253 launchapp)。
func LaunchApp(pfn string) error {
	return WriteIn("w_arg253^launchapp^" + pfn + "^no")
}

// LaunchAppKillOther 启动 UWP 并杀其他会话实例。
func LaunchAppKillOther(pfn, processName string) error {
	return WriteIn("w_arg253^launchapp^" + pfn + "^yes^" + processName)
}

// ExecProcess 让服务执行程序(w_arg255 带参)。
func ExecProcess(exe, args string) error {
	return WriteIn("w_arg255^" + exe + "^" + args)
}

// ExecCommand 让服务执行命令行(w_arg254,以控制台用户身份)。
func ExecCommand(cmdline string) error {
	return WriteIn("w_arg254^" + cmdline)
}

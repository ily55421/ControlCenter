// Package flexikey 宏键域:kb.dll(HKFltrLib 用户态封装)syscall 绑定 + profile.xml 持久化。
// 数据结构与调用时机与原 CC.exe 完全一致。
package flexikey

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ClevoKB kb.dll ClevoKB 结构(12 字节)。
type ClevoKB struct {
	VKKey     int32
	DownDelay int32
	UpDelay   int32
}

// ClevoKeyEx kb.dll ClevoKeyEx 结构(12 字节;status 1=按下 0=释放)。
type ClevoKeyEx struct {
	Key       int32
	Status    int32
	DelayTime int32
}

// FilterLog KeyboardFilterLog 结构(8 字节)。
type FilterLog struct {
	KeyCode    int16
	Reserved   int16
	MakeCount  int16
	BreakCount int16
}

// 鼠标键位标志(ClevoMouse)。
const (
	MouseLeft     = 0x0001
	MouseRight    = 0x0004
	MouseMiddle   = 0x0010
	MouseBackward = 0x0040
	MouseForward  = 0x0100
)

// 内置键盘触发别名(E0 77 / E0 78)。
const (
	aliasE077 = 57463
	aliasE078 = 57464
)

// Driver kb.dll 动态绑定(每次调用独立 LoadLibrary,与原版 Set2Driver 一致)。
type Driver struct {
	mu sync.Mutex
}

// NewDriver 创建驱动绑定。
func NewDriver() *Driver { return &Driver{} }

var (
	modkernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procLoadLibrary  = modkernel32.NewProc("LoadLibraryW")
	procGetProcAddress = modkernel32.NewProc("GetProcAddress")
	procFreeLibrary  = modkernel32.NewProc("FreeLibrary")
)

// callVoid 无参调用。
func (d *Driver) callVoid(dll, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL(dll)
	if h == 0 {
		return errNotFound(dll)
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, name)
	if p == 0 {
		return errNotFound(dll + "!" + name)
	}
	syscall.SyscallN(p)
	return nil
}

// callMacro 注册宏:同一热键注册三次(KeyCode、57463、57464 别名)。
func (d *Driver) registerHotkey(h uintptr, name string, keyCode int32, fn func(p uintptr, vk int32)) {
	for _, vk := range []int32{keyCode, aliasE077, aliasE078} {
		hk := []ClevoKB{{VKKey: vk}}
		fn(getProc(h, name), hk[0].VKKey)
	}
}

// EnableKB EnableFilter。
func (d *Driver) EnableKB() error { return d.callVoid("kb.dll", "EnableFilter") }

// DisableKB DisableFilter。
func (d *Driver) DisableKB() error { return d.callVoid("kb.dll", "DisableFilter") }

// ClearKB ClearFilterMacro。
func (d *Driver) ClearKB() error { return d.callVoid("kb.dll", "ClearFilterMacro") }

// EnableMouse EnableFilter_Mouse。
func (d *Driver) EnableMouse() error { return d.callVoid("kb.dll", "EnableFilter_Mouse") }

// DisableMouse DisableFilter_Mouse。
func (d *Driver) DisableMouse() error { return d.callVoid("kb.dll", "DisableFilter_Mouse") }

// ClearMouse ClearFilterMacro_Mouse。
func (d *Driver) ClearMouse() error { return d.callVoid("kb.dll", "ClearFilterMacro_Mouse") }

func loadDLL(name string) uintptr {
	p, _ := windows.UTF16PtrFromString(name)
	h, _, _ := procLoadLibrary.Call(uintptr(unsafe.Pointer(p)))
	return h
}

func getProc(h uintptr, name string) uintptr {
	n, _ := windows.BytePtrFromString(name)
	p, _, _ := procGetProcAddress.Call(h, uintptr(unsafe.Pointer(n)))
	return p
}

type notFoundError string

func (e notFoundError) Error() string { return "flexikey: 找不到 " + string(e) }

func errNotFound(what string) error { return notFoundError(what) }

// SetMacroKeyEx CreateMacroKeyEx(Hotkey[2], Macro[255])。
// 每个宏对 KeyCode 与 57463/57464 别名各注册一次(原版三重注册)。
func (d *Driver) SetMacroKeyEx(keyCode int32, macro []ClevoKeyEx) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateMacroKeyEx")
	if p == 0 {
		return errNotFound("kb.dll!CreateMacroKeyEx")
	}
	macros := make([]ClevoKeyEx, 255)
	n := len(macro)
	if n > 255 {
		n = 255
	}
	copy(macros[:n], macro[:n])
	for _, vk := range []int32{keyCode, aliasE077, aliasE078} {
		hk := [2]ClevoKB{{VKKey: vk}, {}}
		syscall.SyscallN(p,
			uintptr(unsafe.Pointer(&hk[0])),
			uintptr(unsafe.Pointer(&macros[0])))
	}
	return nil
}

// SetKeyDisable 禁用键:全零宏注册(CreateMacroKey 路径,Macro[0]=0)。
func (d *Driver) SetKeyDisable(keyCode int32) error {
	return d.SetMacroKeyEx(keyCode, []ClevoKeyEx{{Key: 0, Status: 0, DelayTime: 0}})
}

// SetStringKey CreateStringKey(Hotkey[2], Startkey, UTF16LE 文本, Endkey)。
func (d *Driver) SetStringKey(keyCode int32, startKeys, endKeys []ClevoKB, text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateStringKey")
	if p == 0 {
		return errNotFound("kb.dll!CreateStringKey")
	}
	sk := make([]ClevoKB, 16)
	ek := make([]ClevoKB, 16)
	copy(sk, startKeys)
	copy(ek, endKeys)
	// UTF-16LE 编码
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	raw := make([]byte, len(u16)*2)
	for i, v := range u16 {
		raw[i*2] = byte(v)
		raw[i*2+1] = byte(v >> 8)
	}
	for _, vk := range []int32{keyCode, aliasE077, aliasE078} {
		hk := [2]ClevoKB{{VKKey: vk}, {}}
		syscall.SyscallN(p,
			uintptr(unsafe.Pointer(&hk[0])),
			uintptr(unsafe.Pointer(&sk[0])),
			uintptr(unsafe.Pointer(&raw[0])),
			uintptr(unsafe.Pointer(&ek[0])))
	}
	return nil
}

// SetSysEvent CreateFilterSysEvent(Hotkey[2], EventID≥256)。
func (d *Driver) SetSysEvent(keyCode int32, eventID int32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateFilterSysEvent")
	if p == 0 {
		return errNotFound("kb.dll!CreateFilterSysEvent")
	}
	for _, vk := range []int32{keyCode, aliasE077, aliasE078} {
		hk := [2]ClevoKB{{VKKey: vk}, {}}
		syscall.SyscallN(p, uintptr(unsafe.Pointer(&hk[0])), uintptr(eventID))
	}
	return nil
}

// SetMouseMacro CreateMacroKey_MouseEx(MyBtn, Macro[255])。
func (d *Driver) SetMouseMacro(btn int32, macro []ClevoKeyEx) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateMacroKey_MouseEx")
	if p == 0 {
		return errNotFound("kb.dll!CreateMacroKey_MouseEx")
	}
	macros := make([]ClevoKeyEx, 255)
	n := len(macro)
	if n > 255 {
		n = 255
	}
	copy(macros[:n], macro[:n])
	syscall.SyscallN(p, uintptr(btn), uintptr(unsafe.Pointer(&macros[0])))
	return nil
}

// SetMouseDisable 鼠标键禁用(全零宏)。
func (d *Driver) SetMouseDisable(btn int32) error {
	return d.SetMouseMacro(btn, []ClevoKeyEx{{}})
}

// SetMouseStringKey CreateStringKey_Mouse(MyBtn, Start, UTF16 文本, End)。
func (d *Driver) SetMouseStringKey(btn int32, startKeys, endKeys []ClevoKB, text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateStringKey_Mouse")
	if p == 0 {
		return errNotFound("kb.dll!CreateStringKey_Mouse")
	}
	sk := make([]ClevoKB, 16)
	ek := make([]ClevoKB, 16)
	copy(sk, startKeys)
	copy(ek, endKeys)
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	raw := make([]byte, len(u16)*2)
	for i, v := range u16 {
		raw[i*2] = byte(v)
		raw[i*2+1] = byte(v >> 8)
	}
	syscall.SyscallN(p, uintptr(btn), uintptr(unsafe.Pointer(&sk[0])),
		uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&ek[0])))
	return nil
}

// SetMouseSysEvent CreateFilterSysEvent_Mouse(MyBtn, EventID)。
func (d *Driver) SetMouseSysEvent(btn int32, eventID int32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	p := getProc(h, "CreateFilterSysEvent_Mouse")
	if p == 0 {
		return errNotFound("kb.dll!CreateFilterSysEvent_Mouse")
	}
	syscall.SyscallN(p, uintptr(btn), uintptr(eventID))
	return nil
}

// StartLog StartFilterLog(按键统计开始)。
func (d *Driver) StartLog() error { return d.callVoid("kb.dll", "StartFilterLog") }

// StopLog StopFilterLog。
func (d *Driver) StopLog() error { return d.callVoid("kb.dll", "StopFilterLog") }

// ReadLogStats CountFilterLog + GetFilterLog:返回 键码→按下次数。
func (d *Driver) ReadLogStats() (map[int]int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := loadDLL("kb.dll")
	if h == 0 {
		return nil, errNotFound("kb.dll")
	}
	defer procFreeLibrary.Call(h)
	pc := getProc(h, "CountFilterLog")
	pg := getProc(h, "GetFilterLog")
	if pc == 0 || pg == 0 {
		return nil, errNotFound("kb.dll!FilterLog")
	}
	r, _, _ := syscall.SyscallN(pc)
	count := int(int32(r))
	out := make(map[int]int)
	for i := 0; i < count; i++ {
		var log FilterLog
		syscall.SyscallN(pg, uintptr(i), uintptr(unsafe.Pointer(&log)))
		out[int(log.KeyCode)&0xFFFF] = int(log.BreakCount)
	}
	return out, nil
}

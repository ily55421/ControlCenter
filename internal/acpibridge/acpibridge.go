// Package acpibridge 是 InsydeDCHU.dll 的 Go 等价实现:
// 直接与 AcpiBridge.sys 内核驱动(IOCTL 0x322400/0x322404/0x322408/0x32240C)
// 通信,复刻其全部 6 个导出函数的语义。
//
// 协议细节见 docs/01-ACPI桥驱动协议.md。
package acpibridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 设备接口类 GUID {86994C74-AD43-4812-B7E7-0C420B5C5FD7}(AcpiBridge.inf AddInterface)。
var DevInterfaceGUID = windows.GUID{
	Data1: 0x86994c74, Data2: 0xad43, Data3: 0x4812,
	Data4: [8]byte{0xb7, 0xe7, 0x0c, 0x42, 0x0b, 0x5c, 0x5f, 0xd7},
}

// ACPIBIOS_READ WMI GUID {93F224E4-FBDC-4BBF-ADD6-DB71BDC0AFAD}
// (原生 DLL 中以小端字节序嵌入输入结构头部)。
var wmiReadGUIDBytes = [16]byte{
	0xe4, 0x24, 0xf2, 0x93, 0xdc, 0xfb, 0xbf, 0x4b,
	0xad, 0xd6, 0xdb, 0x71, 0xbd, 0xc0, 0xaf, 0xad,
}

const (
	ioctlWMIMethod      = 0x322400 // WMI _DSM 求值(Get/Set)
	ioctlWaitEvent      = 0x322404 // 异步等待热键事件
	ioctlGetNotifyValue = 0x322408 // 取回通知值
	ioctlAppSettings    = 0x32240C // 4KB 设置区读写

	wmiInputSize  = 0x420 // 0x22 头 + 0x400 载荷
	wmiOutputSize = 0x40C // 0xC 头 + 0x400 数据
	payloadOffset = 0x22
	payloadMax    = 0x100
	settingsSize  = 0x1000
)

// ErrNoDevice 表示找不到 ACPI 桥设备接口(驱动未安装或未加载)。
var ErrNoDevice = errors.New("acpibridge: 未找到 ACPI 桥设备接口(请确认 AcpiBridge 驱动已安装)")

// Bridge 维护与驱动的单连接。EC 是一条串行通道,故全部 IOCTL 在 mu 下
// 整段执行(open + DeviceIoControl),并发调用方依次排队,与原生 DLL 行为一致。
type Bridge struct {
	mu   sync.Mutex
	dev  windows.Handle
	path string
}

var defaultBridge = &Bridge{}

// Default 返回进程级默认桥接实例。
func Default() *Bridge { return defaultBridge }

// DevicePath 枚举设备接口并返回第一个实例路径。
func DevicePath() (string, error) {
	var n uint32
	// CM_Get_Device_Interface_List_SizeW
	if err := cmGetInterfaceListSize(&n, &DevInterfaceGUID, 0); err != nil {
		return "", fmt.Errorf("acpibridge: CM_Get_Device_Interface_List_Size: %w", err)
	}
	if n < 2 {
		return "", ErrNoDevice
	}
	buf := make([]uint16, n)
	if err := cmGetInterfaceList(&DevInterfaceGUID, buf, 0); err != nil {
		return "", fmt.Errorf("acpibridge: CM_Get_Device_Interface_List: %w", err)
	}
	s := windows.UTF16ToString(buf)
	if s == "" {
		return "", ErrNoDevice
	}
	return s, nil
}

// openLocked 打开(或复用)设备句柄。调用方必须持有 b.mu。
func (b *Bridge) openLocked() (windows.Handle, error) {
	if b.dev != windows.InvalidHandle && b.dev != 0 {
		return b.dev, nil
	}
	path := b.path
	if path == "" {
		p, err := DevicePath()
		if err != nil {
			return 0, err
		}
		path = p
	}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(
		path16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return 0, fmt.Errorf("acpibridge: CreateFile(%s): %w", path, err)
	}
	b.dev = h
	b.path = path
	return h, nil
}

// Close 关闭设备句柄(与在途 IOCTL 互斥,不会抽走正在使用的句柄)。
func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dev == windows.InvalidHandle || b.dev == 0 {
		return nil
	}
	err := windows.CloseHandle(b.dev)
	b.dev = windows.InvalidHandle
	return err
}

// slicePtr 返回切片首地址;空切片返回 nil(驱动按长度 0 处理)。
func slicePtr(b []byte) *byte {
	if len(b) == 0 {
		return nil
	}
	return &b[0]
}

// buildWMIInput 构造 0x420 字节的 WMI 求值输入结构。
// command:WMI 命令号;payload:载荷(可为 nil,最多取 0x100 字节,不足补零)。
func buildWMIInput(command int, payload []byte) []byte {
	in := make([]byte, wmiInputSize)
	copy(in[0:16], wmiReadGUIDBytes[:])
	// in[0x10..0x13] 保持 0
	copy(in[0x14:0x18], "_DSM")
	binary.LittleEndian.PutUint32(in[0x18:0x1c], uint32(command))
	binary.LittleEndian.PutUint16(in[0x1c:0x1e], 0x0104)
	binary.LittleEndian.PutUint32(in[0x1e:0x22], 0x01000002)
	n := payloadMax
	if len(payload) < n {
		n = len(payload)
	}
	copy(in[payloadOffset:payloadOffset+n], payload[:n])
	return in
}

// wmiEntry 是输出包中的 WMI 数据对象。
type wmiEntry struct {
	Type uint16
	Size uint16
	Data []byte
}

// parseWMIOutput 解析 0x40C 输出缓冲,返回全部条目。
func parseWMIOutput(out []byte) ([]wmiEntry, error) {
	if len(out) < 0xC {
		return nil, errors.New("acpibridge: WMI 输出过短")
	}
	count := binary.LittleEndian.Uint32(out[8:12])
	// count 由驱动缓冲给出:先按容量钳制上限(每条至少 4 字节头 + 4 字节数据),
	// 否则畸形 count 会让 make 直接触发不可 recover 的 out-of-memory。
	if max := uint32((len(out) - 0xC) / 8); count > max {
		count = max
	}
	entries := make([]wmiEntry, 0, count)
	off := uint32(0xC)
	for i := uint32(0); i < count; i++ {
		if off+4 > uint32(len(out)) {
			break
		}
		t := binary.LittleEndian.Uint16(out[off : off+2])
		sz := binary.LittleEndian.Uint16(out[off+2 : off+4])
		dataLen := uint32(sz)
		if dataLen < 4 {
			dataLen = 4
		}
		if off+4+dataLen > uint32(len(out)) {
			// 尾部截断:取剩余可用数据
			if off+4 <= uint32(len(out)) {
				entries = append(entries, wmiEntry{Type: t, Size: sz, Data: append([]byte{}, out[off+4:]...)})
			}
			break
		}
		entries = append(entries, wmiEntry{Type: t, Size: sz, Data: append([]byte{}, out[off+4:off+4+dataLen]...)})
		off += 4 + dataLen
	}
	return entries, nil
}

func (b *Bridge) deviceIoControl(code uint32, in []byte, out []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, err := b.openLocked()
	if err != nil {
		return err
	}
	var ret uint32
	err = windows.DeviceIoControl(
		h, code,
		slicePtr(in), uint32(len(in)),
		slicePtr(out), uint32(len(out)),
		&ret, nil,
	)
	if err != nil {
		return fmt.Errorf("acpibridge: DeviceIoControl(0x%08x): %w", code, err)
	}
	return nil
}

// GetInt 等价 GetDCHU_Data_Integer:执行 WMI 读,返回第一个整数条目的值。
func (b *Bridge) GetInt(command int) (int, error) {
	out, err := b.exchange(command, nil)
	if err != nil {
		return 0, err
	}
	entries, err := parseWMIOutput(out)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.Type == 0 && len(e.Data) >= 4 {
			return int(int32(binary.LittleEndian.Uint32(e.Data))), nil
		}
	}
	return 0, fmt.Errorf("acpibridge: GetInt(%d): 输出无整数条目", command)
}

// GetBuffer 等价 GetDCHU_Data_Buffer:读取 256 字节 WMI 包(type==2 条目数据)。
// 失败返回全零数组(与原生 DLL 行为一致),但把原因一并返回,调用方得以区分"读到 0"与"没读到"。
func (b *Bridge) GetBuffer(command int) ([]byte, error) {
	buf := make([]byte, 256)
	out, err := b.exchange(command, nil)
	if err != nil {
		return buf, err
	}
	entries, err := parseWMIOutput(out)
	if err != nil {
		return buf, err
	}
	found := false
	for _, e := range entries {
		if e.Type == 2 {
			n := len(e.Data)
			if n > 256 {
				n = 256
			}
			copy(buf[:n], e.Data[:n])
			found = true
		}
	}
	if !found {
		return buf, fmt.Errorf("acpibridge: GetBuffer(%d): 输出无缓冲条目", command)
	}
	return buf, nil
}

// SetData 等价 SetDCHU_Data:下发 WMI 写(载荷最多 256 字节,不足补零)。
func (b *Bridge) SetData(command int, payload []byte) error {
	_, _, err := b.SetDataResult(command, payload)
	return err
}

// SetDataResult SetDCHU_Data 并解析输出条目(WMI 65 等命令的读数经 Set 返回值携带)。
// 返回第一个整数条目值与第一个缓冲条目数据。
func (b *Bridge) SetDataResult(command int, payload []byte) (int, []byte, error) {
	if len(payload) > payloadMax {
		payload = payload[:payloadMax]
	}
	out := make([]byte, wmiOutputSize)
	in := buildWMIInput(command, payload)
	if err := b.deviceIoControl(ioctlWMIMethod, in, out); err != nil {
		return 0, nil, err
	}
	entries, err := parseWMIOutput(out)
	if err != nil {
		return 0, nil, err
	}
	intVal := 0
	var bufVal []byte
	for _, e := range entries {
		if e.Type == 0 && len(e.Data) >= 4 && intVal == 0 && bufVal == nil {
			intVal = int(int32(binary.LittleEndian.Uint32(e.Data)))
		}
		if e.Type == 2 && bufVal == nil {
			bufVal = e.Data
		}
	}
	return intVal, bufVal, nil
}

// SetDataEx 等价 SetDCHU_DataEx:下发 256 字节包并取回 256 字节输出包。
func (b *Bridge) SetDataEx(command int, payload []byte) ([]byte, error) {
	result := make([]byte, 256)
	out := make([]byte, wmiOutputSize)
	if len(payload) > payloadMax {
		payload = payload[:payloadMax]
	}
	in := buildWMIInput(command, payload)
	if err := b.deviceIoControl(ioctlWMIMethod, in, out); err != nil {
		return result, err
	}
	entries, err := parseWMIOutput(out)
	if err != nil {
		return result, err
	}
	for _, e := range entries {
		if e.Type == 2 {
			n := len(e.Data)
			if n > 256 {
				n = 256
			}
			copy(result[:n], e.Data[:n])
		}
	}
	return result, nil
}

// exchange 执行一次 WMI IOCTL 并返回原始输出。
func (b *Bridge) exchange(command int, payload []byte) ([]byte, error) {
	out := make([]byte, wmiOutputSize)
	in := buildWMIInput(command, payload)
	if err := b.deviceIoControl(ioctlWMIMethod, in, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadAppSettings 等价 ReadAppSettings:读取设置区 page/offset/length 切片。
// 底层整读 4KB 镜像后切片(与原生实现一致)。
func (b *Bridge) ReadAppSettings(page, offset, length int) ([]byte, error) {
	if page < 0 || page > 15 || offset < 0 || offset > 255 || length < 0 || offset+length > 256 {
		return make([]byte, max0(length)), fmt.Errorf("acpibridge: AppSettings 参数越界 page=%d off=%d len=%d", page, offset, length)
	}
	image, err := b.ReadSettingsImage()
	if err != nil {
		return make([]byte, max0(length)), err
	}
	start := page*256 + offset
	return append([]byte{}, image[start:start+length]...), nil
}

// ReadSettingsImage 读取整个 4KB 设置区镜像。
func (b *Bridge) ReadSettingsImage() ([]byte, error) {
	in := make([]byte, 12)
	binary.LittleEndian.PutUint32(in[0:4], 0)
	binary.LittleEndian.PutUint32(in[4:8], 0x10000000)
	binary.LittleEndian.PutUint32(in[8:12], 0)
	out := make([]byte, settingsSize)
	if err := b.deviceIoControl(ioctlAppSettings, in, out); err != nil {
		return nil, err
	}
	return out, nil
}

// WriteAppSettings 等价 WriteAppSettings:写设置区。
// 线性地址 = page*0x100 + offset,长度与数据打包后整包下发(0x1008)。
func (b *Bridge) WriteAppSettings(page, offset, length int, data []byte) error {
	if page < 0 || page > 15 || offset < 0 || offset > 255 || length < 0 || offset+length > 256 {
		return fmt.Errorf("acpibridge: AppSettings 参数越界 page=%d off=%d len=%d", page, offset, length)
	}
	if page*0x100+offset+length > settingsSize {
		return fmt.Errorf("acpibridge: AppSettings 越界写 page=%d off=%d len=%d", page, offset, length)
	}
	in := make([]byte, 8+settingsSize)
	binary.LittleEndian.PutUint32(in[0:4], 1) // 写命令
	binary.LittleEndian.PutUint16(in[4:6], uint16(page*0x100+offset))
	binary.LittleEndian.PutUint16(in[6:8], uint16(length))
	n := length
	if n > len(data) {
		n = len(data)
	}
	copy(in[8:8+n], data[:n])
	out := make([]byte, 8)
	return b.deviceIoControl(ioctlAppSettings, in, out)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// EventLoop 热键事件循环:阻塞调用 fn(通知值),直到 fn 返回 false 或出错。
// 等价 DCHU.DoEventWork:CreateEvent → WAIT_EVENT → 等待 → GET_NOTIFY_VALUE 循环。
type EventLoop struct {
	b       *Bridge
	dev     windows.Handle
	event   windows.Handle
	stopped bool
	mu      sync.Mutex
}

// NewEventLoop 创建独立设备连接的事件循环(与命令通道隔离,避免互锁)。
func NewEventLoop() (*EventLoop, error) {
	path, err := DevicePath()
	if err != nil {
		return nil, err
	}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		path16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("acpibridge: EventLoop CreateFile: %w", err)
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil) // 手动复位
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &EventLoop{b: Default(), dev: h, event: ev}, nil
}

// Run 阻塞运行;fn 收到通知值(低 8 位事件号,高位数据);返回 false 停止。
// sendReady 为可选回调:事件注册成功后调用(用于写 AppSettings(1,12)=1)。
func (e *EventLoop) Run(fn func(notifyValue uint64) bool, sendReady func()) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return errors.New("acpibridge: EventLoop 已停止")
	}
	e.mu.Unlock()

	// 注册等待事件:ACPI_HSA_WAIT_EVENT{HANDLE}
	in := make([]byte, 8)
	*(*windows.Handle)(unsafe.Pointer(&in[0])) = e.event
	out := make([]byte, 256)
	var ret uint32
	if err := windows.DeviceIoControl(e.dev, ioctlWaitEvent,
		(*byte)(unsafe.Pointer(&in[0])), uint32(len(in)),
		(*byte)(unsafe.Pointer(&out[0])), uint32(len(out)), &ret, nil); err != nil {
		return fmt.Errorf("acpibridge: WAIT_EVENT: %w", err)
	}
	if sendReady != nil {
		sendReady()
	}
	for {
		s, err := windows.WaitForSingleObject(e.event, windows.INFINITE)
		if err != nil || s == windows.WAIT_FAILED {
			return fmt.Errorf("acpibridge: WaitForSingleObject: %v", err)
		}
		windows.ResetEvent(e.event)
		var val int64
		vout := make([]byte, 8)
		if err := windows.DeviceIoControl(e.dev, ioctlGetNotifyValue,
			nil, 0,
			(*byte)(unsafe.Pointer(&vout[0])), 8, &ret, nil); err != nil {
			return fmt.Errorf("acpibridge: GET_NOTIFY_VALUE: %w", err)
		}
		val = int64(binary.LittleEndian.Uint64(vout))
		if !fn(uint64(val)) {
			return nil
		}
	}
}

// Stop 请求停止并释放资源。
func (e *EventLoop) Stop() {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	if e.event != 0 && e.event != windows.InvalidHandle {
		windows.SetEvent(e.event)
	}
	if e.dev != 0 && e.dev != windows.InvalidHandle {
		windows.CancelIoEx(e.dev, nil)
		windows.CloseHandle(e.dev)
		e.dev = windows.InvalidHandle
	}
	if e.event != 0 && e.event != windows.InvalidHandle {
		windows.CloseHandle(e.event)
		e.event = windows.InvalidHandle
	}
}

// DebugExchange 调试:返回命令的原始 IOCTL 输出(诊断用)。
func (b *Bridge) DebugExchange(command int) ([]byte, error) {
	return b.exchange(command, nil)
}

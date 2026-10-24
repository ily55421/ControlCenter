// audio.go — CoreAudio COM 麦克风静音(等价原版 FnKey 的 DgMic:
// MMDeviceEnumerator → 默认捕获端点 → IAudioEndpointVolume.GetMute/SetMute)。
// COM 接口指针以 unsafe.Pointer 持有;所有 unsafe.Pointer→uintptr 转换均内联在
// SyscallN 调用表达式中(避免栈移动导致的悬垂指针),vtable 手工索引,无 CGO。
package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modole32           = windows.NewLazySystemDLL("ole32.dll")
	procCoInitEx       = modole32.NewProc("CoInitializeEx")
	procCoCreateInst   = modole32.NewProc("CoCreateInstance")
	procCoUninitialize = modole32.NewProc("CoUninitialize")
)

// COM GUID。
var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioEndpointVolume = windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	eCapture                = 1 // 麦克风等捕获设备
	eMultimedia             = 0
	rpcEChangedMode         = 0x80010106
)

// vtableOf 取 COM 接口的 vtable(obj 指向的结构第一个字段是 vtable 指针)。
func vtableOf(obj unsafe.Pointer) (*[64]uintptr, error) {
	if obj == nil {
		return nil, fmt.Errorf("audio: 空 COM 接口")
	}
	return (*[64]uintptr)(*(*unsafe.Pointer)(obj)), nil
}

func comRelease(obj unsafe.Pointer) {
	if obj == nil {
		return
	}
	if vt, err := vtableOf(obj); err == nil {
		syscall.SyscallN(vt[2], uintptr(obj)) // IUnknown::Release
	}
}

func hresultErr(r uintptr, what string) error {
	if int32(r) < 0 {
		return fmt.Errorf("audio: %s 失败 HRESULT=0x%08x", what, uint32(r))
	}
	return nil
}

// coInit 初始化 COM(容忍 RPC_E_CHANGED_MODE),返回是否需要 Uninit。
func coInit() bool {
	hr, _, _ := procCoInitEx.Call(0, coinitApartmentThreaded)
	return int32(hr) >= 0 || uint32(hr) == rpcEChangedMode
}

// defaultCaptureEndpoint 返回默认麦克风 IMMDevice(调用方负责 Release)。
func defaultCaptureEndpoint() (unsafe.Pointer, error) {
	var enum unsafe.Pointer
	r, _, _ := procCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if int32(r) < 0 || enum == nil {
		return nil, fmt.Errorf("audio: CoCreateInstance(MMDeviceEnumerator) HRESULT=0x%08x", uint32(r))
	}
	defer comRelease(enum)
	// IMMDeviceEnumerator vtable: 0-2 IUnknown, 3 EnumAudioEndpoints, 4 GetDefaultAudioEndpoint
	vt, err := vtableOf(enum)
	if err != nil {
		return nil, err
	}
	var dev unsafe.Pointer
	r, _, _ = syscall.SyscallN(vt[4], uintptr(enum), eCapture, eMultimedia, uintptr(unsafe.Pointer(&dev)))
	if err := hresultErr(r, "GetDefaultAudioEndpoint(eCapture)"); err != nil {
		return nil, err
	}
	return dev, nil
}

// endpointVolume 激活设备的 IAudioEndpointVolume(调用方负责 Release)。
func endpointVolume(dev unsafe.Pointer) (unsafe.Pointer, error) {
	// IMMDevice vtable: 0-2 IUnknown, 3 Activate, 4 OpenPropertyStore
	vt, err := vtableOf(dev)
	if err != nil {
		return nil, err
	}
	var vol unsafe.Pointer
	r, _, _ := syscall.SyscallN(vt[3], uintptr(dev),
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxInprocServer, 0,
		uintptr(unsafe.Pointer(&vol)))
	if err := hresultErr(r, "IMMDevice.Activate(IAudioEndpointVolume)"); err != nil {
		return nil, err
	}
	return vol, nil
}

// micVolume 一次性取得 IAudioEndpointVolume(内部完成 CoInit/设备枚举)。
// 返回 vol 接口与清理函数。
func micVolume() (unsafe.Pointer, func(), error) {
	cleanup := func() {}
	if coInit() {
		cleanup = func() { procCoUninitialize.Call() }
	}
	dev, err := defaultCaptureEndpoint()
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	vol, err := endpointVolume(dev)
	comRelease(dev)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return vol, func() { comRelease(vol); cleanup() }, nil
}

// MicMuted 查询麦克风当前静音状态。
func MicMuted() (bool, error) {
	vol, done, err := micVolume()
	if err != nil {
		return false, err
	}
	defer done()
	vt, err := vtableOf(vol)
	if err != nil {
		return false, err
	}
	var muted int32
	// IAudioEndpointVolume vtable: 3 RegisterControlChangeNotify … 14 SetMute, 15 GetMute
	r, _, _ := syscall.SyscallN(vt[15], uintptr(vol), uintptr(unsafe.Pointer(&muted)))
	if err := hresultErr(r, "GetMute"); err != nil {
		return false, err
	}
	return muted != 0, nil
}

// SetMicMute 设置麦克风静音(true=静音)。
func SetMicMute(mute bool) error {
	vol, done, err := micVolume()
	if err != nil {
		return err
	}
	defer done()
	vt, err := vtableOf(vol)
	if err != nil {
		return err
	}
	m := uintptr(0)
	if mute {
		m = 1
	}
	r, _, _ := syscall.SyscallN(vt[14], uintptr(vol), m, 0) // SetMute(mute, ctx=NULL)
	return hresultErr(r, "SetMute")
}

// ToggleMicMute 翻转麦克风静音,返回翻转后的状态(true=已静音)。
func ToggleMicMute() (bool, error) {
	cur, err := MicMuted()
	if err != nil {
		return false, err
	}
	if err := SetMicMute(!cur); err != nil {
		return cur, err
	}
	return !cur, nil
}

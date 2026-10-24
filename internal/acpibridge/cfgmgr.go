package acpibridge

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// cfgmgr32 — CM_Get_Device_Interface_List{,Size}W(设备接口枚举)。

var (
	modcfgmgr32       = windows.NewLazySystemDLL("cfgmgr32.dll")
	procGetIfListSize = modcfgmgr32.NewProc("CM_Get_Device_Interface_List_SizeW")
	procGetIfList     = modcfgmgr32.NewProc("CM_Get_Device_Interface_ListW")
)

const crSuccess = 0

func cmGetInterfaceListSize(pulLen *uint32, guid *windows.GUID, flags uint32) error {
	r0, _, _ := procGetIfListSize.Call(
		uintptr(unsafe.Pointer(pulLen)),
		uintptr(unsafe.Pointer(guid)),
		uintptr(unsafe.Pointer(nil)),
		uintptr(flags))
	if r0 != crSuccess {
		return syscall.Errno(r0)
	}
	return nil
}

func cmGetInterfaceList(guid *windows.GUID, buf []uint16, flags uint32) error {
	r0, _, _ := procGetIfList.Call(
		uintptr(unsafe.Pointer(guid)),
		uintptr(unsafe.Pointer(nil)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(flags))
	if r0 != crSuccess {
		return syscall.Errno(r0)
	}
	return nil
}

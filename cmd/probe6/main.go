package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ppi struct {
	Number, MaxMhz, CurrentMhz, MhzLimit, MaxIdle, CurIdle uint32
}

func main() {
	mod := windows.NewLazySystemDLL("powrprof.dll")
	p := mod.NewProc("CallNtPowerInformation")
	buf := make([]ppi, 64)
	r, _, _ := p.Call(11, 0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Sizeof(buf[0]))*uintptr(len(buf)))
	fmt.Printf("NTSTATUS: %#x\n", r)
	n := 0
	for i, b := range buf {
		if b.CurrentMhz == 0 {
			break
		}
		n = i + 1
	}
	var sum int
	for _, b := range buf[:n] {
		sum += int(b.CurrentMhz)
	}
	fmt.Printf("cores=%d avg=%d MHz\n", n, sum/max(n, 1))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

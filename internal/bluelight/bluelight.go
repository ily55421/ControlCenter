// Package bluelight 实现蓝光过滤器(gdi32 SetDeviceGammaRamp,与原版 BlueLightFilter.cs 一致)。
package bluelight

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modgdi32          = windows.NewLazySystemDLL("gdi32.dll")
	moduser32         = windows.NewLazySystemDLL("user32.dll")
	procGetDC         = moduser32.NewProc("GetDC")
	procReleaseDC     = moduser32.NewProc("ReleaseDC")
	procSetGammaRamp  = modgdi32.NewProc("SetDeviceGammaRamp")
)

// SetRamp 对主屏 DC 设置 RGB 斜坡(r/g/b 各 256 项 uint16)。
func SetRamp(r, g, b [256]uint16) bool {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return false
	}
	defer procReleaseDC.Call(0, hdc)

	var ramp [768]uint16
	copy(ramp[0:256], r[:])
	copy(ramp[256:512], g[:])
	copy(ramp[512:768], b[:])
	ret, _, _ := procSetGammaRamp.Call(hdc, uintptr(unsafe.Pointer(&ramp[0])))
	return ret != 0
}

// ResetDeviceGammaRamp 恢复线性斜坡(R=G=B=256*i)。
func ResetDeviceGammaRamp() bool {
	var lin [256]uint16
	for i := 0; i < 256; i++ {
		lin[i] = uint16(256 * i)
	}
	return SetRamp(lin, lin, lin)
}

// SetBlueLightRamp 蓝光过滤:range ∈ [1.0,2.0];R=G=线性,B=256/range*i。越界返回 false。
func SetBlueLightRamp(rang float64) bool {
	if rang < 1.0 || rang > 2.0 {
		return false
	}
	var rg [256]uint16
	var b [256]uint16
	for i := 0; i < 256; i++ {
		rg[i] = uint16(256 * i)
		bv := 256.0/rang*float64(i) + 0.5
		b[i] = uint16(math.Min(bv, 65535))
	}
	return SetRamp(rg, rg, b)
}

// SetGammaRamp 幂律三通道(γ∈[0.23,4.45];与原版 SetDeviceGammaRamp(r,g,b) 一致)。
func SetGammaRamp(gr, gg, gb float64) int {
	if gr < 0.23 || gr > 4.45 {
		return -1
	}
	if gg < 0.23 || gg > 4.45 {
		return -2
	}
	if gb < 0.23 || gb > 4.45 {
		return -3
	}
	var r, g, b [256]uint16
	for i := 0; i < 256; i++ {
		x := float64(i+1) / 256.0
		r[i] = gammaVal(x, gr)
		g[i] = gammaVal(x, gg)
		b[i] = gammaVal(x, gb)
	}
	if !SetRamp(r, g, b) {
		return -4
	}
	return 0
}

func gammaVal(x, gamma float64) uint16 {
	v := math.Pow(x, gamma)*65535.0 + 0.5
	if v < 0 {
		v = 0
	}
	if v > 65535 {
		v = 65535
	}
	return uint16(v)
}

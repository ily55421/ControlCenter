// Package smbios 解析 SMBIOS 表(经 GetSystemFirmwareTable("RSMB")),
// 提供原版 SMBIOS.cs 所用的结构与字段。
package smbios

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// BIOSInformation Type0。
type BIOSInformation struct {
	Vendor, Version string
}

// SystemInformation Type1。
type SystemInformation struct {
	ManufacturerName, ProductName, Version, SerialNumber, Family string
}

// BaseBoardInformation Type2。
type BaseBoardInformation struct {
	ManufacturerName, ProductName, Version, SerialNumber string
}

// ProcessorInformation Type4。
type ProcessorInformation struct {
	ManufacturerName, Version string
	CoreCount, CoreEnabled, ThreadCount int
	ExternalClockMHz int
}

// MemoryDevice Type17。
type MemoryDevice struct {
	DeviceLocator, BankLocator, ManufacturerName, SerialNumber, PartNumber string
	SpeedMTs int
}

// Table 解析结果。
type Table struct {
	Version string
	BIOS    BIOSInformation
	System  SystemInformation
	Board   BaseBoardInformation
	CPU     ProcessorInformation
	Memory  []MemoryDevice
}

// Get 读取并解析 SMBIOS 表;失败返回空表(原版静默吞错)。
func Get() *Table {
	t := &Table{}
	raw := firmwareTableRSMB()
	if len(raw) < 8 {
		return t
	}
	major := int(raw[1])
	minor := int(raw[2])
	t.Version = itoa(major) + "." + itoa(minor)
	// RawSMBiosTables:8 字节头(Used20CallingMethod, SMBMajor, SMBMinor, DmiRevision, Length, ...)
	length := int(binary.LittleEndian.Uint32(raw[4:8]))
	if 8+length > len(raw) {
		length = len(raw) - 8
	}
	parseStructures(raw[8:8+length], t)
	return t
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// parseStructures 遍历 SMBIOS 结构区(与原版算法一致)。
func parseStructures(raw []byte, t *Table) {
	off := 0
	for off+4 < len(raw) {
		typ := int(raw[off])
		hdrLen := int(raw[off+1])
		if hdrLen < 4 || off+hdrLen > len(raw) {
			break
		}
		formatted := raw[off : off+hdrLen]
		// 字符串区
		sOff := off + hdrLen
		if sOff < len(raw) && raw[sOff] == 0 {
			sOff++ // 单 0:无字符串
		}
		var strs []string
		var sb strings.Builder
		for sOff < len(raw) {
			c := raw[sOff]
			sOff++
			if c == 0 {
				if sb.Len() == 0 {
					break // 双 0 结束
				}
				strs = append(strs, sb.String())
				sb.Reset()
				continue
			}
			sb.WriteByte(c)
		}
		apply(typ, formatted, strs, t)
		if typ == 127 {
			break
		}
		off = sOff
	}
}

func str(strs []string, idx int) string {
	if idx == 0 || idx > len(strs) {
		return ""
	}
	return strings.TrimSpace(strs[idx-1])
}

func word(b []byte, o int) int {
	if o+1 >= len(b) {
		return 0
	}
	return int(binary.LittleEndian.Uint16(b[o : o+2]))
}

func byt(b []byte, o int) int {
	if o >= len(b) {
		return 0
	}
	return int(b[o])
}

func apply(typ int, f []byte, strs []string, t *Table) {
	switch typ {
	case 0:
		t.BIOS.Vendor = str(strs, byt(f, 4))
		t.BIOS.Version = str(strs, byt(f, 5))
	case 1:
		t.System.ManufacturerName = str(strs, byt(f, 4))
		t.System.ProductName = str(strs, byt(f, 5))
		t.System.Version = str(strs, byt(f, 6))
		t.System.SerialNumber = str(strs, byt(f, 7))
		t.System.Family = str(strs, byt(f, 26))
	case 2:
		t.Board.ManufacturerName = str(strs, byt(f, 4))
		t.Board.ProductName = str(strs, byt(f, 5))
		t.Board.Version = str(strs, byt(f, 6))
		t.Board.SerialNumber = str(strs, byt(f, 7))
	case 4:
		t.CPU.ManufacturerName = str(strs, byt(f, 7))
		t.CPU.Version = str(strs, byt(f, 16))
		t.CPU.ExternalClockMHz = word(f, 18)
		t.CPU.CoreCount = byt(f, 35)
		t.CPU.CoreEnabled = byt(f, 36)
		t.CPU.ThreadCount = byt(f, 37)
	case 17:
		if len(f) >= 0x17 {
			t.Memory = append(t.Memory, MemoryDevice{
				DeviceLocator:   str(strs, byt(f, 16)),
				BankLocator:     str(strs, byt(f, 17)),
				ManufacturerName: str(strs, byt(f, 23)),
				SerialNumber:    str(strs, byt(f, 24)),
				PartNumber:      str(strs, byt(f, 26)),
				SpeedMTs:        word(f, 21),
			})
		}
	}
}

var (
	modkernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemFirmwareT = modkernel32.NewProc("GetSystemFirmwareTable")
)

func firmwareTableRSMB() []byte {
	const rsmb = 0x52534D42 // 'RSMB' little-endian marker per API docs
	size, _, _ := procGetSystemFirmwareT.Call(
		uintptr(rsmb), 0, 0, 0)
	if size == 0 {
		return nil
	}
	buf := make([]byte, size)
	got, _, _ := procGetSystemFirmwareT.Call(
		uintptr(rsmb), 0, uintptr(unsafe.Pointer(&buf[0])), size)
	if got == 0 {
		return nil
	}
	return buf[:got]
}

var _ = bytes.MinRead

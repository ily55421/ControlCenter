// Package gpu GPU 超频域(复刻 GPUOverclocking.exe:AppSettings page5 偏移存储 + ControlGPU.exe 应用通道)。
package gpu

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/eventbus"
	"controlcenter/internal/wmi"
)

// OffsetPair 一块 GPU 的核心/显存偏移(MHz)。
type OffsetPair struct {
	Core int16
	Mem  int16
}

// EncodeInt16 非对称负数编码(写侧:n<0 时先 -1 再截 16 位)。
func EncodeInt16(n int) []byte {
	if n < 0 {
		n = n - 1
	}
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(int16(n)))
	return b[:]
}

// DecodeInt16 非对称解码(读侧:b1&0x80 → raw-65535)。
func DecodeInt16(b []byte) int {
	raw := int(uint16(b[0]) | uint16(b[1])<<8)
	if b[1]&0x80 == 0x80 {
		return raw - 65535
	}
	return raw
}

// Store page5 偏移存储。
type Store struct {
	as *appsettings.Store
	w  *wmi.Client
}

// New 创建存储。
func New(as *appsettings.Store, w *wmi.Client) *Store { return &Store{as: as, w: w} }

// Default 默认存储。
var Default = New(appsettings.Default, wmi.Default)

// Save 保存偏移((5,16)(5,18)(5,20)(5,22))并触发 ControlGPU.exe 应用。
// controlGPUPath:ControlGPU.exe 完整路径(一般位于 CC 安装目录)。
func (s *Store) Save(controlGPUPath string, g0, g1 OffsetPair, dual bool) error {
	_ = s.as.Write(appsettings.AddrGPUOC, 2, EncodeInt16(int(g0.Core)))
	_ = s.as.Write(appsettings.AddrGPUOC+2, 2, EncodeInt16(int(g0.Mem)))
	if dual {
		_ = s.as.Write(appsettings.AddrGPUOC+4, 2, EncodeInt16(int(g1.Core)))
		_ = s.as.Write(appsettings.AddrGPUOC+6, 2, EncodeInt16(int(g1.Mem)))
	}
	args := fmt.Sprintf("set_increment:0:%d:%d", g0.Core, g0.Mem)
	if err := eventbus.ExecProcess(controlGPUPath, args); err != nil {
		return err
	}
	if dual {
		args1 := fmt.Sprintf("set_increment:1:%d:%d", g1.Core, g1.Mem)
		return eventbus.ExecProcess(controlGPUPath, args1)
	}
	return nil
}

// Current 读当前生效偏移((5,2):默认/基准区)。
func (s *Store) Current() OffsetPair {
	b := s.as.Read(appsettings.AddrGPUOC-14, 4) // 5/2 起 4 字节(core+mem)
	if len(b) < 4 {
		return OffsetPair{}
	}
	return OffsetPair{Core: int16(DecodeInt16(b[0:2])), Mem: int16(DecodeInt16(b[2:4]))}
}

// Saved 读已保存偏移。
func (s *Store) Saved() (g0, g1 OffsetPair) {
	b := s.as.Read(appsettings.AddrGPUOC, 8)
	g0 = OffsetPair{Core: int16(DecodeInt16(b[0:2])), Mem: int16(DecodeInt16(b[2:4]))}
	g1 = OffsetPair{Core: int16(DecodeInt16(b[4:6])), Mem: int16(DecodeInt16(b[6:8]))}
	return
}

// Reset 恢复默认:把 (5,2) 的基准值重新下发并写满 4 字段。
func (s *Store) Reset(controlGPUPath string, dual bool) error {
	cur := s.Current()
	args := fmt.Sprintf("set_increment:0:%d:%d", cur.Core, cur.Mem)
	if err := eventbus.ExecProcess(controlGPUPath, args); err != nil {
		return err
	}
	if dual {
		if err := eventbus.ExecProcess(controlGPUPath,
			fmt.Sprintf("set_increment:1:%d:%d", cur.Core, cur.Mem)); err != nil {
			return err
		}
	}
	base := s.as.Read(appsettings.AddrGPUOC-14, 4)
	data := append(append([]byte{}, base...), base...)
	return s.as.Write(appsettings.AddrGPUOC, 8, data)
}

// LockFrequency 限频(FnKey 模式联动):ControlGPU.exe set_lock_frequency:<freq>。
func LockFrequency(controlGPUPath string, freq int) error {
	return eventbus.ExecProcess(controlGPUPath, fmt.Sprintf("set_lock_frequency:%d", freq))
}

// GPUInfo ControlGPU.exe info 结果缓存((5,6) 7 字节:数量/基频/就绪/驱动版本)。
func (s *Store) Info() (count int, baseClock int, ready bool, driverVer string) {
	b := s.as.Read(appsettings.AddrGPUOC-10, 7)
	count = int(b[0])
	baseClock = int(b[1]) | int(b[2])<<8
	ready = b[3] == 1
	driverVer = strings.TrimRight(string(b[4:7]), "\x00 ")
	return
}

// RequestInfo 触发 ControlGPU.exe info 填充 (5,6)。
func RequestInfo(controlGPUPath string) error {
	return eventbus.ExecProcess(controlGPUPath, "info")
}

// ControlGPUPath 推测 ControlGPU.exe 路径(与 FnKey 同目录约定)。
func ControlGPUPath(baseDir string) string {
	if baseDir == "" {
		return `C:\Program Files (x86)\ControlCenter\DCHU\ControlGPU.exe`
	}
	return filepath.Join(baseDir, "ControlGPU.exe")
}

// FindControlGPU 在 dir 与原版默认安装目录中查找 ControlGPU.exe;找不到返回空串。
func FindControlGPU(dir string) string {
	for _, p := range []string{
		filepath.Join(dir, "ControlGPU.exe"),
		`C:\Program Files (x86)\ControlCenter\DCHU\ControlGPU.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

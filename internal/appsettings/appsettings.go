// Package appsettings 封装 ACPI 桥驱动的 4KB NV 设置区(页/偏移寻址),
// 提供 CleDef.AppSetting 线性地址(= page*256+offset)与类型化访问。
package appsettings

import (
	"encoding/binary"
	"math"
	"sync"

	"controlcenter/internal/acpibridge"
)

// access 是桥驱动设置区的最小子集(便于以假实现单测读改写原子性)。
type access interface {
	ReadAppSettings(page, offset, length int) ([]byte, error)
	WriteAppSettings(page, offset, length int, data []byte) error
}

// Store 在桥之上提供线程安全的类型化设置访问。
// mu 覆盖单次访问,以及"读旧值→改→写回"这类复合操作(UpdateU8/WithPage)。
type Store struct {
	b  access
	mu sync.Mutex
}

// New 创建设置存储。
func New(b *acpibridge.Bridge) *Store { return &Store{b: b} }

// Default 默认存储。
var Default = New(acpibridge.Default())

// Read 读取切片(失败返回全零,与原生行为一致)。
func (s *Store) Read(index, length int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(index, length)
}

func (s *Store) readLocked(index, length int) []byte {
	out, _ := s.b.ReadAppSettings(index/256, index%256, length)
	if len(out) < length {
		o := make([]byte, length)
		copy(o, out)
		return o
	}
	return out
}

// ReadAt 页/偏移版本。
func (s *Store) ReadAt(page, offset, length int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, _ := s.b.ReadAppSettings(page, offset, length)
	if len(out) < length {
		o := make([]byte, length)
		copy(o, out)
		return o
	}
	return out
}

// Write 写入。
func (s *Store) Write(index, length int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.WriteAppSettings(index/256, index%256, length, data)
}

// WriteAt 页/偏移版本。
func (s *Store) WriteAt(page, offset, length int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.WriteAppSettings(page, offset, length, data)
}

// UpdateU8 原子地完成"读 1 字节 → f(旧值) → 写回 1 字节",
// 避免并发切换同一字节内其它位时丢失更新。
func (s *Store) UpdateU8(index int, f func(cur byte) byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.readLocked(index, 1)
	return s.b.WriteAppSettings(index/256, index%256, 1, []byte{f(cur[0])})
}

// WithPage 原子地完成"读整页 → edit 就地修改 → 写回整页"(page 0..15)。
func (s *Store) WithPage(page int, edit func(pg []byte)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pg, err := s.b.ReadAppSettings(page, 0, 256)
	if err != nil {
		return err
	}
	if len(pg) < 256 {
		full := make([]byte, 256)
		copy(full, pg)
		pg = full
	}
	edit(pg)
	return s.b.WriteAppSettings(page, 0, 256, pg)
}

// --- 类型化便捷访问 ---

func (s *Store) ReadU8(index int) int   { return int(s.Read(index, 1)[0]) }
func (s *Store) WriteU8(index int, v byte) error {
	return s.Write(index, 1, []byte{v})
}

func (s *Store) ReadU16(index int) int {
	b := s.Read(index, 2)
	return int(binary.LittleEndian.Uint16(b))
}
func (s *Store) WriteU16(index int, v uint16) error {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	return s.Write(index, 2, b[:])
}

func (s *Store) ReadU32(index int) uint32 {
	b := s.Read(index, 4)
	return binary.LittleEndian.Uint32(b)
}
func (s *Store) WriteU32(index int, v uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return s.Write(index, 4, b[:])
}

func (s *Store) ReadInt(index, length int) int {
	b := s.Read(index, length)
	switch length {
	case 1:
		return int(int8(b[0]))
	case 2:
		return int(int16(binary.LittleEndian.Uint16(b)))
	default:
		return int(int32(binary.LittleEndian.Uint32(b)))
	}
}

func (s *Store) WriteInt(index, length, v int) error {
	switch length {
	case 1:
		return s.Write(index, 1, []byte{byte(v)})
	case 2:
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(int16(v)))
		return s.Write(index, 2, b[:])
	default:
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(int32(v)))
		return s.Write(index, 4, b[:])
	}
}

func (s *Store) ReadFloat(index int) float32 {
	b := s.Read(index, 4)
	return float32FromBits(binary.LittleEndian.Uint32(b))
}

func (s *Store) WriteFloat(index int, v float32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], float32ToBits(v))
	return s.Write(index, 4, b[:])
}

func float32FromBits(b uint32) float32 { return math.Float32frombits(b) }
func float32ToBits(f float32) uint32   { return math.Float32bits(f) }

// --- CleDef.AppSetting 关键线性地址(见 docs/01)---

const (
	AddrClevoID          = 0     // 2B,字节交换
	AddrNVSSID           = 2     // 2B
	AddrFnKeyVersion     = 8     // 4B
	AddrPowerMode        = 257   // 1B 当前电源模式
	AddrUILanguage       = 258   // 1B
	AddrShowOSD          = 259   // 1B
	AddrLanguageAuto     = 260   // 1B
	AddrQuietNVPwrOffDis = 261   // 1B
	AddrCamera           = 262   // 1B
	AddrCC30Launch       = 263   // 1B
	AddrFlexikeyKB       = 264   // 1B
	AddrFlexikeyMouse    = 265   // 1B
	AddrTouchPad         = 266   // 1B
	AddrArg001ByService  = 268   // 1B
	AddrACModePowerMode  = 272   // 1B
	AddrAMDMode          = 273   // 1B AMD Auto/Manual
	AddrAMDFreq          = 274   // 4B
	AddrAMDVolt          = 278   // 4B float
	AddrLowRefreshSup    = 283   // 1B
	AddrLowRefreshOn     = 284   // 1B
	AddrSavedRefreshRate = 285   // 1B
	AddrDTT              = 288   // 1B CPU 动态(DTT)
	AddrPgUpDn           = 289   // 1B 1=禁用
	AddrBlueLight        = 290   // 1B 0..10
	AddrLockPowerModeUI  = 291   // 1B
	AddrFnLock           = 292   // 3B FnLock/WinKey/FnWin(1=on,2=off)
	AddrCallMute         = 297   // 1B
	AddrFn09Key          = 299   // 1B
	AddrCopilotKey       = 300   // 1B
	AddrUIStyle          = 301   // 1B
	AddrFlexiCharger     = 302   // 1B
	AddrLowBatt1         = 303   // 3B Low/Critical/Reserve
	AddrPreviousMode     = 286   // 1B(1,30)
	AddrLowRefreshChk    = 284   // 别名
	AddrKBType           = 516   // 2/4
	AddrOverrideBootEff  = 519   // 2/7
	AddrLedKBLaunch      = 520   // 2/8
	AddrFanPage          = 1024  // page4 起始
	AddrFanMode          = 1029  // 4/5
	AddrFanOffset        = 1031  // 4/7
	AddrTurboFan         = 1032  // 4/8
	AddrNoiselessFan     = 1033  // 4/9
	AddrFanQuietMode     = 1094  // 各模式 FanMode/FanOffset 1094..1101
	AddrAntiDustRun      = 1104  // 4/80
	AddrAntiDustSched    = 1105  // 4/81 起 5B
	AddrGPUOC            = 1296  // 5/16 起 8B(GPU0 core/mem,GPU1 core/mem int16)
	AddrXTUPage          = 1536  // page6
	AddrXTUSaved         = 1568  // 6/32
	AddrXTUBusy          = 1787  // 6/251
	AddrXTUInfoRead      = 1789  // 6/253
	AddrFeatureList      = 1792  // page7 BIOS 特性表镜像
)

// ModeType 电源模式。
type ModeType int

const (
	Quiet         ModeType = 0
	PowerSaving   ModeType = 1
	Performance   ModeType = 2
	Entertainment ModeType = 3
	QuietPlus     ModeType = 4
)

// normalizePowerMode 非法模式值回退 Entertainment(与原版一致)。
func normalizePowerMode(v int) ModeType {
	m := ModeType(v)
	if m < Quiet || m > QuietPlus {
		return Entertainment
	}
	return m
}

// GetPowerMode 读当前电源模式(非法值回退 Entertainment)。
func (s *Store) GetPowerMode() ModeType {
	return normalizePowerMode(s.ReadU8(AddrPowerMode))
}

// SetPowerModeOnly 仅写模式字节(完整切换在 powermode 包)。
func (s *Store) SetPowerModeOnly(m ModeType) error { return s.WriteU8(AddrPowerMode, byte(m)) }

// clevoID 机型 ID 解码(两字节交换后 Int16)。
func clevoID(b []byte) int {
	return int(int16(uint16(b[1]) | uint16(b[0])<<8))
}

// GetClevoID 读机型 ID。
func (s *Store) GetClevoID() int {
	return clevoID(s.Read(AddrClevoID, 2))
}

// GetCustomerID 读定制厂商 ID(WMI17[2],通常由上层缓存)。
func (s *Store) GetUILanguage() int { return s.ReadU8(AddrUILanguage) }

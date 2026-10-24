// Package wmi 封装 Clevo WMI 命令编码(SetWMI 子命令协议、包命令、WMI4 多路复用)
// 以及常用命令号的语义化调用。
package wmi

import (
	"encoding/binary"

	"controlcenter/internal/acpibridge"
	"controlcenter/internal/appsettings"
)

// Client 提供 WMI 命令访问。
type Client struct {
	b  *acpibridge.Bridge
	as *appsettings.Store
}

// New 创建客户端。
func New(b *acpibridge.Bridge, as *appsettings.Store) *Client {
	return &Client{b: b, as: as}
}

// Default 默认客户端。
var Default = New(acpibridge.Default(), appsettings.Default)

// GetInt GetDCHU_Data_Integer 语义:读 WMI 整数(失败返回 0,与原生一致)。
func (c *Client) GetInt(command int) int {
	v, err := c.b.GetInt(command)
	if err != nil {
		return 0
	}
	return v
}

// GetPackage GetDCHU_Data_Buffer 语义:读 256 字节 WMI 包(失败返回全零)。
func (c *Client) GetPackage(command int) []byte {
	v, _ := c.b.GetBuffer(command)
	return v
}

// encodeSetWMI 构造 SetWMI 的 256 字节缓冲:data 低 3 字节 + 子命令在 byte[3]。
func encodeSetWMI(subCommand int, data uint32) []byte {
	buf := make([]byte, 256)
	binary.LittleEndian.PutUint32(buf[0:4], data)
	buf[3] = byte(subCommand)
	return buf
}

// SetWMI 子命令编码下发。
// 对应原生 DCHU.SetWMI(command, SubCommand, data)。
func (c *Client) SetWMI(command, subCommand int, data uint32) error {
	return c.b.SetData(command, encodeSetWMI(subCommand, data))
}

// SetWMIValue 无子命令版:直接下发 4 字节值(256 字节缓冲)。
func (c *Client) SetWMIValue(command int, data uint32) error {
	buf := make([]byte, 256)
	binary.LittleEndian.PutUint32(buf[0:4], data)
	return c.b.SetData(command, buf)
}

// SetWMIValueInt 下发并取回整型返回值(WMI 65 等:读数经 SetDCHU_Data 返回)。
func (c *Client) SetWMIValueInt(command int, data uint32) int {
	buf := make([]byte, 256)
	binary.LittleEndian.PutUint32(buf[0:4], data)
	v, _, err := c.b.SetDataResult(command, buf)
	if err != nil {
		return 0
	}
	return v
}

// SetWMIValue16 部分调用点使用 4 字节缓冲(Fan/Battery 应用风格)。
func (c *Client) SetWMIValue16(command int, data uint32) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], data)
	return c.b.SetData(command, buf)
}

// SetWMIPackage 整包下发(256 字节)。
func (c *Client) SetWMIPackage(command int, pkg []byte) error {
	buf := make([]byte, 256)
	n := len(pkg)
	if n > 256 {
		n = 256
	}
	copy(buf[:n], pkg[:n])
	return c.b.SetData(command, buf)
}

// SetWMIPackageEx 双向包命令(WMI4 多路复用):in[0] 为子命令号。
func (c *Client) SetWMIPackageEx(command int, in []byte) []byte {
	out, err := c.b.SetDataEx(command, in)
	if err != nil {
		return make([]byte, 256)
	}
	return out
}

// RWEC EC RAM 直访(WMI4 子命令 1):in={1,d0..d4,CMD},返回 out[1..5]。
func (c *Client) RWEC(ecCmd byte, data [5]byte) [5]byte {
	in := make([]byte, 256)
	in[0] = 1
	copy(in[1:6], data[:])
	in[6] = ecCmd
	out := c.SetWMIPackageEx(4, in)
	var r [5]byte
	copy(r[:], out[1:6])
	return r
}

// ReadECRAM 读 EC RAM(WMI4 sub1 + EC 命令 177)。
func (c *Client) ReadECRAM(index, length int) []byte {
	in := make([]byte, 256)
	in[0] = 1
	in[1] = byte(index >> 8)
	in[2] = byte(index)
	in[3] = byte(length)
	out := c.SetWMIPackageEx(4, in)
	n := length
	if n > 5 {
		n = 5
	}
	if n < 0 {
		n = 0
	}
	return append([]byte{}, out[1:1+n]...)
}

// WriteECRAM 读改写 EC RAM 2 字节(WMI4 sub1 + EC 命令 178;掩码语义与原生一致)。
func (c *Client) WriteECRAM(index int, data uint16) {
	// 读旧值
	old := c.ReadECRAM(index, 2)
	var oldV uint16
	if len(old) >= 2 {
		oldV = uint16(old[0]) | uint16(old[1])<<8
	}
	newV := oldV | data // 原生:(old|Data)&(~Mask|Data),Mask=0 时即 old|Data
	in := make([]byte, 256)
	in[0] = 1
	in[1] = byte(index >> 8)
	in[2] = byte(index)
	in[3] = 1
	in[4] = byte(newV)
	in[5] = byte(newV >> 8)
	c.SetWMIPackageEx(4, in)
}

// --- 高频语义化命令 ---

// SetFanMode WMI(121,1,mode)。
func (c *Client) SetFanMode(mode int) error { return c.SetWMI(121, 1, uint32(mode)) }

// SetFanOffsetPct WMI(121,14,round(255*off/100))。
func (c *Client) SetFanOffsetPct(off int) error {
	d := uint32(255 * off / 100)
	return c.SetWMI(121, 14, d)
}

// SetPerformanceData WMI(121,25,data):data=2|(turbo<<6)|(dtt<<7)。
func (c *Client) SetPerformanceData(turbo, dtt bool) error {
	d := uint32(2)
	if turbo {
		d |= 0x40
	}
	if dtt {
		d |= 0x80
	}
	return c.SetWMI(121, 25, d)
}

// SetAirplaneLED WMI(121,26,v)。
func (c *Client) SetAirplaneLED(v int) error { return c.SetWMI(121, 26, uint32(v)) }

// SetWOL WMI(121,22,flags)。flags: bit0 allowWakeup bit1 s5wol bit2 faststartup bit3 shutdown。
func (c *Client) SetWOL(flags int) error { return c.SetWMI(121, 22, uint32(flags)) }

// SetBootEffect WMI(121,30,v) 键盘灯开机效果。
func (c *Client) SetBootEffect(v int) error { return c.SetWMI(121, 30, uint32(v)) }

// RestoreFanDefault WMI(121,34,1) 恢复 EC 默认风扇表。
func (c *Client) RestoreFanDefault() error { return c.SetWMI(121, 34, 1) }

// SetACOFF WMI(121,28,v):1=正常供电 0=强制电池放电(电池校准)。
func (c *Client) SetACOFF(v int) error { return c.SetWMI(121, 28, uint32(v)) }

// SetBatteryRefresh WMI(121,29,v)。
func (c *Client) SetBatteryRefresh(v int) error { return c.SetWMI(121, 29, uint32(v)) }

// SetCameraPower WMI(34,v)。
func (c *Client) SetCameraPower(v int) error { return c.SetWMIValue(34, uint32(v)) }

// SetEnergyStar WMI(79,m)。
func (c *Client) SetEnergyStar(m int) error { return c.SetWMIValue(79, uint32(m)) }

// SetWhiteKB WMI(39,level) 白光键盘亮度。
func (c *Client) SetWhiteKB(level int) error { return c.SetWMIValue(39, uint32(level)) }

// GetWhiteKB WMI(61) 白光键盘亮度回读。
func (c *Client) GetWhiteKB() int { return c.GetInt(61) }

// SetRGBKB WMI(103,cmd32) RGB 键盘命令。
func (c *Client) SetRGBKB(cmd32 uint32) error { return c.SetWMIValue(103, cmd32) }

// SetH2RAMIndex WMI(107,index) 后读 115 包。
func (c *Client) GetH2RAM(index int) byte {
	_ = c.SetWMI(107, 0, uint32(index))
	return c.GetPackage(115)[0]
}

// EnableEnergySave WMI(118,0x05000000|v) 节能总开关。
func (c *Client) EnableEnergySave(on bool) error {
	if on {
		return c.SetWMIValue(118, 0x05000001)
	}
	return c.SetWMIValue(118, 0x05000000)
}

// SetChargeThresholds WMI(118,0x06000000|discharge|charge<<8)。
func (c *Client) SetChargeThresholds(chargeLimit, dischargeLimit int) error {
	v := uint32(dischargeLimit&0xFF) | uint32(chargeLimit&0xFF)<<8
	return c.SetWMIValue(118, 0x06000000|v)
}

// SetSystemTimeSync WMI(118,0x01000000|packed) 时间同步:sec+min<<6+hour<<12+dow<<17。
func (c *Client) SetSystemTimeSync(sec, min, hour, dow int) error {
	v := sec | min<<6 | hour<<12 | dow<<17
	return c.SetWMIValue(118, 0x01000000|uint32(v))
}

// SetChargeDays WMI(118,0x02000000|days|discharge<<8) 生效日+放电下限。
func (c *Client) SetChargeDays(days, dischargeLimit int) error {
	v := days&0xFF | dischargeLimit<<8
	return c.SetWMIValue(118, 0x02000000|uint32(v))
}

// SetSchedule WMI(118,0x03000000/0x04000000|(start<<6)|((end+1)<<17))。
func (c *Client) SetSchedule(slot, start, end int, enabled bool) error {
	base := uint32(0x03000000)
	if slot == 2 {
		base = 0x04000000
	}
	if !enabled {
		return c.SetWMIValue(118, base)
	}
	v := uint32(start&0x3F) | uint32(end+1)<<17
	return c.SetWMIValue(118, base|v)
}

// SetBatteryFlexi WMI(118,sub7,v)。
func (c *Client) SetBatteryFlexi(v int) error { return c.SetWMI(118, 7, uint32(v)) }

// SetNotifyEC WMI(94,v)。
func (c *Client) SetNotifyEC(v int) error { return c.SetWMIValue(94, uint32(v)) }

// SetSleepNotify WMI(72) 睡眠通知 EC。
func (c *Client) SetSleepNotify() error { return c.SetWMIValue(72, 0) }

// --- WMI4 多路复用子命令语义封装 ---

// GetFeatureList WMI4 sub8:BIOS 特性表(256B)。
func (c *Client) GetFeatureList() []byte {
	in := make([]byte, 256)
	in[0] = 8
	return c.SetWMIPackageEx(4, in)
}

// QueryGPUSwitch WMI4 sub21:返回 [0]=当前模式(1 iGPU/2 dGPU/3 MSHybrid/4 Dynamic),[1]=支持位。
func (c *Client) QueryGPUSwitch() (cur, support byte) {
	in := make([]byte, 256)
	in[0] = 21
	out := c.SetWMIPackageEx(4, in)
	return out[0], out[1]
}

// SetGPUSwitch WMI4 sub22。
func (c *Client) SetGPUSwitch(mode int) {
	in := make([]byte, 256)
	in[0] = 22
	in[1] = byte(mode)
	c.SetWMIPackageEx(4, in)
}

// QueryKeyboard WMI4 sub24:[0]=FnLock(0 隐藏 1 on 2 off),[1]=WinKey(1 可用 2 禁用),[2]=FnWin。
func (c *Client) QueryKeyboard() (fnLock, winKey, fnWin byte) {
	in := make([]byte, 256)
	in[0] = 24
	out := c.SetWMIPackageEx(4, in)
	return out[0], out[1], out[2]
}

// SetKeyboardSetting WMI4 sub25:{25,fn,win,winFn}(1=on,2=off;win 1=启用 2=禁用)。
func (c *Client) SetKeyboardSetting(fnLock, winKey, fnWin byte) {
	in := make([]byte, 256)
	in[0] = 25
	in[1] = fnLock
	in[2] = winKey
	in[3] = fnWin
	c.SetWMIPackageEx(4, in)
}

// SetPgUpDn WMI4 sub26(新 BIOS):0=启用 1=禁用。
func (c *Client) SetPgUpDn(disabled bool) {
	in := make([]byte, 256)
	in[0] = 26
	if disabled {
		in[1] = 1
	}
	c.SetWMIPackageEx(4, in)
}

// QueryFlexiCharger WMI4 sub30:[0]=使能 [1]=起始% [2]=停止% [16..31]/[32..47]=可选档位表。
func (c *Client) QueryFlexiCharger() (enabled bool, start, stop byte, startTable, stopTable []byte) {
	in := make([]byte, 256)
	in[0] = 30
	out := c.SetWMIPackageEx(4, in)
	return out[0] == 1, out[1], out[2], append([]byte{}, out[16:32]...), append([]byte{}, out[32:48]...)
}

// SetFlexiCharger WMI4 sub31:{31,enable,start,stop}。
func (c *Client) SetFlexiCharger(enabled bool, start, stop byte) {
	in := make([]byte, 256)
	in[0] = 31
	if enabled {
		in[1] = 1
	}
	in[2] = start
	in[3] = stop
	c.SetWMIPackageEx(4, in)
}

// QueryAMDMode WMI4 sub7 读:{7,0} → out[2]=mode out[3]=频率索引 out[4]=电压索引。
func (c *Client) QueryAMDMode() (mode, freqIdx, voltIdx byte) {
	in := make([]byte, 256)
	in[0] = 7
	out := c.SetWMIPackageEx(4, in)
	return out[2], out[3], out[4]
}

// SetAMDMode WMI4 sub7 写:{7,1,mode,freqIdx,voltIdx}。
func (c *Client) SetAMDMode(mode, freqIdx, voltIdx byte) {
	in := make([]byte, 256)
	in[0] = 7
	in[1] = 1
	in[2] = mode
	in[3] = freqIdx
	in[4] = voltIdx
	c.SetWMIPackageEx(4, in)
}

// GetECVersion WMI4 sub222:EC 版本字符串("1.xxx")。
func (c *Client) GetECVersion() string {
	ver := ""
	for sub := byte(0); sub < 4; sub++ {
		in := make([]byte, 256)
		in[0] = 1
		in[6] = 222 // EC 命令在 in[6](RWEC 格式)
		out := c.SetWMIPackageEx(4, in)
		if sub == 0 {
			continue // 首次调用仅初始化
		}
		for i := 1; i < 6 && i < len(out); i++ {
			if out[i] == '$' || out[i] == 0 {
				return ver
			}
			ver += string(rune(out[i]))
		}
	}
	return ver
}

// GetAntiDustStatus WMI4 sub163:运行状态。
func (c *Client) GetAntiDustStatus() byte {
	in := make([]byte, 256)
	in[0] = 163
	out := c.SetWMIPackageEx(4, in)
	return out[1]
}

// RunAntiDust WMI(121,41,1)。
func (c *Client) RunAntiDust() error { return c.SetWMI(121, 41, 1) }

// SetAntiDustScheduler WMI(121,40,data):data=days|resume<<7|hour<<8。
func (c *Client) SetAntiDustScheduler(days int, resume bool, hour int) error {
	d := days | hour<<8
	if resume {
		d |= 1 << 7
	}
	return c.SetWMI(121, 40, uint32(d))
}

// GetTurboFanMaxDuty WMI4 sub11:Turbo 关闭时的满载 duty 基准。
func (c *Client) GetTurboFanMaxDuty() byte {
	in := make([]byte, 256)
	in[0] = 11
	out := c.SetWMIPackageEx(4, in)
	return out[2]
}

// SetSystemTimeToEC WMI(118,1,packed) 防尘时间同步。
func (c *Client) SetSystemTimeToEC(sec, min, hour, dow int) error {
	return c.SetSystemTimeSync(sec, min, hour, dow)
}

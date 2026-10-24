// Package battery 电池域(WMI7 包解析、健康判定、校准控制;复刻 BatteryPackUtility.exe)。
package battery

import (
	"encoding/binary"
	"strings"
	"time"

	"controlcenter/internal/wmi"
)

// Data WMI7 电池数据包。
type Data struct {
	Version            int
	ManufacturerDate   int  // (年-1980)*512 + 月*32 + 日
	CycleCount         int
	FullChargeCapacity int  // mAh
	DesignCapacity     int  // mAh
	BatteryStatus      int  // bit0x10 完全放电 bit0x20 完全充满
	PFStatus           int
	OperationStatus    int  // bit12 永久失效
	StopChargeThresh   int  // %

	RCapacity int // 更换-容量阈值%
	RCycle    int // 更换-循环阈值
	RDate     int // 更换-年限阈值
	R1Capacity int
	R1Cycle   int
	WCapacity int // 警告-容量阈值%
	WCycle    int
	WDate      int
	W1Capacity int
	W1Cycle    int

	ReplaceCondition  string
	WarningCondition  string
}

// Parse 解析 WMI7 包。
func Parse(p []byte) Data {
	var d Data
	g16 := func(o int) int { return int(uint16(p[o]) | uint16(p[o+1])<<8) }
	d.Version = g16(0)
	d.ManufacturerDate = g16(2)
	d.CycleCount = g16(4)
	d.FullChargeCapacity = g16(6)
	d.DesignCapacity = g16(8)
	d.BatteryStatus = g16(10)
	d.PFStatus = int(binary.LittleEndian.Uint32(p[12:16]))
	d.OperationStatus = int(binary.LittleEndian.Uint32(p[16:20]))
	d.StopChargeThresh = int(p[20])
	d.RCapacity = int(p[32])
	d.RCycle = int(uint16(p[33]) | uint16(p[34])<<8)
	d.RDate = int(p[35])
	d.R1Capacity = int(p[36])
	d.R1Cycle = int(uint16(p[37]) | uint16(p[38])<<8)
	d.WCapacity = int(p[39])
	d.WCycle = int(uint16(p[40]) | uint16(p[41])<<8)
	d.WDate = int(p[42])
	d.W1Capacity = int(p[43])
	d.W1Cycle = int(uint16(p[44]) | uint16(p[45])<<8)
	d.ReplaceCondition = cstr(p[48:80])
	d.WarningCondition = cstr(p[80:112])
	return d
}

func cstr(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, '$'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "\x00")
}

// Read 读取电池数据包。
func Read(w *wmi.Client) Data { return Parse(w.GetPackage(7)) }

// Health 健康判定结果。
type Health int

const (
	Healthy Health = iota
	Deteriorated
	NeedReplacement
	PermanentFailure
)

// String 中文描述。
func (h Health) String() string {
	switch h {
	case Deteriorated:
		return "老化"
	case NeedReplacement:
		return "需要更换"
	case PermanentFailure:
		return "永久失效"
	default:
		return "健康"
	}
}

// CapacityPct 满充容量/设计容量百分比(损耗指标)。
func CapacityPct(d Data) int {
	if d.DesignCapacity == 0 {
		return 0
	}
	return d.FullChargeCapacity * 100 / d.DesignCapacity
}

// AgeYears 电池年龄(按打包日期)。
func AgeYears(d Data) float64 {
	now := time.Now()
	packedNow := (now.Year()-1980)*512 + int(now.Month())*32 + now.Day()
	return float64(packedNow-d.ManufacturerDate) / 512.0
}

// Evaluate 健康判定(替换原版 MSScriptControl VBScript 表达式求值:
// 以等价的 Go 实现,变量语义与原版逐条一致)。
func Evaluate(d Data) Health {
	if d.OperationStatus&(1<<12) != 0 {
		return PermanentFailure
	}
	if evalReplace(d) {
		return NeedReplacement
	}
	if evalWarning(d) {
		return Deteriorated
	}
	return Healthy
}

func evalReplace(d Data) bool {
	cap := CapacityPct(d)
	age := AgeYears(d)
	a := b2i(!(cap >= d.RCapacity))
	b := b2i(!(d.RCycle >= d.CycleCount))
	c := b2i(!(age <= float64(d.RDate)))
	g := b2i(!(d.RCapacity >= cap || cap >= d.R1Capacity))
	h := b2i(!(d.R1Cycle >= cap || cap >= d.R1Cycle)) // 原版笔误照抄(与 cap 比较)
	return exprValue(d.ReplaceCondition, map[string]int{"A": a, "B": b, "C": c, "G": g, "H": h})
}

func evalWarning(d Data) bool {
	cap := CapacityPct(d)
	age := AgeYears(d)
	dd := b2i(!(d.R1Capacity >= cap || cap >= d.WCapacity))
	i := b2i(!(d.WCapacity >= cap || cap >= d.W1Capacity))
	e := b2i(!(d.WCycle >= d.CycleCount || d.CycleCount >= d.R1Cycle))
	j := b2i(!(d.W1Cycle >= d.CycleCount || d.CycleCount >= d.WCycle))
	f := b2i(!(float64(d.WDate) <= age || age <= float64(d.RDate)))
	return exprValue(d.WarningCondition, map[string]int{"D": dd, "E": e, "F": f, "I": i, "J": j})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// exprValue 求值 "(A+B+C)+(G*H)$" 类表达式(变量相加/相乘;>0 为真)。
// 原版默认表达式:Replace="(A+B+C)+(G*H)$",Warning="(D+E+F)+(I*J)$"。
func exprValue(expr string, vars map[string]int) bool {
	if expr == "" {
		return false
	}
	expr = strings.TrimSuffix(expr, "$")
	total := 0
	// 按括号组累加;组内按 + 分段,段内按 * 相乘(变量为单字母)
	for _, group := range splitTop(expr) {
		prod := 1
		for _, term := range strings.Split(group, "*") {
			v, ok := vars[strings.ToUpper(term)]
			if !ok {
				return false // 未知变量:与原版一致按"表达式错误→条件不成立"
			}
			prod *= v
		}
		total += prod
	}
	return total > 0
}

func splitTop(expr string) []string {
	// 去掉所有括号后按 + 分段(变量仅单字母,无嵌套语义)
	clean := strings.Map(func(r rune) rune {
		if r == '(' || r == ')' {
			return -1
		}
		return r
	}, expr)
	var out []string
	for _, seg := range strings.Split(clean, "+") {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// --- 校准控制 ---

// RefreshCommand 电池校准命令三元组(ACOFF/Refresh/Flexi)。
func RefreshCommand(w *wmi.Client, acoff, refresh, flexi int) {
	_ = w.SetACOFF(acoff)
	_ = w.SetBatteryRefresh(refresh)
	_ = w.SetBatteryFlexi(flexi)
}

// 校准阶段(OngoingStatus)。
type Ongoing int

const (
	Stop Ongoing = iota
	Discharge1
	Charge1
	Discharge2
	Charge2
	AdjustDischarge
)

// StageCommand 各阶段的 (ACOFF, Refresh, Flexi) 参数表。
func StageCommand(s Ongoing) (acoff, refresh, flexi int) {
	switch s {
	case Discharge1, Discharge2:
		return 0, 1, 0
	case Charge1, Charge2, AdjustDischarge:
		return 1, 1, 0
	default:
		return 1, 0, 1
	}
}

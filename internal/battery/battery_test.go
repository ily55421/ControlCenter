package battery

import (
	"encoding/binary"
	"testing"
)

func TestParseAndEvaluate(t *testing.T) {
	p := make([]byte, 256)
	binary.LittleEndian.PutUint16(p[0:], 1)     // version
	binary.LittleEndian.PutUint16(p[2:], 5000)  // date
	binary.LittleEndian.PutUint16(p[4:], 120)   // cycles
	binary.LittleEndian.PutUint16(p[6:], 4800)  // full charge
	binary.LittleEndian.PutUint16(p[8:], 5000)  // design
	copy(p[48:], "(A+B+C)+(G*H)$")
	copy(p[80:], "(D+E+F)+(I*J)$")

	d := Parse(p)
	if d.CycleCount != 120 || d.DesignCapacity != 5000 || d.FullChargeCapacity != 4800 {
		t.Fatalf("解析错误: %+v", d)
	}
	if CapacityPct(d) != 96 {
		t.Fatalf("容量百分比 = %d, want 96", CapacityPct(d))
	}
	if d.ReplaceCondition != "(A+B+C)+(G*H)" {
		t.Fatalf("表达式 = %q", d.ReplaceCondition)
	}
	// 默认阈值全 0:cap(96)>=RCapacity(0) → a=0;RCycle(0)>=120 为假 → b=1 → Replace 命中
	if got := Evaluate(d); got != NeedReplacement {
		t.Fatalf("健康判定 = %v, want NeedReplacement(阈值全零时)", got)
	}

	// 完全放电/充满位
	binary.LittleEndian.PutUint16(p[10:], 0x10)
	if Parse(p).BatteryStatus&0x10 == 0 {
		t.Fatal("完全放电位解析失败")
	}
}

func TestExprValue(t *testing.T) {
	// (0+0+0)+(0*1) = 0 → 健康
	if exprValue("(A+B+C)+(G*H)$", map[string]int{"A": 0, "B": 0, "C": 0, "G": 0, "H": 1}) {
		t.Fatal("全零表达式不应命中")
	}
	// (1+0+0)+(0*1) = 1 → 命中
	if !exprValue("(A+B+C)+(G*H)$", map[string]int{"A": 1, "B": 0, "C": 0, "G": 0, "H": 1}) {
		t.Fatal("单变量命中失败")
	}
}

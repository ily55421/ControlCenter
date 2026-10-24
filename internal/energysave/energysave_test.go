package energysave

import "testing"

// TestProfileOverlap 验证原版调度重叠判定(e'=end+1;
// (d1<=0||d2<=0) && (d1>=0||d2>=0),其中 d1=s1-e2', d2=e1'-s2)。
func TestProfileOverlap(t *testing.T) {
	base := Profile{ChargeLimit: 100, DischargeLimit: 95, Everyday: true}
	with := func(s1On bool, s1, e1 int, s2On bool, s2, e2 int) Profile {
		p := base
		p.Schedule1On, p.Schedule1Start, p.Schedule1End = s1On, s1, e1
		p.Schedule2On, p.Schedule2Start, p.Schedule2End = s2On, s2, e2
		return p
	}
	// 任一调度未启用 → 不判重叠
	if p := with(false, 22, 6, true, 22, 6); p.Overlap() {
		t.Error("调度一未启用时应无重叠")
	}
	if p := with(true, 1, 2, false, 1, 2); p.Overlap() {
		t.Error("调度二未启用时应无重叠")
	}
	// 完全相同的两个调度 → 重叠
	if p := with(true, 22, 6, true, 22, 6); !p.Overlap() {
		t.Error("相同调度应判重叠")
	}
	// 同日不交叠(1-2 与 10-11)→ 不重叠
	if p := with(true, 1, 2, true, 10, 11); p.Overlap() {
		t.Error("1-2 与 10-11 不应判重叠")
	}
	// 部分交叠(10-14 与 12-16)→ 重叠
	if p := with(true, 10, 14, true, 12, 16); !p.Overlap() {
		t.Error("10-14 与 12-16 应判重叠")
	}
	// 紧邻(10-12 与 13-14,e'=13,d2=0)→ 按原版公式判重叠(需至少间隔 1 小时)
	if p := with(true, 10, 12, true, 13, 14); !p.Overlap() {
		t.Error("紧邻调度按原版公式应判重叠")
	}
}

// TestProfileNoOverlapGapHour 间隔 1 小时(10-12 与 14-15)→ 不重叠。
func TestProfileNoOverlapGapHour(t *testing.T) {
	p := Profile{Schedule1On: true, Schedule1Start: 10, Schedule1End: 12,
		Schedule2On: true, Schedule2Start: 14, Schedule2End: 15}
	if p.Overlap() {
		t.Error("间隔 1 小时的调度不应判重叠")
	}
}

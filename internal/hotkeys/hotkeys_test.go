package hotkeys

import "testing"

// TestBatteryTemp 温度解析:0.1K 定点小端,raw 为 0 或结果越出物理量程都判为无效,
// 避免"没读到"被当成 -273℃ 触发误报。
func TestBatteryTemp(t *testing.T) {
	cases := []struct {
		name string
		in   [5]byte
		want int
		ok   bool
	}{
		{"全零(读失败)", [5]byte{}, 0, false},
		{"30.0℃", [5]byte{0xB8, 0x0B}, 26, true}, // raw=3000 → (3000-2732)/10 = 26
		{"6.0℃", [5]byte{0xF0, 0x0A}, 6, true},   // raw=2800
		{"0℃边界附近", [5]byte{0xAC, 0x0A}, 0, true}, // raw=2732
		{"满量程噪声", [5]byte{0xFF, 0xFF}, 0, false},
		{"过低(低于 -30℃)", [5]byte{0x0C, 0x09}, 0, false}, // raw=2316 → -41.6℃
		{"过高(高于 120℃)", [5]byte{0x64, 0x19}, 0, false}, // raw=6500 → 376℃
	}
	for _, c := range cases {
		got, ok := batteryTemp(c.in)
		if ok != c.ok {
			t.Errorf("%s: 有效性 = %v, 期望 %v", c.name, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("%s: 温度 = %d, 期望 %d", c.name, got, c.want)
		}
	}
}

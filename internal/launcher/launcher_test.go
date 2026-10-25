package launcher

import "testing"

// TestNormalizePage 页名归一化:面板启动器/托盘/旧版协议传入的页名都要能落到合法页。
func TestNormalizePage(t *testing.T) {
	cases := map[string]string{
		"":         "system",
		"#":        "system",
		"  ":       "system",
		"Fan":      "fan",
		" KB ":     "kb",
		"#GPUOC":   "gpuoc",
		"Charger":  "charger",
		"Energy":   "energy",
		"FlexiKey": "flexikey",
	}
	for in, want := range cases {
		if got := NormalizePage(in); got != want {
			t.Errorf("NormalizePage(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

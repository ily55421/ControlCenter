package win32

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestGetActivePowerScheme 真实 API 冒烟:成功返回非空 GUID,且内部完成 LocalFree。
// 无可枚举电源计划的环境跳过。
func TestGetActivePowerScheme(t *testing.T) {
	g, err := GetActivePowerScheme()
	if err != nil {
		t.Skipf("PowerGetActiveScheme 不可用: %v", err)
	}
	if g == (windows.GUID{}) {
		t.Errorf("GetActivePowerScheme 返回空 GUID")
	}
}

// TestCPULoadPercent 真实采样:init 已建立基线,故首次调用即应落在 0..100。
func TestCPULoadPercent(t *testing.T) {
	if p := CPULoadPercent(); p < 0 || p > 100 {
		t.Fatalf("CPULoadPercent 越界: %d", p)
	}
}

// TestCPUFrequencyMHz 真实采样:应读到大于 0 的平均频率,否则实时频率区会显示 0 MHz。
func TestCPUFrequencyMHz(t *testing.T) {
	mhz := CPUFrequencyMHz()
	if mhz < 0 {
		t.Skip("CallNtPowerInformation(ProcessorInformation) 不可用")
	}
	if mhz == 0 {
		t.Errorf("CPUFrequencyMHz 返回 0")
	}
}

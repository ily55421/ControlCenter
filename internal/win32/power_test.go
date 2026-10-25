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

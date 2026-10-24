package powermode

import (
	"testing"

	"controlcenter/internal/appsettings"
)

// TestNextModeFor Fn 键模式循环链(全模式支持时:Quiet→Performance→Entertainment→PowerSaving→Quiet)。
func TestNextModeFor(t *testing.T) {
	e := &Engine{Sup: SupportBits{
		Quiet: true, PowerSaving: true, Performance: true, Entertainment: true,
	}}
	cases := []struct{ cur, want appsettings.ModeType }{
		{appsettings.Quiet, appsettings.Performance},
		{appsettings.Performance, appsettings.Entertainment},
		{appsettings.Entertainment, appsettings.PowerSaving},
		{appsettings.PowerSaving, appsettings.Quiet},
		{appsettings.QuietPlus, appsettings.Performance},
	}
	for _, c := range cases {
		if got := e.NextModeFor(c.cur); got != c.want {
			t.Errorf("NextModeFor(%d) = %d, 期望 %d", c.cur, got, c.want)
		}
	}
}

// TestNextModeForQuietPlusOnly QuietPlusOnly 机型:Entertainment→QuietPlus。
func TestNextModeForQuietPlusOnly(t *testing.T) {
	e := &Engine{Sup: SupportBits{
		PowerSaving: true, Performance: true, Entertainment: true, QuietPlusOnly: true,
	}}
	if got := e.NextModeFor(appsettings.Entertainment); got != appsettings.QuietPlus {
		t.Errorf("Entertainment 下一模式 = %d, 期望 QuietPlus(4)", got)
	}
}

// TestFmtSet ControlGPU.exe set_increment 参数格式(含负偏移)。
func TestFmtSet(t *testing.T) {
	if got := fmtSet(0, 100, -200); got != "set_increment:0:100:-200" {
		t.Errorf("fmtSet = %q", got)
	}
	if got := fmtSet(1, 0, 0); got != "set_increment:1:0:0" {
		t.Errorf("fmtSet = %q", got)
	}
}

// TestItoa 内置整数转字符串(含 0 与负数)。
func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 5: "5", -5: "-5", 1600: "1600", -200: "-200"}
	for n, want := range cases {
		if got := itoa(n); got != want {
			t.Errorf("itoa(%d) = %q, 期望 %q", n, got, want)
		}
	}
}

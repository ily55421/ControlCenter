package battery

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempHome 把用户目录指向临时目录,避免测试写到真实的 ~/.controlcenter。
func useTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestChargerModeOf(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		start   byte
		stop    byte
		want    int
	}{
		{"EC 复位后=最大电池电量", false, 95, 100, ChargerMaxBattery},
		{"禁用但阈值残留", false, 70, 80, ChargerMaxBattery},
		{"推荐档", true, 70, 80, ChargerRecommended},
		{"自定义 50/60", true, 50, 60, ChargerCustom},
		{"自定义 95/100", true, 95, 100, ChargerCustom},
	}
	for _, c := range cases {
		if got := ChargerModeOf(c.enabled, c.start, c.stop); got != c.want {
			t.Errorf("%s: ChargerModeOf = %d, 期望 %d", c.name, got, c.want)
		}
	}
}

func TestChargerPrefRoundTrip(t *testing.T) {
	home := useTempHome(t)
	want := ChargerPref{Mode: ChargerCustom, Start: 55, Stop: 65}
	if err := SaveChargerPref(want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, `.controlcenter`, "flexicharger.json")); err != nil {
		t.Fatalf("记忆文件未落在用户目录: %v", err)
	}
	got, ok := LoadChargerPref()
	if !ok {
		t.Fatal("刚保存的记忆读不回来")
	}
	if got != want {
		t.Fatalf("读回 = %+v, 期望 %+v", got, want)
	}
}

func TestLoadChargerPrefRejectsUnusable(t *testing.T) {
	useTempHome(t)
	if _, ok := LoadChargerPref(); ok {
		t.Fatal("无文件时应返回 ok=false")
	}
	for _, body := range []string{"not json", `{"mode":7,"start":70,"stop":80}`, `{"mode":-1}`} {
		if err := SaveChargerPref(ChargerPref{}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ChargerPrefPath(), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := LoadChargerPref(); ok {
			t.Errorf("非法记忆被接受: %s", body)
		}
	}
}

// TestRestoreChargerNoMemory 从未保存过选择时不得下发任何 EC 写(此处传 nil 客户端,
// 一旦走到硬件调用就会 panic,即证明它确实提前返回)。
func TestRestoreChargerNoMemory(t *testing.T) {
	useTempHome(t)
	applied, err := RestoreCharger(nil)
	if err != nil || applied {
		t.Fatalf("无记忆时 applied=%v err=%v", applied, err)
	}
}

// TestRestoreChargerRemembersOnlyWhenEnabled 记忆为"最大电池电量"时无需回写(EC 冷启动就是这个状态)。
func TestRestoreChargerRemembersOnlyWhenEnabled(t *testing.T) {
	useTempHome(t)
	if err := SaveChargerPref(ChargerPref{Mode: ChargerMaxBattery, Start: 95, Stop: 100}); err != nil {
		t.Fatal(err)
	}
	applied, err := RestoreCharger(nil)
	if err != nil || applied {
		t.Fatalf("mode=0 不应回写: applied=%v err=%v", applied, err)
	}
}

// TestRestoreChargerRejectsBadThresholds 阈值非法时必须报错跳过,而不是把垃圾值写进 EC。
func TestRestoreChargerRejectsBadThresholds(t *testing.T) {
	useTempHome(t)
	for _, p := range []ChargerPref{
		{Mode: ChargerCustom, Start: 0, Stop: 0},
		{Mode: ChargerCustom, Start: 80, Stop: 80},
		{Mode: ChargerCustom, Start: 90, Stop: 60},
		{Mode: ChargerCustom, Start: 50, Stop: 120},
	} {
		if err := SaveChargerPref(p); err != nil {
			t.Fatal(err)
		}
		applied, err := RestoreCharger(nil)
		if err == nil {
			t.Errorf("%+v: 非法阈值未拦截(applied=%v)", p, applied)
		}
	}
}

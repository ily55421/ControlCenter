package flexikey

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadMissingCreatesDefault 文件不存在时创建默认 Profile0(选中、键鼠均启用)。
func TestLoadMissingCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(filepath.Join(dir, "profile.xml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Profiles) != 1 {
		t.Fatalf("Profiles 数 = %d, 期望 1", len(s.Profiles))
	}
	p := s.Profiles[0]
	if p.Name != "Profile0" || !p.IsSelected || !p.IsKbEnable || !p.IsMouseEnable {
		t.Errorf("默认 Profile 不符: %+v", p)
	}
	if sel := s.Selected(); sel == nil || sel.Name != "Profile0" {
		t.Error("Selected() 应返回 Profile0")
	}
}

// TestSaveLoadRoundtrip 保存→重载后配置档与宏序列一致(profile.xml 兼容格式)。
func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.xml")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := s.Profiles[0]
	p.Keys = append(p.Keys, &KeyInfo{
		Device: DeviceKeyboard, KeyCode: 41, Action: ActionKeypress,
		Macro: []MacroEvent{
			{KeyCode: 29, MakeBreak: 0, Delay: 10, Display: "LCtrl"},
			{KeyCode: 56, MakeBreak: 0, Delay: 20, Display: "LAlt"},
			{KeyCode: 15, MakeBreak: 1, Delay: 30, Display: "Tab"},
		},
	})
	p.Keys = append(p.Keys, &KeyInfo{Device: DeviceKeyboard, KeyCode: 58, Action: ActionNone})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("profile.xml 未生成: %v", err)
	}

	s2, err := Load(path)
	if err != nil {
		t.Fatalf("二次 Load: %v", err)
	}
	if len(s2.Profiles) != 1 {
		t.Fatalf("重载 Profiles 数 = %d", len(s2.Profiles))
	}
	p2 := s2.Profiles[0]
	if p2.Name != "Profile0" || !p2.IsSelected || !p2.IsKbEnable || !p2.IsMouseEnable {
		t.Errorf("重载 Profile 属性不符: %+v", p2)
	}
	// ActionNone 槽位不落盘(与原版一致),仅保留 1 个 Set
	if len(p2.Keys) != 1 {
		t.Fatalf("重载槽位数 = %d, 期望 1(None 不落盘)", len(p2.Keys))
	}
	k := p2.Keys[0]
	if k.Device != DeviceKeyboard || k.KeyCode != 41 || k.Action != ActionKeypress {
		t.Errorf("槽位属性不符: %+v", k)
	}
	if len(k.Macro) != 3 {
		t.Fatalf("宏事件数 = %d, 期望 3", len(k.Macro))
	}
	if k.Macro[0].Display != "LCtrl" || k.Macro[0].MakeBreak != 0 {
		t.Errorf("宏事件 0 不符: %+v", k.Macro[0])
	}
	if k.Macro[2].MakeBreak != 1 {
		t.Errorf("宏事件 2 MakeBreak = %d, 期望 1(KeyBreak)", k.Macro[2].MakeBreak)
	}
}

// TestParseMakeBreak MakeBreak 文本→数值映射。
func TestParseMakeBreak(t *testing.T) {
	if v := parseMakeBreak("KeyBreak"); v != 1 {
		t.Errorf("parseMakeBreak(KeyBreak) = %d", v)
	}
	if v := parseMakeBreak("KeyMake"); v != 0 {
		t.Errorf("parseMakeBreak(KeyMake) = %d", v)
	}
	if v := parseMakeBreak(""); v != 0 {
		t.Errorf("parseMakeBreak(\"\") = %d", v)
	}
}

// TestSetupMacroKeyEx 宏编码:首键 Status=1、末键 Status=0、中间键带 MakeBreak、上限 32。
func TestSetupMacroKeyEx(t *testing.T) {
	if out := SetupMacroKeyEx(nil, false); out != nil {
		t.Errorf("空序列应返回 nil, 得到 %v", out)
	}
	events := []MacroEvent{
		{KeyCode: 29}, {KeyCode: 56}, {KeyCode: 15, MakeBreak: 1},
	}
	out := SetupMacroKeyEx(events, false)
	if len(out) != 3 { // 首键 + 中间键(1) + 末尾释放
		t.Fatalf("输出长度 = %d, 期望 3", len(out))
	}
	if out[0].Key != 29 || out[0].Status != 1 {
		t.Errorf("首键 = %+v, 期望 Key=29 Status=1", out[0])
	}
	if out[len(out)-1].Status != 0 {
		t.Errorf("末项 Status = %d, 期望 0(强制释放)", out[len(out)-1].Status)
	}
	// 超 32 个事件截断
	many := make([]MacroEvent, 40)
	for i := range many {
		many[i] = MacroEvent{KeyCode: i + 1}
	}
	if out := SetupMacroKeyEx(many, false); len(out) > 33 {
		t.Errorf("40 事件输出长度 = %d, 期望 ≤33(32 上限+释放)", len(out))
	}
}

// TestRenumberAppEvents launch 动作 AppEventID 从 256 起跨 Profile 连续编号。
func TestRenumberAppEvents(t *testing.T) {
	s := &Store{Profiles: []*Profile{
		{Name: "A", Keys: []*KeyInfo{
			{Action: ActionLaunch}, {Action: ActionKeypress}, {Action: ActionLaunch},
		}},
		{Name: "B", Keys: []*KeyInfo{{Action: ActionLaunch}}},
	}}
	s.RenumberAppEvents()
	if got := s.Profiles[0].Keys[0].AppEventID; got != 256 {
		t.Errorf("A[0].AppEventID = %d, 期望 256", got)
	}
	if got := s.Profiles[0].Keys[2].AppEventID; got != 257 {
		t.Errorf("A[2].AppEventID = %d, 期望 257", got)
	}
	if got := s.Profiles[1].Keys[0].AppEventID; got != 258 {
		t.Errorf("B[0].AppEventID = %d, 期望 258", got)
	}
}

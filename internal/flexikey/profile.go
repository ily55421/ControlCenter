// flexikey — 配置数据模型与 profile.xml 持久化(与原 CC.exe XML 格式兼容)。
package flexikey

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ActionType 动作类型。
type ActionType int

const (
	ActionNone     ActionType = 0
	ActionKeypress ActionType = 1
	ActionLaunch   ActionType = 2
	ActionText     ActionType = 3
	ActionDisable  ActionType = 4
)

// DeviceType 设备。
type DeviceType int

const (
	DeviceNone     DeviceType = 0
	DeviceKeyboard DeviceType = 1
	DeviceMouse    DeviceType = 2
)

// MacroEvent 宏序列事件。
type MacroEvent struct {
	KeyCode    int    `xml:"KeyCode,attr"`
	KeysGroup  int    `xml:"KeysGroup,attr,omitempty"`
	MakeBreak  int    `xml:"KeyMakeBreak,attr,omitempty"` // 0=按下 1=释放
	Delay      int    `xml:"KeyDelay,attr,omitempty"`
	Display    string `xml:"KeyDisplay,attr,omitempty"`
}

// KeyInfo 一个槽位。
type KeyInfo struct {
	Device       DeviceType
	KeyCode      int
	Name         string
	Action       ActionType
	Tip          string
	AppPath      string
	AppEventID   int
	Text         string
	StartKeys    []MacroEvent
	SendKeys     []MacroEvent
	Macro        []MacroEvent
	KeypressDelay bool
}

// Profile 一个配置档。
type Profile struct {
	Index        int
	Name         string
	IsKbEnable   bool
	IsMouseEnable bool
	IsSelected   bool
	Keys         []*KeyInfo
}

// Store 配置存储(profile.xml)。
type Store struct {
	path     string
	Profiles []*Profile
}

// DefaultPath C:\ComboKey\profile.xml(与原版一致)。
func DefaultPath() string { return filepath.Join(`C:\ComboKey`, "profile.xml") }

// Load 读取配置;文件不存在时创建首个 Profile。
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		p := &Profile{Index: 0, Name: "Profile0", IsKbEnable: true, IsMouseEnable: true, IsSelected: true}
		s.Profiles = append(s.Profiles, p)
		return s, nil
	}
	var doc xmlRoot
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("flexikey: profile.xml 解析失败: %w", err)
	}
	for _, xp := range doc.Profiles {
		p := &Profile{
			Index:         xp.Index,
			Name:          xp.Name,
			IsKbEnable:    xp.IsKbEnable == 1,
			IsMouseEnable: xp.IsMouseEnable == 1,
			IsSelected:    xp.IsSelected == 1,
		}
		for _, xs := range xp.Sets {
			k := &KeyInfo{
				Device:        DeviceType(xs.Device),
				KeyCode:       xs.KeyCode,
				Action:        ActionType(xs.Action),
				Tip:           xs.Tip,
				AppPath:       xs.AppPath,
				Text:          xs.Text,
				KeypressDelay: xs.IsKeypressDelay == 1,
			}
			for _, ev := range xs.Macro {
				k.Macro = append(k.Macro, MacroEvent{KeyCode: ev.KeyCode, KeysGroup: ev.KeysGroup,
					MakeBreak: parseMakeBreak(ev.MakeBreakText), Delay: ev.Delay, Display: ev.Display})
			}
			for _, ev := range xs.StartKeys {
				k.StartKeys = append(k.StartKeys, MacroEvent{KeyCode: ev.KeyCode, KeysGroup: ev.KeysGroup, MakeBreak: parseMakeBreak(ev.MakeBreakText), Delay: ev.Delay, Display: ev.Display})
			}
			for _, ev := range xs.SendKeys {
				k.SendKeys = append(k.SendKeys, MacroEvent{KeyCode: ev.KeyCode, KeysGroup: ev.KeysGroup, MakeBreak: parseMakeBreak(ev.MakeBreakText), Delay: ev.Delay, Display: ev.Display})
			}
			p.Keys = append(p.Keys, k)
		}
		s.Profiles = append(s.Profiles, p)
	}
	if len(s.Profiles) == 0 {
		p := &Profile{Index: 0, Name: "Profile0", IsKbEnable: true, IsMouseEnable: true, IsSelected: true}
		s.Profiles = append(s.Profiles, p)
	}
	return s, nil
}

// Save 保存配置。
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	var doc xmlRoot
	active := 0
	for i, p := range s.Profiles {
		if p.IsSelected {
			active = i
		}
		xp := xmlProfile{
			Index: p.Index, Name: p.Name,
			IsKbEnable: b2i(p.IsKbEnable), IsMouseEnable: b2i(p.IsMouseEnable),
			IsSelected: b2i(p.IsSelected),
		}
		for _, k := range p.Keys {
			if k.Action == ActionNone {
				continue // 原版:none 槽位不落盘
			}
			xs := xmlSet{
				Device: int(k.Device), KeyCode: k.KeyCode, Action: int(k.Action),
				Tip: k.Tip, AppPath: k.AppPath, AppEventID: k.AppEventID,
				Text: k.Text, IsKeypressDelay: b2i(k.KeypressDelay),
			}
			for i, ev := range k.Macro {
				xs.Macro = append(xs.Macro, xmlEvent{
					Index: i, KeyCode: ev.KeyCode, KeysGroup: ev.KeysGroup,
					MakeBreakText: makeBreakText(ev.MakeBreak), Delay: ev.Delay, Display: ev.Display,
				})
			}
			for i, ev := range k.StartKeys {
				xs.StartKeys = append(xs.StartKeys, xmlEvent{Index: i, KeyCode: ev.KeyCode,
					MakeBreakText: makeBreakText(ev.MakeBreak), Delay: ev.Delay, Display: ev.Display})
			}
			for i, ev := range k.SendKeys {
				xs.SendKeys = append(xs.SendKeys, xmlEvent{Index: i, KeyCode: ev.KeyCode,
					MakeBreakText: makeBreakText(ev.MakeBreak), Delay: ev.Delay, Display: ev.Display})
			}
			xp.Sets = append(xp.Sets, xs)
		}
		doc.Profiles = append(doc.Profiles, xp)
	}
	doc.Active = active
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	header := []byte(xml.Header + "\r\n")
	return os.WriteFile(s.path, append(header, out...), 0o644)
}

func makeBreakText(mb int) string {
	if mb == 0 {
		return "KeyMake"
	}
	return "KeyBreak"
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RenumberAppEvents 全局重编号 launch 动作 AppEventID(从 256 起,跨 Profile 唯一)。
func (s *Store) RenumberAppEvents() {
	next := 256
	for _, p := range s.Profiles {
		for _, k := range p.Keys {
			if k.Action == ActionLaunch {
				k.AppEventID = next
				next++
			}
		}
	}
}

// Selected 当前选中 Profile。
func (s *Store) Selected() *Profile {
	for _, p := range s.Profiles {
		if p.IsSelected {
			return p
		}
	}
	if len(s.Profiles) > 0 {
		s.Profiles[0].IsSelected = true
		return s.Profiles[0]
	}
	return nil
}

// SortedProfileNames 返回名称列表。
func (s *Store) SortedProfileNames() []string {
	var names []string
	for _, p := range s.Profiles {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// XML 结构。
type xmlRoot struct {
	XMLName  xml.Name     `xml:"root"`
	Active   int          `xml:"Active,attr"`
	Profiles []xmlProfile `xml:"Profile"`
}

type xmlProfile struct {
	XMLName       xml.Name `xml:"Profile_0"`
	Index         int      `xml:"-"`
	Name          string   `xml:"Name,attr"`
	IsSelected    int      `xml:"IsSelected,attr"`
	IsKbEnable    int      `xml:"IsKbEnable,attr"`
	IsMouseEnable int      `xml:"IsMouseEnable,attr"`
	Sets          []xmlSet `xml:",any"`
}

type xmlSet struct {
	XMLName        xml.Name    `xml:"Set"`
	Device         int         `xml:"Device,attr"`
	KeyCode        int         `xml:"KeyCode,attr"`
	Action         int         `xml:"Action,attr"`
	IsKeypressDelay int        `xml:"IsKeypressDelay,attr"`
	Tip            string      `xml:"Tip,attr"`
	AppPath        string      `xml:"AppPath,attr"`
	AppEventID     int         `xml:"AppEventID,attr,omitempty"`
	Text           string      `xml:"TextContent,attr,omitempty"`
	Macro          []xmlEvent  `xml:",any"`
	StartKeys      []xmlEvent
	SendKeys       []xmlEvent
}

type xmlEvent struct {
	XMLName       xml.Name `xml:"Index"`
	Index         int      `xml:",attr"`
	KeyCode       int      `xml:"KeyCode,attr"`
	KeysGroup     int      `xml:"KeysGroup,attr"`
	MakeBreakText string   `xml:"KeyMakeBreak,attr"`
	Delay         int      `xml:"KeyDelay,attr"`
	Display       string   `xml:"KeyDisplay,attr"`
}

// SetupMacroKeyEx 宏序列 → ClevoKeyEx[255](原 SetupMacroKeyEx 编码规则)。
func SetupMacroKeyEx(events []MacroEvent, timeRecord bool) []ClevoKeyEx {
	n := len(events)
	if n > 32 {
		n = 32
	}
	if n == 0 {
		return nil
	}
	out := make([]ClevoKeyEx, 0, n+1)
	first := ClevoKeyEx{Key: int32(events[0].KeyCode), Status: 1}
	if n > 1 {
		if timeRecord {
			first.DelayTime = int32(events[1].Delay)
		}
	}
	out = append(out, first)
	for i := 1; i < n-1; i++ {
		if events[i].KeyCode == 0 {
			continue
		}
		e := ClevoKeyEx{Key: int32(events[i].KeyCode), Status: int32(events[i].MakeBreak)}
		if timeRecord && i+1 < n {
			e.DelayTime = int32(events[i+1].Delay)
		}
		out = append(out, e)
	}
	out = append(out, ClevoKeyEx{Status: 0}) // 末尾强制释放
	return out
}

// SetupKeys ClevoKB 序列(text 宏 Start/Send 键;仅保留按下事件)。
func SetupKeys(events []MacroEvent, timeRecord bool) []ClevoKB {
	var out []ClevoKB
	for i, ev := range events {
		if ev.MakeBreak != 0 {
			continue
		}
		kb := ClevoKB{VKKey: int32(ev.KeyCode)}
		if timeRecord {
			if i+1 < len(events) {
				kb.DownDelay = int32(events[i+1].Delay)
			}
			if i+2 < len(events) {
				kb.UpDelay = int32(events[i+2].Delay)
			}
		}
		out = append(out, kb)
	}
	if out == nil {
		out = []ClevoKB{}
	}
	return out
}

func parseMakeBreak(s string) int {
	if s == "KeyBreak" {
		return 1
	}
	return 0
}

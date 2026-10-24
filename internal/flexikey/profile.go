// flexikey — 配置数据模型与 profile.xml 持久化。
// XML 结构与原版 CC.exe(XmlRW.Save2Xml/LoadXml)逐元素兼容:
// 动态元素名 Profile_<i> / Set_<n> / Index_<n>,文本宏用 StartKey/Text/SendKey 子元素。
package flexikey

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	Device        DeviceType
	KeyCode       int
	Name          string // 原版 UiName
	Display       string // 原版 Set 级 KeyDisplay
	Action        ActionType
	Tip           string
	AppPath       string
	AppEventID    int
	Text          string
	StartKeys     []MacroEvent
	SendKeys      []MacroEvent
	Macro         []MacroEvent
	KeypressDelay bool
}

// Profile 一个配置档。
type Profile struct {
	Index         int
	Name          string
	IsKbEnable    bool
	IsMouseEnable bool
	IsSelected    bool
	Keys          []*KeyInfo
}

// Store 配置存储(profile.xml)。
type Store struct {
	path     string
	Profiles []*Profile
}

// DefaultPath C:\ComboKey\profile.xml(与原版一致)。
func DefaultPath() string { return filepath.Join(`C:\ComboKey`, "profile.xml") }

func defaultProfile() *Profile {
	return &Profile{Index: 0, Name: "Profile0", IsKbEnable: true, IsMouseEnable: true, IsSelected: true}
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// eventFromAttrs 从 XML 属性集解析一个宏事件。
func eventFromAttrs(attrs []xml.Attr) MacroEvent {
	var ev MacroEvent
	for _, a := range attrs {
		switch a.Name.Local {
		case "KeyCode":
			ev.KeyCode = atoiSafe(a.Value)
		case "KeysGroup":
			ev.KeysGroup = atoiSafe(a.Value)
		case "KeyMakeBreak":
			ev.MakeBreak = parseMakeBreak(a.Value)
		case "KeyDelay":
			ev.Delay = atoiSafe(a.Value)
		case "KeyDisplay":
			ev.Display = a.Value
		}
	}
	return ev
}

// Load 读取配置(原版 XmlRW.LoadXml 兼容);文件不存在时创建首个 Profile。
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		s.Profiles = append(s.Profiles, defaultProfile())
		return s, nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur *Profile
	var curKey *KeyInfo
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("flexikey: profile.xml 解析失败: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case strings.HasPrefix(name, "Profile_"):
				p := &Profile{Index: atoiSafe(strings.TrimPrefix(name, "Profile_"))}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "Name":
						p.Name = a.Value
					case "IsSelected":
						p.IsSelected = atoiSafe(a.Value) == 1
					case "IsKbEnable":
						p.IsKbEnable = atoiSafe(a.Value) == 1
					case "IsMouseEnable":
						p.IsMouseEnable = atoiSafe(a.Value) == 1
					}
				}
				s.Profiles = append(s.Profiles, p)
				cur, curKey = p, nil
			case strings.HasPrefix(name, "Set_") && cur != nil:
				k := &KeyInfo{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "Device":
						k.Device = DeviceType(atoiSafe(a.Value))
					case "KeyCode":
						k.KeyCode = atoiSafe(a.Value)
					case "UiName":
						k.Name = a.Value
					case "Action":
						k.Action = ActionType(atoiSafe(a.Value))
					case "IsKeypressDelay":
						k.KeypressDelay = atoiSafe(a.Value) == 1
					case "Tip":
						k.Tip = a.Value
					case "KeyDisplay":
						k.Display = a.Value
					}
				}
				cur.Keys = append(cur.Keys, k)
				curKey = k
			case strings.HasPrefix(name, "Index_") && curKey != nil:
				ev := eventFromAttrs(t.Attr)
				var appPath string
				var appEvent int
				hasApp := false
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "AppPath":
						appPath, hasApp = a.Value, true
					case "AppEventId":
						appEvent, hasApp = atoiSafe(a.Value), true
					}
				}
				if hasApp || curKey.Action == ActionLaunch {
					curKey.AppPath = appPath
					curKey.AppEventID = appEvent
				} else {
					curKey.Macro = append(curKey.Macro, ev)
				}
			case name == "StartKey" && curKey != nil:
				curKey.StartKeys = append(curKey.StartKeys, eventFromAttrs(t.Attr))
			case name == "SendKey" && curKey != nil:
				curKey.SendKeys = append(curKey.SendKeys, eventFromAttrs(t.Attr))
			case name == "Text" && curKey != nil:
				for _, a := range t.Attr {
					if a.Name.Local == "TextContent" {
						curKey.Text = a.Value
					}
				}
			}
		case xml.EndElement:
			name := t.Name.Local
			switch {
			case strings.HasPrefix(name, "Set_"):
				curKey = nil
			case strings.HasPrefix(name, "Profile_"):
				cur, curKey = nil, nil
			}
		}
	}
	if len(s.Profiles) == 0 {
		s.Profiles = append(s.Profiles, defaultProfile())
	}
	return s, nil
}

// xmlAttrEsc 属性值转义(& < > " ')。
func xmlAttrEsc(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// writeEventEl 输出一个宏事件元素(name 形如 Index_0 / StartKey / SendKey)。
func writeEventEl(b *bytes.Buffer, indent, name string, ev MacroEvent, withGroup bool) {
	if withGroup {
		fmt.Fprintf(b, "%s<%s KeyCode=\"%d\" KeysGroup=\"%d\" KeyMakeBreak=\"%s\" KeyDelay=\"%d\" KeyDisplay=\"%s\"/>\r\n",
			indent, name, ev.KeyCode, ev.KeysGroup, makeBreakText(ev.MakeBreak), ev.Delay, xmlAttrEsc(ev.Display))
		return
	}
	fmt.Fprintf(b, "%s<%s KeyCode=\"%d\" KeyMakeBreak=\"%s\" KeyDelay=\"%d\" KeyDisplay=\"%s\"/>\r\n",
		indent, name, ev.KeyCode, makeBreakText(ev.MakeBreak), ev.Delay, xmlAttrEsc(ev.Display))
}

// defaultTextKeys 原版行为:文本宏 Start/Send 序列为空时补一对 Make/Break 空事件。
func defaultTextKeys() []MacroEvent {
	return []MacroEvent{{MakeBreak: 0}, {MakeBreak: 1}}
}

// Save 保存配置(与原版 Save2Xml 元素/属性布局一致;None 槽位不落盘但参与编号)。
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	active := 0
	for i, p := range s.Profiles {
		if p.IsSelected {
			active = i
		}
	}
	var b bytes.Buffer
	b.WriteString(xml.Header)
	fmt.Fprintf(&b, "<root Active=\"%d\">\r\n", active)
	for i, p := range s.Profiles {
		fmt.Fprintf(&b, "  <Profile_%d Name=\"%s\" IsSelected=\"%d\" CurrentDevice=\"0\" IsKbEnable=\"%d\" IsMouseEnable=\"%d\" Icon=\"\">\r\n",
			i, xmlAttrEsc(p.Name), b2i(p.IsSelected), b2i(p.IsKbEnable), b2i(p.IsMouseEnable))
		for n, k := range p.Keys {
			if k.Action == ActionNone {
				continue // 原版:none 槽位不落盘(Set 编号仍占位)
			}
			fmt.Fprintf(&b, "    <Set_%d Device=\"%d\" KeyCode=\"%d\" UiName=\"%s\" KeyDisplay=\"%s\" Action=\"%d\" IsKeypressDelay=\"%d\" Tip=\"%s\" CheckBoxName=\"\">\r\n",
				n+1, int(k.Device), k.KeyCode, xmlAttrEsc(k.Name), xmlAttrEsc(k.Display),
				int(k.Action), b2i(k.KeypressDelay), xmlAttrEsc(k.Tip))
			switch k.Action {
			case ActionKeypress:
				for j, ev := range k.Macro {
					writeEventEl(&b, "      ", fmt.Sprintf("Index_%d", j), ev, true)
				}
			case ActionLaunch:
				fmt.Fprintf(&b, "      <Index_0 AppPath=\"%s\" AppEventId=\"%d\"/>\r\n",
					xmlAttrEsc(k.AppPath), k.AppEventID)
			case ActionText:
				start, send := k.StartKeys, k.SendKeys
				if len(start) == 0 {
					start = defaultTextKeys()
				}
				if len(send) == 0 {
					send = defaultTextKeys()
				}
				for _, ev := range start {
					writeEventEl(&b, "      ", "StartKey", ev, false)
				}
				fmt.Fprintf(&b, "      <Text TextContent=\"%s\"/>\r\n", xmlAttrEsc(k.Text))
				for _, ev := range send {
					writeEventEl(&b, "      ", "SendKey", ev, false)
				}
			}
			fmt.Fprintf(&b, "    </Set_%d>\r\n", n+1)
		}
		fmt.Fprintf(&b, "  </Profile_%d>\r\n", i)
	}
	b.WriteString("</root>\r\n")
	return os.WriteFile(s.path, b.Bytes(), 0o644)
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

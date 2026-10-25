// flexicharger.go — FlexiCharger 选择的持久化与开机回写。
//
// EC 的充电控制寄存器是易失的:冷启动时 BIOS 把 sub30 的 status 复位为 0,
// 而阈值也只存在 EC 里。原版 CC40 只把模式号记在 AppSettings 302 且从不回写,
// 所以重启后单选必然回到"最大电池电量"。这里把模式号与自定义阈值另存到用户目录,
// 由 ccapp / ccfnkey 启动时重新下发 sub31,使选择跨重启生效。
package battery

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"controlcenter/internal/wmi"
)

// FlexiCharger 模式(与原版写入 AppSettings 302 的取值一致:0/1/2)。
const (
	ChargerMaxBattery  = 0 // 最大电池电量:持续充到 100%
	ChargerRecommended = 1 // 推荐电池充电:70% 起充、80% 停止
	ChargerCustom      = 2 // 自定义阈值
)

// RecommendedStart/Stop 推荐档的固定阈值。
const (
	RecommendedStart = 70
	RecommendedStop  = 80
)

// ChargerPref 用户记忆(存 ~/.controlcenter\flexicharger.json,不占用与原版共享的 4KB 设置区)。
type ChargerPref struct {
	Mode  int `json:"mode"`
	Start int `json:"start"`
	Stop  int `json:"stop"`
}

// ChargerPrefPath 记忆文件路径。
func ChargerPrefPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return home + `\.controlcenter\flexicharger.json`
}

// LoadChargerPref 读取记忆;第二个返回值为 false 表示从未保存过。
func LoadChargerPref() (ChargerPref, bool) {
	b, err := os.ReadFile(ChargerPrefPath())
	if err != nil {
		return ChargerPref{}, false
	}
	var p ChargerPref
	if err := json.Unmarshal(b, &p); err != nil {
		return ChargerPref{}, false
	}
	if p.Mode < ChargerMaxBattery || p.Mode > ChargerCustom {
		return ChargerPref{}, false
	}
	return p, true
}

// SaveChargerPref 保存记忆。
func SaveChargerPref(p ChargerPref) error {
	path := ChargerPrefPath()
	if i := strings.LastIndexByte(path, '\\'); i > 0 {
		if err := os.MkdirAll(path[:i], 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ChargerModeOf 由 EC 现状推断模式(无记忆时的回退,与原版 Load_Preference 的判据一致)。
func ChargerModeOf(enabled bool, start, stop byte) int {
	switch {
	case !enabled:
		return ChargerMaxBattery
	case start == RecommendedStart && stop == RecommendedStop:
		return ChargerRecommended
	default:
		return ChargerCustom
	}
}

// RestoreCharger 开机回写:记忆为"启用"且 EC 现状与记忆不符时重新下发 sub31。
// EC 已与记忆一致则不写(两个入口先后启动时保持幂等)。返回本次是否下发了回写。
func RestoreCharger(w *wmi.Client) (applied bool, err error) {
	p, ok := LoadChargerPref()
	if !ok || p.Mode == ChargerMaxBattery {
		return false, nil
	}
	start, stop := p.Start, p.Stop
	if p.Mode == ChargerRecommended {
		start, stop = RecommendedStart, RecommendedStop
	}
	if start <= 0 || stop <= 0 || start >= stop || stop > 100 {
		return false, fmt.Errorf("battery: 记忆阈值非法(start=%d stop=%d),跳过回写", start, stop)
	}
	en, curStart, curStop, _, _ := w.QueryFlexiCharger()
	if en && int(curStart) == start && int(curStop) == stop {
		return false, nil
	}
	w.SetFlexiCharger(true, byte(start), byte(stop))
	return true, nil
}

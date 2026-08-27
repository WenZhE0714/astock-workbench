package strategy

import "strings"

// EntryModeDescriptor is the shared vocabulary for the controlled technical
// entry setups used by historical research, realtime evidence and the Web UI.
// Keeping this catalog at the provider-neutral strategy boundary prevents the
// individual surfaces from drifting into different names or meanings.
type EntryModeDescriptor struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Family      string `json:"family"`
	Thesis      string `json:"thesis"`
	VolumeStyle string `json:"volume_style"`
}

const (
	EntryModeBreakout   = "breakout"
	EntryModeReclaim    = "trend-reclaim"
	EntryModePullback   = "ma-pullback"
	EntryModeMomentum   = "momentum-continuation"
	EntryModeMeanRevert = "mean-reversion"
	EntryModeVolSqueeze = "volatility-squeeze"
	EntryModeAdaptive   = "adaptive-ensemble"
)

var entryModeCatalog = []EntryModeDescriptor{
	{ID: EntryModeBreakout, Label: "放量突破", Family: "trend", Thesis: "突破前期结构高点并确认量能扩张", VolumeStyle: "放量，拒绝极端爆量"},
	{ID: EntryModeReclaim, Label: "趋势收复", Family: "trend", Thesis: "回到快均线之上并确认趋势修复", VolumeStyle: "温和放量即可"},
	{ID: EntryModePullback, Label: "均线回踩", Family: "trend", Thesis: "回踩快均线后收回，寻找趋势承接", VolumeStyle: "缩量或温和放量"},
	{ID: EntryModeMomentum, Label: "动量延续", Family: "momentum", Thesis: "中短期相对强度延续但过滤过热", VolumeStyle: "放量但不超过高潮区间"},
	{ID: EntryModeMeanRevert, Label: "均值回归反弹", Family: "mean-reversion", Thesis: "上升结构内从布林下轨和超卖区反弹", VolumeStyle: "控制在正常放量区间"},
	{ID: EntryModeVolSqueeze, Label: "波动收缩突破", Family: "volatility", Thesis: "波动压缩后突破短周期结构并确认量能", VolumeStyle: "需要放量，过滤量能尖峰"},
	{ID: EntryModeAdaptive, Label: "自适应多形态", Family: "ensemble", Thesis: "按当前趋势、动量和波动状态择优使用已验证入场形态", VolumeStyle: "沿用被选形态的量能区间"},
}

func EntryModeDescriptors() []EntryModeDescriptor {
	return append([]EntryModeDescriptor(nil), entryModeCatalog...)
}

func EntryModes() []string {
	result := make([]string, 0, len(entryModeCatalog))
	for _, descriptor := range entryModeCatalog {
		result = append(result, descriptor.ID)
	}
	return result
}

func ValidEntryMode(mode string) bool {
	for _, candidate := range entryModeCatalog {
		if mode == candidate.ID {
			return true
		}
	}
	return false
}

func EntryModeLabel(mode string) string {
	for _, descriptor := range entryModeCatalog {
		if descriptor.ID == mode {
			return descriptor.Label
		}
	}
	return mode
}

func EntryModeFamily(mode string) string {
	for _, descriptor := range entryModeCatalog {
		if descriptor.ID == mode {
			return descriptor.Family
		}
	}
	return "unknown"
}

func EntryModeLabels() []string {
	labels := make([]string, 0, len(entryModeCatalog))
	for _, descriptor := range entryModeCatalog {
		labels = append(labels, descriptor.Label)
	}
	return labels
}

func EntryModeCatalogText() string {
	return strings.Join(EntryModeLabels(), "、")
}

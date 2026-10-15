package web

import (
	"path/filepath"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func NewShadowTrialSet(now time.Time) paper.ShadowTrialSet {
	balanced := paper.DefaultConfig()
	balanced.UnlimitedOpenPositions = true
	balanced.TradingStartAt = now
	monster := monsterShadowExecutionProfile(nil).Config
	monster.TradingStartAt = now
	noAdd, bounded := balanced, balanced
	noAdd.DisableSignalAdditions = true
	bounded.MaxEntryExtensionPercent = 5
	monsterNoAdd, intraday := monster, monster
	monsterNoAdd.DisableSignalAdditions = true
	intraday.IntradayEntriesOnly = true
	return paper.ShadowTrialSet{EngineVersion: paper.ShadowEngineVersion, Version: 1, CreatedAt: now, Trials: []paper.ShadowTrial{
		{ID: "trial-balanced", Name: "均衡对照", Policy: "原规则同起点对照", Config: balanced},
		{ID: "trial-balanced-no-add", Name: "均衡不加仓", BaselineID: "trial-balanced", Policy: "禁止普通信号加仓", Config: noAdd},
		{ID: "trial-balanced-bounded", Name: "均衡延伸上限", BaselineID: "trial-balanced", Policy: "入场价高于参考价不超过5%", Config: bounded},
		{ID: "trial-monster", Name: "抓妖对照", Policy: "原规则同起点对照", Config: monster},
		{ID: "trial-monster-no-add", Name: "抓妖不加仓", BaselineID: "trial-monster", Policy: "禁止普通信号加仓", Config: monsterNoAdd},
		{ID: "trial-monster-intraday", Name: "抓妖盘中入场", BaselineID: "trial-monster", Policy: "仅盘中事件入场", Config: intraday},
	}}
}

func WithShadowTrials(set paper.ShadowTrialSet, root string) ServerOption {
	return func(s *Server) {
		if s.shadowProfiles == nil {
			s.shadowProfiles = map[string]shadowExecutionProfile{}
		}
		for _, trial := range set.Trials {
			s.shadowProfiles[trial.ID] = shadowExecutionProfile{
				ID: trial.ID, Name: trial.Name, Strategy: trial.Policy,
				Description: "规则已冻结；收益尚待未来交易样本验证", Config: trial.Config,
				Archive:    storage.NewShadowStore(filepath.Join(root, "accounts", trial.ID+".json")),
				BaselineID: trial.BaselineID, TrialHash: set.Hash,
			}
		}
	}
}

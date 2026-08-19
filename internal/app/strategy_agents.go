package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type strategyAgentWireParameters struct {
	EntryMode      string  `json:"entry_mode"`
	FastMA         int     `json:"fast_ma"`
	SlowMA         int     `json:"slow_ma"`
	BreakoutDays   int     `json:"breakout_days"`
	VolumeRatioMin float64 `json:"volume_ratio_min"`
	StopLoss       float64 `json:"stop_loss"`
	TakeProfit     float64 `json:"take_profit"`
	MaxHoldingDays int     `json:"max_holding_days"`
}

type strategyAgentWireProposal struct {
	Strategy   string                      `json:"strategy"`
	Parameters strategyAgentWireParameters `json:"parameters"`
	Hypothesis string                      `json:"hypothesis"`
}

type strategyAgentWireBatch struct {
	SchemaVersion int                         `json:"schema_version"`
	Candidates    []strategyAgentWireProposal `json:"candidates"`
}

type strategyAgentRole struct {
	ID      string
	Mission string
}

var strategyAgentRoles = []strategyAgentRole{
	{ID: "robust-risk", Mission: "优先降低回撤、交易成本和参数敏感性，寻找跨市场状态更稳健的技术参数"},
	{ID: "trend-capture", Mission: "研究趋势捕获和突破/重新站回均线，关注趋势延续但避免追求单次最高收益"},
	{ID: "stability-audit", Mission: "研究基线参数邻域和不同入场模式，优先提出参数平台而不是孤立最优点"},
}

const strategyAgentSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["schema_version","candidates"],
  "properties":{
    "schema_version":{"const":1},
    "candidates":{
      "type":"array","minItems":1,"maxItems":6,
      "items":{
        "type":"object","additionalProperties":false,
        "required":["strategy","parameters","hypothesis"],
        "properties":{
          "strategy":{"const":"technical-breakout"},
          "hypothesis":{"type":"string","minLength":1,"maxLength":240},
          "parameters":{
            "type":"object","additionalProperties":false,
            "required":["entry_mode","fast_ma","slow_ma","breakout_days","volume_ratio_min","stop_loss","take_profit","max_holding_days"],
            "properties":{
              "entry_mode":{"enum":["breakout","trend-reclaim","ma-pullback"]},
              "fast_ma":{"type":"integer","minimum":2,"maximum":60},
              "slow_ma":{"type":"integer","minimum":3,"maximum":250},
              "breakout_days":{"type":"integer","minimum":2,"maximum":120},
              "volume_ratio_min":{"type":"number","minimum":0.5,"maximum":5},
              "stop_loss":{"type":"number","minimum":0.01,"maximum":0.5},
              "take_profit":{"type":"number","minimum":0.02,"maximum":2},
              "max_holding_days":{"type":"integer","minimum":1,"maximum":252}
            }
          }
        }
      }
    }
  }
}`

func strategyAgentPrompt(role strategyAgentRole, request backtest.Request, priorLessons string) string {
	base := request.Technical
	if strings.TrimSpace(priorLessons) == "" {
		priorLessons = "暂无已完成且已退出最终留出期的历史实验。"
	}
	return fmt.Sprintf(`你是受主Agent监督的A股量化研究子Agent，角色：%s。
你的任务：%s。

只允许提出 technical-breakout 策略族的参数候选，不能运行命令、读取文件、搜索网络、调用交易接口，不能写收益、评分、交易结果或晋级结论。不能修改股票池、日期、初始资金、费用、滑点、复权、基准、最大仓位或无未来数据约束。
只能输出符合JSON Schema的对象，最多6个候选。候选必须是可检验假设，hypothesis不超过240字。不同入场模式含义：breakout=放量突破前高；trend-reclaim=收盘重新站回快均线；ma-pullback=日内回踩快均线后收回。
当前基线：entry_mode=%s fast_ma=%d slow_ma=%d breakout_days=%d volume_ratio_min=%.2f stop_loss=%.3f take_profit=%.3f max_holding_days=%d。
历史反思（只来自已经结束、不会与本轮最终留出重叠的实验）：%s
返回的每个候选都必须给出完整参数，不得省略字段。`, role.ID, role.Mission, base.EffectiveEntryMode(), base.FastMA, base.SlowMA, base.BreakoutDays, base.VolumeRatioMin, base.StopLoss, base.TakeProfit, base.MaxHoldingDays, priorLessons)
}

func wireToParameters(wire strategyAgentWireParameters, maxPosition float64) backtest.TechnicalParameters {
	return backtest.TechnicalParameters{
		EntryMode: wire.EntryMode, FastMA: wire.FastMA, SlowMA: wire.SlowMA, BreakoutDays: wire.BreakoutDays,
		VolumeRatioMin: wire.VolumeRatioMin, StopLoss: wire.StopLoss, TakeProfit: wire.TakeProfit,
		MaxHoldingDays: wire.MaxHoldingDays, MaxPosition: maxPosition,
	}
}

func fallbackStrategyProposals(base backtest.TechnicalParameters) []backtest.StrategyProposal {
	variants := []struct {
		id   string
		mode string
		fast int
		vol  float64
		hyp  string
	}{
		{"baseline", base.EffectiveEntryMode(), base.FastMA, base.VolumeRatioMin, "基线参数，用于比较其他候选的增量价值"},
		{"reclaim", backtest.EntryModeReclaim, base.FastMA, base.VolumeRatioMin, "重新站回快均线，减少单纯追逐前高的假突破"},
		{"pullback", backtest.EntryModePullback, base.FastMA, base.VolumeRatioMin, "回踩快均线后收回，观察趋势中的低风险承接"},
		{"slow-trend", backtest.EntryModeBreakout, 30, 1.0, "更慢均线和较低量比，测试趋势延续的稳健性"},
		{"defensive", backtest.EntryModeBreakout, 20, 1.5, "提高放量门槛，减少震荡期的低质量信号"},
	}
	result := make([]backtest.StrategyProposal, 0, len(variants))
	seen := make(map[string]bool, len(variants))
	for _, variant := range variants {
		parameters := base
		parameters.EntryMode = variant.mode
		parameters.FastMA = variant.fast
		parameters.VolumeRatioMin = variant.vol
		key := canonicalParameters(parameters)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, backtest.StrategyProposal{ID: variant.id, Agent: "deterministic-baseline", Thesis: variant.hyp, Parameters: parameters})
	}
	return result
}

func canonicalParameters(parameters backtest.TechnicalParameters) string {
	data, _ := json.Marshal(struct {
		Mode     string  `json:"mode"`
		Fast     int     `json:"fast"`
		Slow     int     `json:"slow"`
		Breakout int     `json:"breakout"`
		Volume   float64 `json:"volume"`
		Stop     float64 `json:"stop"`
		Take     float64 `json:"take"`
		Holding  int     `json:"holding"`
	}{parameters.EffectiveEntryMode(), parameters.FastMA, parameters.SlowMA, parameters.BreakoutDays, parameters.VolumeRatioMin, parameters.StopLoss, parameters.TakeProfit, parameters.MaxHoldingDays})
	return string(data)
}

func (app *App) collectStrategyAgentProposals(ctx context.Context, request backtest.Request, priorLessons string, useAI bool) ([]backtest.AgentResearchRun, []backtest.StrategyProposal) {
	runs := make([]backtest.AgentResearchRun, len(strategyAgentRoles))
	proposals := make([][]backtest.StrategyProposal, len(strategyAgentRoles))
	var wait sync.WaitGroup
	for index, role := range strategyAgentRoles {
		index, role := index, role
		runs[index] = backtest.AgentResearchRun{Agent: role.ID, Status: "pending"}
		if !useAI {
			runs[index].Status = "disabled"
			continue
		}
		structured, ok := app.marketReportAI.(analysis.StructuredSynthesizer)
		if !ok {
			runs[index].Status = "unavailable"
			runs[index].Error = "当前综合器不支持结构化候选输出"
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			runs[index].Status = "running"
			var batch strategyAgentWireBatch
			callContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
			isolated := structured
			temporary, tempError := os.MkdirTemp("", "astock-strategy-agent-*")
			if tempError != nil {
				cancel()
				runs[index].Status = "failed"
				runs[index].Error = tempError.Error()
				return
			}
			defer os.RemoveAll(temporary)
			if _, isRunner := structured.(*analysis.CodexRunner); isRunner {
				isolated = analysis.NewCodexRunner(temporary)
			}
			err := isolated.SynthesizeJSON(callContext, strategyAgentPrompt(role, request, priorLessons), []byte(strategyAgentSchema), &batch)
			cancel()
			runs[index].Status = "failed"
			runs[index].Error = ""
			if err != nil {
				runs[index].Error = err.Error()
				runs[index].Status = "failed"
				return
			}
			if batch.SchemaVersion != 1 || len(batch.Candidates) == 0 {
				runs[index].Error = "候选批次为空或版本不兼容"
				return
			}
			for proposalIndex, candidate := range batch.Candidates {
				if candidate.Strategy != "technical-breakout" {
					continue
				}
				parameters := wireToParameters(candidate.Parameters, request.Technical.MaxPosition)
				if backtest.ValidateTechnicalParameters(parameters) != nil {
					continue
				}
				proposals[index] = append(proposals[index], backtest.StrategyProposal{
					ID: fmt.Sprintf("%s-%02d", role.ID, proposalIndex+1), Agent: role.ID,
					Thesis: strings.TrimSpace(candidate.Hypothesis), Parameters: parameters,
				})
			}
			runs[index].Proposals = append([]backtest.StrategyProposal(nil), proposals[index]...)
			if len(runs[index].Proposals) > 0 {
				runs[index].Status = "ok"
			} else {
				runs[index].Error = "没有有效的技术策略候选"
			}
		}()
	}
	wait.Wait()
	all := fallbackStrategyProposals(request.Technical)
	seen := make(map[string]bool)
	for _, proposal := range all {
		seen[canonicalParameters(proposal.Parameters)] = true
	}
	for index := range proposals {
		for _, proposal := range proposals[index] {
			key := canonicalParameters(proposal.Parameters)
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, proposal)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		left := canonicalParameters(all[i].Parameters)
		right := canonicalParameters(all[j].Parameters)
		return left < right
	})
	for index := range all {
		all[index].ID = fmt.Sprintf("P%03d", index+1)
	}
	return runs, all
}

func (app *App) reviewContinuousOptimization(ctx context.Context, result backtest.ContinuousOptimizationResult) (string, error) {
	if app.marketReportAI == nil {
		return "", nil
	}
	data := struct {
		ID           string                               `json:"id"`
		Stage        string                               `json:"stage"`
		Manifest     backtest.ExperimentManifest          `json:"manifest"`
		Quality      backtest.DataQualitySummary          `json:"quality"`
		PriorLessons string                               `json:"prior_lessons,omitempty"`
		Agents       []backtest.AgentResearchRun          `json:"agents"`
		Candidates   []backtest.ContinuousCandidateResult `json:"candidates"`
		Selected     *backtest.ContinuousCandidateResult  `json:"selected,omitempty"`
		Holdout      any                                  `json:"holdout,omitempty"`
		Stress       any                                  `json:"stress,omitempty"`
		Gate         []string                             `json:"gate_reasons"`
	}{result.ID, result.Stage, result.Manifest, result.Quality, result.PriorLessons, result.Agents, result.Candidates, result.Selected, metricsFact(result.Holdout), stressFact(result.Stress), result.GateReasons}
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	prompt := `你是多Agent量化研究主Agent的只读监督员。只审计下面已经由Go确定性引擎生成的不可变事实，不得修改候选、评分、门禁或阶段，不得读取文件、搜索网络或给出确定性买卖指令。
请用中文Markdown输出：1.候选分歧；2.参数稳定性；3.滚动窗口风险；4.建议继续观察还是重新实验；5.风险声明。最终阶段以JSON事实中的stage为准，不得自行晋级。不可变事实：` + string(bytes)
	text, err := app.marketReportAI.Synthesize(ctx, prompt)
	return text, err
}

func metricsFact(result *backtest.Result) any {
	if result == nil {
		return nil
	}
	return struct {
		Metrics      backtest.Metrics                 `json:"metrics"`
		DataSources  map[string]string                `json:"data_sources"`
		DataCoverage map[string]backtest.DataCoverage `json:"data_coverage"`
		Warnings     []string                         `json:"warnings,omitempty"`
	}{result.Metrics, result.DataSources, result.DataCoverage, result.Warnings}
}

func stressFact(stress backtest.StressResult) any {
	if stress.DoubleCost == nil {
		return nil
	}
	return struct {
		DoubleCost           any     `json:"double_cost"`
		BestTradeProfitShare float64 `json:"best_trade_profit_share"`
	}{metricsFact(stress.DoubleCost), stress.BestTradeProfitShare}
}

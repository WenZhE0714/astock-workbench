package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

const candidateObservationTimeout = 3 * time.Minute

type strategyCandidateAction struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

type strategyCandidateSummary struct {
	ExperimentID       string                        `json:"experiment_id"`
	Cycle              int                           `json:"cycle"`
	GeneratedAt        time.Time                     `json:"generated_at"`
	DataCutoff         string                        `json:"data_cutoff"`
	ResearchStage      string                        `json:"research_stage"`
	SelectedID         string                        `json:"selected_id,omitempty"`
	Tickers            []string                      `json:"tickers"`
	Names              map[string]string             `json:"names,omitempty"`
	Parameters         *backtest.TechnicalParameters `json:"parameters,omitempty"`
	Quality            backtest.DataQualitySummary   `json:"quality"`
	GateReasons        []string                      `json:"gate_reasons,omitempty"`
	PositiveFoldRatio  float64                       `json:"positive_fold_ratio,omitempty"`
	ValidationTrades   int                           `json:"validation_trades,omitempty"`
	AverageValidation  float64                       `json:"average_validation_return_percent,omitempty"`
	WorstDrawdown      float64                       `json:"worst_validation_drawdown_percent,omitempty"`
	ConsensusAgents    int                           `json:"consensus_agents,omitempty"`
	NeighborhoodSize   int                           `json:"neighborhood_size,omitempty"`
	HoldoutMetrics     *backtest.Metrics             `json:"holdout_metrics,omitempty"`
	StressMetrics      *backtest.Metrics             `json:"stress_metrics,omitempty"`
	Lifecycle          backtest.CandidateLifecycle   `json:"lifecycle"`
	ObservationMetrics *backtest.Metrics             `json:"observation_metrics,omitempty"`
	RefreshDue         bool                          `json:"refresh_due"`
	NextAction         string                        `json:"next_action"`
}

type strategyCandidateListResponse struct {
	Items []strategyCandidateSummary `json:"items"`
}

func (s *Server) handleStrategyCandidates(writer http.ResponseWriter, request *http.Request) {
	if s.candidateEngine == nil || s.candidateArchive == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "策略候选生命周期服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.writeStrategyCandidates(writer, request)
	case http.MethodPost:
		s.updateStrategyCandidate(writer, request)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "策略候选只支持 GET、POST"})
	}
}

func (s *Server) writeStrategyCandidates(writer http.ResponseWriter, request *http.Request) {
	limit := 20
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "候选数量必须在 1 到 50 之间"})
			return
		}
		limit = parsed
	}
	results, err := s.candidateArchive.All()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	items := make([]strategyCandidateSummary, 0, min(limit, len(results)))
	for _, result := range results {
		if len(items) >= limit {
			break
		}
		item, itemError := s.strategyCandidateSummary(result)
		if itemError != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: itemError.Error()})
			return
		}
		items = append(items, item)
	}
	writeJSON(writer, http.StatusOK, strategyCandidateListResponse{Items: items})
}

func (s *Server) updateStrategyCandidate(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimSpace(request.URL.Query().Get("id"))
	if id == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少策略候选实验 ID"})
		return
	}
	var input strategyCandidateAction
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "候选操作格式无效: " + err.Error()})
		return
	}
	s.candidateMu.Lock()
	defer s.candidateMu.Unlock()

	result, err := s.candidateArchive.Load(id)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: err.Error()})
		return
	}
	lifecycle, err := s.candidateArchive.LoadLifecycle(id)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	now := s.currentTime().In(strategyLocation)
	if lifecycle.ExperimentID != "" && !backtest.CandidateLifecycleMatches(result, lifecycle) {
		writeJSON(writer, http.StatusConflict, errorResponse{Error: "候选指纹与生命周期记录不一致，请保留归档并重新研究"})
		return
	}
	switch strings.TrimSpace(input.Action) {
	case "start-observation":
		if lifecycle.ExperimentID != "" {
			writeJSON(writer, http.StatusConflict, errorResponse{Error: "该候选已经进入生命周期管理"})
			return
		}
		lifecycle, err = backtest.NewCandidateLifecycle(result, now, candidateObservationStart(now))
	case "refresh-observation":
		if lifecycle.ExperimentID == "" || (lifecycle.Status != backtest.CandidateLifecycleObserving && lifecycle.Status != backtest.CandidateLifecycleApprovalReady) {
			err = fmt.Errorf("当前候选不处于前向观察阶段")
			break
		}
		ctx, cancel := context.WithTimeout(request.Context(), candidateObservationTimeout)
		lifecycle, err = s.refreshCandidateObservation(ctx, result, lifecycle, now)
		cancel()
	case "approve-baseline":
		err = backtest.ApproveCandidateLifecycle(&lifecycle, now, input.Note)
	case "reject-candidate":
		if lifecycle.ExperimentID == "" {
			lifecycle, err = backtest.NewAwaitingCandidateLifecycle(result)
		}
		if err == nil {
			err = backtest.RejectCandidateLifecycle(&lifecycle, now, input.Note)
		}
	case "revoke-approval":
		err = backtest.RevokeCandidateLifecycle(&lifecycle, now, input.Note)
	default:
		err = fmt.Errorf("不支持的候选操作 %q", input.Action)
	}
	if err != nil {
		writeJSON(writer, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	if err := s.candidateArchive.SaveLifecycle(lifecycle); err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "候选生命周期归档失败: " + err.Error()})
		return
	}
	item, err := s.strategyCandidateSummary(result)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (s *Server) refreshCandidateObservation(ctx context.Context, result backtest.ContinuousOptimizationResult, lifecycle backtest.CandidateLifecycle, now time.Time) (backtest.CandidateLifecycle, error) {
	end := candidateLatestCompleted(now)
	start, err := time.ParseInLocation("2006-01-02", lifecycle.ObservationStart, strategyLocation)
	if err != nil {
		return lifecycle, fmt.Errorf("前向观察开始日期无效")
	}
	if end.Before(start) {
		lifecycle.EvaluationEnd = end.Format("2006-01-02")
		lifecycle.Assessment = backtest.AssessCandidateObservation(lifecycle.Policy, lifecycle.Observation)
		return lifecycle, nil
	}
	request := result.Request.BaseRequest
	request.Start = start
	request.End = end
	request.Technical = result.Selected.Proposal.Parameters
	request.StrategyVersion = "forward-" + result.ID
	request.LiquidateAtEnd = false
	observation, err := s.candidateEngine.Run(ctx, request)
	if err != nil {
		if strings.Contains(err.Error(), "交易日不足") {
			lifecycle.EvaluationEnd = end.Format("2006-01-02")
			lifecycle.Assessment = backtest.AssessCandidateObservation(lifecycle.Policy, lifecycle.Observation)
			return lifecycle, nil
		}
		return lifecycle, fmt.Errorf("前向观察更新失败: %w", err)
	}
	backtest.EnrichRiskMetrics(&observation)
	backtest.EnrichMarketRegimeMetrics(&observation)
	backtest.UpdateCandidateObservation(&lifecycle, observation, end.Format("2006-01-02"), now)
	return lifecycle, nil
}

func (s *Server) strategyCandidateSummary(result backtest.ContinuousOptimizationResult) (strategyCandidateSummary, error) {
	lifecycle, err := s.candidateArchive.LoadLifecycle(result.ID)
	if err != nil {
		return strategyCandidateSummary{}, err
	}
	if lifecycle.ExperimentID == "" && result.Stage == backtest.ContinuousStageShadow && result.Selected != nil {
		lifecycle, err = backtest.NewAwaitingCandidateLifecycle(result)
		if err != nil {
			return strategyCandidateSummary{}, err
		}
	}
	item := strategyCandidateSummary{
		ExperimentID: result.ID, Cycle: result.Cycle, GeneratedAt: result.GeneratedAt, DataCutoff: result.DataCutoff,
		ResearchStage: result.Stage, Tickers: append([]string(nil), result.Request.BaseRequest.Tickers...),
		Names: result.Request.BaseRequest.Names, Quality: result.Quality, GateReasons: append([]string(nil), result.GateReasons...),
		Lifecycle: lifecycle,
	}
	if result.Selected != nil {
		parameters := result.Selected.Proposal.Parameters
		item.SelectedID = result.Selected.Proposal.ID
		item.Parameters = &parameters
		item.PositiveFoldRatio = result.Selected.PositiveFoldRatio
		item.ValidationTrades = result.Selected.ValidationTrades
		item.AverageValidation = result.Selected.AverageValidation
		item.WorstDrawdown = result.Selected.WorstDrawdown
		item.ConsensusAgents = result.Selected.ConsensusAgents
		item.NeighborhoodSize = result.Selected.NeighborhoodSize
	}
	if result.Holdout != nil {
		metrics := result.Holdout.Metrics
		item.HoldoutMetrics = &metrics
	}
	if result.Stress.DoubleCost != nil {
		metrics := result.Stress.DoubleCost.Metrics
		item.StressMetrics = &metrics
	}
	if lifecycle.Observation != nil {
		metrics := lifecycle.Observation.Metrics
		item.ObservationMetrics = &metrics
	}
	item.RefreshDue = candidateRefreshDue(lifecycle, s.currentTime())
	item.NextAction = candidateNextAction(result, lifecycle)
	return item, nil
}

func candidateObservationStart(now time.Time) string {
	local := now.In(strategyLocation)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, strategyLocation)
	day = nextStrategyWeekday(day.AddDate(0, 0, 1))
	return day.Format("2006-01-02")
}

func candidateLatestCompleted(now time.Time) time.Time {
	local := now.In(strategyLocation)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, strategyLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return previousStrategyWeekday(day.AddDate(0, 0, -1))
	}
	if local.Before(day.Add(15*time.Hour + 5*time.Minute)) {
		return previousStrategyWeekday(day.AddDate(0, 0, -1))
	}
	return day
}

func nextStrategyWeekday(day time.Time) time.Time {
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

func previousStrategyWeekday(day time.Time) time.Time {
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func candidateRefreshDue(lifecycle backtest.CandidateLifecycle, now time.Time) bool {
	if lifecycle.Status != backtest.CandidateLifecycleObserving && lifecycle.Status != backtest.CandidateLifecycleApprovalReady {
		return false
	}
	latest := candidateLatestCompleted(now).Format("2006-01-02")
	return lifecycle.EvaluationEnd == "" || lifecycle.EvaluationEnd < latest
}

func candidateNextAction(result backtest.ContinuousOptimizationResult, lifecycle backtest.CandidateLifecycle) string {
	if result.Stage != backtest.ContinuousStageShadow || result.Selected == nil {
		return "修复未通过的历史研究门禁"
	}
	switch lifecycle.Status {
	case backtest.CandidateLifecycleAwaitingObservation:
		return "人工批准进入真实时间观察"
	case backtest.CandidateLifecycleObserving:
		return "继续积累前向交易日与成交样本"
	case backtest.CandidateLifecycleApprovalReady:
		return "等待人工批准为下一轮研究基线"
	case backtest.CandidateLifecycleApproved:
		return "已批准；仅供下一轮持续优化继承"
	case backtest.CandidateLifecycleRejected:
		return "已拒绝；保留实验与拒绝原因"
	case backtest.CandidateLifecycleRevoked:
		return "批准已撤销；后续轮次不得继承"
	default:
		return "等待历史门禁结果"
	}
}

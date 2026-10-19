package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type volumeForwardGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Return    *float64 `json:"return_percent"`
	Drawdown  *float64 `json:"drawdown_percent"`
	Cash      float64  `json:"cash"`
	Trades    int      `json:"trades"`
	Positions int      `json:"positions"`
	Pending   int      `json:"pending"`
	Fees      float64  `json:"fees"`
}

type volumeForwardEvidence struct {
	Name       string                        `json:"name"`
	Minimum    float64                       `json:"entry_volume_floor"`
	Normal     backtest.PositionStudySummary `json:"normal"`
	DoubleCost backtest.PositionStudySummary `json:"double_cost"`
}

type volumeForwardStudyResponse struct {
	Configured  bool                                     `json:"configured"`
	Verified    bool                                     `json:"verified"`
	ReadOnly    bool                                     `json:"read_only"`
	Plan        *backtest.VolumeForwardPlan              `json:"plan,omitempty"`
	SealedDays  int                                      `json:"sealed_days"`
	LastDate    string                                   `json:"last_date,omitempty"`
	SampleReady bool                                     `json:"sample_ready"`
	Assessment  *backtest.CandidateObservationAssessment `json:"assessment,omitempty"`
	Groups      []volumeForwardGroup                     `json:"groups"`
	Evidence    []volumeForwardEvidence                  `json:"evidence"`
	Status      storage.VolumeForwardStatus              `json:"status"`
	StatusStale bool                                     `json:"status_stale"`
	Warnings    []string                                 `json:"warnings"`
}

func WithVolumeForwardStudy(store *storage.VolumeForwardStore) ServerOption {
	return func(s *Server) { s.volumeForwardStudy = store }
}

func (s *Server) handleVolumeForwardStudy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "前瞻研究展示只支持 GET"})
		return
	}
	if s.volumeForwardStudy == nil {
		writeJSON(w, http.StatusOK, volumeForwardStudyResponse{ReadOnly: true})
		return
	}
	store := s.volumeForwardStudy
	if err := store.Exists(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "前瞻研究目录不可用: " + err.Error()})
		return
	}
	plan, day, count, err := store.Load()
	// Reading the dashboard must not contend with the collector's writer lock.
	// Retry a transient head/day mismatch across an atomic day publication.
	if err != nil {
		plan, day, count, err = store.Load()
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "前瞻归档校验失败: " + err.Error()})
		return
	}
	study, err := store.ReadStudy(plan)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "前瞻研究证据校验失败: " + err.Error()})
		return
	}
	result := summarizeVolumeForwardStudy(plan, day, count, study)
	result.Status, err = store.ReadStatus()
	if err != nil {
		result.Warnings = append(result.Warnings, "采集调度状态读取失败，展示最近已封存结果")
	}
	age := s.currentTime().Sub(result.Status.CheckedAt)
	result.StatusStale = result.Status.CheckedAt.IsZero() || age > 15*time.Minute || age < -time.Minute || result.Status.SealedDays != count
	if strings.EqualFold(r.URL.Query().Get("download"), "1") {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="volume-forward-%s.json"`, plan.CreatedAt.Format("20060102")))
	}
	writeJSON(w, http.StatusOK, result)
}

func summarizeVolumeForwardStudy(plan backtest.VolumeForwardPlan, day *backtest.VolumeForwardDay, count int, study backtest.VolumeStudyResult) volumeForwardStudyResponse {
	result := volumeForwardStudyResponse{Configured: true, Verified: true, ReadOnly: true, Plan: &plan, SealedDays: count, Groups: []volumeForwardGroup{}, Evidence: []volumeForwardEvidence{}, Warnings: append([]string{}, study.Warnings...)}
	definitions := []struct{ id, name string }{{"baseline", "原规则"}, {"candidate", "量比候选"}, {"baseline-double", "原规则 · 双倍成本"}, {"candidate-double", "量比候选 · 双倍成本"}}
	for index, definition := range definitions {
		group := volumeForwardGroup{ID: definition.id, Name: definition.name, Cash: plan.Baseline.InitialCash}
		if day != nil && len(day.Runs) > index {
			run := day.Runs[index]
			group.Return = finitePointer(run.Metrics.TotalReturn)
			group.Drawdown = finitePointer(run.Metrics.MaxDrawdown)
			group.Trades = run.Metrics.Trades
			group.Positions = len(run.OpenPositions)
			group.Pending = len(run.PendingOrders)
			group.Fees = run.Metrics.TotalFees
			if len(run.Equity) > 0 {
				group.Cash = run.Equity[len(run.Equity)-1].Cash
			}
		}
		result.Groups = append(result.Groups, group)
	}
	if day != nil {
		result.LastDate = day.Date
		result.SampleReady = day.SampleReady
		assessment := day.Assessment
		result.Assessment = &assessment
	}
	for index, variant := range study.Variants {
		name := "原规则"
		if index == 1 {
			name = "追加量比下限"
		}
		result.Evidence = append(result.Evidence, volumeForwardEvidence{Name: name, Minimum: variant.Minimum, Normal: variant.Normal, DoubleCost: variant.DoubleCost})
	}
	return result
}

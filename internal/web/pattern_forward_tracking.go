package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func (s *Server) forwardTrackingEvidence(task domain.PatternForwardTask, day domain.PatternForwardDay) (domain.PatternForwardEvidence, error) {
	entry := domain.PatternForwardEvidence{Day: day, Stocks: []domain.PatternForwardSnapshot{}}
	benchmark, err := s.patternForwards.Benchmark(task.ID, day.Date)
	if err == nil {
		entry.Benchmark = &benchmark
	} else if !os.IsNotExist(err) {
		return entry, err
	}
	for _, item := range day.Items {
		if item.SnapshotHash == "" {
			continue
		}
		snapshot, err := s.patternForwards.Snapshot(task.ID, day.Date, item.Symbol)
		if err != nil {
			return entry, err
		}
		if snapshot.Hash != item.SnapshotHash || snapshot.ConfigHash != task.ConfigHash || entry.Benchmark == nil || snapshot.BenchmarkHash != entry.Benchmark.Hash {
			return entry, fmt.Errorf("跟踪引用的原始快照指纹不一致")
		}
		entry.Stocks = append(entry.Stocks, snapshot)
	}
	return entry, nil
}

func (s *Server) reconcileForwardTracking(ctx context.Context, task domain.PatternForwardTask, dates []string) (string, error) {
	logs, err := s.patternForwards.Tracking(task.ID)
	if err != nil {
		return "", err
	}
	through := ""
	if len(logs) > 0 {
		through = logs[len(logs)-1].Date
	}
	days, err := s.patternForwards.Days(task.ID)
	if err != nil {
		return through, err
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	sealed := []domain.PatternForwardDay{}
	for _, day := range days {
		if day.SealedAt.IsZero() {
			break
		}
		sealed = append(sealed, day)
	}
	if len(sealed) == 0 || sealed[len(sealed)-1].Date <= through {
		return through, nil
	}
	evidence := []domain.PatternForwardEvidence{}
	for _, day := range sealed {
		if err := ctx.Err(); err != nil {
			return through, err
		}
		control, err := s.patternForwards.Control(task.ID)
		if err != nil || !control.Enabled {
			return through, err
		}
		entry, err := s.forwardTrackingEvidence(task, day)
		if err != nil {
			return through, err
		}
		evidence = append(evidence, entry)
		if day.Date <= through {
			continue
		}
		ledger, err := strategy.BuildPatternForwardTracking(ctx, task, evidence, logs, dates, s.currentTime())
		if err != nil {
			return through, err
		}
		control, err = s.patternForwards.Control(task.ID)
		if err != nil || !control.Enabled || ctx.Err() != nil {
			return through, err
		}
		ledger, err = s.patternForwards.SaveTracking(ledger)
		if err != nil {
			return through, err
		}
		logs = append(logs, ledger)
		through = day.Date
	}
	return through, nil
}

func (s *Server) forwardTrackingResponse(w http.ResponseWriter, r *http.Request, task domain.PatternForwardTask) {
	logs, err := s.patternForwards.Tracking(task.ID)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	query := r.URL.Query()
	cutoff := query.Get("date")
	if cutoff != "" {
		if _, err = time.Parse(time.DateOnly, cutoff); err != nil {
			writeJSON(w, 400, errorResponse{Error: "无效跟踪截止日"})
			return
		}
	}
	selected := []domain.PatternForwardTracking{}
	latest := map[string]domain.PatternForwardProgress{}
	through, hash := "", ""
	for _, ledger := range logs {
		if ledger.ConfigHash != task.ConfigHash {
			writeJSON(w, 500, errorResponse{Error: "跟踪日志配置不匹配"})
			return
		}
		if cutoff != "" && ledger.Date > cutoff {
			continue
		}
		selected = append(selected, ledger)
		through, hash = ledger.Date, ledger.Hash
		for _, record := range ledger.Records {
			latest[record.Sample.ID] = record
		}
	}
	if query.Get("download") == "1" {
		evidence := []domain.PatternForwardEvidence{}
		for _, ledger := range selected {
			day, err := s.patternForwards.Day(task.ID, ledger.Date)
			if err != nil {
				writeJSON(w, 500, errorResponse{Error: err.Error()})
				return
			}
			entry, err := s.forwardTrackingEvidence(task, day)
			if err != nil {
				writeJSON(w, 500, errorResponse{Error: err.Error()})
				return
			}
			evidence = append(evidence, entry)
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"forward-tracking-%s.json\"", task.ID))
		writeJSON(w, 200, map[string]any{"task": task, "through": through, "hash": hash, "logs": selected, "evidence": evidence})
		return
	}
	if id := query.Get("sample"); id != "" {
		history := []domain.PatternForwardProgress{}
		for _, ledger := range selected {
			for _, record := range ledger.Records {
				if record.Sample.ID == id {
					history = append(history, record)
				}
			}
		}
		if len(history) == 0 {
			writeJSON(w, 404, errorResponse{Error: "未找到前向样本跟踪记录"})
			return
		}
		writeJSON(w, 200, map[string]any{"through": through, "history": history})
		return
	}
	filter := strategy.PatternValidationFilter{Pattern: query.Get("pattern"), Bias: query.Get("bias"), State: query.Get("state"), Action: query.Get("action")}
	for key, target := range map[string]*int{"horizon": &filter.Horizon, "offset": &filter.Offset, "limit": &filter.Limit} {
		if value := query.Get(key); value != "" {
			*target, err = strconv.Atoi(value)
			if err != nil {
				writeJSON(w, 400, errorResponse{Error: "无效窗口或分页参数"})
				return
			}
		}
	}
	samples := []domain.PatternValidationSample{}
	for _, record := range latest {
		samples = append(samples, record.Sample)
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].ObservedOn != samples[j].ObservedOn {
			return samples[i].ObservedOn > samples[j].ObservedOn
		}
		return samples[i].ID < samples[j].ID
	})
	view, err := strategy.PatternValidationResult(domain.PatternValidationReport{Samples: samples}, filter)
	if err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	records := []domain.PatternForwardProgress{}
	for _, sample := range view.Samples {
		records = append(records, latest[sample.ID])
	}
	writeJSON(w, 200, map[string]any{"version": strategy.PatternForwardTrackingVersion, "through": through, "hash": hash, "filter": view.Filter, "totals": view.Totals, "total": view.Total, "records": records})
}

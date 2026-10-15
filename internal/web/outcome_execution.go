package web

import (
	"context"
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type outcomeSignalArchive interface {
	ListOutcomeSignals(context.Context) ([]realtime.Signal, error)
}

func (s *Server) readOutcomeSignals(ctx context.Context, limit int) ([]realtime.Signal, error) {
	if limit < 0 {
		if archive, ok := s.realtimeArchive.(outcomeSignalArchive); ok {
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			return archive.ListOutcomeSignals(readCtx)
		}
	}
	return s.realtimeArchive.List(limit)
}

func outcomeEvaluationTimeout(limit int) time.Duration {
	if limit < 0 {
		return 10 * time.Minute
	}
	return realtimeOutcomeTimeout
}

func (s *Server) evaluateOutcomeSignals(ctx context.Context, signals []realtime.Signal, options realtime.OutcomeOptions) (realtime.OutcomeReport, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, outcomeEvaluationTimeout(options.SignalLimit))
	defer cancel()
	return s.realtimeOutcomes.Evaluate(fetchCtx, signals, options)
}

func (s *Server) recordOutcomeResult(report realtime.OutcomeReport) {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()
	task := s.automationTasks[automationTaskOutcomes]
	task.LastResultDetail = fmt.Sprintf("评估数据截至%s，%d条数据提示", report.DataThrough, len(report.Warnings))
	task.Warnings = append([]string(nil), report.Warnings...)
	s.automationTasks[automationTaskOutcomes] = task
}

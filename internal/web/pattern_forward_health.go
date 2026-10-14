package web

import (
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type patternForwardRuntime struct {
	Supported      bool      `json:"supported"`
	Running        bool      `json:"running"`
	Checking       bool      `json:"checking"`
	TaskID         string    `json:"task_id,omitempty"`
	Stage          string    `json:"stage,omitempty"`
	StartedAt      time.Time `json:"started_at,omitzero"`
	CycleStartedAt time.Time `json:"cycle_started_at,omitzero"`
	LastCycleAt    time.Time `json:"last_cycle_at,omitzero"`
	Error          string    `json:"error,omitempty"`
}

func (s *Server) updateForwardRuntime(update func(*patternForwardRuntime)) {
	s.patternForwardStateMu.Lock()
	defer s.patternForwardStateMu.Unlock()
	update(&s.patternForwardRuntime)
}

func (s *Server) forwardRuntime() patternForwardRuntime {
	s.patternForwardStateMu.Lock()
	defer s.patternForwardStateMu.Unlock()
	v := s.patternForwardRuntime
	v.Supported = s.patternForwards != nil
	return v
}

func (s *Server) recordForwardOperations(id string, status domain.PatternForwardStatus) error {
	task, err := s.patternForwards.Load(id)
	if err != nil {
		return err
	}
	control, err := s.patternForwards.Control(id)
	if err != nil {
		return err
	}
	days, err := s.patternForwards.Days(id)
	if err != nil {
		return err
	}
	now := s.currentTime()
	health := strategy.PatternForwardHealth(task, control, status, days, now)
	latest := ""
	for _, day := range days {
		if !day.SealedAt.IsZero() && day.Date > latest {
			latest = day.Date
		}
	}
	started := s.forwardRuntime().StartedAt
	_, err = s.patternForwards.UpdateOperations(id, func(v domain.PatternForwardOperations) (domain.PatternForwardOperations, error) {
		needsAudit := v.Audit.CheckedAt.IsZero() || (latest != "" && v.Audit.Through < latest) || started.After(v.Audit.CheckedAt)
		if v.Audit.State == "tracking-pending" && status.TrackingThrough >= v.Audit.Through {
			needsAudit = true
		}
		if v.Audit.Error != "" && now.Sub(v.Audit.CheckedAt) >= 10*time.Minute {
			needsAudit = true
		}
		if needsAudit {
			v.Audit = s.patternForwards.Audit(id, now)
		}
		if v.Audit.Error != "" {
			health.Issues = append(health.Issues, domain.PatternForwardIssue{Key: "archive-integrity", Code: "archive-integrity", Severity: "error", Message: v.Audit.Error})
		}
		return strategy.AdvancePatternForwardOperations(v, health.Issues, now), nil
	})
	if err != nil {
		return fmt.Errorf("保存前向运行记录失败: %w", err)
	}
	return nil
}

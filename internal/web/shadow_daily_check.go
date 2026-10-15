package web

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

func checkShadowDaily(response shadowDailyResponse) paper.ShadowDailyCheck {
	check := paper.ShadowDailyCheck{Date: response.Date, CheckedAt: response.GeneratedAt, Status: "passed", AccountStatus: "passed", SourceSignature: response.Source.Signature, Issues: []paper.ShadowDailyCheckIssue{}}
	ranks := map[string]int{"passed": 0, "pending": 1, "warning": 2, "failed": 3}
	add := func(id, code, status, message string) {
		check.Issues = append(check.Issues, paper.ShadowDailyCheckIssue{ProfileID: id, Code: code, Status: status, Message: message})
		if ranks[status] > ranks[check.Status] {
			check.Status = status
		}
	}
	day, _ := time.ParseInLocation(time.DateOnly, response.Date, realtimeWebLocation)
	closeAt := day.Add(15 * time.Hour)
	due := !response.GeneratedAt.Before(closeAt.Add(30 * time.Minute))
	missingStatus := "pending"
	if due {
		missingStatus = "failed"
	} else {
		add("", "before-close-check", "pending", "收盘验收时点为15:30")
	}
	if len(response.Rows) == 0 {
		add("", "accounts-missing", "failed", "没有可检查的账户")
	}
	if !response.Source.Available || response.Source.Records == 0 {
		add("", "scan-archive-missing", "warning", "扫描档案缺失或为空，不能确认当日候选情况")
	}
	if response.Source.InvalidLines > 0 || response.Source.PartialTail || response.Source.FutureRecords > 0 {
		add("", "scan-archive-incomplete", "warning", "扫描档案含损坏、未完成或未来记录")
	}
	for _, warning := range response.Warnings {
		add("", "review-warning", "warning", warning)
	}
	if response.ArchiveError != "" {
		add("", "archive-error", "failed", response.ArchiveError)
	}
	for _, row := range response.Rows {
		if !row.TradingStartAt.IsZero() && !row.TradingStartAt.Before(closeAt) {
			check.NotStarted++
			continue
		}
		check.Accounts++
		issue := func(code, status, message string) { add(row.ID, code, status, row.Name+"："+message) }
		if len(row.LedgerIssues) > 0 {
			issue("ledger-invalid", "failed", "账本校验未通过")
		}
		if row.FrozenConfigMatches != nil && !*row.FrozenConfigMatches {
			issue("frozen-config-drift", "failed", "账本或当日配置偏离冻结对照")
		}
		if row.Ledger == nil {
			issue("ledger-missing", missingStatus, "账本未覆盖所选日期")
		} else {
			check.Orders += row.Ledger.OrderCount
			check.Fees += row.Ledger.Fees
			check.RealizedProfit += row.Ledger.RealizedProfit
			if row.Ledger.OrderCount > 0 {
				check.TradedAccounts++
			} else if row.Scans.Eligible > 0 && row.Ledger.DecisionCount == 0 && row.Ledger.RejectedCount == 0 {
				issue("candidate-without-execution", "warning", "存在合格候选，但没有决策、拒单或成交记录")
			}
		}
		counts := row.Scans.Eligible
		for _, gate := range row.Scans.Gates {
			counts += gate.Count
		}
		if counts != row.Scans.Observations || row.Scans.Scans+row.Scans.ExcludedScans+row.Scans.DuplicateScans+row.Scans.BeforeStart != response.Source.Records-response.Source.FutureRecords {
			issue("scan-count-mismatch", "failed", "扫描、资格通过和拦截统计无法对齐")
		}
		if row.Scans.InvalidSignals > 0 {
			issue("invalid-signals", "warning", fmt.Sprintf("排除了%d次无效信号", row.Scans.InvalidSignals))
		}
		if response.Source.Available && row.Scans.Scans == 0 {
			issue("trading-scans-missing", "warning", "没有可核验的交易时段扫描")
		}
		if row.Snapshot == nil {
			issue("snapshot-missing", missingStatus, "当日权益快照未留存")
			continue
		}
		snapshot := row.Snapshot
		if !snapshot.Sealed {
			issue("snapshot-unsealed", missingStatus, "收盘快照尚未封存")
		} else {
			check.SealedAccounts++
		}
		if !snapshot.Fresh || len(snapshot.LedgerIssues) > 0 || snapshot.Revision != row.LedgerRevision || snapshot.Date != response.Date || snapshot.ProfileID != row.ID || snapshot.TrialHash != row.TrialHash || snapshot.ConfigHash != row.ConfigHash || snapshot.GeneratedAt.After(response.GeneratedAt) || snapshot.GeneratedAt.In(realtimeWebLocation).Format(time.DateOnly) != response.Date || snapshot.Sealed && snapshot.GeneratedAt.Before(closeAt) {
			issue("snapshot-invalid", "failed", "快照估值、身份、配置、修订或时点不一致")
		}
		if math.Abs(snapshot.Equity-snapshot.Cash-snapshot.MarketValue) > .02 {
			issue("equity-mismatch", "failed", "权益与现金及持仓市值不一致")
		}
		if row.BaselineID != "" && (row.RealizedDifference == nil || row.ReturnDifference == nil) {
			issue("comparison-unavailable", missingStatus, "同组基线比较尚不可用")
		}
	}
	if check.Accounts == 0 && check.NotStarted > 0 {
		add("", "accounts-not-started", "pending", "所选日期早于全部对照的首个交易时段")
	}
	check.AccountStatus = check.Status
	// Historical reviews must not inherit today's scheduler state.
	if response.Date == response.Today {
		if !response.Runtime.Enabled {
			add("", "automation-disabled", "warning", "自动调度未启用")
		}
		if task, ok := response.Runtime.TaskStates[automationTaskShadow]; ok {
			if task.Status == "error" || task.Status == "warning" {
				add("", "shadow-task", "warning", "账户调度："+task.Detail)
			} else if due && task.LastSuccessAt.Before(closeAt) {
				add("", "shadow-close-not-confirmed", "warning", "尚无收盘后的账户同步成功记录")
			}
		} else {
			add("", "shadow-task-missing", "warning", "账户调度记录缺失")
		}
		if due {
			task, ok := response.Runtime.TaskStates[automationTaskOutcomes]
			if len(task.Warnings) > 0 {
				add("", "outcomes-data-warnings", "warning", fmt.Sprintf("信号验证有%d条数据提示，需查看验证结果", len(task.Warnings)))
			}
			if !ok || task.LastSuccessAt.Before(closeAt.Add(30*time.Minute)) || task.LastSuccessAt.After(response.GeneratedAt) {
				status, detail := "warning", "收盘信号验证尚未完成"
				if ok && task.Detail != "" {
					detail = task.Detail
				}
				if ok && (task.Status == "running" || task.Status == "busy" || task.NextRunAt.After(response.GeneratedAt) && task.NextRunAt.In(realtimeWebLocation).Format(time.DateOnly) == response.Date) {
					status = "pending"
				}
				add("", "outcomes-pending", status, detail)
			}
		}
	}
	return check
}

func (s *Server) saveShadowDailyCheck(ctx context.Context, now time.Time) error {
	if s.shadowDailyStore == nil || s.shadowDailyScans == nil {
		return nil
	}
	date := now.In(realtimeWebLocation).Format(time.DateOnly)
	response, err := s.buildShadowDailyResponse(ctx, date, now)
	check := response.Check
	if err != nil {
		check = paper.ShadowDailyCheck{Date: date, CheckedAt: now, Status: "failed", AccountStatus: "failed", Issues: []paper.ShadowDailyCheckIssue{{Code: "review-unreadable", Status: "failed", Message: err.Error()}}}
	}
	if saveErr := s.shadowDailyStore.SaveCheck(check); saveErr != nil {
		return saveErr
	}
	return err
}

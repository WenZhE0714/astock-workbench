package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type ForwardAccountExecution struct {
	Account        string `json:"account"`
	FirstBuy       string `json:"first_buy,omitempty"`
	FirstSell      string `json:"first_sell,omitempty"`
	Buys           int    `json:"buys"`
	Sells          int    `json:"sells"`
	DeferredOrders int    `json:"deferred_orders"`
}

type ForwardExecutionAcceptance struct {
	Status          string                    `json:"status"`
	Detail          string                    `json:"detail"`
	VerifiedThrough string                    `json:"verified_through,omitempty"`
	Transitions     int                       `json:"transitions"`
	Accounts        []ForwardAccountExecution `json:"accounts"`
}

// This is the on-disk status contract written by cmd/forward-acceptance.
// Execution is optional so old reports cannot silently become zero-fill proof.
type ForwardAcceptanceReport struct {
	CheckedAt        time.Time                   `json:"checked_at"`
	Status           string                      `json:"status"`
	Detail           string                      `json:"detail"`
	Archive          string                      `json:"archive"`
	PlanFingerprint  string                      `json:"plan_fingerprint"`
	FirstDate        string                      `json:"first_date,omitempty"`
	FirstFingerprint string                      `json:"first_fingerprint,omitempty"`
	SealedDays       int                         `json:"sealed_days"`
	PromotionAllowed bool                        `json:"promotion_allowed"`
	Execution        *ForwardExecutionAcceptance `json:"execution,omitempty"`
}

type ForwardAcceptanceStore struct{ root string }

func NewForwardAcceptanceStore(root string) *ForwardAcceptanceStore {
	return &ForwardAcceptanceStore{root: root}
}

func (s *ForwardAcceptanceStore) Read(plan backtest.VolumeForwardPlan, archive string, now time.Time) (ForwardAcceptanceReport, error) {
	var report ForwardAcceptanceReport
	data, err := os.ReadFile(filepath.Join(s.root, "status.json"))
	if err != nil {
		return report, err
	}
	if err = json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("验收报告格式无效: %w", err)
	}
	if report.PlanFingerprint != plan.Fingerprint {
		return report, fmt.Errorf("验收报告的实验指纹不匹配")
	}
	expected, err := filepath.EvalSymlinks(archive)
	if err != nil {
		return report, err
	}
	expected, err = filepath.Abs(expected)
	if err != nil {
		return report, err
	}
	actual, err := filepath.EvalSymlinks(report.Archive)
	if err != nil {
		return report, fmt.Errorf("验收报告指向的归档不可用")
	}
	actual, err = filepath.Abs(actual)
	if err != nil {
		return report, err
	}
	if actual != expected {
		return report, fmt.Errorf("验收报告属于另一归档目录")
	}
	if report.CheckedAt.IsZero() || report.CheckedAt.Before(plan.CreatedAt) || report.CheckedAt.After(now.Add(time.Minute)) || report.SealedDays < 0 || report.PromotionAllowed {
		return report, fmt.Errorf("验收报告时间、数量或晋级标记无效")
	}
	switch report.Status {
	case "waiting", "attention", "error", "passed":
	default:
		return report, fmt.Errorf("未知验收状态")
	}
	validDate := func(value string) bool {
		if value == "" {
			return true
		}
		date, err := time.Parse(time.DateOnly, value)
		zone := time.FixedZone("Asia/Shanghai", 8*3600)
		return err == nil && date.Format(time.DateOnly) == value && value >= plan.Baseline.Start.In(zone).Format(time.DateOnly) && value <= report.CheckedAt.In(zone).Format(time.DateOnly)
	}
	if !validDate(report.FirstDate) || report.Status == "passed" && (report.FirstDate == "" || report.FirstFingerprint == "" || report.SealedDays == 0) {
		return report, fmt.Errorf("首日验收证据不完整")
	}
	if execution := report.Execution; execution != nil {
		switch execution.Status {
		case "waiting-first-day", "waiting-next-day", "waiting-fill", "partial", "passed", "paused", "error":
		default:
			return report, fmt.Errorf("未知成交验收状态")
		}
		if execution.Transitions < 0 || execution.Transitions > max(0, report.SealedDays-1) || !validDate(execution.VerifiedThrough) || (execution.Transitions > 0) != (execution.VerifiedThrough != "") || len(execution.Accounts) != 4 {
			return report, fmt.Errorf("成交验收覆盖记录无效")
		}
		names := map[string]bool{"原规则": false, "量比1.5": false, "原规则双倍成本": false, "量比1.5双倍成本": false}
		for _, account := range execution.Accounts {
			seen, known := names[account.Account]
			if !known || seen || account.Buys < 0 || account.Sells < 0 || account.DeferredOrders < 0 || !validDate(account.FirstBuy) || !validDate(account.FirstSell) {
				return report, fmt.Errorf("成交验收账户记录无效")
			}
			names[account.Account] = true
			if (account.Buys > 0) != (account.FirstBuy != "") || (account.Sells > 0) != (account.FirstSell != "") || account.FirstSell != "" && (account.FirstBuy == "" || account.FirstSell <= account.FirstBuy) || account.Sells > account.Buys || account.FirstBuy > execution.VerifiedThrough || account.FirstSell > execution.VerifiedThrough {
				return report, fmt.Errorf("首次买卖日期与次数不一致")
			}
			if execution.Status == "passed" && account.Buys == 0 {
				return report, fmt.Errorf("首次买入通过标记与账户记录不一致")
			}
		}
	}
	return report, nil
}

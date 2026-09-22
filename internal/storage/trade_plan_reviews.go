package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type TradePlanReviewStore struct{ root string }

func NewTradePlanReviewStore(root string) *TradePlanReviewStore {
	return &TradePlanReviewStore{root: root}
}

func (store *TradePlanReviewStore) path(symbol, planID string) (string, error) {
	if store == nil || strings.TrimSpace(store.root) == "" || !tradePlanSymbol.MatchString(symbol) || !validTradePlanID(planID) {
		return "", fmt.Errorf("计划复盘目录、股票或计划标识无效")
	}
	return filepath.Join(store.root, symbol, planID+".json"), nil
}

func normalizeReviewRevision(input domain.TradePlanReviewRevision, plan domain.TradePlan, now time.Time) (domain.TradePlanReviewRevision, error) {
	input.UpdatedAt = now
	input.Note = strings.TrimSpace(input.Note)
	input.ExitReason = strings.TrimSpace(input.ExitReason)
	input.ExecutionStatus = strings.ToLower(strings.TrimSpace(input.ExecutionStatus))
	input.Discipline = strings.ToLower(strings.TrimSpace(input.Discipline))
	if len([]rune(input.Note)) > 2000 || len([]rune(input.ExitReason)) > 240 {
		return input, fmt.Errorf("复盘备注或退出原因过长")
	}
	switch input.ExecutionStatus {
	case "watching", "followed", "deviated", "skipped", "not_traded":
	default:
		return input, fmt.Errorf("执行状态无效")
	}
	switch input.Discipline {
	case "", "followed", "partial", "deviated", "not_applicable":
	default:
		return input, fmt.Errorf("计划纪律评价无效")
	}
	seen := make(map[string]bool)
	tags := make([]string, 0, len(input.Tags))
	for _, raw := range input.Tags {
		tag := strings.TrimSpace(raw)
		if tag == "" || seen[tag] {
			continue
		}
		if len([]rune(tag)) > 24 || len(tags) >= 10 {
			return input, fmt.Errorf("复盘标签最多10个且每个不超过24字")
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	input.Tags = tags
	for _, price := range []*float64{input.ActualEntry, input.ActualExit} {
		if price != nil && (*price <= 0 || math.IsNaN(*price) || math.IsInf(*price, 0)) {
			return input, fmt.Errorf("实际成交价格无效")
		}
	}
	if input.ActualEntry == nil && !input.EntryAt.IsZero() || input.ActualExit == nil && !input.ExitAt.IsZero() {
		return input, fmt.Errorf("成交时间必须与实际成交价格同时填写")
	}
	if input.ActualExit != nil && input.ActualEntry == nil {
		return input, fmt.Errorf("填写退出价前必须填写实际入场价")
	}
	if input.ActualEntry != nil && input.ExecutionStatus != "followed" && input.ExecutionStatus != "deviated" {
		return input, fmt.Errorf("填写实际成交价后，执行状态须为按计划执行或偏离计划")
	}
	if !input.ExitAt.IsZero() && (input.EntryAt.IsZero() || input.ExitAt.Before(input.EntryAt)) {
		return input, fmt.Errorf("退出时间不能早于入场时间")
	}
	input.RealizedR = nil
	if input.ActualEntry != nil && input.ActualExit != nil && plan.Structure.Plan != nil {
		risk := *input.ActualEntry - plan.Structure.Plan.Invalidation
		if plan.Structure.Plan.Invalidation <= 0 || math.IsNaN(risk) || math.IsInf(risk, 0) || risk <= 0 {
			return input, fmt.Errorf("实际入场价必须高于冻结失效位才能计算R倍数")
		}
		value := math.Round(((*input.ActualExit-*input.ActualEntry)/risk)*100) / 100
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return input, fmt.Errorf("实际成交价格无法计算有效R倍数")
		}
		input.RealizedR = &value
	}
	return input, nil
}

func validateTradePlanReview(review domain.TradePlanReview) error {
	if review.Version != 1 || !validTradePlanID(review.PlanID) || !tradePlanSymbol.MatchString(review.Symbol) || review.Fingerprint == "" || review.UpdatedAt.IsZero() || review.Sequence != uint64(len(review.Revisions)) || len(review.Revisions) == 0 {
		return fmt.Errorf("计划复盘格式无效")
	}
	for index, revision := range review.Revisions {
		if revision.Sequence != uint64(index+1) || revision.UpdatedAt.IsZero() {
			return fmt.Errorf("计划复盘修订序列无效")
		}
	}
	if !sameReviewRevision(review.Current, review.Revisions[len(review.Revisions)-1]) {
		return fmt.Errorf("计划复盘当前版本与历史不一致")
	}
	return nil
}

func sameReviewRevision(left, right domain.TradePlanReviewRevision) bool {
	if left.Sequence != right.Sequence || !left.UpdatedAt.Equal(right.UpdatedAt) || left.Note != right.Note ||
		left.ExecutionStatus != right.ExecutionStatus || !left.EntryAt.Equal(right.EntryAt) || !left.ExitAt.Equal(right.ExitAt) ||
		left.ExitReason != right.ExitReason || left.Discipline != right.Discipline || len(left.Tags) != len(right.Tags) {
		return false
	}
	for index := range left.Tags {
		if left.Tags[index] != right.Tags[index] {
			return false
		}
	}
	return sameOptionalReviewNumber(left.ActualEntry, right.ActualEntry) &&
		sameOptionalReviewNumber(left.ActualExit, right.ActualExit) &&
		sameOptionalReviewNumber(left.RealizedR, right.RealizedR)
}

func sameOptionalReviewNumber(left, right *float64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (store *TradePlanReviewStore) Load(symbol, planID string) (domain.TradePlanReview, error) {
	path, err := store.path(symbol, planID)
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.TradePlanReview{}, nil
	}
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	var review domain.TradePlanReview
	if err := json.Unmarshal(data, &review); err != nil {
		return domain.TradePlanReview{}, err
	}
	if review.Symbol != symbol || review.PlanID != planID {
		return domain.TradePlanReview{}, fmt.Errorf("计划复盘文件与计划不一致")
	}
	return review, validateTradePlanReview(review)
}

func (store *TradePlanReviewStore) Update(plan domain.TradePlan, revision domain.TradePlanReviewRevision, now time.Time) (domain.TradePlanReview, error) {
	path, err := store.path(plan.Symbol, plan.ID)
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	if now.IsZero() || plan.Analysis.Fingerprint == "" {
		return domain.TradePlanReview{}, fmt.Errorf("计划复盘时间或原始快照无效")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return domain.TradePlanReview{}, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return domain.TradePlanReview{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	current, err := store.Load(plan.Symbol, plan.ID)
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	if current.Version != 0 && current.Fingerprint != plan.Analysis.Fingerprint {
		return current, fmt.Errorf("复盘与原始计划快照不一致")
	}
	if current.Version != 0 && now.Before(current.UpdatedAt) {
		return current, fmt.Errorf("复盘更新时间不能早于已有修订")
	}
	revision, err = normalizeReviewRevision(revision, plan, now)
	if err != nil {
		return current, err
	}
	if current.Version != 0 {
		comparison := revision
		comparison.Sequence, comparison.UpdatedAt = current.Current.Sequence, current.Current.UpdatedAt
		if sameReviewRevision(comparison, current.Current) {
			return current, nil
		}
	}
	if current.Version == 0 {
		current = domain.TradePlanReview{Version: 1, PlanID: plan.ID, Symbol: plan.Symbol, Fingerprint: plan.Analysis.Fingerprint, Revisions: []domain.TradePlanReviewRevision{}}
	}
	revision.Sequence = current.Sequence + 1
	current.Sequence, current.UpdatedAt, current.Current = revision.Sequence, now, revision
	current.Revisions = append(current.Revisions, revision)
	if err := validateTradePlanReview(current); err != nil {
		return domain.TradePlanReview{}, err
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return domain.TradePlanReview{}, err
	}
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return domain.TradePlanReview{}, err
	}
	return current, nil
}

func (store *TradePlanReviewStore) List(symbol string) ([]domain.TradePlanReview, error) {
	if !tradePlanSymbol.MatchString(symbol) || store == nil || strings.TrimSpace(store.root) == "" {
		return nil, fmt.Errorf("计划复盘股票或目录无效")
	}
	directory := filepath.Join(store.root, symbol)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.TradePlanReview{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]domain.TradePlanReview, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		planID := strings.TrimSuffix(entry.Name(), ".json")
		review, err := store.Load(symbol, planID)
		if err != nil {
			return nil, err
		}
		result = append(result, review)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (store *TradePlanReviewStore) All(limit int) ([]domain.TradePlanReview, error) {
	if store == nil || strings.TrimSpace(store.root) == "" {
		return nil, fmt.Errorf("计划复盘目录未初始化")
	}
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.TradePlanReview{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]domain.TradePlanReview, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !tradePlanSymbol.MatchString(entry.Name()) {
			continue
		}
		items, listErr := store.List(entry.Name())
		if listErr != nil {
			return nil, listErr
		}
		result = append(result, items...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].PlanID < result[j].PlanID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

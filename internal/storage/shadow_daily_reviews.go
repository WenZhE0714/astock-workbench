package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

type ShadowDailySnapshot struct {
	Date         string              `json:"date"`
	ProfileID    string              `json:"profile_id"`
	TrialHash    string              `json:"trial_hash,omitempty"`
	ConfigHash   string              `json:"config_hash"`
	Config       paper.Config        `json:"config"`
	Engine       string              `json:"engine"`
	Revision     int                 `json:"revision"`
	GeneratedAt  time.Time           `json:"generated_at"`
	RecordedAt   time.Time           `json:"recorded_at"`
	Sealed       bool                `json:"sealed"`
	Fresh        bool                `json:"fresh"`
	Equity       float64             `json:"equity"`
	Return       float64             `json:"return_percent"`
	Cash         float64             `json:"cash"`
	MarketValue  float64             `json:"market_value"`
	Positions    int                 `json:"positions"`
	LedgerIssues []paper.LedgerIssue `json:"ledger_issues"`
	Hash         string              `json:"hash,omitempty"`
}

type ShadowDailyReviewStore struct{ root string }

var shadowReviewProfileID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var shadowReviewLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func NewShadowDailyReviewStore(root string) *ShadowDailyReviewStore {
	return &ShadowDailyReviewStore{root: root}
}

func (store *ShadowDailyReviewStore) Capture(profileID, trialHash string, report paper.Report, now time.Time) error {
	date := now.In(shadowReviewLocation).Format(time.DateOnly)
	if report.AsOf != date || report.GeneratedAt.IsZero() || report.GeneratedAt.In(shadowReviewLocation).Format(time.DateOnly) != date || report.GeneratedAt.After(now) {
		return nil
	}
	if !shadowReviewProfileID.MatchString(profileID) {
		return fmt.Errorf("无效影子账户ID")
	}
	issues := paper.AuditShadowLedger(report)
	fresh := true
	for _, position := range report.Positions {
		if position.Quantity > 0 && (position.LastDate != date || position.LastPrice <= 0) {
			fresh = false
		}
	}
	snapshot := ShadowDailySnapshot{
		Date: date, ProfileID: profileID, TrialHash: trialHash, ConfigHash: paper.ConfigFingerprint(report.Config), Config: report.Config,
		Engine: report.EngineVersion, Revision: report.LedgerRevision, GeneratedAt: report.GeneratedAt, RecordedAt: now,
		Sealed: report.CheckpointPhase == paper.CheckpointClose && fresh && len(issues) == 0, Fresh: fresh,
		Equity: report.TotalEquity, Return: report.TotalReturnPercent, Cash: report.RemainingCash,
		MarketValue: report.TotalMarketValue, Positions: report.OpenPositions, LedgerIssues: issues,
	}
	dir := filepath.Join(store.root, date)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, profileID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	sealedPath := filepath.Join(dir, fmt.Sprintf("%s-r%d.json", profileID, snapshot.Revision))
	if existing, err := readShadowDailySnapshot(sealedPath); err == nil {
		if existing.ConfigHash != snapshot.ConfigHash || existing.Engine != snapshot.Engine {
			return fmt.Errorf("当日封存账户配置不同，保留原快照")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	snapshot.Hash = shadowBytesHash(data)
	data, err = json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if snapshot.Sealed {
		return immutableWrite(sealedPath, append(data, '\n'))
	}
	livePath := filepath.Join(dir, profileID+"-live.json")
	if previous, err := readShadowDailySnapshot(livePath); err == nil {
		if previous.Revision > snapshot.Revision || previous.GeneratedAt.After(snapshot.GeneratedAt) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(livePath, append(data, '\n'), 0o600)
}

func readShadowDailySnapshot(path string) (snapshot ShadowDailySnapshot, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, err
	}
	hash := snapshot.Hash
	snapshot.Hash = ""
	canonical, err := json.Marshal(snapshot)
	snapshot.Hash = hash
	if err != nil || hash == "" || hash != shadowBytesHash(canonical) {
		return snapshot, fmt.Errorf("每日账本快照指纹不一致: %s", path)
	}
	return snapshot, nil
}

func (store *ShadowDailyReviewStore) Day(date string) (map[string]ShadowDailySnapshot, error) {
	if !validReviewDate(date) {
		return nil, fmt.Errorf("无效复盘日期")
	}
	result := map[string]ShadowDailySnapshot{}
	entries, err := os.ReadDir(filepath.Join(store.root, date))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		snapshot, err := readShadowDailySnapshot(filepath.Join(store.root, date, entry.Name()))
		if err != nil {
			return nil, err
		}
		if snapshot.Date != date || !shadowReviewProfileID.MatchString(snapshot.ProfileID) || snapshot.ConfigHash != paper.ConfigFingerprint(snapshot.Config) {
			return nil, fmt.Errorf("每日账本快照身份不一致")
		}
		previous, ok := result[snapshot.ProfileID]
		if !ok || snapshot.Revision > previous.Revision || snapshot.Revision == previous.Revision && (snapshot.Sealed && !previous.Sealed || snapshot.Sealed == previous.Sealed && snapshot.GeneratedAt.After(previous.GeneratedAt)) {
			result[snapshot.ProfileID] = snapshot
		}
	}
	return result, nil
}

func (store *ShadowDailyReviewStore) Dates() ([]string, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	dates := []string{}
	for _, entry := range entries {
		if entry.IsDir() && validReviewDate(entry.Name()) {
			dates = append(dates, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

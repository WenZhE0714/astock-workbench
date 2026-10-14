package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternForwardStore struct{ root string }

func NewPatternForwardStore(root string) *PatternForwardStore {
	return &PatternForwardStore{root: root}
}
func (s *PatternForwardStore) Acquire() (func(), error) {
	return NewPatternBatchStore(s.root).lock("collector")
}

func immutableWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "snapshot-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Link(f.Name(), path)
}

func (s *PatternForwardStore) taskPath(id string) (string, error) {
	if !patternID(id, 16) {
		return "", fmt.Errorf("无效前向任务ID")
	}
	return filepath.Join(s.root, "tasks", id), nil
}
func (s *PatternForwardStore) dayPath(id, date string) (string, error) {
	root, err := s.taskPath(id)
	if err != nil {
		return "", err
	}
	if _, err = time.Parse(time.DateOnly, date); err != nil {
		return "", fmt.Errorf("无效采集日期")
	}
	return filepath.Join(root, "days", date), nil
}
func forwardHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *PatternForwardStore) Create(task domain.PatternForwardTask) error {
	root, err := s.taskPath(task.ID)
	if err != nil {
		return err
	}
	if task.ConfigHash != task.Fingerprint() || task.CreatedAt.IsZero() || poolDigest(task.Pool) != task.Pool.Version {
		return fmt.Errorf("前向配置指纹无效")
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return immutableWrite(filepath.Join(root, "config.json"), data)
}
func (s *PatternForwardStore) Load(id string) (domain.PatternForwardTask, error) {
	var t domain.PatternForwardTask
	root, err := s.taskPath(id)
	if err != nil {
		return t, err
	}
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(data, &t); err != nil {
		return t, err
	}
	if t.ID != id || t.ConfigHash != t.Fingerprint() || poolDigest(t.Pool) != t.Pool.Version {
		return t, fmt.Errorf("前向配置指纹不匹配")
	}
	return t, nil
}
func (s *PatternForwardStore) Tasks() ([]domain.PatternForwardTask, error) {
	items := []domain.PatternForwardTask{}
	entries, err := os.ReadDir(filepath.Join(s.root, "tasks"))
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := s.Load(e.Name())
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}
func (s *PatternForwardStore) Control(id string) (domain.PatternForwardControl, error) {
	root, err := s.taskPath(id)
	value := domain.PatternForwardControl{Enabled: true}
	if err != nil {
		return value, err
	}
	data, err := os.ReadFile(filepath.Join(root, "control.json"))
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(data, &value)
	return value, err
}
func (s *PatternForwardStore) SetControl(id string, value domain.PatternForwardControl) error {
	if _, err := s.Load(id); err != nil {
		return err
	}
	root, _ := s.taskPath(id)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(root, "control.json"), data, 0o600)
}
func (s *PatternForwardStore) Status(id string) (domain.PatternForwardStatus, error) {
	root, err := s.taskPath(id)
	var v domain.PatternForwardStatus
	if err != nil {
		return v, err
	}
	data, err := os.ReadFile(filepath.Join(root, "status.json"))
	if os.IsNotExist(err) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(data, &v)
	return v, err
}
func (s *PatternForwardStore) SaveStatus(id string, v domain.PatternForwardStatus) error {
	root, err := s.taskPath(id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(root, "status.json"), data, 0o600)
}

func (s *PatternForwardStore) Day(id, date string) (domain.PatternForwardDay, error) {
	var v domain.PatternForwardDay
	root, err := s.dayPath(id, date)
	if err != nil {
		return v, err
	}
	data, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	if v.TaskID != id || v.Date != date {
		return v, fmt.Errorf("采集日索引不匹配")
	}
	return v, nil
}
func (s *PatternForwardStore) SaveDay(v domain.PatternForwardDay) error {
	root, err := s.dayPath(v.TaskID, v.Date)
	if err != nil {
		return err
	}
	if old, e := s.Day(v.TaskID, v.Date); e == nil && !old.SealedAt.IsZero() {
		return fmt.Errorf("采集日已封存")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(root, "index.json"), data, 0o600)
}
func (s *PatternForwardStore) Days(id string) ([]domain.PatternForwardDay, error) {
	root, err := s.taskPath(id)
	items := []domain.PatternForwardDay{}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "days"))
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		day, err := s.Day(id, e.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, day)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date > items[j].Date })
	return items, nil
}

func (s *PatternForwardStore) SaveBenchmark(v domain.PatternForwardBenchmark) (domain.PatternForwardBenchmark, error) {
	root, err := s.dayPath(v.TaskID, v.Date)
	if err != nil {
		return v, err
	}
	v.Hash = ""
	v.Hash = forwardHash(v)
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	return v, immutableWrite(filepath.Join(root, "benchmark.json"), data)
}
func (s *PatternForwardStore) Benchmark(id, date string) (domain.PatternForwardBenchmark, error) {
	var v domain.PatternForwardBenchmark
	root, err := s.dayPath(id, date)
	if err != nil {
		return v, err
	}
	data, err := os.ReadFile(filepath.Join(root, "benchmark.json"))
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	hash := v.Hash
	v.Hash = ""
	valid := hash == forwardHash(v)
	v.Hash = hash
	if !valid || v.TaskID != id || v.Date != date {
		return v, fmt.Errorf("基准快照指纹不匹配")
	}
	return v, nil
}
func (s *PatternForwardStore) SaveSnapshot(v domain.PatternForwardSnapshot) (domain.PatternForwardSnapshot, error) {
	root, err := s.dayPath(v.TaskID, v.Date)
	if err != nil {
		return v, err
	}
	if !patternPoolSymbol.MatchString(v.Symbol) {
		return v, fmt.Errorf("无效快照证券")
	}
	v.Hash = ""
	v.Hash = forwardHash(v)
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	return v, immutableWrite(filepath.Join(root, v.Symbol+".json"), data)
}
func (s *PatternForwardStore) Snapshot(id, date, symbol string) (domain.PatternForwardSnapshot, error) {
	var v domain.PatternForwardSnapshot
	root, err := s.dayPath(id, date)
	if err != nil {
		return v, err
	}
	if !patternPoolSymbol.MatchString(symbol) {
		return v, fmt.Errorf("无效快照证券")
	}
	data, err := os.ReadFile(filepath.Join(root, symbol+".json"))
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	hash := v.Hash
	v.Hash = ""
	valid := hash == forwardHash(v)
	v.Hash = hash
	if !valid || v.TaskID != id || v.Date != date || v.Symbol != symbol {
		return v, fmt.Errorf("观察快照指纹不匹配")
	}
	return v, nil
}

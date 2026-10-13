package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternBatchStore struct{ root string }

func NewPatternBatchStore(root string) *PatternBatchStore { return &PatternBatchStore{root: root} }

func patternID(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && value == strings.ToLower(value)
}

func (s *PatternBatchStore) lock(name string) (func(), error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.root, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("研究存储正在由其他任务使用: %w", err)
	}
	return func() { syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close() }, nil
}

func (s *PatternBatchStore) Acquire() (func(), error) { return s.lock("worker") }

func poolDigest(pool domain.PatternPool) string {
	data, _ := json.Marshal(struct {
		Name, Source string
		Members      []domain.PatternPoolMember
	}{pool.Name, pool.Source, pool.Members})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

var patternPoolSymbol = regexp.MustCompile(`^(sh|sz|bj)[0-9]{6}$`)

func (s *PatternBatchStore) SavePool(pool domain.PatternPool) (domain.PatternPool, error) {
	pool.Name, pool.Source = strings.TrimSpace(pool.Name), strings.TrimSpace(pool.Source)
	if pool.Name == "" || pool.Source == "" || len(pool.Name) > 160 || len(pool.Source) > 2000 || len(pool.Members) < 1 || len(pool.Members) > domain.PatternPoolLimit {
		return pool, fmt.Errorf("股票池需要名称、来源及1至50只股票")
	}
	pool.Members = append([]domain.PatternPoolMember{}, pool.Members...)
	sort.Slice(pool.Members, func(i, j int) bool { return pool.Members[i].Symbol < pool.Members[j].Symbol })
	for i := range pool.Members {
		m := &pool.Members[i]
		m.Name, m.Industry = strings.TrimSpace(m.Name), strings.TrimSpace(m.Industry)
		if !patternPoolSymbol.MatchString(m.Symbol) || len(m.Name) > 160 || len(m.Industry) > 160 || (i > 0 && pool.Members[i-1].Symbol == m.Symbol) {
			return pool, fmt.Errorf("股票代码重复、无效或标签过长")
		}
	}
	pool.Version = poolDigest(pool)
	unlock, err := s.lock("pools")
	if err != nil {
		return pool, err
	}
	defer unlock()
	if old, err := s.LoadPool(pool.Version); err == nil {
		return old, nil
	} else if !os.IsNotExist(err) {
		return pool, err
	}
	pool.CreatedAt = time.Now()
	data, err := json.Marshal(pool)
	if err != nil {
		return pool, err
	}
	return pool, atomicWrite(filepath.Join(s.root, "pools", pool.Version+".json"), data, 0o600)
}

func (s *PatternBatchStore) LoadPool(id string) (domain.PatternPool, error) {
	var pool domain.PatternPool
	if !patternID(id, 32) {
		return pool, fmt.Errorf("无效股票池版本")
	}
	data, err := os.ReadFile(filepath.Join(s.root, "pools", id+".json"))
	if err != nil {
		return pool, err
	}
	if err = json.Unmarshal(data, &pool); err != nil {
		return pool, err
	}
	if pool.Version != id || poolDigest(pool) != id {
		return pool, fmt.Errorf("股票池版本指纹不匹配")
	}
	return pool, nil
}

func (s *PatternBatchStore) Pools() ([]domain.PatternPool, error) {
	items := []domain.PatternPool{}
	entries, err := os.ReadDir(filepath.Join(s.root, "pools"))
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		pool, err := s.LoadPool(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		items = append(items, pool)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *PatternBatchStore) SaveJob(job domain.PatternBatchJob) error {
	if !patternID(job.ID, 16) || job.ConfigHash == "" || job.CreatedAt.IsZero() {
		return fmt.Errorf("无效批量任务")
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.root, "jobs", job.ID, "state.json"), data, 0o600)
}

func (s *PatternBatchStore) LoadJob(id string) (domain.PatternBatchJob, error) {
	var job domain.PatternBatchJob
	if !patternID(id, 16) {
		return job, fmt.Errorf("无效批量任务ID")
	}
	data, err := os.ReadFile(filepath.Join(s.root, "jobs", id, "state.json"))
	if err != nil {
		return job, err
	}
	if err = json.Unmarshal(data, &job); err != nil {
		return job, err
	}
	if job.ID != id {
		return job, fmt.Errorf("批量任务ID不匹配")
	}
	if poolDigest(job.Pool) != job.Pool.Version {
		return job, fmt.Errorf("任务股票池快照指纹不匹配")
	}
	return job, nil
}

func (s *PatternBatchStore) Jobs() ([]domain.PatternBatchJob, error) {
	jobs := []domain.PatternBatchJob{}
	entries, err := os.ReadDir(filepath.Join(s.root, "jobs"))
	if os.IsNotExist(err) {
		return jobs, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, err := s.LoadJob(entry.Name())
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	if len(jobs) > 50 {
		jobs = jobs[:50]
	}
	return jobs, nil
}

func (s *PatternBatchStore) SaveInput(id string, input domain.PatternValidationInput) (string, error) {
	if !patternID(id, 16) || !patternPoolSymbol.MatchString(input.Symbol) {
		return "", fmt.Errorf("无效检查点路径")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	err = atomicWrite(filepath.Join(s.root, "jobs", id, "inputs", input.Symbol+".json"), data, 0o600)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash[:]), nil
}

func (s *PatternBatchStore) LoadInput(id, symbol, digest string) (domain.PatternValidationInput, error) {
	var input domain.PatternValidationInput
	if !patternID(id, 16) || !patternPoolSymbol.MatchString(symbol) {
		return input, fmt.Errorf("无效检查点路径")
	}
	data, err := os.ReadFile(filepath.Join(s.root, "jobs", id, "inputs", symbol+".json"))
	if err != nil {
		return input, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return input, fmt.Errorf("检查点指纹不匹配")
	}
	if err = json.Unmarshal(data, &input); err != nil {
		return input, err
	}
	if input.Symbol != symbol {
		return input, fmt.Errorf("检查点证券不匹配")
	}
	return input, nil
}

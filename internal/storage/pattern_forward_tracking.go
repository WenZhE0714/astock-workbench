package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func (s *PatternForwardStore) SaveTracking(v domain.PatternForwardTracking) (domain.PatternForwardTracking, error) {
	root, err := s.taskPath(v.TaskID)
	if err != nil {
		return v, err
	}
	if _, err = time.Parse(time.DateOnly, v.Date); err != nil {
		return v, fmt.Errorf("无效跟踪日期")
	}
	task, err := s.Load(v.TaskID)
	if err != nil {
		return v, err
	}
	if v.ConfigHash != task.ConfigHash || v.EvaluatedAt.IsZero() || v.Version == "" {
		return v, fmt.Errorf("跟踪配置或时间无效")
	}
	previous, err := s.Tracking(v.TaskID)
	if err != nil {
		return v, err
	}
	hash := ""
	if len(previous) > 0 {
		last := previous[len(previous)-1]
		if v.Date <= last.Date || v.Version != last.Version {
			return v, fmt.Errorf("跟踪日志只能按日期与原协议追加")
		}
		hash = last.Hash
	}
	if v.PreviousHash != hash {
		return v, fmt.Errorf("跟踪日志前序指纹不匹配")
	}
	v.Hash = ""
	v.Hash = forwardHash(v)
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	return v, immutableWrite(filepath.Join(root, "tracking", v.Date+".json"), data)
}

func (s *PatternForwardStore) Tracking(id string) ([]domain.PatternForwardTracking, error) {
	root, err := s.taskPath(id)
	if err != nil {
		return nil, err
	}
	items := []domain.PatternForwardTracking{}
	entries, err := os.ReadDir(filepath.Join(root, "tracking"))
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".json")
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return nil, fmt.Errorf("无效跟踪日志文件名")
		}
		data, err := os.ReadFile(filepath.Join(root, "tracking", entry.Name()))
		if err != nil {
			return nil, err
		}
		var v domain.PatternForwardTracking
		if err = json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		hash := v.Hash
		v.Hash = ""
		if hash != forwardHash(v) || v.TaskID != id || v.Date != date {
			return nil, fmt.Errorf("跟踪日志指纹不匹配")
		}
		v.Hash = hash
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	previous := ""
	for i, v := range items {
		if v.PreviousHash != previous || (i > 0 && (v.Version != items[0].Version || v.ConfigHash != items[0].ConfigHash)) {
			return nil, fmt.Errorf("跟踪日志链不连续")
		}
		previous = v.Hash
	}
	return items, nil
}

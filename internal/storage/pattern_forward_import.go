package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternForwardImport struct {
	Source      string                       `json:"source"`
	Destination string                       `json:"destination"`
	Applied     bool                         `json:"applied"`
	Files       int                          `json:"files"`
	Bytes       int64                        `json:"bytes"`
	Existing    int                          `json:"existing_tasks"`
	Tasks       []domain.PatternForwardAudit `json:"tasks"`
}

type forwardImportFile struct {
	relative string
	hash     [32]byte
}

// ImportPatternForwards copies only forward tasks and their exact pool versions.
// Existing files must match byte for byte; a task is published as one directory.
func ImportPatternForwards(source, destination string, apply bool, now time.Time) (result PatternForwardImport, err error) {
	source, err = filepath.Abs(source)
	if err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	result = PatternForwardImport{Source: source, Destination: destination, Tasks: []domain.PatternForwardAudit{}}
	if source == destination {
		return result, fmt.Errorf("源目录与目标目录相同")
	}
	from := NewPatternForwardStore(filepath.Join(source, "pattern-forwards"))
	to := NewPatternForwardStore(filepath.Join(destination, "pattern-forwards"))
	if apply {
		unlock, err := from.Acquire()
		if err != nil {
			return result, err
		}
		defer unlock()
		unlock, err = to.Acquire()
		if err != nil {
			return result, err
		}
		defer unlock()
		unlock, err = NewPatternBatchStore(filepath.Join(destination, "pattern-batches")).lock("pools")
		if err != nil {
			return result, err
		}
		defer unlock()
	}
	tasks, err := from.Tasks()
	if err != nil {
		return result, err
	}
	if len(tasks) == 0 {
		return result, fmt.Errorf("源目录没有前向任务")
	}
	poolFiles := map[string][]byte{}
	taskFiles := map[string][]forwardImportFile{}
	existing := map[string]bool{}
	for _, task := range tasks {
		audit := from.Audit(task.ID, now)
		result.Tasks = append(result.Tasks, audit)
		if audit.Error != "" {
			return result, fmt.Errorf("源任务 %s 验收失败: %s", task.ID, audit.Error)
		}
		if _, err := from.Operations(task.ID); err != nil {
			return result, err
		}
		pool, err := NewPatternBatchStore(filepath.Join(source, "pattern-batches")).LoadPool(task.Pool.Version)
		if err != nil || pool.Version != task.Pool.Version {
			return result, fmt.Errorf("源股票池 %s 不可用: %v", task.Pool.Version, err)
		}
		poolPath := filepath.Join("pattern-batches", "pools", task.Pool.Version+".json")
		poolData, err := regularForwardImportFile(filepath.Join(source, poolPath))
		if err != nil {
			return result, err
		}
		if _, seen := poolFiles[poolPath]; !seen {
			poolFiles[poolPath] = poolData
			result.Files++
			result.Bytes += int64(len(poolData))
		}
		root, _ := from.taskPath(task.ID)
		allowed := map[string]bool{"config.json": true, "control.json": true, "status.json": true, "operations.json": true}
		days, err := from.Days(task.ID)
		if err != nil {
			return result, err
		}
		for _, day := range days {
			allowed[filepath.Join("days", day.Date, "index.json")] = true
			allowed[filepath.Join("days", day.Date, "benchmark.json")] = true
			for _, item := range day.Items {
				if item.SnapshotHash != "" {
					allowed[filepath.Join("days", day.Date, item.Symbol+".json")] = true
				}
			}
		}
		logs, err := from.Tracking(task.ID)
		if err != nil {
			return result, err
		}
		for _, log := range logs {
			allowed[filepath.Join("tracking", log.Date+".json")] = true
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("迁移源不接受符号链接: %s", path)
			}
			if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") || entry.Name() == ".DS_Store" {
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".json") {
				return fmt.Errorf("迁移源包含未知文件: %s", path)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if !allowed[relative] {
				return fmt.Errorf("%s 不在已核验索引内，须先恢复或核对源归档", relative)
			}
			data, err := regularForwardImportFile(path)
			if err != nil {
				return err
			}
			taskFiles[task.ID] = append(taskFiles[task.ID], forwardImportFile{relative, sha256.Sum256(data)})
			result.Files++
			result.Bytes += int64(len(data))
			return nil
		})
		if err != nil {
			return result, err
		}
		target, _ := to.taskPath(task.ID)
		if _, err := os.Lstat(target); err == nil {
			existing[task.ID] = true
			result.Existing++
			if audit := to.Audit(task.ID, now); audit.Error != "" {
				return result, fmt.Errorf("目标任务已有无效内容: %s", audit.Error)
			}
			for _, file := range taskFiles[task.ID] {
				data, err := regularForwardImportFile(filepath.Join(target, file.relative))
				if err != nil {
					return result, err
				}
				if sha256.Sum256(data) != file.hash {
					return result, fmt.Errorf("目标任务文件已存在且不同，未覆盖: %s", file.relative)
				}
			}
		} else if !os.IsNotExist(err) {
			return result, err
		}
	}
	for path, data := range poolFiles {
		if err := matchingForwardImportFile(filepath.Join(destination, path), data, true); err != nil {
			return result, err
		}
	}
	if !apply {
		return result, nil
	}
	for path, data := range poolFiles {
		if err = immutableWrite(filepath.Join(destination, path), data); err != nil && !os.IsExist(err) {
			return result, err
		}
	}
	for _, task := range tasks {
		if existing[task.ID] {
			continue
		}
		stage, err := os.MkdirTemp(to.root, "import-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(stage)
		sourceRoot, _ := from.taskPath(task.ID)
		for _, file := range taskFiles[task.ID] {
			data, err := regularForwardImportFile(filepath.Join(sourceRoot, file.relative))
			if err != nil {
				return result, err
			}
			if sha256.Sum256(data) != file.hash {
				return result, fmt.Errorf("迁移期间源数据发生变化: %s", file.relative)
			}
			if err = immutableWrite(filepath.Join(stage, file.relative), data); err != nil {
				return result, err
			}
		}
		target, _ := to.taskPath(task.ID)
		if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return result, err
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return result, fmt.Errorf("迁移期间目标任务已出现，未覆盖")
		}
		if err = os.Rename(stage, target); err != nil {
			return result, err
		}
	}
	result.Applied = true
	return result, nil
}

func regularForwardImportFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("迁移仅接受普通文件: %s", path)
	}
	return os.ReadFile(path)
}

func matchingForwardImportFile(path string, expected []byte, allowMissing bool) error {
	data, err := regularForwardImportFile(path)
	if os.IsNotExist(err) && allowMissing {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(data, expected) {
		hash := sha256.Sum256(data)
		return fmt.Errorf("文件已存在且内容不同，未覆盖: %s (SHA-256 %s)", path, hex.EncodeToString(hash[:]))
	}
	return nil
}

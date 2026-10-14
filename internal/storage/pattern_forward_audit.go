package storage

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func (s *PatternForwardStore) Audit(id string, now time.Time) (result domain.PatternForwardAudit) {
	result = domain.PatternForwardAudit{TaskID: id, CheckedAt: now, State: "invalid"}
	fail := func(err error) domain.PatternForwardAudit {
		result.Error = err.Error()
		return result
	}
	task, err := s.Load(id)
	if err != nil {
		return fail(err)
	}
	result.ConfigHash = task.ConfigHash
	if _, err = s.Control(id); err != nil {
		return fail(err)
	}
	if _, err = s.Status(id); err != nil {
		return fail(err)
	}
	days, err := s.Days(id)
	if err != nil {
		return fail(err)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	members := map[string]bool{}
	for _, member := range task.Pool.Members {
		members[member.Symbol] = true
	}
	evidence := []domain.PatternForwardEvidence{}
	origins := map[string]domain.PatternForwardSnapshot{}
	samples := map[string]domain.PatternValidationSample{}
	latestSealed := ""
	for _, day := range days {
		result.Through = day.Date
		result.Days++
		if len(day.Items) != len(task.Pool.Members) || day.Date < task.Config.StartOn {
			return fail(fmt.Errorf("%s 采集日与冻结股票池或起点不一致", day.Date))
		}
		entry := domain.PatternForwardEvidence{Day: day, Stocks: []domain.PatternForwardSnapshot{}}
		benchmark, err := s.Benchmark(id, day.Date)
		if err == nil {
			entry.Benchmark = &benchmark
			bars := benchmark.Input.Bars
			if benchmark.Input.Symbol != "sh000300" || len(bars) == 0 || bars[len(bars)-1].Date != day.Date || bars[len(bars)-1].Volume <= 0 {
				return fail(fmt.Errorf("%s 基准未覆盖当日有效日K", day.Date))
			}
		} else if !os.IsNotExist(err) {
			return fail(err)
		}
		seen, captured := map[string]bool{}, 0
		for _, item := range day.Items {
			if !members[item.Symbol] || seen[item.Symbol] {
				return fail(fmt.Errorf("%s 采集日证券重复或不在冻结股票池", day.Date))
			}
			seen[item.Symbol] = true
			if item.SnapshotHash == "" {
				if item.State == "captured" {
					return fail(fmt.Errorf("%s %s 留存项缺少快照指纹", day.Date, item.Symbol))
				}
				continue
			}
			snapshot, err := s.Snapshot(id, day.Date, item.Symbol)
			if err != nil {
				return fail(err)
			}
			if snapshot.Hash != item.SnapshotHash || snapshot.ConfigHash != task.ConfigHash || entry.Benchmark == nil || snapshot.BenchmarkHash != entry.Benchmark.Hash || item.State != "captured" {
				return fail(fmt.Errorf("%s %s 快照引用不一致", day.Date, item.Symbol))
			}
			bars := snapshot.Input.Bars
			if snapshot.Input.Symbol != item.Symbol || len(bars) == 0 || bars[len(bars)-1].Date != day.Date || bars[len(bars)-1].Volume <= 0 || !item.CollectedAt.Equal(snapshot.CollectedAt) {
				return fail(fmt.Errorf("%s %s 快照日期、采集时间或成交量不一致", day.Date, item.Symbol))
			}
			ids := []string{}
			for _, sample := range snapshot.Samples {
				if sample.ObservedOn != day.Date || sample.Symbol != item.Symbol || origins[sample.ID].Hash != "" || len(sample.Outcomes) != 0 {
					return fail(fmt.Errorf("%s 首次观察身份或结果边界不一致", day.Date))
				}
				ids = append(ids, sample.ID)
				origins[sample.ID], samples[sample.ID] = snapshot, sample
			}
			if !reflect.DeepEqual(ids, item.SampleIDs) {
				return fail(fmt.Errorf("%s %s 索引样本与快照不一致", day.Date, item.Symbol))
			}
			captured++
			result.Snapshots++
			entry.Stocks = append(entry.Stocks, snapshot)
		}
		if !day.SealedAt.IsZero() {
			latestSealed = day.Date
			expectedState := "partial"
			if captured == len(day.Items) {
				expectedState = "captured"
				result.CompleteDays++
			} else {
				result.MissingDays++
				if captured == 0 {
					expectedState = "missed"
				}
			}
			if day.State != expectedState {
				return fail(fmt.Errorf("%s 封存状态与留存数量不一致", day.Date))
			}
		}
		evidence = append(evidence, entry)
	}
	logs, err := s.Tracking(id)
	if err != nil {
		return fail(err)
	}
	// Check every historical prefix without repeatedly encoding all prior days.
	prefixes := map[string]string{}
	digest := sha256.New()
	digest.Write([]byte("["))
	for i, entry := range evidence {
		if i > 0 {
			digest.Write([]byte(","))
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return fail(err)
		}
		digest.Write(data)
		state, err := digest.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return fail(err)
		}
		copy := sha256.New()
		if err := copy.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
			return fail(err)
		}
		copy.Write([]byte("]"))
		prefixes[entry.Day.Date] = hex.EncodeToString(copy.Sum(nil))
	}
	for _, log := range logs {
		end := sort.Search(len(evidence), func(i int) bool { return evidence[i].Day.Date >= log.Date })
		if log.ConfigHash != task.ConfigHash || end == len(evidence) || evidence[end].Day.Date != log.Date || evidence[end].Day.SealedAt.IsZero() || log.EvidenceHash != prefixes[log.Date] {
			return fail(fmt.Errorf("%s 跟踪日志证据指纹不一致", log.Date))
		}
		for _, record := range log.Records {
			origin := origins[record.Sample.ID]
			if origin.Hash == "" || origin.Hash != record.ObservationHash || origin.Date > log.Date || record.AsOf != log.Date || !reflect.DeepEqual(record.Sample.Snapshot, samples[record.Sample.ID].Snapshot) {
				return fail(fmt.Errorf("%s 跟踪结果与首次观察不一致", log.Date))
			}
		}
		result.TrackingThrough = log.Date
	}
	result.State = "verified"
	switch {
	case len(days) == 0:
		result.State = "waiting"
	case days[len(days)-1].SealedAt.IsZero():
		result.State = "collecting"
	case result.MissingDays > 0:
		result.State = "partial"
	case latestSealed == "":
		result.State = "collecting"
	case result.TrackingThrough < latestSealed:
		result.State = "tracking-pending"
	}
	return result
}

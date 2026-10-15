package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/web"
)

func (app *App) runShadowOperations(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: astock shadow audit|repair|trials [--profile 账户] [--calendar 日K缓存] [--apply]")
	}
	flags := flag.NewFlagSet("shadow", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	profile := flags.String("profile", "all", "影子账户ID")
	calendarFile := flags.String("calendar", "", "含 bars 字段的沪深300未复权日K缓存")
	apply := flags.Bool("apply", false, "执行已预检的修复或创建对照")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("不接受额外参数")
	}
	encoder := json.NewEncoder(app.out)
	encoder.SetIndent("", "  ")
	root := filepath.Join(filepath.Dir(app.paths.ShadowReportFile), "trials")
	if args[0] == "trials" {
		if *profile != "all" || *calendarFile != "" {
			return fmt.Errorf("创建对照不接受账户或日历参数")
		}
		set, err := storage.LoadShadowTrials(root)
		if err != nil {
			return err
		}
		if len(set.Trials) == 0 {
			set = web.NewShadowTrialSet(time.Now())
			if *apply {
				set, err = storage.SaveShadowTrials(root, set)
			}
		}
		if err != nil {
			return err
		}
		return encoder.Encode(set)
	}
	if args[0] != "audit" && args[0] != "repair" {
		return fmt.Errorf("未知影子操作")
	}
	if args[0] == "audit" && (*apply || *calendarFile != "") {
		return fmt.Errorf("只读校验不接受 --apply 或 --calendar")
	}
	calendar := []string{}
	if *calendarFile != "" {
		data, err := os.ReadFile(*calendarFile)
		if err != nil {
			return err
		}
		var snapshot struct {
			Bars []domain.DailyBar `json:"bars"`
		}
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
		for _, bar := range snapshot.Bars {
			if bar.Symbol != "sh000300" {
				return fmt.Errorf("修复日历必须来自沪深300")
			}
		}
		calendar = realtime.TradingDatesFromBars(snapshot.Bars)
	}
	files := map[string]string{"balanced": app.paths.ShadowReportFile, "conservative": app.paths.ShadowConservativeFile, "aggressive": app.paths.ShadowAggressiveFile, "adaptive": app.paths.ShadowAdaptiveFile, "monster": app.paths.ShadowMonsterFile}
	trials, err := storage.LoadShadowTrials(root)
	if err != nil {
		return err
	}
	for _, trial := range trials.Trials {
		files[trial.ID] = filepath.Join(root, "accounts", trial.ID+".json")
	}
	ids := []string{}
	for id := range files {
		if *profile == "all" || id == *profile {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("未知影子账户: %s", *profile)
	}
	sort.Strings(ids)
	results := []any{}
	invalid := false
	for _, id := range ids {
		store := storage.NewShadowStore(files[id])
		if args[0] == "repair" {
			if _, err := os.Stat(files[id]); os.IsNotExist(err) {
				continue
			}
			result, err := store.Repair(calendar, time.Now(), *apply)
			if err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			results = append(results, result)
		} else {
			report, err := store.Load()
			if err != nil {
				return err
			}
			issues := paper.AuditShadowLedger(report)
			invalid = invalid || len(issues) > 0
			results = append(results, map[string]any{"profile": id, "revision": report.LedgerRevision, "issues": issues})
		}
	}
	if err := encoder.Encode(results); err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("影子账本未通过完整性校验")
	}
	return nil
}

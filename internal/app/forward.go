package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func (app *App) runForward(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("用法: astock forward audit [--id 任务ID] | import --from 数据目录 [--apply]")
	}
	set := flag.NewFlagSet("forward", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	id := set.String("id", "", "前向任务ID")
	from := set.String("from", "", "源 astock-workbench 数据目录")
	apply := set.Bool("apply", false, "执行已预检的数据复制，保留源文件")
	if err := set.Parse(arguments[1:]); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("不接受额外参数")
	}
	encoder := json.NewEncoder(app.out)
	encoder.SetIndent("", "  ")
	switch arguments[0] {
	case "import":
		if *from == "" || *id != "" {
			return fmt.Errorf("迁移须提供 --from，默认仅预检，使用 --apply 才复制")
		}
		result, err := storage.ImportPatternForwards(*from, app.paths.DataDir, *apply, time.Now())
		if outputErr := encoder.Encode(result); outputErr != nil {
			return outputErr
		}
		return err
	case "audit":
		if *from != "" || *apply {
			return fmt.Errorf("验收不接受迁移参数")
		}
		store := storage.NewPatternForwardStore(app.paths.PatternForwardsDir)
		ids := []string{*id}
		if *id == "" {
			tasks, err := store.Tasks()
			if err != nil {
				return err
			}
			ids = nil
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
		}
		results := []domain.PatternForwardAudit{}
		invalid := false
		for _, id := range ids {
			result := store.Audit(id, time.Now())
			invalid = invalid || result.Error != ""
			results = append(results, result)
		}
		if err := encoder.Encode(map[string]any{"data_dir": app.paths.DataDir, "tasks": results}); err != nil {
			return err
		}
		if invalid {
			return fmt.Errorf("前向归档存在完整性错误")
		}
		return nil
	default:
		return fmt.Errorf("未知前向操作，支持 audit 或 import")
	}
}

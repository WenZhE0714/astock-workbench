package app

import (
	"context"
	"fmt"

	"github.com/wenzhe/astock-workbench/internal/storage"
)

func (app *App) runWatchlistMutation(ctx context.Context, action string, inputs []string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("%s 后至少需要一个股票代码或名称", action)
	}
	resolvedInputs := make([]string, 0)
	for _, input := range inputs {
		resolvedInputs = append(resolvedInputs, splitInputs(input)...)
	}
	symbols, err := app.resolver.ResolveMany(ctx, resolvedInputs)
	if err != nil {
		return err
	}
	var changed []bool
	if action == "add" {
		changed, err = storage.AddWatchlist(app.paths.WatchlistFile, symbols)
	} else {
		changed, err = storage.RemoveWatchlist(app.paths.WatchlistFile, symbols)
	}
	if err != nil {
		return err
	}
	for index, symbol := range symbols {
		status := "已存在"
		if action == "remove" {
			status = "未找到"
		}
		if changed[index] {
			if action == "add" {
				status = "已添加"
			} else {
				status = "已删除"
			}
		}
		name := app.names.LookupName(symbol)
		if name != "" {
			fmt.Fprintf(app.out, "%s  %s  %s\n", status, symbol[2:], name)
		} else {
			fmt.Fprintf(app.out, "%s  %s\n", status, symbol[2:])
		}
	}
	if action == "add" {
		fmt.Fprintf(app.out, "自选股文件: %s\n", app.paths.WatchlistFile)
	}
	return nil
}

func (app *App) runWatchlistList(arguments []string) error {
	if len(arguments) > 0 {
		return fmt.Errorf("list 不接受股票代码或名称")
	}
	return storage.PrintWatchlist(app.out, app.paths.WatchlistFile, app.names)
}

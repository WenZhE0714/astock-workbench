package app

import (
	"context"
	"flag"
	"fmt"

	"github.com/wenzhe/astock-workbench/internal/analysis"
)

func (app *App) runStockReport(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("stock-report", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	full := set.Bool("full", false, "生成后打印完整报告")
	noAI := set.Bool("no-ai", false, "仅生成确定性多维报告")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return fmt.Errorf("用法: astock stock-report [--full] [--no-ai] 股票代码或名称")
	}
	symbol, err := app.resolver.Resolve(ctx, set.Arg(0))
	if err != nil {
		return err
	}
	var previousAI analysis.TextSynthesizer
	if *noAI {
		previousAI = app.marketReportAI
		app.marketReportAI = nil
		defer func() { app.marketReportAI = previousAI }()
	}
	report, err := app.generateStockReport(ctx, symbol, nil, func(message string) {
		fmt.Fprintf(app.errOut, "个股多维研判: %s...\n", message)
	})
	if err != nil {
		return err
	}
	engine := "Codex综合"
	if !report.AIUsed {
		engine = "确定性量价"
	}
	fmt.Fprintf(app.out, "%s %s 个股研判已生成（%s）\n", report.Symbol[2:], report.Name, engine)
	fmt.Fprintf(app.out, "Markdown  %s\n", report.MarkdownPath)
	fmt.Fprintf(app.out, "Snapshot  %s\n", report.FactsPath)
	if report.AIError != "" {
		fmt.Fprintf(app.out, "Codex     %s\n", report.AIError)
	}
	if *full {
		fmt.Fprintln(app.out)
		fmt.Fprint(app.out, report.Markdown)
	}
	return nil
}

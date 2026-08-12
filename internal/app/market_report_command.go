package app

import (
	"context"
	"flag"
	"fmt"

	"github.com/wenzhe/astock-workbench/internal/analysis"
)

func (app *App) runMarketReport(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("scan", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	full := set.Bool("full", false, "生成后打印完整报告")
	noAI := set.Bool("no-ai", false, "仅生成确定性量价报告")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("用法: astock scan [--full] [--no-ai]")
	}
	var previousAI analysis.TextSynthesizer
	if *noAI {
		previousAI = app.marketReportAI
		app.marketReportAI = nil
		defer func() { app.marketReportAI = previousAI }()
	}
	report, err := app.generateMarketReport(ctx, func(message string) {
		fmt.Fprintf(app.errOut, "智能市场扫描: %s...\n", message)
	})
	if err != nil {
		return err
	}
	engine := "Codex综合"
	if !report.AIUsed {
		engine = "确定性量价"
	}
	fmt.Fprintf(app.out, "智能市场报告已生成（%s）\n", engine)
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

package ui

import (
	"strings"
	"testing"
)

func TestBuildStrategyLabFrameShowsUnifiedMenuAndSelection(t *testing.T) {
	frame := BuildStrategyLabFrame(StrategyLabFrame{
		Context:  "当前：600519 贵州茅台  ·  范围：当前股票（1只）",
		Controls: "↑/↓选择  Enter进入/运行  Esc返回  q退出",
		Items:    []string{"运行单次回测", "运行训练/验证/样本外优化", "研究设置"}, Selected: 1,
		Status: "统一入口：历史模拟不触发自动交易。",
	}, 80, false, false)
	for _, expected := range []string{"ASTOCK 策略研究中心", "运行单次回测", "> 运行训练/验证/样本外优化", "研究设置", "历史模拟不触发自动交易"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("strategy lab frame missing %q:\n%s", expected, frame)
		}
	}
}

func TestBuildStrategyLabFrameUsesSemanticColorOnlyWhenEnabled(t *testing.T) {
	data := StrategyLabFrame{
		Context: "当前：600176 中国巨石\n单次回测：2023-08-12 至 2026-08-11\n候选：30",
		Status:  "统一入口：历史模拟不触发自动交易。", StatusTone: "warning",
		Controls: "↑/↓选择  Enter进入/运行", Items: []string{"运行单次回测", "研究设置"}, Selected: 0,
	}
	colored := BuildStrategyLabFrame(data, 80, false, true)
	for _, expected := range []string{"\x1b[1;36mASTOCK 策略研究中心", "\x1b[1;30;46m> 运行单次回测", "\x1b[1;33m统一入口"} {
		if !strings.Contains(colored, expected) {
			t.Fatalf("colored frame missing %q:\n%q", expected, colored)
		}
	}
	plain := BuildStrategyLabFrame(data, 80, false, false)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("no-color frame contains ANSI: %q", plain)
	}
	moyu := BuildStrategyLabFrame(data, 80, true, true)
	if strings.Contains(moyu, "\x1b[") {
		t.Fatalf("moyu frame contains ANSI: %q", moyu)
	}
}

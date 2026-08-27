package app

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const launchdLabel = "com.github.wenzhe.astock-workbench.web"

type launchdServiceConfig struct {
	Listen string
	Source string
	Symbol string
}

// runService manages an optional macOS launchd wrapper for the read-only Web
// server. The wrapper only keeps `astock web` alive; it never enables a broker
// or changes the research/ordering boundary.
func (app *App) runService(arguments []string) error {
	set := flag.NewFlagSet("service", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	listen := set.String("listen", "127.0.0.1:8765", "Web 监听地址")
	source := set.String("source", "", "行情源：http 或 tdx")
	symbol := set.String("symbol", "", "首次打开的股票")
	if len(arguments) == 0 {
		return fmt.Errorf("用法: astock service [install | uninstall | status] [--listen 地址] [--source http|tdx] [--symbol 代码]")
	}
	action := strings.ToLower(strings.TrimSpace(arguments[0]))
	if err := set.Parse(arguments[1:]); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("用法: astock service [install | uninstall | status] [--listen 地址] [--source http|tdx] [--symbol 代码]")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchd 服务仅支持 macOS")
	}
	config := launchdServiceConfig{Listen: strings.TrimSpace(*listen), Source: strings.TrimSpace(*source), Symbol: strings.TrimSpace(*symbol)}
	switch action {
	case "install":
		return app.installLaunchdService(config)
	case "uninstall", "remove", "rm":
		return app.uninstallLaunchdService()
	case "status":
		return app.launchdServiceStatus()
	default:
		return fmt.Errorf("未知服务操作 %q；可选 install、uninstall 或 status", action)
	}
}

func (app *App) launchdServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func (app *App) installLaunchdService(config launchdServiceConfig) error {
	if config.Listen == "" {
		return errors.New("Web 监听地址不能为空")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取 astock 可执行文件路径失败: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("解析 astock 可执行文件路径失败: %w", err)
	}
	path, err := app.launchdServicePath()
	if err != nil {
		return err
	}
	arguments := []string{executable, "web", "--listen", config.Listen}
	if config.Source != "" {
		arguments = append(arguments, "--source", config.Source)
	}
	if config.Symbol != "" {
		arguments = append(arguments, "--symbol", config.Symbol)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(home, "Library", "Logs", "astock-workbench")
	plist, err := renderLaunchdPlist(arguments, filepath.Join(home, "Library", "Logs", "astock-workbench", "web.out.log"), filepath.Join(logDir, "web.err.log"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("创建服务日志目录失败: %w", err)
	}
	if err := os.WriteFile(path, plist, 0o600); err != nil {
		return fmt.Errorf("写入 launchd 配置失败: %w", err)
	}
	uid, err := launchdUserID()
	if err != nil {
		return fmt.Errorf("读取当前用户 ID 失败（配置已写入 %s）: %w", path, err)
	}
	// Reinstalling the same label is common after a binary update. Boot out the
	// old definition first so bootstrap does not depend on launchctl's wording
	// for an "already loaded" error.
	_, _ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+launchdLabel).CombinedOutput()
	if output, loadErr := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); loadErr != nil {
		return fmt.Errorf("配置已写入 %s，但 launchd 加载失败: %v (%s)", path, loadErr, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(app.out, "ASTOCK Web 自动服务已安装: %s\n", path)
	fmt.Fprintln(app.out, "服务会在登录后自动启动并在进程退出时拉起；仍然只运行只读行情、研究和影子账户。")
	return nil
}

func (app *App) uninstallLaunchdService() error {
	path, err := app.launchdServicePath()
	if err != nil {
		return err
	}
	uid, err := launchdUserID()
	if err != nil {
		return err
	}
	_, _ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+launchdLabel).CombinedOutput()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 launchd 配置失败: %w", err)
	}
	fmt.Fprintf(app.out, "ASTOCK Web 自动服务已卸载: %s\n", path)
	return nil
}

func (app *App) launchdServiceStatus() error {
	path, err := app.launchdServicePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(app.out, "ASTOCK Web 自动服务未安装")
		return nil
	} else if err != nil {
		return err
	}
	uid, err := launchdUserID()
	if err != nil {
		return err
	}
	output, statusErr := exec.Command("launchctl", "print", "gui/"+uid+"/"+launchdLabel).CombinedOutput()
	if statusErr != nil {
		fmt.Fprintf(app.out, "ASTOCK Web 配置已安装但当前未加载: %s\n", path)
		return nil
	}
	fmt.Fprintf(app.out, "ASTOCK Web 自动服务运行中: %s\n%s", path, output)
	return nil
}

func launchdUserID() (string, error) {
	output, err := exec.Command("id", "-u").Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", errors.New("id -u 返回空值")
	}
	return value, nil
}

func renderLaunchdPlist(arguments []string, stdoutPath, stderrPath string) ([]byte, error) {
	if len(arguments) == 0 {
		return nil, errors.New("launchd 参数不能为空")
	}
	var builder strings.Builder
	builder.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	builder.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	builder.WriteString("<plist version=\"1.0\"><dict>\n")
	writeKey := func(key string) {
		builder.WriteString("<key>")
		writeXMLEscaped(&builder, key)
		builder.WriteString("</key>")
	}
	writeString := func(value string) {
		builder.WriteString("<string>")
		writeXMLEscaped(&builder, value)
		builder.WriteString("</string>\n")
	}
	writeKey("Label")
	writeString(launchdLabel)
	writeKey("ProgramArguments")
	builder.WriteString("<array>\n")
	for _, argument := range arguments {
		writeString(argument)
	}
	builder.WriteString("</array>\n")
	writeKey("RunAtLoad")
	builder.WriteString("<true/>\n")
	writeKey("KeepAlive")
	builder.WriteString("<true/>\n")
	writeKey("ThrottleInterval")
	builder.WriteString("<integer>30</integer>\n")
	writeKey("ProcessType")
	writeString("Interactive")
	writeKey("StandardOutPath")
	writeString(stdoutPath)
	writeKey("StandardErrorPath")
	writeString(stderrPath)
	builder.WriteString("</dict></plist>\n")
	return []byte(builder.String()), nil
}

func writeXMLEscaped(builder *strings.Builder, value string) {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(value))
	builder.WriteString(escaped.String())
}

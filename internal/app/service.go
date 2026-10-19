package app

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const launchdLabel = "com.github.wenzhe.astock-workbench.web"

type launchdServiceConfig struct {
	Name             string
	HTTPProxy        string
	HTTPSProxy       string
	NoProxy          string
	Listen           string
	Source           string
	Symbol           string
	DataHome         string
	ConfigHome       string
	CacheHome        string
	CalendarFile     string
	VolumeForwardDir string
	MacNotifications bool
	DryRun           bool
}

// runService manages an optional macOS launchd wrapper for the read-only Web
// server. The wrapper only keeps `astock web` alive; it never enables a broker
// or changes the research/ordering boundary.
func (app *App) runService(arguments []string) error {
	set := flag.NewFlagSet("service", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	name := set.String("name", "", "独立服务名称，留空管理原默认服务")
	httpProxy := set.String("http-proxy", os.Getenv("HTTP_PROXY"), "固定 HTTP_PROXY")
	httpsProxy := set.String("https-proxy", os.Getenv("HTTPS_PROXY"), "固定 HTTPS_PROXY")
	noProxy := set.String("no-proxy", os.Getenv("NO_PROXY"), "固定 NO_PROXY")
	listen := set.String("listen", "127.0.0.1:8765", "Web 监听地址")
	source := set.String("source", "", "行情源：http、tdx 或 ths")
	symbol := set.String("symbol", "", "首次打开的股票")
	dataHome := set.String("data-home", filepath.Dir(app.paths.DataDir), "固定 XDG_DATA_HOME")
	configHome := set.String("config-home", filepath.Dir(app.paths.ConfigDir), "固定 XDG_CONFIG_HOME")
	cacheHome := set.String("cache-home", filepath.Dir(app.paths.CacheDir), "固定 XDG_CACHE_HOME")
	calendarFile := set.String("calendar", os.Getenv("ASTOCK_TRADING_CALENDAR_FILE"), "固定交易日历文件")
	volumeForwardDir := set.String("volume-forward-dir", os.Getenv("ASTOCK_VOLUME_FORWARD_DIR"), "只读展示已冻结量比前瞻实验的目录")
	dryRun := set.Bool("dry-run", false, "仅输出 launchd 配置，不安装服务")
	macNotifications := set.Bool("mac-notifications", false, "开启 macOS 候选触发和模拟买卖通知")
	if len(arguments) == 0 {
		return fmt.Errorf("用法: astock service [install | uninstall | status] [--listen 地址] [--source http|tdx|ths] [--symbol 代码]")
	}
	action := strings.ToLower(strings.TrimSpace(arguments[0]))
	if err := set.Parse(arguments[1:]); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("用法: astock service [install | uninstall | status] [--listen 地址] [--source http|tdx|ths] [--symbol 代码]")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchd 服务仅支持 macOS")
	}
	config := launchdServiceConfig{Listen: strings.TrimSpace(*listen), Source: strings.TrimSpace(*source), Symbol: strings.TrimSpace(*symbol)}
	config.DataHome, config.ConfigHome, config.CacheHome = *dataHome, *configHome, *cacheHome
	config.CalendarFile, config.DryRun = *calendarFile, *dryRun
	config.VolumeForwardDir = *volumeForwardDir
	config.MacNotifications = *macNotifications
	config.Name, config.HTTPProxy, config.HTTPSProxy, config.NoProxy = *name, *httpProxy, *httpsProxy, *noProxy
	label, err := launchdServiceLabel(config.Name)
	if err != nil {
		return err
	}
	if *dryRun && action != "install" {
		return fmt.Errorf("--dry-run 仅支持 service install")
	}
	switch action {
	case "install":
		return app.installLaunchdService(config)
	case "uninstall", "remove", "rm":
		return app.uninstallLaunchdService(label)
	case "status":
		return app.launchdServiceStatus(label)
	default:
		return fmt.Errorf("未知服务操作 %q；可选 install、uninstall 或 status", action)
	}
}

func launchdServiceLabel(name string) (string, error) {
	if name == "" {
		return launchdLabel, nil
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(name) {
		return "", fmt.Errorf("服务名称须为小写字母开头的字母、数字或连字符，最多48字符")
	}
	return launchdLabel + "." + name, nil
}

func (app *App) launchdServicePath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func (app *App) installLaunchdService(config launchdServiceConfig) error {
	label, err := launchdServiceLabel(config.Name)
	if err != nil {
		return err
	}
	if config.Listen == "" {
		return errors.New("Web 监听地址不能为空")
	}
	_, port, err := net.SplitHostPort(config.Listen)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("服务需要有效的固定监听端口")
	}
	if config.Source != "" && config.Source != "http" && config.Source != "tdx" && config.Source != "ths" {
		return fmt.Errorf("行情源须为 http、tdx 或 ths")
	}
	environment := map[string]string{}
	for key, value := range map[string]string{"HTTP_PROXY": config.HTTPProxy, "HTTPS_PROXY": config.HTTPSProxy} {
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5" && parsed.Scheme != "socks5h") {
			return fmt.Errorf("%s 必须是有效代理地址", key)
		}
		environment[key] = value
	}
	if config.NoProxy != "" {
		environment["NO_PROXY"] = config.NoProxy
	}
	for key, value := range map[string]string{"XDG_DATA_HOME": config.DataHome, "XDG_CONFIG_HOME": config.ConfigHome, "XDG_CACHE_HOME": config.CacheHome, "ASTOCK_TRADING_CALENDAR_FILE": config.CalendarFile, "ASTOCK_VOLUME_FORWARD_DIR": config.VolumeForwardDir} {
		if value == "" {
			continue
		}
		path, err := filepath.Abs(value)
		if err != nil {
			return err
		}
		environment[key] = path
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取 astock 可执行文件路径失败: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("解析 astock 可执行文件路径失败: %w", err)
	}
	path, err := app.launchdServicePath(label)
	if err != nil {
		return err
	}
	arguments := []string{executable, "web", "--listen", config.Listen}
	if config.MacNotifications {
		arguments = append(arguments, "--mac-notifications")
	}
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
	logName := "web"
	if config.Name != "" {
		logName += "-" + config.Name
	}
	plist, err := renderLaunchdPlist(label, arguments, filepath.Join(logDir, logName+".out.log"), filepath.Join(logDir, logName+".err.log"), environment)
	if err != nil {
		return err
	}
	if config.DryRun {
		_, err := app.out.Write(plist)
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
	_, _ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+label).CombinedOutput()
	if output, loadErr := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); loadErr != nil {
		return fmt.Errorf("配置已写入 %s，但 launchd 加载失败: %v (%s)", path, loadErr, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(app.out, "ASTOCK Web 自动服务已安装: %s\n", path)
	fmt.Fprintln(app.out, "服务会在登录后自动启动并在进程退出时拉起；仍然只运行只读行情、研究和影子账户。")
	return nil
}

func (app *App) uninstallLaunchdService(label string) error {
	path, err := app.launchdServicePath(label)
	if err != nil {
		return err
	}
	uid, err := launchdUserID()
	if err != nil {
		return err
	}
	_, _ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+label).CombinedOutput()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 launchd 配置失败: %w", err)
	}
	fmt.Fprintf(app.out, "ASTOCK Web 自动服务已卸载: %s\n", path)
	return nil
}

func (app *App) launchdServiceStatus(label string) error {
	path, err := app.launchdServicePath(label)
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
	output, statusErr := exec.Command("launchctl", "print", "gui/"+uid+"/"+label).CombinedOutput()
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

func renderLaunchdPlist(label string, arguments []string, stdoutPath, stderrPath string, environments ...map[string]string) ([]byte, error) {
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
	writeString(label)
	writeKey("ProgramArguments")
	builder.WriteString("<array>\n")
	for _, argument := range arguments {
		writeString(argument)
	}
	builder.WriteString("</array>\n")
	if len(environments) > 0 && len(environments[0]) > 0 {
		writeKey("EnvironmentVariables")
		builder.WriteString("<dict>\n")
		keys := []string{}
		for key := range environments[0] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeKey(key)
			writeString(environments[0][key])
		}
		builder.WriteString("</dict>\n")
	}
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

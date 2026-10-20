package notify

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Values are passed as arguments, never interpolated into AppleScript code.
const macScript = `on run argv
display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv) sound name "Glass"
end run`

type MacSender struct {
	BaseURL string
	Allowed func(Message) (bool, error)
}

func NotificationURL(base string, message Message) string {
	address, err := url.Parse(base)
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") {
		return ""
	}
	address.Path = "/"
	address.RawQuery = url.Values{"view": {"monitor"}, "monitor": {"native"}, "event": {message.ID}}.Encode()
	if strings.HasPrefix(message.Kind, "forward-") {
		address.RawQuery = url.Values{"view": {"strategy"}, "forward": {"acceptance"}}.Encode()
	}
	address.Fragment = ""
	return address.String()
}

func terminalNotifier() string {
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		return path
	}
	for _, path := range []string{"/opt/homebrew/bin/terminal-notifier", "/usr/local/bin/terminal-notifier"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		installed := filepath.Join(home, "Applications", "terminal-notifier.app", "Contents", "MacOS", "terminal-notifier")
		if info, err := os.Stat(installed); err == nil && !info.IsDir() {
			return installed
		}
		path := filepath.Join(home, ".local", "share", "astock-workbench", "tools", "terminal-notifier-3.1.0", "terminal-notifier.app", "Contents", "MacOS", "terminal-notifier")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func (sender MacSender) Send(ctx context.Context, message Message) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("原生 Mac 通知仅支持 macOS")
	}
	if sender.Allowed != nil {
		allowed, err := sender.Allowed(message)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrMuted
		}
	}
	if path := terminalNotifier(); path != "" {
		args := []string{"-title", message.Title, "-subtitle", message.Subtitle, "-message", message.Body, "-sound", "Glass"}
		if link := NotificationURL(sender.BaseURL, message); link != "" {
			args = append(args, "-open", link)
		}
		output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("macOS 可点击通知提交失败: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", macScript, "--", message.Title, message.Subtitle, message.Body).CombinedOutput()
	if err != nil {
		return fmt.Errorf("macOS 通知提交失败: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

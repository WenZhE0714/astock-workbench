package app

import (
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderLaunchdPlistEscapesArgumentsAndKeepsReadOnlyWebCommand(t *testing.T) {
	data, err := renderLaunchdPlist(launchdLabel, []string{"/tmp/astock", "web", "--listen", "127.0.0.1:8765", "--symbol", "600519&x"}, "/tmp/out.log", "/tmp/err.log")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		"<key>Label</key><string>com.github.wenzhe.astock-workbench.web</string>",
		"<key>ProgramArguments</key>",
		"<string>/tmp/astock</string>",
		"<string>web</string>",
		"<string>600519&amp;x</string>",
		"<key>RunAtLoad</key><true/>",
		"<key>KeepAlive</key><true/>",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("plist missing %q: %s", expected, text)
		}
	}
}

func TestLaunchdDryRunFreezesDataRootsWithoutInstalling(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	app := &App{out: &output, errOut: io.Discard}
	err := app.installLaunchdService(launchdServiceConfig{Listen: "127.0.0.1:18828", Source: "http", DataHome: filepath.Join(root, "data & research"), ConfigHome: filepath.Join(root, "config"), CacheHome: filepath.Join(root, "cache"), CalendarFile: filepath.Join(root, "calendar.json"), MacNotifications: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(output.String()))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid service XML: %v", err)
		}
	}
	for _, value := range []string{"EnvironmentVariables", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "ASTOCK_TRADING_CALENDAR_FILE", "data &amp; research", "127.0.0.1:18828", "--mac-notifications"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing durable configuration: %s", value)
		}
	}
}

func TestRenderLaunchdPlistRejectsEmptyArguments(t *testing.T) {
	if _, err := renderLaunchdPlist(launchdLabel, nil, "", ""); err == nil {
		t.Fatal("empty launchd arguments should fail")
	}
}

func TestNamedLaunchdServiceFreezesProxyAndPreservesNotifications(t *testing.T) {
	var output bytes.Buffer
	app := &App{out: &output, errOut: io.Discard}
	err := app.installLaunchdService(launchdServiceConfig{Name: "shadow-8788", Listen: "127.0.0.1:8788", HTTPProxy: "http://127.0.0.1:7897", HTTPSProxy: "http://127.0.0.1:7897", NoProxy: "127.0.0.1,localhost", MacNotifications: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{launchdLabel + ".shadow-8788", "HTTP_PROXY", "HTTPS_PROXY", "http://127.0.0.1:7897", "NO_PROXY", "127.0.0.1,localhost", "web-shadow-8788.out.log", "--mac-notifications"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %s", value)
		}
	}
	for _, name := range []string{"../web", "name/other", "-bad", "A", strings.Repeat("a", 49)} {
		if _, err := launchdServiceLabel(name); err == nil {
			t.Fatalf("invalid service name accepted: %s", name)
		}
	}
	if err := app.installLaunchdService(launchdServiceConfig{Name: "shadow", Listen: "127.0.0.1:8788", HTTPSProxy: "invalid", DryRun: true}); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}

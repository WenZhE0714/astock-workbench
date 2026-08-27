package app

import (
	"strings"
	"testing"
)

func TestRenderLaunchdPlistEscapesArgumentsAndKeepsReadOnlyWebCommand(t *testing.T) {
	data, err := renderLaunchdPlist([]string{"/tmp/astock", "web", "--listen", "127.0.0.1:8765", "--symbol", "600519&x"}, "/tmp/out.log", "/tmp/err.log")
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

func TestRenderLaunchdPlistRejectsEmptyArguments(t *testing.T) {
	if _, err := renderLaunchdPlist(nil, "", ""); err == nil {
		t.Fatal("empty launchd arguments should fail")
	}
}

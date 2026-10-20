package notify

import (
	"context"
	"errors"
	"net/url"
	"runtime"
	"testing"
)

func TestNotificationURLKeepsEventIdentityWithoutScriptInterpolation(t *testing.T) {
	id := `candidate:sh600000 & "quoted"`
	link := NotificationURL("http://127.0.0.1:8788", Message{ID: id})
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("event") != id || parsed.Query().Get("monitor") != "native" {
		t.Fatalf("bad link: %s %v", link, err)
	}
	if NotificationURL("javascript:alert(1)", Message{}) != "" {
		t.Fatal("unsafe URL accepted")
	}
}

func TestMacSenderMuteSkipsSystemDelivery(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	err := (MacSender{Allowed: func(Message) (bool, error) { return false, nil }}).Send(context.Background(), Message{})
	if !errors.Is(err, ErrMuted) {
		t.Fatalf("muted notification not suppressed: %v", err)
	}
}

func TestForwardNotificationOpensAcceptanceResearch(t *testing.T) {
	link := NotificationURL("http://127.0.0.1:8788/old?view=monitor#old", Message{ID: "failure", Kind: "forward-collection"})
	parsed, err := url.Parse(link)
	if err != nil || parsed.Path != "/" || parsed.Query().Get("view") != "strategy" || parsed.Query().Get("forward") != "acceptance" || parsed.Fragment != "" {
		t.Fatalf("wrong research link: %s", link)
	}
}

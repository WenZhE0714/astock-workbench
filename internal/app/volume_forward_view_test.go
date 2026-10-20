package app

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceKeepsReadOnlyVolumeForwardDirectory(t *testing.T) {
	var output bytes.Buffer
	app := &App{out: &output, errOut: io.Discard}
	directory := filepath.Join(t.TempDir(), "forward study")
	acceptance := filepath.Join(t.TempDir(), "forward acceptance")
	if err := app.installLaunchdService(launchdServiceConfig{Name: "study-view", Listen: "127.0.0.1:8788", VolumeForwardDir: directory, ForwardAcceptanceDir: acceptance, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ASTOCK_VOLUME_FORWARD_DIR") || !strings.Contains(output.String(), directory) {
		t.Fatal("forward study directory not preserved")
	}
	if !strings.Contains(output.String(), "ASTOCK_VOLUME_FORWARD_ACCEPTANCE_DIR") || !strings.Contains(output.String(), acceptance) {
		t.Fatal("independent acceptance directory not preserved")
	}
}

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
	if err := app.installLaunchdService(launchdServiceConfig{Name: "study-view", Listen: "127.0.0.1:8788", VolumeForwardDir: directory, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ASTOCK_VOLUME_FORWARD_DIR") || !strings.Contains(output.String(), directory) {
		t.Fatal("forward study directory not preserved")
	}
}

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Values are passed as arguments, never interpolated into AppleScript code.
const macScript = `on run argv
display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv) sound name "Glass"
end run`

type MacSender struct{}

func (MacSender) Send(ctx context.Context, message Message) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("原生 Mac 通知仅支持 macOS")
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", macScript, "--", message.Title, message.Subtitle, message.Body).CombinedOutput()
	if err != nil {
		return fmt.Errorf("macOS 通知提交失败: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly

package app

import "io"

func detectedTerminalSize(io.Writer) (width, height int, ok bool) {
	return 0, 0, false
}

//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package app

import (
	"io"
	"os"

	"golang.org/x/term"
)

func detectedTerminalSize(output io.Writer) (width, height int, ok bool) {
	file, ok := output.(*os.File)
	if !ok {
		return 0, 0, false
	}
	width, height, err := term.GetSize(int(file.Fd()))
	if err != nil || width == 0 || height == 0 {
		return 0, 0, false
	}
	return width, height, true
}

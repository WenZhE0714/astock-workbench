package ui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Navigation is a logical movement within a rendered frame.
type Navigation int

const (
	NavigateUp Navigation = iota
	NavigateDown
	NavigatePageUp
	NavigatePageDown
	NavigateHome
	NavigateEnd
)

type Renderer struct {
	output   io.Writer
	terminal bool
	frame    string
	lines    []string
	width    int
	height   int
	offset   int
	started  bool
}

func NewRenderer(output io.Writer, terminal bool) *Renderer {
	return &Renderer{output: output, terminal: terminal}
}

func (renderer *Renderer) contentRows() int {
	if renderer.height <= 1 {
		return 1
	}
	if len(renderer.lines) > renderer.height {
		return renderer.height - 1
	}
	return renderer.height
}

func (renderer *Renderer) maximumOffset() int {
	maximum := len(renderer.lines) - renderer.contentRows()
	if maximum < 0 {
		return 0
	}
	return maximum
}

func (renderer *Renderer) clampOffset() {
	maximum := renderer.maximumOffset()
	if renderer.offset < 0 {
		renderer.offset = 0
	}
	if renderer.offset > maximum {
		renderer.offset = maximum
	}
}

func (renderer *Renderer) statusLine() string {
	contentRows := renderer.contentRows()
	start := renderer.offset + 1
	end := renderer.offset + contentRows
	if end > len(renderer.lines) {
		end = len(renderer.lines)
	}
	status := "行 " + strconv.Itoa(start) + "–" + strconv.Itoa(end) + "/" + strconv.Itoa(len(renderer.lines))
	if renderer.offset > 0 {
		status += "  ↑"
	}
	if renderer.offset < renderer.maximumOffset() {
		status += "  ↓"
	}
	return status
}

func (renderer *Renderer) drawTerminalFrame() {
	if renderer.height < 1 {
		renderer.height = 1
	}
	renderer.clampOffset()
	contentRows := renderer.contentRows()
	overflow := len(renderer.lines) > contentRows
	for row := 0; row < renderer.height; row++ {
		// CUP uses an absolute screen coordinate. No LF/CR is emitted, so the
		// terminal cannot create scrollback rows during a refresh.
		fmt.Fprintf(renderer.output, "\x1b[%d;1H\x1b[2K", row+1)
		if row < contentRows {
			lineIndex := renderer.offset + row
			if lineIndex < len(renderer.lines) {
				fmt.Fprint(renderer.output, renderer.lines[lineIndex])
			}
			continue
		}
		if overflow && row == contentRows {
			status := renderer.statusLine()
			if renderer.width > 1 {
				status = truncateWidth(status, renderer.width-1)
			}
			fmt.Fprint(renderer.output, status)
		}
	}
}

// Render replaces the cached frame and draws the current viewport. width and
// height are the visible terminal dimensions, not the scrollback dimensions.
func (renderer *Renderer) Render(frame string, width, height int) {
	frame = strings.TrimRight(frame, "\n")
	frameChanged := frame != renderer.frame
	if !renderer.terminal {
		if !frameChanged {
			return
		}
		fmt.Fprintln(renderer.output, frame)
		renderer.frame = frame
		return
	}
	if width < 2 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	if !renderer.started {
		// The alternate screen gives the renderer a stable origin even when the
		// command starts at the bottom of the user's main terminal.
		fmt.Fprint(renderer.output, "\x1b[?1049h\x1b[?25l\x1b[?6l\x1b[r\x1b[2J")
		renderer.started = true
	}
	dimensionsChanged := width != renderer.width || height != renderer.height
	if !frameChanged && !dimensionsChanged {
		return
	}
	renderer.frame = frame
	renderer.lines = strings.Split(frame, "\n")
	renderer.width = width
	renderer.height = height
	renderer.clampOffset()
	renderer.drawTerminalFrame()
}

// Navigate moves the viewport and redraws the cached frame without fetching
// or appending any output lines. It returns false when already at the edge.
func (renderer *Renderer) Navigate(action Navigation) bool {
	if !renderer.terminal || !renderer.started || len(renderer.lines) == 0 {
		return false
	}
	oldOffset := renderer.offset
	page := renderer.contentRows()
	if page < 1 {
		page = 1
	}
	switch action {
	case NavigateUp:
		renderer.offset--
	case NavigateDown:
		renderer.offset++
	case NavigatePageUp:
		renderer.offset -= page
	case NavigatePageDown:
		renderer.offset += page
	case NavigateHome:
		renderer.offset = 0
	case NavigateEnd:
		renderer.offset = renderer.maximumOffset()
	default:
		return false
	}
	renderer.clampOffset()
	if renderer.offset == oldOffset {
		return false
	}
	renderer.drawTerminalFrame()
	return true
}

func (renderer *Renderer) Offset() int {
	return renderer.offset
}

// ResetViewport makes the next frame start at its first row. It does not draw,
// so callers can switch between list and detail views without an intermediate
// repaint of the old frame.
func (renderer *Renderer) ResetViewport() {
	renderer.offset = 0
}

func (renderer *Renderer) VisibleRange() (start, end, total int) {
	if len(renderer.lines) == 0 {
		return 0, 0, 0
	}
	contentRows := renderer.contentRows()
	start = renderer.offset
	end = start + contentRows
	if end > len(renderer.lines) {
		end = len(renderer.lines)
	}
	return start, end, len(renderer.lines)
}

func (renderer *Renderer) Close() {
	if renderer.terminal && renderer.started {
		fmt.Fprint(renderer.output, "\x1b[?25h\x1b[?1049l")
		renderer.started = false
	}
}

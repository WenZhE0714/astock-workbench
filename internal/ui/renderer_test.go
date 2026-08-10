package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalRendererKeepsAllDrawingInsideScreen(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, true)

	renderer.Render("one\ntwo\nthree", 80, 5)
	initial := output.String()
	if strings.Contains(initial, "\n") || strings.Contains(initial, "\r") {
		t.Fatalf("terminal drawing must not emit line breaks: %q", initial)
	}
	if !strings.HasPrefix(initial, "\x1b[?1049h\x1b[?25l\x1b[?6l\x1b[r\x1b[2J") {
		t.Fatalf("initial frame must enter and clear the alternate screen: %q", initial)
	}
	if !strings.Contains(initial, "\x1b[1;1H\x1b[2Kone") || !strings.Contains(initial, "\x1b[3;1H\x1b[2Kthree") {
		t.Fatalf("initial frame must use absolute row coordinates: %q", initial)
	}
	if strings.Contains(initial, "\x1b[6;1H") {
		t.Fatalf("renderer wrote below the visible terminal: %q", initial)
	}

	renderer.Close()
	if !strings.HasSuffix(output.String(), "\x1b[?25h\x1b[?1049l") {
		t.Fatalf("close should restore cursor and main screen: %q", output.String())
	}
}

func TestTerminalRendererScrollsOverflowingFrame(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, true)
	renderer.Render("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight", 80, 4)

	initial := output.String()
	if !strings.Contains(initial, "\x1b[3;1H\x1b[2Kthree") || !strings.Contains(initial, "\x1b[4;1H\x1b[2K行 1–3/8  ↓") {
		t.Fatalf("unexpected initial viewport: %q", initial)
	}
	if strings.Contains(initial, "four") {
		t.Fatalf("hidden content leaked into initial viewport: %q", initial)
	}

	beforePage := output.Len()
	if !renderer.Navigate(NavigatePageDown) {
		t.Fatal("expected page down to move")
	}
	page := output.String()[beforePage:]
	if strings.Contains(page, "\n") || strings.Contains(page, "\r") {
		t.Fatalf("navigation must not emit line breaks: %q", page)
	}
	if !strings.Contains(page, "\x1b[1;1H\x1b[2Kfour") || !strings.Contains(page, "行 4–6/8  ↑  ↓") {
		t.Fatalf("unexpected second page: %q", page)
	}

	if !renderer.Navigate(NavigateEnd) {
		t.Fatal("expected end navigation to move")
	}
	start, end, total := renderer.VisibleRange()
	if start != 5 || end != 8 || total != 8 {
		t.Fatalf("unexpected end viewport: %d %d %d", start, end, total)
	}
	if renderer.Navigate(NavigateDown) {
		t.Fatal("down at end should not redraw")
	}
}

func TestTerminalRendererPreservesViewportAcrossRefresh(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, true)
	renderer.Render("one\ntwo\nthree\nfour\nfive", 80, 3)
	renderer.Navigate(NavigatePageDown)
	if renderer.Offset() != 2 {
		t.Fatalf("unexpected offset before refresh: %d", renderer.Offset())
	}

	beforeUpdate := output.Len()
	renderer.Render("ONE\nTWO\nTHREE\nFOUR\nFIVE", 80, 3)
	update := output.String()[beforeUpdate:]
	if renderer.Offset() != 2 {
		t.Fatalf("refresh reset scroll offset: %d", renderer.Offset())
	}
	if !strings.Contains(update, "\x1b[1;1H\x1b[2KTHREE") || !strings.Contains(update, "\x1b[2;1H\x1b[2KFOUR") {
		t.Fatalf("refresh did not redraw the active viewport: %q", update)
	}
	if strings.Contains(update, "\n") || strings.Contains(update, "\r") {
		t.Fatalf("refresh must not emit line breaks: %q", update)
	}
}

func TestTerminalRendererDoesNotAddressRowsRemovedByResize(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, true)
	renderer.Render("one\ntwo\nthree\nfour\nfive", 80, 6)

	beforeResize := output.Len()
	renderer.Render("one\ntwo\nthree\nfour\nfive", 80, 3)
	resize := output.String()[beforeResize:]
	if strings.Contains(resize, "\x1b[4;1H") || strings.Contains(resize, "\x1b[5;1H") || strings.Contains(resize, "\x1b[6;1H") {
		t.Fatalf("resize wrote below the new terminal height: %q", resize)
	}
	if !strings.Contains(resize, "\x1b[3;1H\x1b[2K行 1–2/5  ↓") {
		t.Fatalf("resize did not reserve the status row: %q", resize)
	}
}

func TestNonTerminalRendererKeepsPlainSnapshotBehavior(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, false)
	renderer.Render("one\ntwo", 80, 2)
	renderer.Render("one\ntwo", 80, 2)
	renderer.Render("ONE\nTWO", 80, 2)
	if output.String() != "one\ntwo\nONE\nTWO\n" {
		t.Fatalf("unexpected plain output: %q", output.String())
	}
}

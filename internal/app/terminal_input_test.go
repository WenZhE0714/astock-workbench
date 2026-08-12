package app

import (
	"reflect"
	"testing"
)

func TestKeyDecoderSupportsPlainNavigation(t *testing.T) {
	decoder := keyDecoder{}
	got := decoder.Feed([]byte{'k', 'j', 'b', '[', ']', ' ', 'g', 'G', '\r', '\n', 'q', 0x03})
	want := []terminalKey{
		terminalKeyUp,
		terminalKeyDown,
		terminalKeyPageUp,
		terminalKeyPageUp,
		terminalKeyPageDown,
		terminalKeySpace,
		terminalKeyHome,
		terminalKeyEnd,
		terminalKeyEnter,
		terminalKeyEnter,
		terminalKeyQuit,
		terminalKeyQuit,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected keys: %#v", got)
	}
}

func TestKeyDecoderDistinguishesSpaceFromPageDown(t *testing.T) {
	decoder := keyDecoder{}
	got := decoder.Feed([]byte{' ', ']', 0x04})
	want := []terminalKey{terminalKeySpace, terminalKeyPageDown, terminalKeyPageDown}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected space/page-down keys: %#v", got)
	}
}

func TestKeyDecoderFlushesStandaloneEscapeAsBack(t *testing.T) {
	decoder := keyDecoder{}
	if got := decoder.Feed([]byte{0x1b}); len(got) != 0 {
		t.Fatalf("standalone escape should wait briefly: %#v", got)
	}
	if got := decoder.FlushEscape(); !reflect.DeepEqual(got, []terminalKey{terminalKeyBack}) {
		t.Fatalf("unexpected flushed key: %#v", got)
	}
}

func TestKeyDecoderSupportsFragmentedTerminalSequences(t *testing.T) {
	decoder := keyDecoder{}
	if got := decoder.Feed([]byte("\x1b[")); len(got) != 0 {
		t.Fatalf("incomplete sequence produced keys: %#v", got)
	}
	got := decoder.Feed([]byte("A\x1b[B\x1b[5~\x1b[6~\x1bOH\x1bOF"))
	want := []terminalKey{
		terminalKeyUp,
		terminalKeyDown,
		terminalKeyPageUp,
		terminalKeyPageDown,
		terminalKeyHome,
		terminalKeyEnd,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected terminal keys: %#v", got)
	}
}

func TestKeyDecoderIgnoresUnknownInput(t *testing.T) {
	decoder := keyDecoder{}
	if got := decoder.Feed([]byte("xyz\x1b[Z\x1b[15~")); len(got) != 0 {
		t.Fatalf("unexpected keys: %#v", got)
	}
}

func TestKeyDecoderEventsPreserveCommandTextAndBackspace(t *testing.T) {
	decoder := keyDecoder{}
	events := decoder.FeedEvents([]byte("a6005\x7f19\r"))
	if len(events) != 9 {
		t.Fatalf("unexpected event count: %#v", events)
	}
	for index, value := range []string{"a", "6", "0", "0", "5"} {
		if events[index].Key != terminalKeyNone || events[index].Text != value {
			t.Fatalf("event %d: %#v", index, events[index])
		}
	}
	if events[5].Key != terminalKeyBackspace || events[5].Text != "" {
		t.Fatalf("expected backspace event: %#v", events[5])
	}
	if events[6].Text != "1" || events[7].Text != "9" || events[8].Key != terminalKeyEnter {
		t.Fatalf("unexpected tail events: %#v", events[6:])
	}
}

func TestKeyDecoderEventsPreservePrintableNavigationShortcuts(t *testing.T) {
	decoder := keyDecoder{}
	events := decoder.FeedEvents([]byte("gjkb[]qQ "))
	wantKeys := []terminalKey{
		terminalKeyHome,
		terminalKeyDown,
		terminalKeyUp,
		terminalKeyPageUp,
		terminalKeyPageUp,
		terminalKeyPageDown,
		terminalKeyQuit,
		terminalKeyQuit,
		terminalKeySpace,
	}
	wantText := []string{"g", "j", "k", "b", "[", "]", "q", "Q", " "}
	if len(events) != len(wantKeys) {
		t.Fatalf("unexpected event count: %#v", events)
	}
	for index := range events {
		if events[index].Key != wantKeys[index] || events[index].Text != wantText[index] {
			t.Fatalf("event %d: %#v", index, events[index])
		}
	}
}

func TestKeyDecoderControlShortcutsDoNotBecomeCommandText(t *testing.T) {
	decoder := keyDecoder{}
	events := decoder.FeedEvents([]byte{0x03, 0x04, 0x15})
	if len(events) != 3 {
		t.Fatalf("unexpected event count: %#v", events)
	}
	for index, event := range events {
		if event.Text != "" {
			t.Fatalf("control event %d became text: %#v", index, event)
		}
	}
}

func TestKeyDecoderEventsPreserveUTF8Bytes(t *testing.T) {
	decoder := keyDecoder{}
	events := decoder.FeedEvents([]byte("贵州茅台"))
	var got []byte
	for _, event := range events {
		if event.Key != terminalKeyNone {
			t.Fatalf("unexpected key event: %#v", event)
		}
		got = append(got, []byte(event.Text)...)
	}
	if string(got) != "贵州茅台" {
		t.Fatalf("UTF-8 text was not preserved: %q", string(got))
	}
}

func TestKeyDecoderWaitsForCompleteUTF8Rune(t *testing.T) {
	decoder := keyDecoder{}
	encoded := []byte("石")
	if events := decoder.FeedEvents(encoded[:2]); len(events) != 0 {
		t.Fatalf("fragmented UTF-8 rune produced events: %#v", events)
	}
	events := decoder.FeedEvents(encoded[2:])
	if len(events) != 1 || events[0].Text != "石" || events[0].Key != terminalKeyNone {
		t.Fatalf("unexpected completed UTF-8 event: %#v", events)
	}
}

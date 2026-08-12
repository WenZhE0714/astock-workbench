package app

import (
	"context"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

type terminalKey int

const (
	terminalKeyNone terminalKey = iota
	terminalKeyUp
	terminalKeyDown
	terminalKeyPageUp
	terminalKeyPageDown
	terminalKeyHome
	terminalKeyEnd
	terminalKeyEnter
	terminalKeyBack
	terminalKeyBackspace
	terminalKeySpace
	terminalKeyQuit
)

type terminalEvent struct {
	Key  terminalKey
	Text string
}

type keyDecoder struct {
	pending []byte
}

func plainKey(value byte) terminalKey {
	switch value {
	case 'k':
		return terminalKeyUp
	case 'j':
		return terminalKeyDown
	case 'b', 0x15:
		return terminalKeyPageUp
	case ']', 0x04:
		return terminalKeyPageDown
	case '[':
		return terminalKeyPageUp
	case ' ':
		return terminalKeySpace
	case 'g':
		return terminalKeyHome
	case 'G':
		return terminalKeyEnd
	case '\r', '\n':
		return terminalKeyEnter
	case 'q', 'Q', 0x03:
		return terminalKeyQuit
	default:
		return terminalKeyNone
	}
}

func (decoder *keyDecoder) hasLoneEscape() bool {
	return len(decoder.pending) == 1 && decoder.pending[0] == 0x1b
}

func (decoder *keyDecoder) FlushEscape() []terminalKey {
	if !decoder.hasLoneEscape() {
		return nil
	}
	decoder.pending = decoder.pending[:0]
	return []terminalKey{terminalKeyBack}
}

func (decoder *keyDecoder) FlushEscapeEvents() []terminalEvent {
	keys := decoder.FlushEscape()
	result := make([]terminalEvent, 0, len(keys))
	for _, key := range keys {
		result = append(result, terminalEvent{Key: key})
	}
	return result
}

func csiKey(value byte) terminalKey {
	switch value {
	case 'A':
		return terminalKeyUp
	case 'B':
		return terminalKeyDown
	case 'H':
		return terminalKeyHome
	case 'F':
		return terminalKeyEnd
	default:
		return terminalKeyNone
	}
}

func tildeKey(value string) terminalKey {
	switch value {
	case "1", "7":
		return terminalKeyHome
	case "4", "8":
		return terminalKeyEnd
	case "5":
		return terminalKeyPageUp
	case "6":
		return terminalKeyPageDown
	default:
		return terminalKeyNone
	}
}

func (decoder *keyDecoder) Feed(input []byte) []terminalKey {
	events := decoder.FeedEvents(input)
	result := make([]terminalKey, 0, len(events))
	for _, event := range events {
		if event.Key != terminalKeyNone {
			result = append(result, event.Key)
		}
	}
	return result
}

func (decoder *keyDecoder) FeedEvents(input []byte) []terminalEvent {
	decoder.pending = append(decoder.pending, input...)
	result := make([]terminalEvent, 0, len(input))
	for len(decoder.pending) > 0 {
		if decoder.pending[0] != 0x1b {
			value := decoder.pending[0]
			if value >= utf8.RuneSelf {
				if !utf8.FullRune(decoder.pending) {
					break
				}
				_, size := utf8.DecodeRune(decoder.pending)
				if size > 1 {
					result = append(result, terminalEvent{Text: string(decoder.pending[:size])})
					decoder.pending = decoder.pending[size:]
					continue
				}
			}
			if key := plainKey(value); key != terminalKeyNone {
				event := terminalEvent{Key: key}
				if value >= 0x20 {
					event.Text = string([]byte{value})
				}
				result = append(result, event)
			} else if value == 0x08 || value == 0x7f {
				result = append(result, terminalEvent{Key: terminalKeyBackspace})
			} else if value >= 0x20 {
				result = append(result, terminalEvent{Text: string([]byte{value})})
			}
			decoder.pending = decoder.pending[1:]
			continue
		}
		if len(decoder.pending) < 2 {
			break
		}
		if decoder.pending[1] == 'O' {
			if len(decoder.pending) < 3 {
				break
			}
			if key := csiKey(decoder.pending[2]); key != terminalKeyNone {
				result = append(result, terminalEvent{Key: key})
			}
			decoder.pending = decoder.pending[3:]
			continue
		}
		if decoder.pending[1] != '[' {
			decoder.pending = decoder.pending[1:]
			continue
		}
		if len(decoder.pending) < 3 {
			break
		}
		if key := csiKey(decoder.pending[2]); key != terminalKeyNone {
			result = append(result, terminalEvent{Key: key})
			decoder.pending = decoder.pending[3:]
			continue
		}
		if decoder.pending[2] < '0' || decoder.pending[2] > '9' {
			decoder.pending = decoder.pending[3:]
			continue
		}
		terminator := -1
		for index := 3; index < len(decoder.pending); index++ {
			if decoder.pending[index] == '~' {
				terminator = index
				break
			}
			if decoder.pending[index] < '0' || decoder.pending[index] > '9' {
				terminator = index
				break
			}
		}
		if terminator == -1 {
			break
		}
		if decoder.pending[terminator] == '~' {
			if key := tildeKey(string(decoder.pending[2:terminator])); key != terminalKeyNone {
				result = append(result, terminalEvent{Key: key})
			}
		}
		decoder.pending = decoder.pending[terminator+1:]
	}
	return result
}

func startTerminalEvents(ctx context.Context, input *os.File, enabled bool) (<-chan terminalEvent, func() error, error) {
	if !enabled || input == nil || !term.IsTerminal(int(input.Fd())) {
		return nil, func() error { return nil }, nil
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return nil, nil, err
	}
	var restoreOnce sync.Once
	var restoreError error
	restore := func() error {
		restoreOnce.Do(func() {
			restoreError = term.Restore(int(input.Fd()), state)
		})
		return restoreError
	}
	keys := make(chan terminalEvent, 64)
	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 4)
	go func() {
		buffer := make([]byte, 32)
		for {
			count, readError := input.Read(buffer)
			result := readResult{err: readError}
			if count > 0 {
				result.data = append([]byte(nil), buffer[:count]...)
			}
			select {
			case reads <- result:
			case <-ctx.Done():
				return
			}
			if readError != nil {
				return
			}
		}
	}()
	go func() {
		defer close(keys)
		decoder := keyDecoder{}
		var escapeTimer *time.Timer
		var escapeTimerChannel <-chan time.Time
		stopEscapeTimer := func() {
			if escapeTimer != nil && !escapeTimer.Stop() {
				select {
				case <-escapeTimer.C:
				default:
				}
			}
			escapeTimerChannel = nil
		}
		armEscapeTimer := func() {
			if !decoder.hasLoneEscape() {
				return
			}
			if escapeTimer == nil {
				escapeTimer = time.NewTimer(30 * time.Millisecond)
			} else {
				escapeTimer.Reset(30 * time.Millisecond)
			}
			escapeTimerChannel = escapeTimer.C
		}
		emit := func(values []terminalEvent) bool {
			for _, event := range values {
				select {
				case keys <- event:
				case <-ctx.Done():
					return false
				}
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				stopEscapeTimer()
				return
			case <-escapeTimerChannel:
				escapeTimerChannel = nil
				if !emit(decoder.FlushEscapeEvents()) {
					return
				}
			case result := <-reads:
				stopEscapeTimer()
				if len(result.data) > 0 && !emit(decoder.FeedEvents(result.data)) {
					return
				}
				armEscapeTimer()
				if result.err != nil {
					if result.err != io.EOF {
						emit([]terminalEvent{{Key: terminalKeyQuit}})
					}
					return
				}
			}
		}
	}()
	return keys, restore, nil
}

func startTerminalKeys(ctx context.Context, input *os.File, enabled bool) (<-chan terminalKey, func() error, error) {
	events, restore, err := startTerminalEvents(ctx, input, enabled)
	if err != nil || events == nil {
		return nil, restore, err
	}
	keys := make(chan terminalKey, 32)
	go func() {
		defer close(keys)
		for event := range events {
			if event.Key == terminalKeyNone {
				continue
			}
			select {
			case keys <- event.Key:
			case <-ctx.Done():
				return
			}
		}
	}()
	return keys, restore, nil
}

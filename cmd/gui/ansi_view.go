package main

import (
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type ansiTextView struct {
	maxLen      int
	maxSegments int

	entry *widget.Entry

	mu        sync.Mutex
	parser    *ansiParser
	pending   string
	flushT    *time.Timer
	flushEach time.Duration

	scrollT *time.Timer
}

func newANSITextView() *ansiTextView {
	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapOff
	entry.Scroll = fyne.ScrollBoth
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.SetPlaceHolder("")

	const maxLen = 300_000
	return &ansiTextView{
		maxLen:      maxLen,
		maxSegments: 4000,
		entry:       entry,
		parser:      newANSIParser(maxLen, 4000),
		flushEach:   50 * time.Millisecond,
	}
}

func (v *ansiTextView) Object() fyne.CanvasObject {
	return v.entry
}

func (v *ansiTextView) SetWrapping(wrapping fyne.TextWrap) {
	v.entry.Wrapping = wrapping
	v.entry.Refresh()
}

func (v *ansiTextView) SetText(s string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.pending = ""
	v.parser = newANSIParser(v.maxLen, v.maxSegments)
	v.parser.Append(sanitizeTerminalText(s))
	v.renderLocked()
}

func (v *ansiTextView) Text() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.entry.Text
}

func (v *ansiTextView) Clear() {
	v.SetText("")
}

func (v *ansiTextView) Append(s string) {
	if s == "" {
		return
	}

	chunk := sanitizeTerminalText(s)
	if chunk == "" {
		return
	}

	v.mu.Lock()
	v.pending += chunk
	if v.flushT == nil {
		v.flushT = time.AfterFunc(v.flushEach, func() {
			v.flush()
		})
	}
	v.mu.Unlock()
}

func (v *ansiTextView) flush() {
	v.mu.Lock()
	p := v.pending
	v.pending = ""
	v.flushT = nil
	if p != "" {
		v.parser.Append(p)
	}
	v.mu.Unlock()

	fyne.Do(func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.renderLocked()
	})
}

func (v *ansiTextView) renderLocked() {
	v.entry.SetText(stripANSIForCopy(v.parser.Text()))
	v.entry.CursorRow = len(strings.Split(v.entry.Text, "\n")) - 1
	v.entry.CursorColumn = 0
	v.entry.Refresh()

	// Some layouts update content size after refresh; schedule a second scroll-to-bottom
	// to keep the view pinned when output is large.
	if v.scrollT == nil {
		v.scrollT = time.AfterFunc(10*time.Millisecond, func() {
			fyne.Do(func() {
				v.entry.CursorRow = len(strings.Split(v.entry.Text, "\n")) - 1
				v.entry.CursorColumn = 0
				v.entry.Refresh()
				v.mu.Lock()
				v.scrollT = nil
				v.mu.Unlock()
			})
		})
	}
}

func sanitizeTerminalText(s string) string {
	if s == "" {
		return ""
	}
	// Normalize line endings and drop invalid UTF-8.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ToValidUTF8(s, "")

	// Filter control characters; keep ESC for ANSI and whitespace controls.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0:
			continue
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == 0x07:
			// BEL is used to terminate OSC sequences.
			b.WriteRune(r)
		case r == 0x1b:
			b.WriteRune(r)
		case r == 0x9b:
			// CSI in single-byte form. Convert to ESC[ so the parser can handle it.
			b.WriteByte(0x1b)
			b.WriteByte('[')
		case r == 0x7f:
			// DEL
			continue
		case r >= 0x80 && r <= 0x9f:
			// C1 control characters (OSC/DCS/etc. in single-byte form). Drop.
			continue
		case r < 0x20:
			// drop other C0 controls
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripANSIForCopy(s string) string {
	s = sanitizeTerminalText(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[':
			j := i + 2
			for j < len(s) {
				c := s[j]
				if c >= 0x40 && c <= 0x7e {
					break
				}
				j++
			}
			if j >= len(s) {
				return b.String()
			}
			i = j
		case ']':
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j++
					break
				}
				j++
			}
			if j >= len(s) {
				return b.String()
			}
			i = j
		case 'P', '^', '_':
			j := i + 2
			for j < len(s) {
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j++
					break
				}
				j++
			}
			if j >= len(s) {
				return b.String()
			}
			i = j
		case '(', ')', '*', '+':
			if i+2 >= len(s) {
				return b.String()
			}
			i += 2
		default:
			i++
		}
	}
	return b.String()
}

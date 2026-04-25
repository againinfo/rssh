package main

import (
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type ansiTextView struct {
	maxLen      int
	maxSegments int

	rt     *widget.RichText
	scroll *container.Scroll

	mu        sync.Mutex
	parser    *ansiParser
	pending   string
	flushT    *time.Timer
	flushEach time.Duration

	scrollT *time.Timer
}

func newANSITextView() *ansiTextView {
	rt := widget.NewRichText()
	rt.Wrapping = fyne.TextWrapWord
	rt.Scroll = fyne.ScrollBoth

	scroll := container.NewScroll(rt)
	scroll.Direction = container.ScrollBoth

	const maxLen = 300_000
	return &ansiTextView{
		maxLen:      maxLen,
		maxSegments: 4000,
		rt:          rt,
		scroll:      scroll,
		parser:      newANSIParser(maxLen, 4000),
		flushEach:   50 * time.Millisecond,
	}
}

func (v *ansiTextView) Object() fyne.CanvasObject {
	return v.scroll
}

func (v *ansiTextView) SetWrapping(wrapping fyne.TextWrap) {
	v.rt.Wrapping = wrapping
	v.rt.Refresh()
}

func (v *ansiTextView) SetText(s string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.pending = ""
	v.parser = newANSIParser(v.maxLen, v.maxSegments)
	v.parser.Append(sanitizeTerminalText(s))
	v.renderLocked()
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
	segments := v.parser.Segments()
	if len(segments) == 0 {
		segments = []widget.RichTextSegment{&widget.TextSegment{
			Text: "",
			Style: widget.RichTextStyle{
				Inline:    true,
				ColorName: ansiFgDefault,
				TextStyle: fyne.TextStyle{Monospace: true},
			},
		}}
	}
	v.rt.Segments = segments
	v.rt.Refresh()
	v.scroll.Refresh()
	v.scroll.ScrollToBottom()
	v.scroll.Refresh()

	// Some layouts update content size after refresh; schedule a second scroll-to-bottom
	// to keep the view pinned when output is large.
	if v.scrollT == nil {
		v.scrollT = time.AfterFunc(10*time.Millisecond, func() {
			fyne.Do(func() {
				v.scroll.ScrollToBottom()
				v.scroll.Refresh()
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

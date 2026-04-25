package main

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type shellInput struct {
	widget.Entry

	mu     sync.RWMutex
	sender func([]byte)
}

func newShellInput() *shellInput {
	e := &shellInput{}
	e.ExtendBaseWidget(e)
	e.Wrapping = fyne.TextWrapOff
	e.MultiLine = false
	return e
}

func (s *shellInput) SetSender(fn func([]byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sender = fn
	s.SetText("")
}

func (s *shellInput) send(b []byte) {
	s.mu.RLock()
	fn := s.sender
	s.mu.RUnlock()
	if fn != nil && len(b) > 0 {
		fn(b)
	}
}

func (s *shellInput) TypedRune(r rune) {
	// Keep local feedback in the input box, while also sending to remote.
	// Note: this is not a terminal prompt; output echoes in the main pane.
	s.Entry.TypedRune(r)
	s.send([]byte(string(r)))
}

func (s *shellInput) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		// Clear local line when the user submits.
		s.Entry.SetText("")
		s.send([]byte("\n"))
	case fyne.KeyTab:
		s.Entry.TypedKey(ev)
		s.send([]byte("\t"))
	case fyne.KeyBackspace:
		s.Entry.TypedKey(ev)
		s.send([]byte{0x7f})
	case fyne.KeyEscape:
		s.send([]byte{0x1b})

	case fyne.KeyUp:
		s.send([]byte("\x1b[A"))
	case fyne.KeyDown:
		s.send([]byte("\x1b[B"))
	case fyne.KeyRight:
		s.send([]byte("\x1b[C"))
	case fyne.KeyLeft:
		s.send([]byte("\x1b[D"))
	case fyne.KeyHome:
		s.send([]byte("\x1b[H"))
	case fyne.KeyEnd:
		s.send([]byte("\x1b[F"))
	case fyne.KeyDelete:
		s.send([]byte("\x1b[3~"))
	case fyne.KeyPageUp:
		s.send([]byte("\x1b[5~"))
	case fyne.KeyPageDown:
		s.send([]byte("\x1b[6~"))
	default:
		// Ignore other keys (Ctrl combos etc.) for now.
	}
}

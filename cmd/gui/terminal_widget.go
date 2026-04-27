package main

import (
	"bytes"
	"errors"
	"image/color"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type textGridStyle struct {
	fg color.Color
	bg color.Color
	ts fyne.TextStyle
}

func (s textGridStyle) Style() fyne.TextStyle        { return s.ts }
func (s textGridStyle) TextColor() color.Color       { return s.fg }
func (s textGridStyle) BackgroundColor() color.Color { return s.bg }

type termStyle struct {
	bold bool
	fg   int // -1 default, 0-7
	bg   int // -1 default, 0-7
	fgHi bool
	bgHi bool
}

func (s termStyle) toGridStyle() widget.TextGridStyle {
	fg := ansiColor(s.fg, s.fgHi || s.bold, false)
	bg := ansiColor(s.bg, s.bgHi, true)
	return textGridStyle{
		fg: fg,
		bg: bg,
		ts: fyne.TextStyle{Monospace: true},
	}
}

func ansiColor(code int, bright bool, isBackground bool) color.Color {
	// Match palette from ansi_theme.go, but with a sensible default background.
	if code < 0 {
		if isBackground {
			return color.NRGBA{R: 15, G: 23, B: 42, A: 255} // slate-900-ish
		}
		return theme.ForegroundColor()
	}

	switch code {
	case 0:
		if bright {
			return color.NRGBA{R: 100, G: 116, B: 139, A: 255}
		}
		return color.NRGBA{R: 15, G: 23, B: 42, A: 255}
	case 1:
		if bright {
			return color.NRGBA{R: 248, G: 113, B: 113, A: 255}
		}
		return color.NRGBA{R: 220, G: 38, B: 38, A: 255}
	case 2:
		if bright {
			return color.NRGBA{R: 74, G: 222, B: 128, A: 255}
		}
		return color.NRGBA{R: 22, G: 163, B: 74, A: 255}
	case 3:
		if bright {
			return color.NRGBA{R: 250, G: 204, B: 21, A: 255}
		}
		return color.NRGBA{R: 202, G: 138, B: 4, A: 255}
	case 4:
		if bright {
			return color.NRGBA{R: 96, G: 165, B: 250, A: 255}
		}
		return color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	case 5:
		if bright {
			return color.NRGBA{R: 232, G: 121, B: 249, A: 255}
		}
		return color.NRGBA{R: 192, G: 38, B: 211, A: 255}
	case 6:
		if bright {
			return color.NRGBA{R: 34, G: 211, B: 238, A: 255}
		}
		return color.NRGBA{R: 8, G: 145, B: 178, A: 255}
	case 7:
		if bright {
			return color.NRGBA{R: 248, G: 250, B: 252, A: 255}
		}
		return color.NRGBA{R: 226, G: 232, B: 240, A: 255}
	default:
		if isBackground {
			return color.NRGBA{R: 15, G: 23, B: 42, A: 255}
		}
		return theme.ForegroundColor()
	}
}

type terminalEmu struct {
	cols     int
	maxLines int

	style     termStyle
	decLineG0 bool

	// Buffer lines, plus current editable line.
	lines [][]widget.TextGridCell
	cur   []widget.TextGridCell
	row   int
	col   int

	escBuf  bytes.Buffer
	inEsc   bool
	utf8Buf []byte
}

func newTerminalEmu(cols, maxLines int) *terminalEmu {
	if cols <= 0 {
		cols = 120
	}
	if maxLines <= 0 {
		maxLines = 5000
	}
	e := &terminalEmu{
		cols:     cols,
		maxLines: maxLines,
		style:    termStyle{fg: -1, bg: -1},
	}
	e.cur = e.blankLine()
	e.lines = [][]widget.TextGridCell{e.cur}
	return e
}

func (e *terminalEmu) blankLine() []widget.TextGridCell {
	cells := make([]widget.TextGridCell, e.cols)
	style := e.style.toGridStyle()
	for i := range cells {
		cells[i] = widget.TextGridCell{Rune: ' ', Style: style}
	}
	return cells
}

func (e *terminalEmu) ensureCursor() {
	if e.row < 0 {
		e.row = 0
	}
	if e.row >= len(e.lines) {
		for len(e.lines) <= e.row {
			e.lines = append(e.lines, e.blankLine())
		}
	}
	if e.col < 0 {
		e.col = 0
	}
	if e.col >= e.cols {
		e.col = e.cols - 1
	}
}

func (e *terminalEmu) newline() {
	e.col = 0
	e.row++
	if e.row >= len(e.lines) {
		e.lines = append(e.lines, e.blankLine())
	}
	if len(e.lines) > e.maxLines {
		drop := len(e.lines) - e.maxLines
		e.lines = e.lines[drop:]
		e.row -= drop
		if e.row < 0 {
			e.row = 0
		}
	}
}

func (e *terminalEmu) writeRune(r rune) {
	if e.decLineG0 {
		r = decSpecialGraphicsRune(r)
	}
	e.ensureCursor()
	if r == '\n' {
		e.newline()
		return
	}
	if r == '\r' {
		e.col = 0
		return
	}
	if r == '\b' {
		if e.col > 0 {
			e.col--
		}
		return
	}
	if r == '\t' {
		next := (e.col/8 + 1) * 8
		for e.col < next {
			e.writeRune(' ')
		}
		return
	}

	if r < 0x20 {
		return
	}
	if r == 0x7f {
		return
	}

	if e.col >= e.cols {
		e.newline()
	}
	style := e.style.toGridStyle()
	e.lines[e.row][e.col] = widget.TextGridCell{Rune: r, Style: style}
	e.col++
	if e.col >= e.cols {
		// hard wrap
		e.newline()
	}
}

func decSpecialGraphicsRune(r rune) rune {
	switch r {
	case '`':
		return '◆'
	case 'a':
		return '▒'
	case 'f':
		return '°'
	case 'g':
		return '±'
	case 'j':
		return '┘'
	case 'k':
		return '┐'
	case 'l':
		return '┌'
	case 'm':
		return '└'
	case 'n':
		return '┼'
	case 'o':
		return '⎺'
	case 'p':
		return '⎻'
	case 'q':
		return '─'
	case 'r':
		return '⎼'
	case 's':
		return '⎽'
	case 't':
		return '├'
	case 'u':
		return '┤'
	case 'v':
		return '┴'
	case 'w':
		return '┬'
	case 'x':
		return '│'
	case 'y':
		return '≤'
	case 'z':
		return '≥'
	case '{':
		return 'π'
	case '|':
		return '≠'
	case '}':
		return '£'
	case '~':
		return '·'
	default:
		return r
	}
}

func (e *terminalEmu) eraseLineFromCursor() {
	e.ensureCursor()
	style := e.style.toGridStyle()
	for c := e.col; c < e.cols; c++ {
		e.lines[e.row][c] = widget.TextGridCell{Rune: ' ', Style: style}
	}
}

func (e *terminalEmu) clearScreen() {
	e.lines = [][]widget.TextGridCell{e.blankLine()}
	e.row = 0
	e.col = 0
}

func (e *terminalEmu) applySGR(params string) {
	if params == "" {
		e.style = termStyle{fg: -1, bg: -1}
		return
	}
	parts := strings.Split(params, ";")
	for _, p := range parts {
		if p == "" {
			p = "0"
		}
		n := 0
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				n = -1
				break
			}
			n = n*10 + int(ch-'0')
		}
		if n < 0 {
			continue
		}
		switch {
		case n == 0:
			e.style = termStyle{fg: -1, bg: -1}
		case n == 1:
			e.style.bold = true
		case n == 22:
			e.style.bold = false
		case n == 39:
			e.style.fg = -1
			e.style.fgHi = false
		case n == 49:
			e.style.bg = -1
			e.style.bgHi = false
		case 30 <= n && n <= 37:
			e.style.fg = n - 30
			e.style.fgHi = false
		case 40 <= n && n <= 47:
			e.style.bg = n - 40
			e.style.bgHi = false
		case 90 <= n && n <= 97:
			e.style.fg = n - 90
			e.style.fgHi = true
		case 100 <= n && n <= 107:
			e.style.bg = n - 100
			e.style.bgHi = true
		}
	}
}

func (e *terminalEmu) handleCSI(seq string) {
	if seq == "" {
		return
	}
	final := seq[len(seq)-1]
	params := seq[:len(seq)-1]
	switch final {
	case 'm':
		e.applySGR(params)
	case 'K':
		// Erase in line
		// 0: cursor to end
		if params == "" || params == "0" {
			e.eraseLineFromCursor()
		}
	case 'J':
		// Clear screen
		if params == "" || params == "2" {
			e.clearScreen()
		}
	case 'H', 'f':
		// Cursor position (row;col), 1-based
		r, c := 1, 1
		if params != "" {
			parts := strings.Split(params, ";")
			if len(parts) >= 1 && parts[0] != "" {
				if n, err := strconvAtoi(parts[0]); err == nil && n > 0 {
					r = n
				}
			}
			if len(parts) >= 2 && parts[1] != "" {
				if n, err := strconvAtoi(parts[1]); err == nil && n > 0 {
					c = n
				}
			}
		}
		e.row = r - 1
		e.col = c - 1
		e.ensureCursor()
	case 'A': // up
		n := 1
		if params != "" {
			if v, err := strconvAtoi(params); err == nil && v > 0 {
				n = v
			}
		}
		e.row -= n
		e.ensureCursor()
	case 'B': // down
		n := 1
		if params != "" {
			if v, err := strconvAtoi(params); err == nil && v > 0 {
				n = v
			}
		}
		e.row += n
		e.ensureCursor()
	case 'C': // right
		n := 1
		if params != "" {
			if v, err := strconvAtoi(params); err == nil && v > 0 {
				n = v
			}
		}
		e.col += n
		e.ensureCursor()
	case 'D': // left
		n := 1
		if params != "" {
			if v, err := strconvAtoi(params); err == nil && v > 0 {
				n = v
			}
		}
		e.col -= n
		e.ensureCursor()
	}
}

func (e *terminalEmu) writeBytes(b []byte) {
	// sanitize and keep BEL/ESC/C1 CSI
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	if len(e.utf8Buf) > 0 {
		prefixed := make([]byte, 0, len(e.utf8Buf)+len(b))
		prefixed = append(prefixed, e.utf8Buf...)
		prefixed = append(prefixed, b...)
		b = prefixed
		e.utf8Buf = nil
	}

	for len(b) > 0 {
		if e.inEsc {
			e.escBuf.WriteByte(b[0])
			seq := e.escBuf.String()
			// Handle ESC sequences; support OSC terminated by BEL and CSI ending in 0x40-0x7e.
			if strings.HasPrefix(seq, "[") {
				// CSI: collect until final byte.
				last := seq[len(seq)-1]
				if last >= 0x40 && last <= 0x7e {
					e.handleCSI(seq[1:])
					e.inEsc = false
					e.escBuf.Reset()
				}
			} else if strings.HasPrefix(seq, "]") {
				// OSC: terminate by BEL or ST (ESC \)
				if b[0] == 0x07 {
					e.inEsc = false
					e.escBuf.Reset()
				} else if strings.HasSuffix(seq, "\x1b\\") {
					e.inEsc = false
					e.escBuf.Reset()
				}
			} else if strings.HasPrefix(seq, "P") || strings.HasPrefix(seq, "^") || strings.HasPrefix(seq, "_") {
				// DCS/PM/APC: terminate by ST
				if strings.HasSuffix(seq, "\x1b\\") {
					e.inEsc = false
					e.escBuf.Reset()
				}
			} else if strings.HasPrefix(seq, "(") || strings.HasPrefix(seq, ")") || strings.HasPrefix(seq, "*") || strings.HasPrefix(seq, "+") {
				if len(seq) >= 2 {
					if seq[0] == '(' {
						switch seq[1] {
						case '0':
							e.decLineG0 = true
						case 'B', 'A', 'U':
							e.decLineG0 = false
						}
					}
					e.inEsc = false
					e.escBuf.Reset()
				}
			} else if len(seq) >= 2 {
				// Unknown 2-byte escape; drop.
				e.inEsc = false
				e.escBuf.Reset()
			}
			b = b[1:]
			continue
		}

		switch b[0] {
		case 0x1b:
			e.inEsc = true
			e.escBuf.Reset()
			b = b[1:]
			continue
		case 0x9b:
			// C1 CSI: treat as ESC[
			e.inEsc = true
			e.escBuf.Reset()
			e.escBuf.WriteByte('[')
			b = b[1:]
			continue
		default:
		}

		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			if !utf8.FullRune(b) {
				e.utf8Buf = append(e.utf8Buf[:0], b...)
				return
			}
			// drop invalid byte
			b = b[1:]
			continue
		}
		// keep BEL as part of sequences; otherwise ignore
		if r == 0x07 {
			b = b[size:]
			continue
		}
		e.writeRune(r)
		b = b[size:]
	}
}

func strconvAtoi(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("invalid")
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

type terminalInputCatcher struct {
	widget.BaseWidget
	send    func([]byte)
	onFocus func()
}

func newTerminalInputCatcher() *terminalInputCatcher {
	c := &terminalInputCatcher{}
	c.ExtendBaseWidget(c)
	return c
}

func (c *terminalInputCatcher) CreateRenderer() fyne.WidgetRenderer {
	// Invisible overlay.
	return widget.NewSimpleRenderer(widget.NewLabel(""))
}

func (c *terminalInputCatcher) FocusGained() {}
func (c *terminalInputCatcher) FocusLost()   {}
func (c *terminalInputCatcher) TypedRune(r rune) {
	if c.send == nil {
		return
	}
	c.send([]byte(string(r)))
}

func (c *terminalInputCatcher) TypedKey(ev *fyne.KeyEvent) {
	if c.send == nil {
		return
	}
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		c.send([]byte("\n"))
	case fyne.KeyTab:
		c.send([]byte("\t"))
	case fyne.KeyBackspace:
		c.send([]byte{0x7f})
	case fyne.KeyEscape:
		c.send([]byte{0x1b})
	case fyne.KeyUp:
		c.send([]byte("\x1b[A"))
	case fyne.KeyDown:
		c.send([]byte("\x1b[B"))
	case fyne.KeyRight:
		c.send([]byte("\x1b[C"))
	case fyne.KeyLeft:
		c.send([]byte("\x1b[D"))
	case fyne.KeyHome:
		c.send([]byte("\x1b[H"))
	case fyne.KeyEnd:
		c.send([]byte("\x1b[F"))
	case fyne.KeyDelete:
		c.send([]byte("\x1b[3~"))
	case fyne.KeyPageUp:
		c.send([]byte("\x1b[5~"))
	case fyne.KeyPageDown:
		c.send([]byte("\x1b[6~"))
	}
}

func (c *terminalInputCatcher) Tapped(*fyne.PointEvent) {
	if c.onFocus != nil {
		c.onFocus()
	}
}

func (c *terminalInputCatcher) TappedSecondary(*fyne.PointEvent) {
	if c.onFocus != nil {
		c.onFocus()
	}
}

type terminalWidget struct {
	grid    *widget.TextGrid
	catcher *terminalInputCatcher

	mu      sync.Mutex
	emu     *terminalEmu
	pending bytes.Buffer
	flushT  *time.Timer
}

func newTerminalWidget(cols int) *terminalWidget {
	g := widget.NewTextGrid()
	g.Scroll = fyne.ScrollBoth
	g.SetText("")

	t := &terminalWidget{
		grid:    g,
		catcher: newTerminalInputCatcher(),
		emu:     newTerminalEmu(cols, 5000),
	}
	t.catcher.onFocus = func() {
		// Focus the catcher itself.
		if c := fyne.CurrentApp(); c != nil {
			// no-op
		}
	}
	return t
}

func (t *terminalWidget) Object(w fyne.Window) fyne.CanvasObject {
	t.catcher.onFocus = func() {
		w.Canvas().Focus(t.catcher)
	}
	return container.NewMax(t.grid, t.catcher)
}

func (t *terminalWidget) SetSender(w fyne.Window, send func([]byte)) {
	t.catcher.send = send
	if send != nil {
		w.Canvas().Focus(t.catcher)
	}
}

func (t *terminalWidget) Clear() {
	t.mu.Lock()
	t.emu = newTerminalEmu(t.emu.cols, t.emu.maxLines)
	t.pending.Reset()
	if t.flushT != nil {
		t.flushT.Stop()
		t.flushT = nil
	}
	t.mu.Unlock()

	fyne.Do(func() {
		t.render()
	})
}

func (t *terminalWidget) AppendOutput(b []byte) {
	if len(b) == 0 {
		return
	}
	t.mu.Lock()
	_, _ = t.pending.Write(b)
	if t.flushT == nil {
		t.flushT = time.AfterFunc(40*time.Millisecond, func() {
			t.flush()
		})
	}
	t.mu.Unlock()
}

func (t *terminalWidget) flush() {
	t.mu.Lock()
	data := t.pending.Bytes()
	if len(data) == 0 {
		t.flushT = nil
		t.mu.Unlock()
		return
	}
	cpy := make([]byte, len(data))
	copy(cpy, data)
	t.pending.Reset()
	t.flushT = nil
	t.emu.writeBytes(cpy)
	t.mu.Unlock()

	fyne.Do(func() {
		t.render()
	})
}

func (t *terminalWidget) render() {
	t.mu.Lock()
	lines := t.emu.lines
	t.mu.Unlock()

	// Replace entire grid. This is fast enough for moderate output and avoids complex diffs.
	rows := make([]widget.TextGridRow, 0, len(lines))
	for _, ln := range lines {
		rows = append(rows, widget.TextGridRow{Cells: ln})
	}
	t.grid.Rows = rows
	t.grid.Refresh()
	t.grid.ScrollToBottom()
}

func (t *terminalWidget) PlainText() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var b strings.Builder
	for _, row := range t.emu.lines {
		line := make([]rune, len(row))
		for i, cell := range row {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			line[i] = r
		}
		b.WriteString(strings.TrimRight(string(line), " "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// Ensure interface satisfaction
var _ fyne.Focusable = (*terminalInputCatcher)(nil)
var _ fyne.Tappable = (*terminalInputCatcher)(nil)
var _ fyne.SecondaryTappable = (*terminalInputCatcher)(nil)

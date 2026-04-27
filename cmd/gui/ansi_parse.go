package main

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type ansiStyle struct {
	bold bool
	fg   int // -1 default, 0-7
	hi   bool
}

func (s ansiStyle) colorName() fyne.ThemeColorName {
	if s.fg < 0 {
		return ansiFgDefault
	}

	bright := s.hi || s.bold
	switch s.fg {
	case 0:
		if bright {
			return ansiFgBrightBlack
		}
		return ansiFgBlack
	case 1:
		if bright {
			return ansiFgBrightRed
		}
		return ansiFgRed
	case 2:
		if bright {
			return ansiFgBrightGreen
		}
		return ansiFgGreen
	case 3:
		if bright {
			return ansiFgBrightYellow
		}
		return ansiFgYellow
	case 4:
		if bright {
			return ansiFgBrightBlue
		}
		return ansiFgBlue
	case 5:
		if bright {
			return ansiFgBrightMagenta
		}
		return ansiFgMagenta
	case 6:
		if bright {
			return ansiFgBrightCyan
		}
		return ansiFgCyan
	case 7:
		if bright {
			return ansiFgBrightWhite
		}
		return ansiFgWhite
	default:
		return ansiFgDefault
	}
}

func parseANSISegments(s string) []widget.RichTextSegment {
	if s == "" {
		return nil
	}

	style := ansiStyle{fg: -1}
	var segs []widget.RichTextSegment
	var cur strings.Builder

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		segs = append(segs, &widget.TextSegment{
			Text: cur.String(),
			Style: widget.RichTextStyle{
				Inline:    true,
				ColorName: style.colorName(),
				TextStyle: fyne.TextStyle{Monospace: true},
			},
		})
		cur.Reset()
	}

	applySGR := func(params string) {
		if params == "" {
			// ESC[m == reset
			style = ansiStyle{fg: -1}
			return
		}
		parts := strings.Split(params, ";")
		for _, p := range parts {
			if p == "" {
				p = "0"
			}
			n, err := strconv.Atoi(p)
			if err != nil {
				continue
			}
			switch {
			case n == 0:
				style = ansiStyle{fg: -1}
			case n == 1:
				style.bold = true
			case n == 22:
				style.bold = false
			case n == 39:
				style.fg = -1
				style.hi = false
			case 30 <= n && n <= 37:
				style.fg = n - 30
				style.hi = false
			case 90 <= n && n <= 97:
				style.fg = n - 90
				style.hi = true
			}
		}
	}

	// Simple ANSI parser: keep text, apply SGR colors, drop other escapes.
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			cur.WriteByte(s[i])
			continue
		}

		// ESC sequence. Flush pending text.
		flush()

		if i+1 >= len(s) {
			break
		}
		if s[i+1] != '[' {
			// not CSI; drop.
			continue
		}

		// CSI: ESC[ ... <final>
		j := i + 2
		for j < len(s) {
			c := s[j]
			// final byte in 0x40-0x7E range.
			if c >= 0x40 && c <= 0x7e {
				break
			}
			j++
		}
		if j >= len(s) {
			break
		}
		final := s[j]
		params := s[i+2 : j]
		if final == 'm' {
			applySGR(params)
		}
		// skip entire CSI
		i = j
	}

	flush()
	return segs
}

type ansiParser struct {
	maxLen      int
	maxSegments int

	style ansiStyle

	segs     []*widget.TextSegment
	totalLen int
	cur      strings.Builder

	remainder string
}

func newANSIParser(maxLen, maxSegments int) *ansiParser {
	return &ansiParser{
		maxLen:      maxLen,
		maxSegments: maxSegments,
		style:       ansiStyle{fg: -1},
	}
}

func (p *ansiParser) Segments() []widget.RichTextSegment {
	out := make([]widget.RichTextSegment, 0, len(p.segs)+1)
	for _, s := range p.segs {
		out = append(out, s)
	}
	if p.cur.Len() > 0 {
		out = append(out, p.makeSeg(p.cur.String(), p.style))
	}
	return out
}

func (p *ansiParser) Text() string {
	var b strings.Builder
	for _, s := range p.segs {
		b.WriteString(s.Text)
	}
	if p.cur.Len() > 0 {
		b.WriteString(p.cur.String())
	}
	return b.String()
}

func (p *ansiParser) Append(s string) {
	if s == "" {
		return
	}

	if p.remainder != "" {
		s = p.remainder + s
		p.remainder = ""
	}

	flush := func() {
		if p.cur.Len() == 0 {
			return
		}
		txt := p.cur.String()
		p.cur.Reset()
		p.push(txt, p.style)
	}

	applySGR := func(params string) {
		if params == "" {
			p.style = ansiStyle{fg: -1}
			return
		}
		parts := strings.Split(params, ";")
		for _, part := range parts {
			if part == "" {
				part = "0"
			}
			n, err := strconv.Atoi(part)
			if err != nil {
				continue
			}
			switch {
			case n == 0:
				p.style = ansiStyle{fg: -1}
			case n == 1:
				p.style.bold = true
			case n == 22:
				p.style.bold = false
			case n == 39:
				p.style.fg = -1
				p.style.hi = false
			case 30 <= n && n <= 37:
				p.style.fg = n - 30
				p.style.hi = false
			case 90 <= n && n <= 97:
				p.style.fg = n - 90
				p.style.hi = true
			}
		}
	}

	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			p.cur.WriteByte(s[i])
			continue
		}

		flush()
		if i+1 >= len(s) {
			p.remainder = s[i:]
			return
		}

		next := s[i+1]
		switch next {
		case '[': // CSI
			j := i + 2
			for j < len(s) {
				c := s[j]
				if c >= 0x40 && c <= 0x7e {
					break
				}
				j++
			}
			if j >= len(s) {
				p.remainder = s[i:]
				return
			}
			final := s[j]
			params := s[i+2 : j]
			if final == 'm' {
				applySGR(params)
			}
			i = j

		case ']': // OSC: ESC ] ... BEL or ST (ESC \)
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 { // BEL
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' { // ST
					j++ // include '\'
					break
				}
				j++
			}
			if j >= len(s) {
				p.remainder = s[i:]
				return
			}
			i = j

		case 'P', '^', '_': // DCS / PM / APC: ESC P|^|_ ... ST
			j := i + 2
			for j < len(s) {
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j++ // include '\'
					break
				}
				j++
			}
			if j >= len(s) {
				p.remainder = s[i:]
				return
			}
			i = j

		case '(', ')', '*', '+': // charset selection, typically 2 bytes: ESC ( X
			if i+2 >= len(s) {
				p.remainder = s[i:]
				return
			}
			i = i + 2
		default:
			// Unknown escape; drop ESC + next if present.
			i = i + 1
		}
	}

	// Don't flush `cur` here; it stays buffered for style carryover. But enforce limits.
	p.trim()
}

func (p *ansiParser) makeSeg(text string, style ansiStyle) *widget.TextSegment {
	return &widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			Inline:    true,
			ColorName: style.colorName(),
			TextStyle: fyne.TextStyle{Monospace: true},
		},
	}
}

func (p *ansiParser) push(text string, style ansiStyle) {
	if text == "" {
		return
	}
	seg := p.makeSeg(text, style)
	p.segs = append(p.segs, seg)
	p.totalLen += len(text)

	// Coalesce with previous if same style.
	if len(p.segs) >= 2 {
		prev := p.segs[len(p.segs)-2]
		if prev.Style.ColorName == seg.Style.ColorName && prev.Style.TextStyle == seg.Style.TextStyle {
			prev.Text += seg.Text
			p.segs = p.segs[:len(p.segs)-1]
		}
	}

	p.trim()
}

func (p *ansiParser) trim() {
	// Trim by segment count first to keep UI responsive.
	if p.maxSegments > 0 && len(p.segs) > p.maxSegments {
		drop := len(p.segs) - p.maxSegments
		for i := 0; i < drop; i++ {
			p.totalLen -= len(p.segs[i].Text)
		}
		p.segs = p.segs[drop:]
	}

	if p.maxLen <= 0 {
		return
	}

	// Include buffered text length in cap.
	total := p.totalLen + p.cur.Len()
	if total <= p.maxLen {
		return
	}
	excess := total - p.maxLen

	// Prefer trimming from the start of segs.
	for excess > 0 && len(p.segs) > 0 {
		if len(p.segs[0].Text) <= excess {
			excess -= len(p.segs[0].Text)
			p.totalLen -= len(p.segs[0].Text)
			p.segs = p.segs[1:]
			continue
		}
		p.segs[0].Text = p.segs[0].Text[excess:]
		p.totalLen -= excess
		excess = 0
	}

	// If still excess, trim current buffer (rare).
	if excess > 0 && p.cur.Len() > 0 {
		s := p.cur.String()
		if excess >= len(s) {
			p.cur.Reset()
		} else {
			p.cur.Reset()
			p.cur.WriteString(s[excess:])
		}
	}
}

package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SleepWindow struct {
	Enabled     bool
	StartMinute int // minutes since midnight
	EndMinute   int // minutes since midnight
	CrossesMid  bool
}

func ParseSleepWindow(s string) (SleepWindow, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SleepWindow{}, nil
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return SleepWindow{}, fmt.Errorf("expected HH:MM-HH:MM")
	}
	start, err := parseHHMM(parts[0])
	if err != nil {
		return SleepWindow{}, fmt.Errorf("start: %w", err)
	}
	end, err := parseHHMM(parts[1])
	if err != nil {
		return SleepWindow{}, fmt.Errorf("end: %w", err)
	}
	w := SleepWindow{
		Enabled:     true,
		StartMinute: start,
		EndMinute:   end,
		CrossesMid:  end < start,
	}
	// If equal, treat as disabled (0-length sleep).
	if w.StartMinute == w.EndMinute {
		return SleepWindow{}, nil
	}
	return w, nil
}

func (w SleepWindow) InWindow(t time.Time) bool {
	if !w.Enabled {
		return false
	}
	min := t.Hour()*60 + t.Minute()
	if !w.CrossesMid {
		return min >= w.StartMinute && min < w.EndMinute
	}
	return min >= w.StartMinute || min < w.EndMinute
}

func (w SleepWindow) UntilWake(now time.Time) time.Duration {
	if !w.InWindow(now) {
		return 0
	}
	// Compute next occurrence of end time in local time.
	endHour := w.EndMinute / 60
	endMin := w.EndMinute % 60
	endToday := time.Date(now.Year(), now.Month(), now.Day(), endHour, endMin, 0, 0, now.Location())

	if !w.CrossesMid {
		if endToday.After(now) {
			return endToday.Sub(now)
		}
		// Shouldn't happen due to InWindow, but be safe.
		return 0
	}

	// crosses midnight: if we're before end time, wake today; else wake tomorrow.
	if now.Before(endToday) {
		return endToday.Sub(now)
	}
	endTomorrow := endToday.Add(24 * time.Hour)
	return endTomorrow.Sub(now)
}

func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute")
	}
	return h*60 + m, nil
}

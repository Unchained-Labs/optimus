// Package format renders numbers and times compactly for terminal output.
package format

import (
	"fmt"
	"strings"
	"time"
)

func Money(v float64) string {
	switch {
	case v == 0:
		return "$0"
	case v < 0.01:
		return "<$0.01"
	case v < 1000:
		return fmt.Sprintf("$%.2f", v)
	case v < 100000:
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.1fk", v/1000)
}

func Tokens(n int64) string {
	f := float64(n)
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1e6:
		return fmt.Sprintf("%.1fk", f/1e3)
	case n < 1e9:
		return fmt.Sprintf("%.1fM", f/1e6)
	}
	return fmt.Sprintf("%.2fB", f/1e9)
}

// Ago renders a relative time like "5m", "3h", "2d".
func Ago(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return Dur(now.Sub(t))
}

func Dur(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if h < 10 && m > 0 {
			return fmt.Sprintf("%dh%02dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
}

// Bar draws a fixed-width progress bar for a 0..1 fraction.
func Bar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	full := int(frac*float64(width) + 0.5)
	if full > width {
		full = width
	}
	return strings.Repeat("█", full) + strings.Repeat("░", width-full)
}

// Spark draws a sparkline for a series.
func Spark(vals []float64) string {
	ticks := []rune("▁▂▃▄▅▆▇█")
	maxv := 0.0
	for _, v := range vals {
		if v > maxv {
			maxv = v
		}
	}
	var sb strings.Builder
	for _, v := range vals {
		if maxv == 0 || v == 0 {
			sb.WriteRune(' ')
			continue
		}
		i := int(v / maxv * float64(len(ticks)-1))
		sb.WriteRune(ticks[i])
	}
	return sb.String()
}

// ShortPath replaces the home dir with ~.
func ShortPath(p, home string) string {
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

package qrcode

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	term, err := Terminal("http://100.64.0.1:7777/?token=0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(term, "\n"), "\n")
	if len(lines) < 15 || !strings.HasPrefix(lines[0], "████") {
		t.Errorf("unexpected terminal QR:\n%s", term)
	}
	svg, err := SVG("x")
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "h1v1h-1z") {
		t.Errorf("svg: %v %q", err, svg)
	}
}

package mux

import (
	"path/filepath"
	"testing"
)

func TestParseExt(t *testing.T) {
	path, pane, ok := parseExt("ext:default:%12")
	if !ok || pane != "%12" || path != filepath.Join(tmuxDir(), "default") {
		t.Fatalf("got %q %q %v", path, pane, ok)
	}
	// ids come from URLs: none of these may reach a socket outside the
	// user's tmux dir or hand tmux anything but a pane id
	for _, id := range []string{
		"ext:../../home/x/sock:%1", "ext:..:%1", "ext:a/b:%1", "ext::%1",
		"ext:default:1", "ext:default:%", "ext:default:%1;kill-server", "ext:default:-t", "ext:default",
	} {
		if _, _, ok := parseExt(id); ok {
			t.Errorf("%q accepted", id)
		}
	}
	if _, err := on("ext:../x:%1", "capture-pane", "-p", "-t", "ext:../x:%1"); err == nil {
		t.Error("a bad external id fell through to the optimus server")
	}
}

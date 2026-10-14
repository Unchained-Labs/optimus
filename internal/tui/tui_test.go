package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/Unchained-Labs/optimus/internal/dirs"
)

func labels(items []pickItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.value)
	}
	return out
}

func TestDirItemsPath(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"api", "app", "web", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a folder ending in "/" lists itself first, then what it contains
	got := labels(dirItems(root+"/", "", nil, nil))
	want := []string{root, filepath.Join(root, "api"), filepath.Join(root, "app"), filepath.Join(root, "web")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dirItems(%q) = %v, want %v", root+"/", got, want)
	}
	// a partial name lists the folders it continues into
	got = labels(dirItems(filepath.Join(root, "ap"), "", nil, nil))
	if strings.Join(got, ",") != filepath.Join(root, "api")+","+filepath.Join(root, "app") {
		t.Fatalf("partial: %v", got)
	}
	// ./ is relative to the current folder
	got = labels(dirItems("./w", root, nil, nil))
	if len(got) != 1 || got[0] != filepath.Join(root, "web") {
		t.Fatalf("relative: %v", got)
	}
}

func TestDirItemsSearch(t *testing.T) {
	root := t.TempDir()
	crawled := []dirs.Dir{
		{Path: filepath.Join(root, "dev", "optimus"), Name: "optimus", Git: true, Project: true},
		{Path: filepath.Join(root, "dev", "web"), Name: "web", Project: true},
	}
	// nothing typed: the current folder first, then every project
	got := dirItems("", "/somewhere", crawled, nil)
	if len(got) != 3 || got[0].value != "/somewhere" || got[0].detail != "current folder" {
		t.Fatalf("empty query: %+v", got)
	}
	// a name searches projects, not only folders with history
	got = dirItems("optim", "/somewhere", crawled, nil)
	if len(got) != 1 || got[0].value != crawled[0].Path || !strings.Contains(got[0].detail, "git") {
		t.Fatalf("search: %+v", got)
	}
}

func TestPickerRefreshKeepsCursor(t *testing.T) {
	rows := []pickItem{{value: "a"}, {value: "b"}}
	p := &picker{filter: textinput.New(), source: func(string) []pickItem { return rows }}
	p.refilter()
	p.cur = 1
	rows = []pickItem{{value: "new"}, {value: "a"}, {value: "b"}} // the crawl found more
	p.refresh()
	if p.filtered[p.cur].value != "b" {
		t.Fatalf("cursor moved to %q", p.filtered[p.cur].value)
	}
}

func TestFitKeysKeepsHelp(t *testing.T) {
	keys := [][2]string{{"enter", "attach"}, {"n", "new"}, {"s", "send"}, {"F", "fan out a task to several agents"}}
	must := [][2]string{{"?", "help"}, {"q", "quit"}}
	for _, w := range []int{30, 40, 80, 200} {
		out := ansi.Strip(fitKeys(keys, must, w))
		if !strings.Contains(out, "? help") || !strings.Contains(out, "q quit") {
			t.Errorf("width %d: %q lost the help key", w, out)
		}
		if ansi.StringWidth(out) > w {
			t.Errorf("width %d: %q is too wide", w, out)
		}
	}
	if out := ansi.Strip(fitKeys(keys, must, 30)); !strings.HasPrefix(out, "enter attach") {
		t.Errorf("narrow: %q should keep the first keys", out)
	}
}

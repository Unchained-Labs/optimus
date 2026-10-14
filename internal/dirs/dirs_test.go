package dirs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tree(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if filepath.Ext(p) != "" || filepath.Base(p) == "Makefile" {
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func paths(ds []Dir, root string) []string {
	var out []string
	for _, d := range ds {
		r, _ := filepath.Rel(root, d.Path)
		out = append(out, r)
	}
	return out
}

func TestCrawlFindsProjectsAndSkipsNoise(t *testing.T) {
	root := tree(t,
		"dev/optimus/.git", "dev/optimus/go.mod",
		"dev/web/package.json", "dev/web/node_modules/dep/package.json",
		"apps/chezmoi/.git", "notes/plain",
		".cache/thing/.git", "deep/a/b/c/d/e/f/.git",
	)
	got := map[string]bool{}
	for _, p := range paths(Crawl(root), root) {
		got[p] = true
	}
	for _, want := range []string{"dev/optimus", "dev/web", "apps/chezmoi"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, not := range []string{"notes/plain", "dev/web/node_modules/dep", ".cache/thing", "deep/a/b/c/d/e/f"} {
		if got[not] {
			t.Errorf("%s should not be found", not)
		}
	}
}

func TestComplete(t *testing.T) {
	root := tree(t, "dev/optimus/.git", "dev/other", "dev/.hidden", "docs")
	if got := paths(Complete(root+"/dev/", root, 10), root); len(got) != 2 || got[0] != "dev/optimus" {
		t.Errorf("children: %v (git repos first, hidden left out)", got)
	}
	if got := paths(Complete(root+"/dev/OT", root, 10), root); len(got) != 1 || got[0] != "dev/other" {
		t.Errorf("prefix: %v", got)
	}
	if got := paths(Complete("~/d", root, 10), root); len(got) != 2 {
		t.Errorf("~ expansion: %v", got)
	}
	if got := Complete(root+"/nope/", root, 10); got != nil {
		t.Errorf("missing parent: %v", got)
	}
}

func TestSearchRanksNameThenHistory(t *testing.T) {
	defer func(f func() []string) { tempRoots = f }(tempRoots)
	tempRoots = func() []string { return nil }
	root := tree(t, "dev/optimus", "apps/optimus-docs", "work/opt/imus-not")
	crawled := []Dir{
		{Path: root + "/apps/optimus-docs", Name: "optimus-docs", Project: true},
		{Path: root + "/dev/optimus", Name: "optimus", Project: true, Git: true},
		{Path: root + "/work/opt", Name: "opt", Project: true},
	}
	got := paths(Search("optimus", crawled, nil, root, 10), root)
	if len(got) < 2 || got[0] != "dev/optimus" || got[1] != "apps/optimus-docs" {
		t.Errorf("ranking: %v", got)
	}
	// history lifts a folder over an equal match, and fills in its count
	known := []Known{{Path: root + "/apps/optimus-docs", Sessions: 3, Last: time.Now()}}
	res := Search("optim", crawled, known, root, 10)
	if res[0].Name != "optimus-docs" || res[0].Sessions != 3 {
		t.Errorf("history: %+v", res[0])
	}
	// gone folders from history are dropped, empty query lists everything
	known = append(known, Known{Path: root + "/deleted", Sessions: 9})
	if got := Search("", crawled, known, root, 10); len(got) != 3 {
		t.Errorf("empty query: %v", paths(got, root))
	}
	if got := Search("zzz", crawled, nil, root, 10); len(got) != 0 {
		t.Errorf("no match: %v", got)
	}
}

func TestSearchDropsScratchHistory(t *testing.T) {
	root := tree(t, "scratch")
	defer func(f func() []string) { tempRoots = f }(tempRoots)
	tempRoots = func() []string { return []string{root} }
	if got := Search("", nil, []Known{{Path: root + "/scratch", Sessions: 1}}, "/home/x", 10); len(got) != 0 {
		t.Errorf("scratch folder offered: %v", got)
	}
}

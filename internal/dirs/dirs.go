// Package dirs finds the folders an agent could be started in, for the
// "new session" picker: every project under the home directory (crawled in
// the background and cached), plus shell-style completion of a typed path.
package dirs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dir is one candidate folder.
type Dir struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Git      bool      `json:"git,omitempty"`
	Project  bool      `json:"project,omitempty"` // a git repo or has a project manifest
	Sessions int       `json:"sessions,omitempty"`
	Last     time.Time `json:"last,omitempty"`
	score    int
}

// Known is a folder with agent history, passed in by the caller.
type Known struct {
	Path     string
	Sessions int
	Last     time.Time
}

// markers make a folder a project. A .git file (not dir) is a worktree.
var markers = []string{".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml", "build.gradle", "Gemfile", "composer.json", "mix.exs", "deno.json", "CMakeLists.txt", "Makefile", "docker-compose.yml", "compose.yaml"}

// skip are folders never worth descending into: dependencies, build output,
// caches. Hidden folders are skipped too.
var skip = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "out": true,
	"__pycache__": true, "venv": true, "env": true, "site-packages": true, "bower_components": true,
	"snap": true, "Library": true, "Applications": true, "AppData": true, "go": true, "pkg": true,
	"coverage": true, "tmp": true, "Pictures": true, "Music": true, "Videos": true, "Movies": true,
}

const (
	maxDepth   = 5
	maxVisited = 30000
	budget     = 3 * time.Second
	ttl        = 2 * time.Minute
)

// Crawl walks root breadth-first for project folders, within a depth,
// a folder count and a time budget so a huge home never stalls the page.
func Crawl(root string) []Dir {
	type item struct {
		path  string
		depth int
	}
	start := time.Now()
	queue := []item{{root, 0}}
	var out []Dir
	visited := 0
	for len(queue) > 0 && visited < maxVisited && time.Since(start) < budget {
		cur := queue[0]
		queue = queue[1:]
		visited++
		ents, err := os.ReadDir(cur.path)
		if err != nil {
			continue
		}
		names := map[string]bool{}
		for _, e := range ents {
			names[e.Name()] = true
		}
		if cur.depth > 0 {
			d := Dir{Path: cur.path, Name: filepath.Base(cur.path), Git: names[".git"]}
			for _, m := range markers {
				if names[m] {
					d.Project = true
					break
				}
			}
			if d.Project {
				out = append(out, d)
			}
		}
		if cur.depth >= maxDepth {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			if !e.IsDir() || strings.HasPrefix(n, ".") || skip[n] {
				continue
			}
			queue = append(queue, item{filepath.Join(cur.path, n), cur.depth + 1})
		}
	}
	return out
}

// Index caches a crawl of one root and refreshes it in the background once
// it is older than ttl, so lookups never wait on the disk after the first.
type Index struct {
	Root string

	mu      sync.Mutex
	dirs    []Dir
	at      time.Time
	running bool
	ready   chan struct{}
}

func NewIndex(root string) *Index { return &Index{Root: root, ready: make(chan struct{})} }

// Dirs returns the cached crawl, waiting for the first one at most wait.
func (x *Index) Dirs(wait time.Duration) []Dir {
	x.mu.Lock()
	stale := time.Since(x.at) > ttl
	if stale && !x.running {
		x.running = true
		go x.refresh()
	}
	ready := x.ready
	x.mu.Unlock()
	select {
	case <-ready:
	case <-time.After(wait):
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.dirs
}

func (x *Index) refresh() {
	d := Crawl(x.Root)
	x.mu.Lock()
	x.dirs, x.at, x.running = d, time.Now(), false
	select {
	case <-x.ready:
	default:
		close(x.ready)
	}
	x.mu.Unlock()
}

// Expand turns ~ and ~/x into absolute paths.
func Expand(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// IsPath reports whether q is typed as a path (and so wants completion)
// rather than a name to search for.
func IsPath(q string) bool {
	return strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, "./")
}

// Complete lists the folders a typed path could continue into: the children
// of q when it ends in "/", else the siblings starting with its last part.
func Complete(q, home string, limit int) []Dir {
	p := Expand(q, home)
	parent, prefix := p, ""
	if !strings.HasSuffix(q, "/") {
		parent, prefix = filepath.Dir(p), filepath.Base(p)
		if q == "~" {
			parent, prefix = home, ""
		}
	}
	ents, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	low := strings.ToLower(prefix)
	var out []Dir
	for _, e := range ents {
		n := e.Name()
		full := filepath.Join(parent, n)
		if !isDir(e, full) {
			continue
		}
		if strings.HasPrefix(n, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(n), low) {
			continue
		}
		d := Dir{Path: full, Name: n}
		if _, err := os.Stat(filepath.Join(full, ".git")); err == nil {
			d.Git, d.Project = true, true
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func isDir(e os.DirEntry, full string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink != 0 {
		st, err := os.Stat(full)
		return err == nil && st.IsDir()
	}
	return false
}

// Search ranks crawled and known folders against q: name matches before
// path matches before scattered letters, then folders with agent history,
// most recent first. An empty q lists recent projects, then the rest.
func Search(q string, crawled []Dir, known []Known, home string, limit int) []Dir {
	byPath := map[string]*Dir{}
	var all []*Dir
	add := func(d Dir) *Dir {
		if e, ok := byPath[d.Path]; ok {
			return e
		}
		c := d
		byPath[d.Path] = &c
		all = append(all, &c)
		return &c
	}
	for _, d := range crawled {
		add(d)
	}
	for _, k := range known {
		if k.Path == "" || temporary(k.Path) {
			continue // scratch folders agents ran in aren't places to start one
		}
		if _, err := os.Stat(k.Path); err != nil {
			continue // a project that's gone can't host a session
		}
		d := add(Dir{Path: k.Path, Name: filepath.Base(k.Path), Project: true})
		d.Sessions, d.Last = k.Sessions, k.Last
	}
	q = strings.ToLower(strings.TrimSpace(q))
	var out []Dir
	for _, d := range all {
		rel := strings.ToLower(strings.TrimPrefix(d.Path, home+"/"))
		name := strings.ToLower(d.Name)
		s := 0
		switch {
		case q == "":
			s = 1
		case name == q:
			s = 1000
		case strings.HasPrefix(name, q):
			s = 800
		case strings.Contains(name, q):
			s = 600
		case strings.Contains(rel, q):
			s = 400
		case subsequence(rel, q):
			s = 200
		default:
			continue
		}
		if d.Sessions > 0 {
			s += 100
		}
		if d.Git {
			s += 20
		}
		s -= strings.Count(rel, "/") * 5 // shallower first
		d.score = s
		out = append(out, *d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// tempRoots are where scratch folders live (a var for tests, which build
// their trees there).
var tempRoots = func() []string { return []string{os.TempDir(), "/tmp", "/var/tmp"} }

func temporary(p string) bool {
	for _, t := range tempRoots() {
		if t = strings.TrimSuffix(t, "/"); t != "" && (p == t || strings.HasPrefix(p, t+"/")) {
			return true
		}
	}
	return false
}

func subsequence(s, q string) bool {
	i := 0
	for _, c := range s {
		if i < len(q) && byte(c) == q[i] {
			i++
		}
	}
	return i == len(q)
}

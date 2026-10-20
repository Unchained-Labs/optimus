package web

import (
	"sync"
	"time"

	"github.com/Unchained-Labs/optimus/internal/worktree"
)

// statsTTL is how long a worktree's diff stats are reused. Every poll of
// /api/state (each open tab, every 2.5s) used to run three git commands per
// worktree window; a few seconds of staleness in "+12 −3 · 2 files" is
// invisible, the git processes on a busy box are not.
var statsTTL = 10 * time.Second

type statsEntry struct {
	base string
	st   worktree.Stats
	at   time.Time
}

// statsCache memoizes worktree.Tree.Stats by worktree path.
type statsCache struct {
	mu sync.Mutex
	m  map[string]statsEntry
}

func (c *statsCache) get(t worktree.Tree) worktree.Stats {
	now := time.Now()
	c.mu.Lock()
	e, ok := c.m[t.Path]
	c.mu.Unlock()
	if ok && e.base == t.Base && now.Sub(e.at) < statsTTL {
		return e.st
	}
	st, _ := t.Stats()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]statsEntry{}
	}
	for p, old := range c.m { // drop worktrees that stopped being asked about
		if now.Sub(old.at) > 30*statsTTL {
			delete(c.m, p)
		}
	}
	c.m[t.Path] = statsEntry{t.Base, st, now}
	return st
}

// forget drops a worktree's stats after it was merged or discarded.
func (c *statsCache) forget(path string) {
	c.mu.Lock()
	delete(c.m, path)
	c.mu.Unlock()
}

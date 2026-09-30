// Package index discovers every agent session and keeps a parse cache so that
// only new or changed transcripts are re-read.
package index

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/pricing"
	"github.com/wardn/optimus/internal/providers"
)

const cacheVersion = 4

type entry struct {
	Key     string
	Session *model.Session // nil for sources that parsed to nothing
}

type cacheFile struct {
	Version int
	Entries map[string]entry // by source path
}

type Index struct {
	Sessions []*model.Session
	Live     []providers.LiveSession
	Errors   []error
	Loaded   time.Time
}

func cachePath() string { return filepath.Join(config.CacheDir(), "index.gob") }

func loadCache() cacheFile {
	c := cacheFile{Version: cacheVersion, Entries: map[string]entry{}}
	f, err := os.Open(cachePath())
	if err != nil {
		return c
	}
	defer f.Close()
	var disk cacheFile
	if gob.NewDecoder(f).Decode(&disk) != nil || disk.Version != cacheVersion || disk.Entries == nil {
		return c
	}
	return disk
}

func saveCache(c cacheFile) error {
	if err := os.MkdirAll(config.CacheDir(), 0o755); err != nil {
		return err
	}
	tmp := cachePath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(c); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, cachePath())
}

// Load scans every provider, parsing only what changed since last time, and
// prices every session with the given table.
func Load(prices *pricing.Table) *Index {
	cache := loadCache()
	idx := &Index{Loaded: time.Now()}

	var srcs []providers.Source
	for _, p := range providers.All() {
		s, err := p.Discover()
		if err != nil {
			idx.Errors = append(idx.Errors, err)
		}
		srcs = append(srcs, s...)
		if lr, ok := p.(providers.LiveReporter); ok {
			idx.Live = append(idx.Live, lr.Live()...)
		}
	}

	next := cacheFile{Version: cacheVersion, Entries: make(map[string]entry, len(srcs))}
	var todo []providers.Source
	for _, s := range srcs {
		if e, ok := cache.Entries[s.Path]; ok && e.Key == s.Key {
			next.Entries[s.Path] = e
			continue
		}
		todo = append(todo, s)
	}

	if len(todo) > 0 {
		var mu sync.Mutex
		work := make(chan providers.Source)
		var wg sync.WaitGroup
		for i := 0; i < runtime.NumCPU(); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for src := range work {
					sess, err := providers.Get(src.Agent).Parse(src)
					mu.Lock()
					if err != nil {
						idx.Errors = append(idx.Errors, err)
					} else {
						next.Entries[src.Path] = entry{Key: src.Key, Session: sess}
					}
					mu.Unlock()
				}
			}()
		}
		for _, s := range todo {
			work <- s
		}
		close(work)
		wg.Wait()
		if err := saveCache(next); err != nil {
			idx.Errors = append(idx.Errors, err)
		}
	}

	live := map[string]providers.LiveSession{}
	for _, l := range idx.Live {
		live[l.Agent+"/"+l.ID] = l
	}
	for _, e := range next.Entries {
		if e.Session == nil {
			continue
		}
		s := *e.Session // copy; runtime fields must not leak into the cache
		Price(&s, prices)
		if l, ok := live[s.Agent+"/"+s.ID]; ok {
			s.Live, s.Status = true, l.Status
		}
		idx.Sessions = append(idx.Sessions, &s)
	}
	sort.Slice(idx.Sessions, func(i, j int) bool { return idx.Sessions[i].End.After(idx.Sessions[j].End) })
	return idx
}

// Price fills a session's runtime Usage and Cost from its buckets.
func Price(s *model.Session, prices *pricing.Table) {
	s.Usage, s.Cost = model.Usage{}, 0
	for _, b := range s.Buckets {
		s.Usage.Add(b.Usage)
		s.Cost += prices.Cost(b.Model, b.Usage)
	}
}

// Find resolves a session by full id, id prefix, or path.
func (idx *Index) Find(q string) *model.Session {
	var hit *model.Session
	for _, s := range idx.Sessions {
		if s.ID == q || s.Path == q {
			return s
		}
		if len(q) >= 4 && len(s.ID) >= len(q) && s.ID[:len(q)] == q {
			if hit != nil {
				return nil // ambiguous
			}
			hit = s
		}
	}
	return hit
}

// Clear deletes the parse cache.
func Clear() error { return os.Remove(cachePath()) }

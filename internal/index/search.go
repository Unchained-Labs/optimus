package index

import (
	"bytes"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/providers"
)

// Hit is a session whose transcript mentions the query.
type Hit struct {
	Session *model.Session
	Snippet string // the matching passage, with some context
	Role    string // who said it: user or assistant (or tool)
}

// Search finds sessions whose transcripts contain query (case-insensitive),
// newest first, with a snippet for each. Raw transcript files are scanned in
// parallel first; only matching sessions are parsed for a snippet.
func Search(sessions []*model.Session, query string, limit int) []Hit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	needle := []byte(q)
	type cand struct {
		i int
		s *model.Session
	}
	work := make(chan cand)
	var mu sync.Mutex
	var found []cand
	var wg sync.WaitGroup
	for n := 0; n < runtime.NumCPU(); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				if rawContains(c.s, needle) {
					mu.Lock()
					found = append(found, c)
					mu.Unlock()
				}
			}
		}()
	}
	for i, s := range sessions {
		work <- cand{i, s}
	}
	close(work)
	wg.Wait()
	sort.Slice(found, func(a, b int) bool { return found[a].i < found[b].i }) // keep index order (newest first)

	var hits []Hit
	for _, c := range found {
		if limit > 0 && len(hits) >= limit {
			break
		}
		h := Hit{Session: c.s}
		if p := providers.Get(c.s.Agent); p != nil {
			if msgs, err := p.Transcript(c.s); err == nil {
				h.Snippet, h.Role = snippet(msgs, q)
			}
		}
		if h.Snippet == "" { // matched in metadata or tool output, not in text we render
			if strings.Contains(strings.ToLower(c.s.DisplayTitle()), q) {
				h.Snippet = c.s.DisplayTitle()
			} else {
				continue
			}
		}
		hits = append(hits, h)
	}
	return hits
}

// rawContains checks the session's files without parsing them. opencode
// keeps text in separate part files, so it is checked through its
// transcript instead.
func rawContains(s *model.Session, needle []byte) bool {
	if s.Agent == "opencode" {
		msgs, err := providers.Get(s.Agent).Transcript(s)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if strings.Contains(strings.ToLower(m.Text), string(needle)) {
				return true
			}
		}
		return false
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(b), needle)
}

func snippet(msgs []model.Message, q string) (string, string) {
	for i := len(msgs) - 1; i >= 0; i-- { // most recent mention first
		m := msgs[i]
		texts := []string{m.Text}
		for _, t := range m.Tools {
			texts = append(texts, t.Name+" "+t.Target)
		}
		for _, t := range texts {
			low := strings.ToLower(t)
			at := strings.Index(low, q)
			if at < 0 {
				continue
			}
			start, end := at-70, at+len(q)+90
			if start < 0 {
				start = 0
			}
			if end > len(t) {
				end = len(t)
			}
			for start > 0 && !utf8Start(t[start]) {
				start--
			}
			for end < len(t) && !utf8Start(t[end]) {
				end++
			}
			sn := model.OneLine(t[start:end])
			if start > 0 {
				sn = "…" + sn
			}
			if end < len(t) {
				sn += "…"
			}
			return sn, m.Role
		}
	}
	return "", ""
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

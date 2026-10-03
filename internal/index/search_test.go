package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Unchained-Labs/optimus/internal/model"
)

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	mk := func(id, text string) *model.Session {
		p := filepath.Join(dir, id+".jsonl")
		os.WriteFile(p, []byte(`{"type":"user","timestamp":"2026-09-01T10:00:00Z","message":{"role":"user","content":"`+text+`"}}`+"\n"), 0o644)
		return &model.Session{Agent: "claude", ID: id, Path: p, Title: "t-" + id}
	}
	ss := []*model.Session{
		mk("new", "the Redis connection pool keeps timing out under load"),
		mk("old", "nothing relevant here"),
		mk("older", "we moved the cache to redis last week"),
	}
	hits := Search(ss, "REDIS", 0)
	if len(hits) != 2 || hits[0].Session.ID != "new" || hits[1].Session.ID != "older" {
		t.Fatalf("hits: %+v", hits)
	}
	if !strings.Contains(hits[0].Snippet, "Redis connection pool") || hits[0].Role != "user" {
		t.Errorf("snippet: %q %q", hits[0].Snippet, hits[0].Role)
	}
	if h := Search(ss, "redis", 1); len(h) != 1 {
		t.Errorf("limit: %d", len(h))
	}
	if h := Search(ss, "  ", 0); h != nil {
		t.Error("empty query should return nothing")
	}
}

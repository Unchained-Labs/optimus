// Package model holds the agent-neutral types every provider normalizes into.
package model

import (
	"path/filepath"
	"strings"
	"time"
)

// Usage is a token tally. SourceCost is a cost reported by the agent itself
// (e.g. opencode), used when optimus has no price for the model.
type Usage struct {
	Input        int64   `json:"in,omitempty"`
	Output       int64   `json:"out,omitempty"`
	CacheRead    int64   `json:"cr,omitempty"`
	CacheWrite5m int64   `json:"cw5,omitempty"`
	CacheWrite1h int64   `json:"cw1,omitempty"`
	Reasoning    int64   `json:"rs,omitempty"` // informational; already counted in Output
	SourceCost   float64 `json:"sc,omitempty"`
}

func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite5m += o.CacheWrite5m
	u.CacheWrite1h += o.CacheWrite1h
	u.Reasoning += o.Reasoning
	u.SourceCost += o.SourceCost
}

// Total is every token the model processed, cache included.
func (u Usage) Total() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h
}

func (u Usage) IsZero() bool { return u.Total() == 0 && u.SourceCost == 0 }

// Bucket is usage for one model within one hour. Hour-level granularity is
// enough for daily charts and 5h block estimates while keeping the index small.
type Bucket struct {
	Hour  int64  `json:"h"` // unix seconds, truncated to the hour
	Model string `json:"m"`
	Usage
}

// Session is one conversation of one agent.
type Session struct {
	Agent       string    `json:"agent"`
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Cwd         string    `json:"cwd"`
	Branch      string    `json:"branch,omitempty"`
	Title       string    `json:"title,omitempty"`
	FirstPrompt string    `json:"first_prompt,omitempty"`
	LastPrompt  string    `json:"last_prompt,omitempty"`
	Model       string    `json:"model,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Messages    int       `json:"messages"`
	Buckets     []Bucket  `json:"buckets,omitempty"`
	// Automated: started by a script or scheduler (e.g. claude -p, codex
	// exec), not by a person. Hidden from session lists by default.
	Automated bool `json:"automated,omitempty"`

	// Filled at runtime, never cached.
	Usage  Usage   `json:"-"`
	Cost   float64 `json:"-"`
	Live   bool    `json:"-"`
	Status string  `json:"-"`
}

// DisplayTitle is the best one-line name for a session.
func (s *Session) DisplayTitle() string {
	for _, t := range []string{s.Title, s.FirstPrompt, s.LastPrompt} {
		t = OneLine(t)
		if t != "" {
			return t
		}
	}
	return "(untitled)"
}

// ProjectName is the short name of the session's working directory.
func (s *Session) ProjectName() string { return ProjectName(s.Cwd) }

func ProjectName(cwd string) string {
	if cwd == "" {
		return "?"
	}
	return filepath.Base(cwd)
}

func (s *Session) ShortID() string {
	id := strings.TrimPrefix(s.ID, "ses_")
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Message is one normalized transcript entry.
type Message struct {
	Role  string // user | assistant | tool
	Text  string
	Time  time.Time
	Tools []ToolCall
}

type ToolCall struct {
	Name   string
	Target string // file path, command, pattern... whatever best summarizes the call
	Edit   bool   // the call modified Target
}

// OneLine collapses whitespace and trims to a single line.
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Truncate cuts s to n runes, appending an ellipsis when it had to cut.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

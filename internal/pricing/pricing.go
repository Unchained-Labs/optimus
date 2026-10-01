// Package pricing turns token usage into dollars.
//
// Rates are API list prices in USD per million tokens. On a subscription plan
// they are "API-equivalent" cost: what the same traffic would cost on the API.
// Override or extend them with the "pricing" map in config.json.
package pricing

import (
	"sort"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/model"
)

// Keys are matched as prefixes of the normalized model id, longest first.
var builtin = map[string]config.Price{
	// Anthropic (cache writes default to 1.25x / 2x input)
	"claude-fable-5-1":  {Input: 10, Output: 50, CacheRead: 0.25},
	"claude-mythos-5-1": {Input: 10, Output: 50, CacheRead: 0.25},
	"claude-fable-5":    {Input: 10, Output: 50, CacheRead: 1},
	"claude-mythos-5":   {Input: 10, Output: 50, CacheRead: 1},
	"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20},
	"claude-opus-5":     {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-8":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-7":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-6":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-5":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4":     {Input: 15, Output: 75, CacheRead: 1.50},
	"claude-sonnet-5-5": {Input: 2, Output: 10, CacheRead: 0.20},
	"claude-sonnet-5":   {Input: 2, Output: 10, CacheRead: 0.20},
	"claude-sonnet-4":   {Input: 3, Output: 15, CacheRead: 0.30},
	"claude-3-7-sonnet": {Input: 3, Output: 15, CacheRead: 0.30},
	"claude-haiku-4-5":  {Input: 1, Output: 5, CacheRead: 0.10},
	"claude-3-5-haiku":  {Input: 0.80, Output: 4, CacheRead: 0.08},

	// OpenAI (cached input replaces CacheRead; no write premium)
	"gpt-5-mini": {Input: 0.25, Output: 2, CacheRead: 0.025},
	"gpt-5-nano": {Input: 0.05, Output: 0.40, CacheRead: 0.005},
	"gpt-5":      {Input: 1.25, Output: 10, CacheRead: 0.125},
	"gpt-4.1":    {Input: 2, Output: 8, CacheRead: 0.50},
	"gpt-4o":     {Input: 2.50, Output: 10, CacheRead: 1.25},
	"o3":         {Input: 2, Output: 8, CacheRead: 0.50},
	"o4-mini":    {Input: 1.10, Output: 4.40, CacheRead: 0.275},
	"codex-mini": {Input: 1.50, Output: 6, CacheRead: 0.375},

	// Google
	"gemini-2.5-pro":   {Input: 1.25, Output: 10, CacheRead: 0.31},
	"gemini-2.5-flash": {Input: 0.30, Output: 2.50, CacheRead: 0.075},
}

type Table struct {
	keys   []string
	prices map[string]config.Price
}

func New(overrides map[string]config.Price) *Table {
	t := &Table{prices: map[string]config.Price{}}
	for k, v := range builtin {
		t.prices[k] = v
	}
	for k, v := range overrides {
		t.prices[Normalize(k)] = v
	}
	for k := range t.prices {
		t.keys = append(t.keys, k)
	}
	sort.Slice(t.keys, func(i, j int) bool { return len(t.keys[i]) > len(t.keys[j]) })
	return t
}

// Normalize strips provider prefixes and context-size markers so that
// "anthropic/claude-opus-4.5[1m]" and "claude-opus-4-5-20251101" both resolve.
func Normalize(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	m = strings.TrimPrefix(m, "anthropic.")
	if i := strings.Index(m, "["); i >= 0 {
		m = m[:i]
	}
	if strings.HasPrefix(m, "claude") {
		m = strings.ReplaceAll(m, ".", "-")
	}
	return m
}

func (t *Table) Lookup(modelID string) (config.Price, bool) {
	n := Normalize(modelID)
	for _, k := range t.keys {
		if strings.HasPrefix(n, k) {
			return t.prices[k], true
		}
	}
	return config.Price{}, false
}

// Cost prices a usage record for a model. Unknown models fall back to the
// cost the agent reported itself, if any.
func (t *Table) Cost(modelID string, u model.Usage) float64 {
	p, ok := t.Lookup(modelID)
	if !ok {
		return u.SourceCost
	}
	cr, w5, w1 := p.CacheRead, p.CacheWrite5m, p.CacheWrite1h
	if cr == 0 {
		cr = p.Input * 0.1
	}
	if w5 == 0 {
		w5 = p.Input * 1.25
	}
	if w1 == 0 {
		w1 = p.Input * 2
	}
	return (float64(u.Input)*p.Input +
		float64(u.Output)*p.Output +
		float64(u.CacheRead)*cr +
		float64(u.CacheWrite5m)*w5 +
		float64(u.CacheWrite1h)*w1) / 1e6
}

// Known reports whether a model has a price.
func (t *Table) Known(modelID string) bool {
	_, ok := t.Lookup(modelID)
	return ok
}

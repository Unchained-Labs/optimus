package usage

import (
	"math"
	"testing"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/pricing"
)

func TestPricing(t *testing.T) {
	p := pricing.New(nil)
	u := model.Usage{Input: 1e6, Output: 1e6, CacheRead: 1e6, CacheWrite5m: 1e6, CacheWrite1h: 1e6}
	// opus 5.5: 4 + 20 + 0.20 + 5 + 8
	if got := p.Cost("claude-opus-5-5[1m]", u); math.Abs(got-37.2) > 1e-9 {
		t.Errorf("opus-5-5 cost %v", got)
	}
	// longest prefix wins: opus-5-5 must not match opus-5
	if got := p.Cost("claude-opus-5", model.Usage{Input: 1e6}); got != 5 {
		t.Errorf("opus-5 cost %v", got)
	}
	if got := p.Cost("anthropic/claude-sonnet-4.5", model.Usage{Output: 1e6}); got != 15 {
		t.Errorf("sonnet 4.5 dotted cost %v", got)
	}
	if got := p.Cost("some-local-model", model.Usage{Input: 5, SourceCost: 0.42}); got != 0.42 {
		t.Errorf("unknown model should fall back to source cost, got %v", got)
	}
	o := pricing.New(map[string]config.Price{"my-model": {Input: 1, Output: 1}})
	if got := o.Cost("my-model-v2", model.Usage{Input: 1e6}); got != 1 {
		t.Errorf("override cost %v", got)
	}
}

func TestBlocksAndGroups(t *testing.T) {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local)
	h := func(d time.Duration) int64 { return base.Add(d).Unix() }
	s := &model.Session{Agent: "claude", Cwd: "/a", Buckets: []model.Bucket{
		{Hour: h(0), Model: "claude-haiku-4-5", Usage: model.Usage{Output: 1e6}},       // $5
		{Hour: h(3 * time.Hour), Model: "claude-haiku-4-5", Usage: model.Usage{Output: 1e6}}, // same block
		{Hour: h(6 * time.Hour), Model: "claude-haiku-4-5", Usage: model.Usage{Output: 1e6}}, // new block
	}}
	p := pricing.New(nil)
	bl := Blocks([]*model.Session{s}, p, "claude", 5, base.Add(7*time.Hour))
	if len(bl) != 2 || bl[0].Cost != 10 || bl[1].Cost != 5 || !bl[1].Active || bl[0].Active {
		t.Fatalf("blocks: %+v", bl)
	}
	rows := GroupBy([]*model.Session{s}, p, Filter{Since: base.Add(2 * time.Hour)}, "total")
	if len(rows) != 1 || rows[0].Cost != 10 || rows[0].Sessions != 1 {
		t.Errorf("filtered total: %+v", rows)
	}
	days := Daily([]*model.Session{s}, p, Filter{}, 3, base)
	if len(days) != 3 || days[2].Cost != 15 || days[0].Cost != 0 {
		t.Errorf("daily: %+v", days)
	}
	cfg := config.Default()
	cfg.Budgets.DailyUSD = 20
	bs := Budgets([]*model.Session{s}, p, cfg, base.Add(7*time.Hour))
	if len(bs) != 1 || bs[0].Spent != 15 || math.Abs(bs[0].Frac()-0.75) > 1e-9 {
		t.Errorf("budgets: %+v", bs)
	}
}

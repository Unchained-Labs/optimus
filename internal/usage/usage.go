// Package usage aggregates session buckets into the numbers optimus shows:
// spend per day/project/model/agent, billing-style blocks, and budgets.
package usage

import (
	"sort"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/pricing"
)

type Row struct {
	Key      string
	Usage    model.Usage
	Cost     float64
	Sessions int
	Last     time.Time
}

type Filter struct {
	Since time.Time
	Until time.Time
	Agent string
	Cwd   string
}

func (f Filter) matches(s *model.Session) bool {
	return (f.Agent == "" || s.Agent == f.Agent) && (f.Cwd == "" || s.Cwd == f.Cwd)
}

func (f Filter) inRange(t time.Time) bool {
	return (f.Since.IsZero() || !t.Before(f.Since)) && (f.Until.IsZero() || t.Before(f.Until))
}

// GroupBy returns one row per key, most expensive first (by day: chronological).
func GroupBy(sessions []*model.Session, prices *pricing.Table, f Filter, by string) []Row {
	rows := map[string]*Row{}
	counted := map[string]map[*model.Session]bool{}
	for _, s := range sessions {
		if !f.matches(s) {
			continue
		}
		for _, b := range s.Buckets {
			t := time.Unix(b.Hour, 0)
			if !f.inRange(t) {
				continue
			}
			var k string
			switch by {
			case "day":
				k = t.Local().Format("2006-01-02")
			case "month":
				k = t.Local().Format("2006-01")
			case "model":
				k = b.Model
			case "agent":
				k = s.Agent
			case "project":
				k = s.Cwd
			case "session":
				k = s.Agent + "/" + s.ID
			default:
				k = "total"
			}
			r, ok := rows[k]
			if !ok {
				r = &Row{Key: k}
				rows[k] = r
				counted[k] = map[*model.Session]bool{}
			}
			r.Usage.Add(b.Usage)
			r.Cost += prices.Cost(b.Model, b.Usage)
			if t.After(r.Last) {
				r.Last = t
			}
			if !counted[k][s] {
				counted[k][s] = true
				r.Sessions++
			}
		}
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if by == "day" || by == "month" {
			return out[i].Key < out[j].Key
		}
		return out[i].Cost > out[j].Cost
	})
	return out
}

// Total sums everything matching the filter.
func Total(sessions []*model.Session, prices *pricing.Table, f Filter) Row {
	rows := GroupBy(sessions, prices, f, "total")
	if len(rows) == 0 {
		return Row{Key: "total"}
	}
	return rows[0]
}

// Daily returns n consecutive local days ending today, zero-filled.
func Daily(sessions []*model.Session, prices *pricing.Table, f Filter, n int, now time.Time) []Row {
	start := startOfDay(now).AddDate(0, 0, -(n - 1))
	f.Since = start
	got := map[string]Row{}
	for _, r := range GroupBy(sessions, prices, f, "day") {
		got[r.Key] = r
	}
	out := make([]Row, n)
	for i := range out {
		k := start.AddDate(0, 0, i).Format("2006-01-02")
		r, ok := got[k]
		if !ok {
			r = Row{Key: k}
		}
		out[i] = r
	}
	return out
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func StartOfDay(t time.Time) time.Time { return startOfDay(t) }

// StartOfWeek is the most recent Monday 00:00 local.
func StartOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	wd := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -wd)
}

func StartOfMonth(t time.Time) time.Time {
	y, m, _ := t.Local().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.Local)
}

// Block is a billing-style usage window: it starts at the first activity after
// the previous block ended and lasts a fixed number of hours (5h for Claude).
type Block struct {
	Start, End time.Time
	Usage      model.Usage
	Cost       float64
	Active     bool
	Last       time.Time
}

// Blocks computes usage windows of the given length across all sessions of
// an agent ("" = all agents). Hour-granular.
func Blocks(sessions []*model.Session, prices *pricing.Table, agent string, hours int, now time.Time) []Block {
	type hb struct {
		t    time.Time
		u    model.Usage
		cost float64
	}
	byHour := map[int64]*hb{}
	for _, s := range sessions {
		if agent != "" && s.Agent != agent {
			continue
		}
		for _, b := range s.Buckets {
			h, ok := byHour[b.Hour]
			if !ok {
				h = &hb{t: time.Unix(b.Hour, 0)}
				byHour[b.Hour] = h
			}
			h.u.Add(b.Usage)
			h.cost += prices.Cost(b.Model, b.Usage)
		}
	}
	hrs := make([]*hb, 0, len(byHour))
	for _, h := range byHour {
		hrs = append(hrs, h)
	}
	sort.Slice(hrs, func(i, j int) bool { return hrs[i].t.Before(hrs[j].t) })

	dur := time.Duration(hours) * time.Hour
	var out []Block
	for _, h := range hrs {
		if n := len(out); n == 0 || !h.t.Before(out[n-1].End) {
			out = append(out, Block{Start: h.t, End: h.t.Add(dur)})
		}
		b := &out[len(out)-1]
		b.Usage.Add(h.u)
		b.Cost += h.cost
		b.Last = h.t
	}
	if n := len(out); n > 0 && now.Before(out[n-1].End) {
		out[n-1].Active = true
	}
	return out
}

// Budget is a spend limit and how much of it is used.
type Budget struct {
	Name  string
	Limit float64
	Spent float64
	Reset time.Time
}

func (b Budget) Frac() float64 {
	if b.Limit <= 0 {
		return 0
	}
	return b.Spent / b.Limit
}

func Budgets(sessions []*model.Session, prices *pricing.Table, cfg config.Config, now time.Time) []Budget {
	var out []Budget
	add := func(name string, limit float64, since, reset time.Time) {
		if limit <= 0 {
			return
		}
		out = append(out, Budget{Name: name, Limit: limit, Spent: Total(sessions, prices, Filter{Since: since}).Cost, Reset: reset})
	}
	add("today", cfg.Budgets.DailyUSD, startOfDay(now), startOfDay(now).AddDate(0, 0, 1))
	add("this week", cfg.Budgets.WeeklyUSD, StartOfWeek(now), StartOfWeek(now).AddDate(0, 0, 7))
	add("this month", cfg.Budgets.MonthlyUSD, StartOfMonth(now), StartOfMonth(now).AddDate(0, 1, 0))
	if cfg.Budgets.BlockUSD > 0 {
		bl := Blocks(sessions, prices, "", cfg.BlockHours, now)
		if n := len(bl); n > 0 && bl[n-1].Active {
			out = append(out, Budget{Name: "current block", Limit: cfg.Budgets.BlockUSD, Spent: bl[n-1].Cost, Reset: bl[n-1].End})
		} else {
			out = append(out, Budget{Name: "current block", Limit: cfg.Budgets.BlockUSD})
		}
	}
	return out
}

// Project aggregates sessions by working directory.
type Project struct {
	Cwd      string
	Name     string
	Sessions int
	Agents   map[string]int
	Cost     float64
	Cost7d   float64
	Tokens   int64
	Last     time.Time
	Live     int
}

func Projects(sessions []*model.Session, prices *pricing.Table, now time.Time) []Project {
	week := now.AddDate(0, 0, -7)
	m := map[string]*Project{}
	for _, s := range sessions {
		p, ok := m[s.Cwd]
		if !ok {
			p = &Project{Cwd: s.Cwd, Name: model.ProjectName(s.Cwd), Agents: map[string]int{}}
			m[s.Cwd] = p
		}
		p.Sessions++
		p.Agents[s.Agent]++
		p.Cost += s.Cost
		p.Tokens += s.Usage.Total()
		if s.Live {
			p.Live++
		}
		if s.End.After(p.Last) {
			p.Last = s.End
		}
		for _, b := range s.Buckets {
			if !time.Unix(b.Hour, 0).Before(week) {
				p.Cost7d += prices.Cost(b.Model, b.Usage)
			}
		}
	}
	out := make([]Project, 0, len(m))
	for _, p := range m {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

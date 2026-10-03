package notify

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

// Watcher turns agent state changes into notifications. Only one watcher
// runs per user (guarded by a file lock), so the web dashboard, the TUI and
// `optimus watch` can all start one without duplicate notifications.
type Watcher struct {
	Cfg config.Config

	mu   sync.Mutex
	last map[string]mux.State // window id → last state seen
	// OnChange, if set, is called for every state change (used by tests and
	// the web dashboard).
	OnChange func(w mux.Window, from, to mux.State, info agentstate.Info)
	// Advisories, if set, is polled about every 30s for other things worth a
	// notification (e.g. quota running out); each Key is sent once.
	Advisories func() []Event

	ticks int
	sent  map[string]bool
}

func lockPath() string { return filepath.Join(config.StateDir(), "watcher.lock") }

// TryLock takes the per-user watcher lock; it returns false when another
// watcher already runs. The lock is released when the process exits.
func TryLock() bool {
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		return false
	}
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false
	}
	return true // keep f open for the life of the process
}

// Run polls until ctx ends. Returns immediately if another watcher holds the
// lock.
func (w *Watcher) Run(ctx context.Context, every time.Duration) {
	if !TryLock() {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		w.Tick()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick checks every window once.
func (w *Watcher) Tick() {
	ws, err := mux.List()
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last == nil {
		w.last = map[string]mux.State{}
		w.sent = map[string]bool{}
	}
	if w.Advisories != nil && w.ticks%15 == 0 {
		for _, e := range w.Advisories() {
			if !w.sent[e.Key] {
				w.sent[e.Key] = true
				Send(w.Cfg, e)
			}
		}
	}
	w.ticks++
	alive := map[string]bool{}
	seen := map[string]bool{}
	for _, win := range ws {
		alive[win.Pane] = true
		seen[win.ID] = true
		screen, _ := mux.Capture(win.ID, 30)
		info := agentstate.Resolve(win, screen)
		mux.SetWindowOption(win.ID, "@optimus_state", string(info.State))
		prev, known := w.last[win.ID]
		w.last[win.ID] = info.State
		if !known || prev == info.State {
			continue
		}
		if w.OnChange != nil {
			w.OnChange(win, prev, info.State, info)
		}
		w.notify(win, prev, info)
	}
	for id := range w.last {
		if !seen[id] {
			delete(w.last, id)
		}
	}
	agentstate.Forget(alive)
}

func (w *Watcher) notify(win mux.Window, prev mux.State, info agentstate.Info) {
	n := w.Cfg.Notifications
	switch {
	case info.State == mux.StateWaiting && n.InputOn():
		body := info.Message
		if body == "" {
			body = "is waiting for your answer"
		}
		Send(w.Cfg, Event{Title: win.Name + " needs input", Body: body, Urgent: true, Window: win.ID})
	case info.State == mux.StateIdle && prev == mux.StateBusy && n.FinishOn():
		body := info.Message
		if body == "" || body == "finished" {
			body = "finished its turn"
		}
		Send(w.Cfg, Event{Title: win.Name + " is done", Body: body, Window: win.ID})
	}
}

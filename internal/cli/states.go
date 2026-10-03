package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/notify"
)

// cmdHook is called by the agents themselves (Claude Code hooks, Codex's
// notify program) from inside their tmux pane. It records the reported state
// and must stay silent and succeed: anything it prints could be read by the
// agent as a hook decision.
func cmdHook(args []string) error {
	if len(args) == 0 {
		return nil
	}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return nil // not running inside optimus
	}
	var (
		rec agentstate.Record
		ok  bool
	)
	switch args[0] {
	case "claude":
		payload, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		rec, ok = agentstate.FromClaude(payload)
	case "codex":
		if len(args) > 1 {
			rec, ok = agentstate.FromCodex([]byte(args[len(args)-1]))
		}
	}
	if !ok {
		return nil
	}
	rec.Pane = pane
	_ = agentstate.Write(rec)
	return nil
}

// waiting lists windows that need an answer, longest-waiting first.
func waiting() ([]mux.Window, map[string]agentstate.Info) {
	ws, _ := mux.List()
	infos := map[string]agentstate.Info{}
	var out []mux.Window
	for _, w := range ws {
		screen, _ := mux.Capture(w.ID, 30)
		info := agentstate.Resolve(w, screen)
		infos[w.ID] = info
		if info.State == mux.StateWaiting {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := infos[out[i].ID].Since, infos[out[j].ID].Since
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return a.Before(b)
	})
	return out, infos
}

// cmdNext prints (or with --switch, jumps to) the next agent needing input.
// The optimus tmux server binds it to Alt-n.
func cmdNext(args []string) error {
	fs := flag.NewFlagSet("next", flag.ExitOnError)
	sw := fs.Bool("switch", false, "make it the current window of attached clients")
	parse(fs, args)
	ws, infos := waiting()
	if len(ws) == 0 {
		if *sw {
			mux.Message(" no agent needs input ")
			return nil
		}
		fmt.Println("no agent needs input")
		return nil
	}
	// cycle: start after the window that's current now
	target := ws[0]
	if cur, err := mux.Current(); err == nil {
		for i, w := range ws {
			if w.ID == cur && len(ws) > 1 {
				target = ws[(i+1)%len(ws)]
			}
		}
	}
	if *sw {
		if err := mux.Select(target.ID); err != nil {
			return err
		}
		msg := infos[target.ID].Message
		if msg == "" {
			msg = "needs input"
		}
		mux.Message(fmt.Sprintf(" ◆ %s: %s  (%d waiting) ", target.Name, msg, len(ws)))
		return nil
	}
	for _, w := range ws {
		fmt.Printf("%d\t%s\t%s\t%s\n", w.Index, w.Name, w.Agent, infos[w.ID].Message)
	}
	return nil
}

// cmdWatch runs the notifier in the foreground (it also runs inside the web
// dashboard and the TUI; only one is active at a time).
func cmdWatch(a *app.App) error {
	if !notify.TryLock() {
		fmt.Println("a watcher is already running (web dashboard, TUI or another `optimus watch`)")
		return nil
	}
	fmt.Println("watching agents — notifications on input and when a turn finishes (Ctrl-C to stop)")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	w := &notify.Watcher{Cfg: a.Cfg, Advisories: a.SuggestionAdvisories(), OnChange: func(win mux.Window, from, to mux.State, info agentstate.Info) {
		fmt.Printf("%s  %-20s %s → %s  %s\n", time.Now().Format("15:04:05"), win.Name, from, to, info.Message)
	}}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		w.Tick()
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// cmdAnswer sends answer keys to an agent without attaching to it, e.g.
// `optimus answer 2 1` picks option 1 of the prompt in window 2.
func cmdAnswer(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: optimus answer <window> <key>...   (keys: 1 2 3 y n Enter Escape Tab Up Down C-c)")
	}
	w, err := mux.Resolve(args[0])
	if err != nil {
		return err
	}
	return mux.Keys(w.ID, args[1:]...)
}

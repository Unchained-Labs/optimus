package app

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/outside"
	"github.com/Unchained-Labs/optimus/internal/providers"
)

// Outside lists coding agents running outside optimus.
func (a *App) Outside(idx *index.Index) []outside.Agent { return outside.Scan(idx) }

// LinkedWindows presents agents running in your own tmux as fleet windows.
func LinkedWindows(agents []outside.Agent) []mux.Window { return outside.Windows(agents) }

// FindOutside looks an outside agent up by pid, session id (or prefix), or
// linked window id.
func (a *App) FindOutside(idx *index.Index, q string) (outside.Agent, bool) {
	for _, ag := range a.Outside(idx) {
		if fmt.Sprint(ag.PID) == q || (ag.SessionID != "" && (ag.SessionID == q || len(q) >= 4 && len(ag.SessionID) >= len(q) && ag.SessionID[:len(q)] == q)) || (ag.Pane != nil && ag.Pane.ID() == q) {
			return ag, true
		}
	}
	return outside.Agent{}, false
}

// TakeOver moves an outside agent into optimus: it asks the process to exit
// (SIGTERM, never SIGKILL), waits for it, then resumes the same session in the
// multiplexer — with hooks, notifications and the web terminal. A busy agent
// is only stopped with force, since its current turn would be cut short.
func (a *App) TakeOver(idx *index.Index, ag outside.Agent, force bool) (string, error) {
	if ag.Container {
		return "", fmt.Errorf("pid %d runs in a container; its session lives there and can't be resumed from here", ag.PID)
	}
	if ag.SessionID == "" {
		return "", fmt.Errorf("can't tell which %s session pid %d is writing, so it can't be resumed here", ag.Agent, ag.PID)
	}
	s := idx.Find(ag.SessionID)
	if s == nil {
		return "", fmt.Errorf("session %s isn't indexed yet — try again in a moment", ag.SessionID)
	}
	if p := providers.Get(ag.Agent); p == nil || p.ResumeArgs(s.ID, providers.LaunchOpts{}) == nil {
		return "", fmt.Errorf("%s sessions can't be resumed, so optimus can't take this one over", ag.Agent)
	}
	if ag.Status == "busy" && !force {
		return "", errors.New("it's in the middle of a turn; stopping it now would cut that turn short (force to do it anyway, or wait until it's idle)")
	}
	if err := syscall.Kill(ag.PID, syscall.SIGTERM); err != nil {
		return "", fmt.Errorf("couldn't stop pid %d: %w", ag.PID, err)
	}
	for i := 0; i < 100 && outside.Alive(ag.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if outside.Alive(ag.PID) {
		return "", fmt.Errorf("pid %d didn't exit after 10s; close it yourself (e.g. /exit) and run this again", ag.PID)
	}
	return a.Resume(s)
}

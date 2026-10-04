package mux

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ExtPane is a pane on one of your own tmux servers (not optimus's).
type ExtPane struct {
	Socket     string `json:"socket"` // socket name, e.g. "default"
	Path       string `json:"-"`      // socket path
	Pane       string `json:"pane"`   // %5
	Session    string `json:"session"`
	WindowID   string `json:"window_id"`
	WindowName string `json:"window_name"`
	WindowIdx  int    `json:"window_index"`
}

// ID is the window id optimus uses for an external pane.
func (p ExtPane) ID() string { return "ext:" + p.Socket + ":" + p.Pane }

// Target is the tmux target as you'd type it in that server.
func (p ExtPane) Target() string { return fmt.Sprintf("%s:%d", p.Session, p.WindowIdx) }

// IsExternal reports whether a window id points at one of your own tmux panes.
func IsExternal(id string) bool { return strings.HasPrefix(id, "ext:") }

func tmuxDir() string {
	if d := os.Getenv("TMUX_TMPDIR"); d != "" {
		return filepath.Join(d, fmt.Sprintf("tmux-%d", os.Getuid()))
	}
	return fmt.Sprintf("/tmp/tmux-%d", os.Getuid())
}

func parseExt(id string) (path, pane string, ok bool) {
	rest, found := strings.CutPrefix(id, "ext:")
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, ":")
	if i < 0 {
		return "", "", false
	}
	return filepath.Join(tmuxDir(), rest[:i]), rest[i+1:], true
}

func runOn(path string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command("tmux", append([]string{"-S", path}, args...)...)
	cmd.Env = envWithoutTMUX()
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New("tmux: " + msg)
	}
	return out.String(), nil
}

// on routes a command to the right tmux server for a window id.
func on(id string, args ...string) (string, error) {
	if path, pane, ok := parseExt(id); ok {
		for i, a := range args {
			if a == id {
				args[i] = pane
			}
		}
		return runOn(path, args...)
	}
	return run(args...)
}

// sockets lists tmux server sockets of this user; optimus ones (any name
// starting with "optimus") are yours only when mine is false.
func sockets(optimus bool) []string {
	ents, _ := os.ReadDir(tmuxDir())
	var out []string
	for _, e := range ents {
		isOpt := strings.HasPrefix(e.Name(), "optimus") || e.Name() == Socket
		if e.Type()&os.ModeSocket != 0 && isOpt == optimus {
			out = append(out, filepath.Join(tmuxDir(), e.Name()))
		}
	}
	return out
}

// ServerPIDs returns the process ids of optimus's tmux servers, so agents
// running under them aren't mistaken for outside agents.
func ServerPIDs() []int {
	var pids []int
	for _, s := range sockets(true) {
		if out, err := runOn(s, "display-message", "-p", "#{pid}"); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// UserPanes maps pane process id → pane, across your own tmux servers.
func UserPanes() map[int]ExtPane {
	out := map[int]ExtPane{}
	f := strings.Join([]string{"#{pane_pid}", "#{pane_id}", "#{session_name}", "#{window_id}", "#{window_name}", "#{window_index}", "#{session_name}"}, fieldSep)
	for _, s := range sockets(false) {
		res, err := runOn(s, "list-panes", "-a", "-F", f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(res), "\n") {
			p := strings.Split(line, fieldSep)
			if len(p) < 6 || strings.HasPrefix(p[2], "view-") {
				continue
			}
			pid, _ := strconv.Atoi(p[0])
			idx, _ := strconv.Atoi(p[5])
			out[pid] = ExtPane{Socket: filepath.Base(s), Path: s, Pane: p[1], Session: p[2], WindowID: p[3], WindowName: p[4], WindowIdx: idx}
		}
	}
	return out
}

// extViewCmd prepares a private view of an external pane's window, like
// ViewCmd but on your own tmux server: a grouped session (so your own
// clients' current window doesn't change), removed by cleanup.
func extViewCmd(id string) (*exec.Cmd, func(), error) {
	path, pane, _ := parseExt(id)
	out, err := runOn(path, "display-message", "-p", "-t", pane, "#{session_name}"+fieldSep+"#{window_id}")
	if err != nil {
		return nil, nil, err
	}
	f := strings.Split(strings.TrimSpace(out), fieldSep)
	if len(f) < 2 {
		return nil, nil, fmt.Errorf("pane %s not found", pane)
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	view := fmt.Sprintf("view-%x", b)
	if _, err := runOn(path, "new-session", "-d", "-s", view, "-t", "="+f[0]); err != nil {
		return nil, nil, err
	}
	cleanup := func() { _, _ = runOn(path, "kill-session", "-t", view) }
	_, _ = runOn(path, "set-option", "-t", view, "status", "off")
	if _, err := runOn(path, "select-window", "-t", view+":"+f[1]); err != nil {
		cleanup()
		return nil, nil, err
	}
	cmd := exec.Command("tmux", "-S", path, "attach-session", "-t", view)
	cmd.Env = append(envWithoutTMUX(), "TERM=xterm-256color")
	return cmd, cleanup, nil
}

// extAttachCmd shows an external pane in the foreground: inside that very
// tmux it just switches to it; otherwise it attaches a private view and
// removes it when you detach.
func extAttachCmd(id string) *exec.Cmd {
	path, pane, _ := parseExt(id)
	if sock := os.Getenv("TMUX"); sock != "" && strings.HasPrefix(sock, path+",") {
		cmd := exec.Command("sh", "-c", `tmux switch-client -t "$1" && tmux select-pane -t "$1"`, "sh", pane)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd
	}
	// a private grouped view, removed again when you detach
	script := `v="view-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
s=$(tmux -S "$1" display-message -p -t "$2" '#{session_name}') || exit 1
w=$(tmux -S "$1" display-message -p -t "$2" '#{window_id}')
tmux -S "$1" new-session -d -s "$v" -t "=$s" && tmux -S "$1" select-window -t "$v:$w"
tmux -S "$1" attach-session -t "$v"; tmux -S "$1" kill-session -t "$v" 2>/dev/null`
	cmd := exec.Command("sh", "-c", script, "sh", path, pane)
	cmd.Env = envWithoutTMUX()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

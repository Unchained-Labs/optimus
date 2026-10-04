// Package outside finds coding agents running outside optimus — in a plain
// terminal tab or in one of your own tmux servers — so optimus can show them,
// link them in place (when they run in tmux) or take them over (stop them
// and resume the same session inside optimus).
package outside

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

// Agent is a coding agent process running outside optimus.
type Agent struct {
	PID       int       `json:"pid"`
	Agent     string    `json:"agent"`
	Cwd       string    `json:"cwd"`
	Started   time.Time `json:"started"`
	SessionID string    `json:"session_id,omitempty"`
	Status    string    `json:"status,omitempty"` // as the agent reports it (Claude: busy, idle…)
	Title     string    `json:"title,omitempty"`
	// Pane is set when the agent runs in one of your tmux servers: optimus
	// can then show and drive it where it is.
	Pane *mux.ExtPane `json:"pane,omitempty"`
	// Container: the agent runs in a container (another mount namespace), so
	// its session lives there and can't be resumed from here.
	Container bool `json:"container,omitempty"`
}

// Where says how the agent runs, for display.
func (a Agent) Where() string {
	if a.Container {
		return "a container"
	}
	if a.Pane != nil {
		return "your tmux (" + a.Pane.Socket + ")"
	}
	return "a terminal tab"
}

// known agent executables (by process name or script name)
var known = map[string]string{
	"claude": "claude", "codex": "codex", "opencode": "opencode", "gemini": "gemini",
	"cursor-agent": "cursor-agent", "aider": "aider", "goose": "goose", "crush": "crush", "amp": "amp",
}

type proc struct {
	pid, ppid int
	comm      string
	args      []string
}

func agentOf(p proc) string {
	cands := []string{p.comm}
	for i, a := range p.args {
		if i > 2 {
			break
		}
		cands = append(cands, filepath.Base(a))
	}
	for _, c := range cands {
		if ag, ok := known[strings.TrimSuffix(c, ".js")]; ok {
			return ag
		}
	}
	return ""
}

var (
	cacheMu sync.Mutex
	cacheAt time.Time
	cached  []Agent
)

// Scan lists agents running outside optimus, matched to their sessions in
// idx (may be nil). The process scan is cached briefly so dashboards polling
// every second stay cheap; session matching runs on every call.
func Scan(idx *index.Index) []Agent {
	cacheMu.Lock()
	if time.Since(cacheAt) >= 2*time.Second || cached == nil {
		cached, cacheAt = scan(), time.Now()
	}
	raw := cached
	cacheMu.Unlock()
	out := make([]Agent, len(raw))
	for i, a := range raw {
		if s := sessionFor(idx, a); s != nil {
			a.SessionID, a.Title = s.ID, s.DisplayTitle()
		}
		out[i] = a
	}
	return out
}

func scan() []Agent {
	procs := processes()
	byPID := map[int]proc{}
	for _, p := range procs {
		byPID[p.pid] = p
	}
	optimusServers := map[int]bool{}
	for _, pid := range mux.ServerPIDs() {
		optimusServers[pid] = true
	}
	panes := mux.UserPanes() // pane pid → pane, on your own tmux servers
	live := claudeLive()

	var out []Agent
	for _, p := range procs {
		ag := agentOf(p)
		if ag == "" {
			continue
		}
		// skip helpers spawned by an agent, and anything under optimus
		inOptimus, nested := false, false
		var pane *mux.ExtPane
		for cur, hops := byPID[p.ppid], 0; cur.pid > 1 && hops < 40; cur, hops = byPID[cur.ppid], hops+1 {
			if optimusServers[cur.pid] {
				inOptimus = true
				break
			}
			if agentOf(cur) == ag {
				nested = true
				break
			}
			if pn, ok := panes[cur.pid]; ok && pane == nil {
				pn := pn
				pane = &pn
			}
		}
		if pn, ok := panes[p.pid]; ok && pane == nil { // the agent is the pane process itself
			pane = &pn
		}
		if inOptimus || nested {
			continue
		}
		a := Agent{PID: p.pid, Agent: ag, Cwd: cwdOf(p.pid), Started: startTime(p.pid), Pane: pane, Container: otherNamespace(p.pid)}
		if l, ok := live[p.pid]; ok {
			a.SessionID, a.Status = l.SessionID, l.Status
			if a.Cwd == "" {
				a.Cwd = l.Cwd
			}
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// sessionFor finds the transcript an outside agent is writing: Claude says so
// itself; for the others, the newest session of that agent in that folder
// that was active since the process started.
func sessionFor(idx *index.Index, a Agent) *model.Session {
	if idx == nil {
		return nil
	}
	if a.SessionID != "" {
		return idx.Find(a.SessionID)
	}
	for _, s := range idx.Sessions { // newest first
		if s.Agent == a.Agent && s.Cwd == a.Cwd && !s.End.Before(a.Started.Add(-time.Minute)) {
			return s
		}
	}
	return nil
}

type liveClaude struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
}

// claudeLive reads Claude Code's own registry of running sessions.
func claudeLive() map[int]liveClaude {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(config.Home(), ".claude")
	}
	out := map[int]liveClaude{}
	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var v struct {
			PID int `json:"pid"`
			liveClaude
		}
		if json.Unmarshal(b, &v) == nil && v.PID > 0 {
			out[v.PID] = v.liveClaude
		}
	}
	return out
}

// --- process table ------------------------------------------------------------

func processes() []proc {
	if runtime.GOOS == "linux" {
		return procfs()
	}
	return psTable()
}

func procfs() []proc {
	ents, _ := os.ReadDir("/proc")
	var out []proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		s := string(st)
		l, r := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if l < 0 || r < l {
			continue
		}
		fields := strings.Fields(s[r+1:])
		if len(fields) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		p := proc{pid: pid, ppid: ppid, comm: s[l+1 : r]}
		if cl, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
			p.args = strings.Split(strings.TrimRight(string(cl), "\x00"), "\x00")
		}
		out = append(out, p)
	}
	return out
}

func psTable() []proc {
	b, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil
	}
	var out []proc
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		comm := strings.Join(f[2:], " ")
		out = append(out, proc{pid: pid, ppid: ppid, comm: filepath.Base(comm), args: []string{comm}})
	}
	return out
}

func cwdOf(pid int) string {
	if runtime.GOOS == "linux" {
		p, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		return p
	}
	b, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "n") {
			return l[1:]
		}
	}
	return ""
}

var (
	bootOnce sync.Once
	bootTime time.Time
)

func startTime(pid int) time.Time {
	if runtime.GOOS != "linux" {
		return time.Time{}
	}
	bootOnce.Do(func() {
		b, _ := os.ReadFile("/proc/stat")
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "btime ") {
				n, _ := strconv.ParseInt(strings.TrimSpace(l[6:]), 10, 64)
				bootTime = time.Unix(n, 0)
			}
		}
	})
	st, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}
	}
	s := string(st)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 20 {
		return time.Time{}
	}
	ticks, _ := strconv.ParseInt(f[19], 10, 64) // field 22: starttime, in clock ticks
	return bootTime.Add(time.Duration(ticks) * time.Second / 100)
}

// Alive reports whether a process still exists.
func Alive(pid int) bool {
	if runtime.GOOS == "linux" {
		_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
		return err == nil
	}
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil
}

// Windows presents agents running in your own tmux as fleet windows: optimus
// shows, answers and drives them where they are.
func Windows(agents []Agent) []mux.Window {
	var ws []mux.Window
	for _, ag := range agents {
		if ag.Pane == nil {
			continue
		}
		ws = append(ws, mux.Window{
			ID: ag.Pane.ID(), Index: -1, Name: ag.Agent + "@" + ag.Pane.Target(), Agent: ag.Agent,
			Cwd: ag.Cwd, SessionID: ag.SessionID, Created: ag.Started, External: true,
		})
	}
	return ws
}

// otherNamespace reports whether a process lives in another mount namespace
// (a container): its files and sessions aren't ours to resume.
func otherNamespace(pid int) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	mine, err1 := os.Readlink("/proc/self/ns/mnt")
	theirs, err2 := os.Readlink(fmt.Sprintf("/proc/%d/ns/mnt", pid))
	return err1 == nil && err2 == nil && mine != theirs
}

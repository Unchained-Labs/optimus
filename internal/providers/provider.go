// Package providers adapts each coding agent (Claude Code, Codex, opencode,
// ...) to optimus: where its sessions live, how to parse them, and how to
// launch or resume it.
package providers

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/model"
)

// Source is one session on disk. Key changes whenever the session changes and
// is what the index cache is keyed on.
type Source struct {
	Agent string
	ID    string
	Path  string
	Key   string
}

type Provider interface {
	Name() string
	// Binary is the default executable name.
	Binary() string
	// Discover lists session sources. Launch-only providers return nil.
	Discover() ([]Source, error)
	Parse(src Source) (*model.Session, error)
	Transcript(s *model.Session) ([]model.Message, error)
	// NewArgs returns args to start a fresh session, optionally with an
	// initial prompt. sessionID is a pre-assigned id the agent should use, if
	// it supports that ("" otherwise).
	NewArgs(prompt, sessionID string, o LaunchOpts) []string
	ResumeArgs(id string, o LaunchOpts) []string
	// PresetID reports whether NewArgs honours a pre-assigned session id.
	PresetID() bool
}

// LaunchOpts are agent-independent launch preferences; each provider maps
// what it supports to flags.
type LaunchOpts struct {
	Name          string // display name for the session
	RemoteControl bool   // make the session controllable from the agent's own apps
}

// LiveSession is a session the agent itself reports as running (outside of
// optimus's own tmux server).
type LiveSession struct {
	Agent  string
	ID     string
	PID    int
	Cwd    string
	Name   string
	Status string
}

// LiveReporter is implemented by providers that can tell which of their
// sessions are currently running.
type LiveReporter interface {
	Live() []LiveSession
}

var registry []Provider

func register(p Provider) { registry = append(registry, p) }

// order puts agents whose history optimus can read first.
var order = map[string]int{"claude": 0, "codex": 1, "opencode": 2}

func All() []Provider {
	out := append([]Provider(nil), registry...)
	sort.SliceStable(out, func(i, j int) bool {
		oi, ok := order[out[i].Name()]
		if !ok {
			oi = 99
		}
		oj, ok := order[out[j].Name()]
		if !ok {
			oj = 99
		}
		return oi < oj
	})
	return out
}

func Get(name string) Provider {
	for _, p := range registry {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func Names() []string {
	var out []string
	for _, p := range All() {
		out = append(out, p.Name())
	}
	return out
}

// Command resolves the executable and base args for an agent, honouring the
// config's per-agent overrides.
func Command(cfg config.Config, p Provider) (string, []string) {
	bin := p.Binary()
	var extra []string
	if a, ok := cfg.Agents[p.Name()]; ok {
		if a.Command != "" {
			bin = a.Command
		}
		extra = a.Args
	}
	return bin, extra
}

func Installed(cfg config.Config, p Provider) bool {
	bin, _ := Command(cfg, p)
	_, err := exec.LookPath(bin)
	return err == nil
}

// --- shared helpers -------------------------------------------------------

func fileKey(path string) (string, time.Time, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, false
	}
	return keyOf(st), st.ModTime(), true
}

func keyOf(st os.FileInfo) string {
	return st.ModTime().UTC().Format(time.RFC3339Nano) + "/" + itoa(st.Size())
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// eachLine calls fn for every line of a JSONL file, tolerating very long lines.
func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// assemble an oversized line
			buf := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				line, err = r.ReadSlice('\n')
				buf = append(buf, line...)
			}
			line = buf
		}
		if len(line) > 1 {
			fn(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func hourOf(t time.Time) int64 { return t.Truncate(time.Hour).Unix() }

// bucketer accumulates usage into per-hour, per-model buckets.
type bucketer map[[2]string]*model.Bucket

func (b bucketer) add(t time.Time, m string, u model.Usage) {
	h := hourOf(t)
	k := [2]string{itoa(h), m}
	bk, ok := b[k]
	if !ok {
		bk = &model.Bucket{Hour: h, Model: m}
		b[k] = bk
	}
	bk.Usage.Add(u)
}

func (b bucketer) list() []model.Bucket {
	out := make([]model.Bucket, 0, len(b))
	for _, v := range b {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hour != out[j].Hour {
			return out[i].Hour < out[j].Hour
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// dominantModel picks the model with the most output tokens.
func dominantModel(bs []model.Bucket) string {
	tot := map[string]int64{}
	for _, b := range bs {
		tot[b.Model] += b.Output + b.Input + 1
	}
	best, bv := "", int64(-1)
	for m, v := range tot {
		if v > bv || (v == bv && m < best) {
			best, bv = m, v
		}
	}
	return best
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	if ms < 1e12 { // seconds
		return time.Unix(ms, 0)
	}
	return time.UnixMilli(ms)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat("/proc/" + itoa(int64(pid)))
	if err == nil {
		return true
	}
	// non-Linux: signal 0
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// isNoise reports user text that is harness plumbing rather than something a
// human typed.
func isNoise(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return true
	}
	for _, p := range []string{"<system-reminder>", "<local-command-", "<command-message>", "<environment_context>", "<user_instructions>", "Caveat: The messages below", "[Request interrupted", "<task-notification>"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// commandName extracts "/foo args" from Claude Code's <command-name> markup.
func commandName(t string) (string, bool) {
	i := strings.Index(t, "<command-name>")
	if i < 0 {
		return "", false
	}
	rest := t[i+len("<command-name>"):]
	j := strings.Index(rest, "</command-name>")
	if j < 0 {
		return "", false
	}
	name := strings.TrimSpace(rest[:j])
	if a := strings.Index(t, "<command-args>"); a >= 0 {
		ar := t[a+len("<command-args>"):]
		if b := strings.Index(ar, "</command-args>"); b >= 0 && strings.TrimSpace(ar[:b]) != "" {
			name += " " + strings.TrimSpace(ar[:b])
		}
	}
	return name, true
}

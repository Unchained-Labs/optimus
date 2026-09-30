// Package handoff turns a session transcript into a compact context document
// another agent (or another session of the same agent) can pick up from.
package handoff

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/model"
)

type Options struct {
	MaxTokens int // approximate budget for the whole document
	Note      string
}

// charsPerToken is a rough heuristic good enough for budgeting.
const charsPerToken = 4

// Build renders the handoff document.
func Build(s *model.Session, msgs []model.Message, o Options) string {
	if o.MaxTokens <= 0 {
		o.MaxTokens = 20000
	}
	budget := o.MaxTokens * charsPerToken

	var head strings.Builder
	fmt.Fprintf(&head, "# Handoff: %s\n\n", s.DisplayTitle())
	fmt.Fprintf(&head, "Context transferred by optimus from a previous **%s** session. Treat it as background: the work below was done in that session, not by you.\n\n", s.Agent)
	fmt.Fprintf(&head, "| | |\n|---|---|\n")
	fmt.Fprintf(&head, "| Session | `%s` (%s) |\n", s.ID, s.Agent)
	if s.Cwd != "" {
		fmt.Fprintf(&head, "| Project | `%s` |\n", s.Cwd)
	}
	if s.Branch != "" {
		fmt.Fprintf(&head, "| Branch | `%s` |\n", s.Branch)
	}
	if s.Model != "" {
		fmt.Fprintf(&head, "| Model | %s |\n", s.Model)
	}
	if !s.Start.IsZero() {
		fmt.Fprintf(&head, "| When | %s → %s |\n", s.Start.Local().Format("2006-01-02 15:04"), s.End.Local().Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&head, "| Turns | %d |\n\n", s.Messages)

	if o.Note != "" {
		fmt.Fprintf(&head, "## Note from the user\n\n%s\n\n", strings.TrimSpace(o.Note))
	}

	msgs = mergeAssistant(msgs)
	var firstUser, lastUser string
	for _, m := range msgs {
		if m.Role == "user" && !isSlashCommand(m.Text) {
			if firstUser == "" {
				firstUser = m.Text
			}
			lastUser = m.Text
		}
	}
	if firstUser != "" {
		fmt.Fprintf(&head, "## Original request\n\n%s\n\n", quote(clip(firstUser, 3000)))
	}

	edited, read, cmds := files(msgs)
	if len(edited) > 0 {
		head.WriteString("## Files changed\n\n")
		bulletList(&head, edited, 60)
	}
	if len(read) > 0 {
		head.WriteString("## Files consulted\n\n")
		bulletList(&head, read, 40)
	}
	if len(cmds) > 0 {
		head.WriteString("## Recent commands\n\n```\n")
		for _, c := range cmds {
			head.WriteString(c + "\n")
		}
		head.WriteString("```\n\n")
	}

	var tail strings.Builder
	tail.WriteString("## Where it left off\n\n")
	if lastUser != "" {
		fmt.Fprintf(&tail, "The most recent request was:\n\n%s\n\n", quote(clip(lastUser, 2000)))
	}
	tail.WriteString("Continue from here. Verify the current state of the files before assuming earlier steps succeeded.\n")

	remaining := budget - head.Len() - tail.Len() - 200
	convo := conversation(msgs, remaining)

	return head.String() + convo + tail.String()
}

func isSlashCommand(t string) bool {
	t = strings.TrimSpace(t)
	return strings.HasPrefix(t, "/") && !strings.Contains(t, "\n") && !strings.Contains(strings.Fields(t)[0][1:], "/")
}

// mergeAssistant folds consecutive assistant messages into one.
func mergeAssistant(msgs []model.Message) []model.Message {
	var out []model.Message
	for _, m := range msgs {
		if n := len(out); n > 0 && m.Role == "assistant" && out[n-1].Role == "assistant" {
			prev := &out[n-1]
			if strings.TrimSpace(m.Text) != "" {
				if strings.TrimSpace(prev.Text) != "" {
					prev.Text += "\n\n"
				}
				prev.Text += m.Text
			}
			prev.Tools = append(append([]model.ToolCall(nil), prev.Tools...), m.Tools...)
			continue
		}
		out = append(out, m)
	}
	return out
}

// conversation renders messages newest-first until the budget runs out, then
// restores chronological order.
func conversation(msgs []model.Message, budget int) string {
	if budget < 500 || len(msgs) == 0 {
		return ""
	}
	var blocks []string
	used, omitted := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		b := render(msgs[i])
		if b == "" {
			continue
		}
		if used+len(b) > budget {
			omitted = i + 1
			break
		}
		blocks = append(blocks, b)
		used += len(b)
	}
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
	}
	var sb strings.Builder
	sb.WriteString("## Conversation (most recent)\n\n")
	if omitted > 0 {
		fmt.Fprintf(&sb, "_%d earlier messages omitted._\n\n", omitted)
	}
	for _, b := range blocks {
		sb.WriteString(b)
	}
	return sb.String()
}

func render(m model.Message) string {
	var sb strings.Builder
	switch m.Role {
	case "user":
		sb.WriteString("**User:**\n")
		sb.WriteString(quote(clip(m.Text, 2500)))
		sb.WriteString("\n\n")
	case "assistant":
		if strings.TrimSpace(m.Text) == "" && len(m.Tools) == 0 {
			return ""
		}
		sb.WriteString("**Assistant:**")
		if t := strings.TrimSpace(m.Text); t != "" {
			sb.WriteString("\n")
			sb.WriteString(clip(t, 1800))
		}
		sb.WriteString("\n")
		lines, more := limit(toolLines(m.Tools), 8)
		for _, tc := range lines {
			sb.WriteString(tc + "\n")
		}
		if more > 0 {
			fmt.Fprintf(&sb, "- … %d more tool calls\n", more)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func toolLines(ts []model.ToolCall) []string {
	var out []string
	for _, t := range ts {
		line := "- ⚙ " + t.Name
		if t.Target != "" {
			line += " `" + strings.ReplaceAll(t.Target, "`", "'") + "`"
		}
		out = append(out, line)
	}
	return out
}

var readTools = map[string]bool{"read": true, "read_file": true, "view": true, "notebookread": true, "cat": true}
var shellTools = map[string]bool{"bash": true, "shell": true, "local_shell_call": true, "exec_command": true, "run_terminal_cmd": true}

func files(msgs []model.Message) (edited, read, cmds []string) {
	em, rm := map[string]time.Time{}, map[string]time.Time{}
	order := 0
	for _, m := range msgs {
		for _, t := range m.Tools {
			order++
			ts := m.Time.Add(time.Duration(order))
			name := strings.ToLower(t.Name)
			switch {
			case t.Edit && t.Target != "":
				em[t.Target] = ts
			case readTools[name] && t.Target != "":
				rm[t.Target] = ts
			case shellTools[name] && t.Target != "":
				cmds = append(cmds, t.Target)
			}
		}
	}
	for f := range em {
		delete(rm, f)
	}
	return recent(em), recent(rm), lastN(cmds, 15)
}

func recent(m map[string]time.Time) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]].After(m[out[j]]) })
	return out
}

func lastN(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// limit keeps the first n items and reports how many were dropped.
func limit(s []string, n int) ([]string, int) {
	if len(s) > n {
		return s[:n], len(s) - n
	}
	return s, 0
}

func bulletList(sb *strings.Builder, items []string, n int) {
	items, more := limit(items, n)
	for _, f := range items {
		fmt.Fprintf(sb, "- `%s`\n", f)
	}
	if more > 0 {
		fmt.Fprintf(sb, "- … and %d more\n", more)
	}
	sb.WriteString("\n")
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, "\n "); i > n/2 {
		cut = cut[:i]
	}
	return cut + fmt.Sprintf("\n[… %d chars truncated]", len(s)-len(cut))
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}

// Save writes the document under the optimus state dir and returns its path.
func Save(s *model.Session, doc string) (string, error) {
	dir := config.HandoffDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s-%s.md", time.Now().Format("20060102-150405"), s.Agent, s.ShortID())
	p := filepath.Join(dir, name)
	return p, os.WriteFile(p, []byte(doc), 0o644)
}

// Kickoff is the short first prompt given to the receiving agent; the full
// context stays in the file so it never hits argv or paste limits.
func Kickoff(path string) string {
	return fmt.Sprintf("Read the handoff document at %s — it contains the context of a previous agent session on this project. Summarize your understanding in a few lines, then continue the work from where it left off.", path)
}

const summarizePrompt = `You are compressing an AI coding session transcript into a handoff brief for another engineer/agent who will continue the work.
Write markdown with these sections: Goal, Current state, Key decisions & constraints, Files touched (with one-line purpose), Open problems / next steps.
Be specific (paths, commands, error messages). Omit pleasantries. Keep it under 800 words. The transcript follows on stdin.`

// Summarize asks an agent CLI in non-interactive mode to condense a document.
func Summarize(agentBin, agent, doc string) (string, error) {
	var args []string
	switch agent {
	case "claude":
		args = []string{"-p", summarizePrompt}
	case "codex":
		args = []string{"exec", summarizePrompt + "\n\n" + doc}
		doc = ""
	case "opencode":
		args = []string{"run", summarizePrompt + "\n\n" + doc}
		doc = ""
	default:
		return "", fmt.Errorf("summarizing with %q is not supported (use claude, codex or opencode)", agent)
	}
	cmd := exec.Command(agentBin, args...)
	cmd.Stdin = strings.NewReader(doc)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %v: %s", agent, err, strings.TrimSpace(errb.String()))
	}
	res := strings.TrimSpace(out.String())
	if res == "" {
		return "", errors.New("summarizer returned nothing")
	}
	return res, nil
}

package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeHome(t *testing.T) string {
	h := t.TempDir()
	t.Setenv("OPTIMUS_AGENT_HOME", h)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("OPENCODE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	return h
}

func only(t *testing.T, p Provider) Source {
	t.Helper()
	srcs, err := p.Discover()
	if err != nil || len(srcs) != 1 {
		t.Fatalf("discover: %v %d sources", err, len(srcs))
	}
	return srcs[0]
}

func TestClaude(t *testing.T) {
	h := fakeHome(t)
	id := "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(h, ".claude", "projects", "-work-app")
	lines := []string{
		`{"type":"user","timestamp":"2026-09-01T10:00:00Z","cwd":"/work/app","gitBranch":"main","message":{"role":"user","content":"<command-name>/model</command-name>"}}`,
		`{"type":"user","timestamp":"2026-09-01T10:00:05Z","cwd":"/work/app","message":{"role":"user","content":"fix the login bug"}}`,
		// one API response split over two lines (same id) must be counted once
		`{"type":"assistant","timestamp":"2026-09-01T10:00:10Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Looking."}],"usage":{"input_tokens":10,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":500}}}}`,
		`{"type":"assistant","timestamp":"2026-09-01T10:00:11Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/work/app/login.go"}}],"usage":{"input_tokens":10,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}`,
		`{"type":"user","timestamp":"2026-09-01T10:00:12Z","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"ai-title","aiTitle":"Login bug fix"}`,
		`not json`,
	}
	write(t, filepath.Join(dir, id+".jsonl"), strings.Join(lines, "\n")+"\n")
	write(t, filepath.Join(dir, id, "subagents", "agent-a.jsonl"),
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-01T10:05:00Z","requestId":"r9","message":{"id":"m9","model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":5,"output_tokens":7}}}`+"\n")

	c := Claude{}
	s, err := c.Parse(only(t, c))
	if err != nil || s == nil {
		t.Fatal(err)
	}
	if s.Title != "Login bug fix" || s.Cwd != "/work/app" || s.Branch != "main" {
		t.Errorf("meta: %+v", s)
	}
	if s.FirstPrompt != "fix the login bug" || s.Messages != 2 {
		t.Errorf("prompts: first=%q messages=%d", s.FirstPrompt, s.Messages)
	}
	var out, cw1 int64
	models := map[string]bool{}
	for _, b := range s.Buckets {
		out += b.Output
		cw1 += b.CacheWrite1h
		models[b.Model] = true
	}
	if out != 107 || cw1 != 500 || !models["claude-haiku-4-5"] {
		t.Errorf("usage: out=%d cw1=%d models=%v", out, cw1, models)
	}
	msgs, _ := c.Transcript(s)
	if len(msgs) != 3 || msgs[2].Role != "assistant" || len(msgs[2].Tools) != 1 || !msgs[2].Tools[0].Edit {
		t.Errorf("transcript: %+v", msgs)
	}
}

func TestCodex(t *testing.T) {
	h := fakeHome(t)
	id := "0199aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	lines := []string{
		`{"timestamp":"2026-09-02T08:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/work/api","git":{"branch":"dev"}}}`,
		`{"timestamp":"2026-09-02T08:00:01Z","type":"turn_context","payload":{"cwd":"/work/api","model":"gpt-5-codex"}}`,
		`{"timestamp":"2026-09-02T08:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>cwd</environment_context>"}]}}`,
		`{"timestamp":"2026-09-02T08:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add rate limiting"}]}}`,
		`{"timestamp":"2026-09-02T08:00:03Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"ls src\"]}"}}`,
		`{"timestamp":"2026-09-02T08:00:04Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: src/limit.rs\n@@"}}`,
		`{"timestamp":"2026-09-02T08:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":50,"reasoning_output_tokens":20}},"rate_limits":{"primary":{"used_percent":12.5,"window_minutes":300,"resets_in_seconds":3600}}}}`,
		`{"timestamp":"2026-09-02T08:00:06Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":50,"reasoning_output_tokens":20}}}}`,
		`{"timestamp":"2026-09-02T08:00:07Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2500,"cached_input_tokens":1600,"output_tokens":80,"reasoning_output_tokens":20}}}}`,
		`{"timestamp":"2026-09-02T08:00:08Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done."}]}}`,
	}
	write(t, filepath.Join(h, ".codex", "sessions", "2026", "09", "02", "rollout-2026-09-02T08-00-00-"+id+".jsonl"), strings.Join(lines, "\n")+"\n")

	c := Codex{}
	s, err := c.Parse(only(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != id || s.Cwd != "/work/api" || s.Branch != "dev" || s.Model != "gpt-5-codex" || s.Messages != 1 {
		t.Errorf("meta: %+v", s)
	}
	var in, cr, out int64
	for _, b := range s.Buckets {
		in, cr, out = in+b.Input, cr+b.CacheRead, out+b.Output
	}
	// totals 2500 in (1600 cached) / 80 out; duplicate event must not double count
	if in != 900 || cr != 1600 || out != 80 {
		t.Errorf("usage in=%d cr=%d out=%d", in, cr, out)
	}
	msgs, _ := c.Transcript(s)
	if len(msgs) != 3 {
		t.Fatalf("transcript: %+v", msgs)
	}
	tools := msgs[1].Tools
	if len(tools) != 2 || tools[0].Target != "ls src" || tools[1].Target != "src/limit.rs" || !tools[1].Edit {
		t.Errorf("tools: %+v", tools)
	}
	rl, _ := c.LatestRateLimits()
	if !strings.Contains(string(rl), "used_percent") {
		t.Errorf("rate limits: %s", rl)
	}
}

func TestOpenCode(t *testing.T) {
	h := fakeHome(t)
	root := filepath.Join(h, ".local", "share", "opencode", "storage")
	write(t, filepath.Join(root, "session", "proj1", "ses_abc.json"),
		`{"id":"ses_abc","title":"Refactor db","directory":"/work/db","time":{"created":1788000000000,"updated":1788000600000}}`)
	write(t, filepath.Join(root, "message", "ses_abc", "msg_1.json"),
		`{"id":"msg_1","role":"user","time":{"created":1788000000000}}`)
	write(t, filepath.Join(root, "part", "msg_1", "prt_1.json"), `{"type":"text","text":"split the db module"}`)
	write(t, filepath.Join(root, "message", "ses_abc", "msg_2.json"),
		`{"id":"msg_2","role":"assistant","modelID":"claude-sonnet-4-5","providerID":"anthropic","cost":0.05,"tokens":{"input":100,"output":40,"reasoning":10,"cache":{"read":2000,"write":300}},"time":{"created":1788000060000}}`)
	write(t, filepath.Join(root, "part", "msg_2", "prt_1.json"), `{"type":"text","text":"Splitting now."}`)
	write(t, filepath.Join(root, "part", "msg_2", "prt_2.json"), `{"type":"tool","tool":"edit","state":{"input":{"filePath":"/work/db/a.go"}}}`)

	o := OpenCode{}
	s, err := o.Parse(only(t, o))
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Refactor db" || s.Cwd != "/work/db" || s.Messages != 1 || s.FirstPrompt != "split the db module" {
		t.Errorf("meta: %+v", s)
	}
	if len(s.Buckets) != 1 || s.Buckets[0].Output != 50 || s.Buckets[0].CacheRead != 2000 || s.Buckets[0].SourceCost != 0.05 {
		t.Errorf("buckets: %+v", s.Buckets)
	}
	msgs, _ := o.Transcript(s)
	if len(msgs) != 2 || len(msgs[1].Tools) != 1 || msgs[1].Tools[0].Target != "/work/db/a.go" || !msgs[1].Tools[0].Edit {
		t.Errorf("transcript: %+v", msgs)
	}
}

func TestClaudeLaunchArgs(t *testing.T) {
	c := Claude{}
	got := strings.Join(c.NewArgs("fix it", "sid", LaunchOpts{Name: "api", RemoteControl: true}), " ")
	if got != "--session-id sid --name=api --remote-control=api fix it" {
		t.Errorf("new: %q", got)
	}
	// unnamed remote control must not swallow the prompt
	got = strings.Join(c.NewArgs("fix it", "", LaunchOpts{RemoteControl: true}), " ")
	if got != "--remote-control -- fix it" {
		t.Errorf("unnamed: %q", got)
	}
	got = strings.Join(c.ResumeArgs("abc", LaunchOpts{Name: "api", RemoteControl: true}), " ")
	if got != "--resume abc --name=api --remote-control=api" {
		t.Errorf("resume: %q", got)
	}
	if a := c.NewArgs("", "", LaunchOpts{}); len(a) != 0 {
		t.Errorf("bare: %v", a)
	}
}

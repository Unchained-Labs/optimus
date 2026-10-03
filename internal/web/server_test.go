package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

// setup isolates config, agent history and the tmux server.
func setup(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPTIMUS_HOME", dir+"/optimus")
	t.Setenv("OPTIMUS_AGENT_HOME", dir+"/home")
	t.Setenv("CLAUDE_CONFIG_DIR", dir+"/home/.claude")
	sock := fmt.Sprintf("optimus-test-%d", time.Now().UnixNano())
	mux.Socket = sock
	t.Cleanup(func() { exec.Command("tmux", "-L", sock, "kill-server").Run() })
	a := app.New()
	off := false
	a.Cfg.Remote.WebAutostart = &off
	s, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func client(t *testing.T, ts *httptest.Server, s *Server) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(ts.URL + "/?token=" + s.token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return c
}

func TestAuth(t *testing.T) {
	ts, s := setup(t)
	resp, _ := http.Get(ts.URL + "/api/state")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	jar, _ := cookiejar.New(nil)
	bad := &http.Client{Jar: jar}
	bad.Get(ts.URL + "/?token=wrong")
	if resp, _ := bad.Get(ts.URL + "/api/state"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token accepted: %d", resp.StatusCode)
	}

	c := client(t, ts, s)
	resp, err := c.Get(ts.URL + "/api/state")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("state with cookie: %v %d", err, resp.StatusCode)
	}
	var st map[string]any
	json.NewDecoder(resp.Body).Decode(&st)
	if _, ok := st["windows"]; !ok || st["default_agent"] != "claude" {
		t.Errorf("state: %v", st)
	}

	// cross-origin state change is refused even with the cookie
	req, _ := http.NewRequest("POST", ts.URL+"/api/windows", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	if resp, _ := c.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d", resp.StatusCode)
	}
	// bearer header works for scripts
	req, _ = http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusOK {
		t.Errorf("bearer: %d", resp.StatusCode)
	}
}

func TestLaunchValidation(t *testing.T) {
	ts, s := setup(t)
	c := client(t, ts, s)
	post := func(body string) (int, string) {
		resp, err := c.Post(ts.URL+"/api/windows", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var e map[string]string
		json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, e["error"]
	}
	if code, msg := post(`{"agent":"shell","dir":"/definitely/not/here"}`); code != 400 || !strings.Contains(msg, "not a directory") {
		t.Errorf("bad dir: %d %s", code, msg)
	}
	if code, msg := post(`{"agent":"nope","dir":"/tmp"}`); code != 400 || !strings.Contains(msg, "unknown agent") {
		t.Errorf("bad agent: %d %s", code, msg)
	}
}

func TestTerminalRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ts, s := setup(t)
	c := client(t, ts, s)
	dir := t.TempDir()
	// explicit prompt: the default differs between users ($) and root (#)
	id, err := mux.Spawn("sh-test", "shell", dir, "", []string{"env", "PS1=optimus-test> ", "sh"})
	if err != nil {
		t.Fatal(err)
	}

	// keys outside the allow-list are rejected
	resp, _ := c.Post(ts.URL+"/api/windows/"+id+"/keys", "application/json", strings.NewReader(`{"keys":["rm -rf"]}`))
	if resp.StatusCode != 400 {
		t.Errorf("disallowed key: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	u := strings.Replace(ts.URL, "http", "ws", 1) + "/api/term/" + id + "?cols=100&rows=30"
	ws, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":25}`)); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	// wait for tmux to draw the shell prompt, like a person would
	for !strings.Contains(seen.String(), "optimus-test>") { // tmux may draw the trailing space as a cursor move
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("no prompt: %q (%v)", seen.String(), err)
		}
		seen.Write(data)
	}
	if strings.Contains(seen.String(), "OPTIMUS") {
		t.Errorf("browser view should not show the tmux status bar")
	}
	seen.Reset()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte("echo optimus-$((40+2))\r")); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(seen.String(), "optimus-42") {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("never saw command output; got %q (%v)", seen.String(), err)
		}
		seen.Write(data)
	}
	ws.Close(websocket.StatusNormalClosure, "")

	// the private view session is cleaned up and the agent window survives
	time.Sleep(300 * time.Millisecond)
	out, _ := exec.Command("tmux", "-L", mux.Socket, "list-sessions", "-F", "#{session_name}").Output()
	if strings.Contains(string(out), "view-") {
		t.Errorf("view session left behind: %s", out)
	}
	if _, err := mux.Resolve(id); err != nil {
		t.Errorf("agent window gone after closing the browser terminal: %v", err)
	}
	_ = os.Remove(dir)
}

func TestStaticAndPage(t *testing.T) {
	ts, _ := setup(t)
	for _, p := range []string{"/", "/static/app.js", "/static/app.css", "/static/vendor/xterm.js"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Errorf("%s: %v %d", p, err, resp.StatusCode)
		}
	}
}

// TestHookRecordsPane runs `optimus hook claude` inside a real tmux pane and
// checks the state lands on that pane's window.
func TestHookRecordsPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	_, _ = setup(t)
	bin := t.TempDir() + "/optimus"
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/optimus").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`
	script := "echo '" + payload + "' | " + bin + " hook claude; sleep 30"
	id, err := mux.Spawn("hook-test", "claude", t.TempDir(), "", []string{"sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w, err := mux.Resolve(id)
		if err == nil {
			info := agentstate.Resolve(w, "")
			if info.State == mux.StateWaiting && info.Message == "wants to use Bash: rm -rf build" {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("hook state never reached the window")
}

func TestWorktreeFlow(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ts, s := setup(t)
	c := client(t, ts, s)
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@x", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@x"} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	post := func(path, body string) map[string]any {
		resp, err := c.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, v)
		}
		return v
	}
	r := post("/api/fanout", `{"agents":["shell"],"dir":"`+repo+`","prompt":"add a readme"}`)
	id := r["windows"].([]any)[0].(string)
	w, err := mux.Resolve(id)
	if err != nil || w.Worktree == "" || !strings.HasPrefix(w.Branch, "optimus/shell-add-a-readme-") {
		t.Fatalf("window not in a worktree: %+v %v", w, err)
	}
	os.WriteFile(w.Worktree+"/README.md", []byte("# hi\n"), 0o644)

	resp, _ := c.Get(ts.URL + "/api/windows/" + id + "/diff")
	var d map[string]string
	json.NewDecoder(resp.Body).Decode(&d)
	if !strings.Contains(d["diff"], "+# hi") {
		t.Fatalf("diff: %q", d["diff"])
	}
	post("/api/windows/"+id+"/merge", `{}`)
	if b, _ := os.ReadFile(repo + "/README.md"); string(b) != "# hi\n" {
		t.Fatal("merge did not reach the main checkout")
	}
	post("/api/windows/"+id+"/discard", `{}`)
	if _, err := os.Stat(w.Worktree); !os.IsNotExist(err) {
		t.Error("worktree still on disk after discard")
	}
}

func TestHandoffPreviewAndEdit(t *testing.T) {
	ts, s := setup(t)
	c := client(t, ts, s)
	home := os.Getenv("OPTIMUS_AGENT_HOME")
	dir := home + "/.claude/projects/-w-app"
	os.MkdirAll(dir, 0o755)
	os.WriteFile(dir+"/aaaa1111-2222-3333-4444-555555555555.jsonl", []byte(
		`{"type":"user","timestamp":"2026-09-01T10:00:00Z","cwd":"/w/app","message":{"role":"user","content":"fix the login bug"}}`+"\n"+
			`{"type":"assistant","timestamp":"2026-09-01T10:00:05Z","requestId":"r","message":{"id":"m","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Fixed."}],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n"), 0o644)

	post := func(body string) map[string]string {
		resp, err := c.Post(ts.URL+"/api/sessions/aaaa1111/handoff", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]string
		json.NewDecoder(resp.Body).Decode(&v)
		if resp.StatusCode != 200 {
			t.Fatalf("%d %v", resp.StatusCode, v)
		}
		return v
	}
	if r := post(`{"target":"preview"}`); !strings.Contains(r["document"], "> fix the login bug") {
		t.Fatalf("preview: %q", r["document"])
	}
	r := post(`{"target":"file","document":"# my edited brief\n"}`)
	if b, _ := os.ReadFile(r["path"]); string(b) != "# my edited brief\n" {
		t.Fatalf("edited document not used: %q", b)
	}
}

func TestPWAAndPhone(t *testing.T) {
	ts, s := setup(t)
	for path, ctype := range map[string]string{"/sw.js": "text/javascript", "/manifest.webmanifest": "application/manifest+json", "/static/icon-192.png": "image/png"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ctype) {
			t.Errorf("%s: %v %d %s", path, err, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	c := client(t, ts, s)
	var v map[string]any
	resp, _ := c.Get(ts.URL + "/api/phone")
	json.NewDecoder(resp.Body).Decode(&v)
	if urls, _ := v["urls"].([]any); len(urls) != 0 || v["svg"] != nil {
		t.Errorf("no phone URLs expected without PhoneURLs: %v", v)
	}
	s.Addr = "100.64.0.1:7777"
	s.PhoneURLs = func(addr string) []string { return []string{"http://" + addr + "/?token=x"} }
	resp, _ = c.Get(ts.URL + "/api/phone")
	v = nil
	json.NewDecoder(resp.Body).Decode(&v)
	if svg, _ := v["svg"].(string); !strings.HasPrefix(svg, "<svg") {
		t.Errorf("phone QR: %v", v)
	}
}

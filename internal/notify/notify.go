// Package notify tells you when an agent needs you: desktop notifications,
// the optimus tmux status line, and (optionally) phone push via ntfy.
package notify

import (
	"context"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

type Event struct {
	Title  string
	Body   string
	Urgent bool // needs an answer, not just information
	Window string
	Key    string // dedupe key for advisories
}

// Send delivers an event through every enabled channel. It never blocks for
// long and never fails loudly: notifications are best effort.
func Send(cfg config.Config, e Event) {
	mux.Message(" ◆ " + e.Title + " — " + e.Body + "  (Alt-n) ")
	if cfg.Notifications.DesktopOn() {
		desktop(e)
	}
	if cfg.Notifications.Ntfy != "" {
		go ntfy(cfg.Notifications.Ntfy, e)
	}
}

func desktop(e Event) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := `display notification "` + esc(e.Body) + `" with title "optimus" subtitle "` + esc(e.Title) + `"`
		cmd = exec.Command("osascript", "-e", script)
	default:
		if _, err := exec.LookPath("notify-send"); err != nil {
			return
		}
		urgency := "normal"
		if e.Urgent {
			urgency = "critical"
		}
		cmd = exec.Command("notify-send", "-a", "optimus", "-u", urgency, "optimus · "+e.Title, e.Body)
	}
	_ = cmd.Start()
	go func() { _ = cmd.Wait() }()
}

func esc(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }

// ntfy posts to an ntfy topic (https://ntfy.sh or self-hosted), which the
// ntfy phone app turns into a push notification.
func ntfy(url string, e Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(e.Body))
	if err != nil {
		return
	}
	req.Header.Set("Title", "optimus · "+e.Title)
	req.Header.Set("Tags", map[bool]string{true: "warning", false: "white_check_mark"}[e.Urgent])
	if e.Urgent {
		req.Header.Set("Priority", "high")
	}
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

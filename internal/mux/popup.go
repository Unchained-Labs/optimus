package mux

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// AttachMode decides how to show an agent when optimus itself runs inside
// your own tmux: "popup" (a tmux popup over your pane), "nested" (tmux
// inside tmux), or "auto" (popup when possible).
var AttachMode = "auto"

// InsideTmux reports whether optimus runs inside the user's own tmux.
func InsideTmux() bool { return os.Getenv("TMUX") != "" }

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// outerVersion is the version of the tmux the user is inside (major, minor).
func outerVersion() (int, int) {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return 0, 0
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return maj, min
}

// UsePopup reports whether attaching should open a popup in the user's tmux
// (display-popup needs tmux 3.2+).
func UsePopup() bool {
	if AttachMode == "nested" || !InsideTmux() {
		return false
	}
	maj, min := outerVersion()
	return maj > 3 || (maj == 3 && min >= 2)
}

// PopupCmd returns the command that opens a window of the optimus server in a
// popup of the user's tmux. Closing it (Alt-q) detaches; the agent keeps
// running.
func PopupCmd(id string) *exec.Cmd {
	_, _ = run("select-window", "-t", id)
	inner := strings.Join([]string{
		"env", "-u", "TMUX", "-u", "TMUX_PANE",
		shellQuote(tmuxBin()), "-L", shellQuote(Socket), "-f", shellQuote(confPath()),
		"attach-session", "-t", shellQuote("=" + Session),
	}, " ")
	args := []string{"display-popup", "-E", "-w", "95%", "-h", "92%"}
	if maj, min := outerVersion(); maj > 3 || (maj == 3 && min >= 3) {
		args = append(args, "-T", " optimus · Alt-q closes · the agent keeps running ")
	}
	cmd := exec.Command(tmuxBin(), append(args, inner)...) // uses the outer TMUX from the environment
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

func tmuxBin() string {
	if p, err := exec.LookPath("tmux"); err == nil {
		return p
	}
	return "tmux"
}

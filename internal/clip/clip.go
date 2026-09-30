// Package clip copies text to the system clipboard, falling back to the
// terminal's OSC 52 escape (works over SSH and inside tmux).
package clip

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Copy returns the method used.
func Copy(text string) (string, error) {
	for _, c := range candidates() {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return c[0], nil
		}
	}
	return "osc52", osc52(text)
}

func candidates() [][]string {
	if runtime.GOOS == "darwin" {
		return [][]string{{"pbcopy"}}
	}
	var c [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		c = append(c, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		c = append(c, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return c
}

func osc52(text string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
	if os.Getenv("TMUX") != "" {
		seq = "\x1bPtmux;\x1b" + seq + "\x1b\\"
	}
	_, err = tty.WriteString(seq)
	return err
}

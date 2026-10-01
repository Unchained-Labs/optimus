package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/Unchained-Labs/optimus/internal/mux"
)

// term bridges a browser terminal (xterm.js) to a private tmux view of one
// agent window over a websocket. Binary frames carry terminal bytes both
// ways; text frames from the browser carry control messages (resize).
func (s *Server) term(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	// websocket.Accept rejects cross-origin upgrades by default.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)

	cmd, cleanup, err := mux.ViewCmd(win.ID)
	if err != nil {
		c.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer cleanup()

	size := &pty.Winsize{Cols: dim(r.URL.Query().Get("cols"), 120), Rows: dim(r.URL.Query().Get("rows"), 36)}
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		c.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// tmux → browser
	go func() {
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
				werr := c.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				c.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
		}
	}()

	// browser → tmux
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			}
			continue
		}
		if _, err := ptmx.Write(data); err != nil {
			return
		}
	}
}

func dim(v string, def uint16) uint16 {
	n, err := strconv.Atoi(v)
	if err != nil || n < 10 || n > 1000 {
		return def
	}
	return uint16(n)
}

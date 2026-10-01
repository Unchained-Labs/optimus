package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/web"
)

func cmdWeb(a *app.App, args []string) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", a.Cfg.WebAddr(), "listen address (use 0.0.0.0:7777 to reach it from your phone/LAN)")
	bg := fs.Bool("bg", false, "run in the background (inside optimus's tmux server)")
	stop := fs.Bool("stop", false, "stop the background dashboard")
	urlOnly := fs.Bool("url", false, "print the dashboard URL and exit")
	open := fs.Bool("open", false, "open the dashboard in a browser")
	parse(fs, args)
	a.Cfg.Remote.Addr = *addr

	switch {
	case *stop:
		return mux.StopService("web")
	case *urlOnly:
		printURLs(a, *addr)
		return nil
	case *bg:
		self, _ := os.Executable()
		home, _ := os.UserHomeDir()
		if !a.WebRunning() {
			if err := mux.StartService("web", home, []string{self, "web", "--addr", *addr}); err != nil {
				return err
			}
			for i := 0; i < 30 && !a.WebRunning(); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if !a.WebRunning() {
				return fmt.Errorf("dashboard did not start; run `optimus web` in the foreground to see why")
			}
		}
		printURLs(a, *addr)
		if *open {
			openBrowser(a.WebURL())
		}
		return nil
	}

	srv, err := web.New(a)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		if a.WebRunning() {
			fmt.Println("the dashboard is already running:")
			printURLs(a, *addr)
			return nil
		}
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Println("optimus web dashboard")
	printURLs(a, *addr)
	if *open {
		openBrowser(a.WebURL())
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = hs.Shutdown(sctx)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// printURLs shows how to reach the dashboard, including from other devices
// when it listens on every interface.
func printURLs(a *app.App, addr string) {
	tok, _ := config.WebToken()
	host, port, _ := net.SplitHostPort(addr)
	fmt.Printf("  %s\n", a.WebURL())
	if host == "" || host == "0.0.0.0" || host == "::" {
		for _, ip := range lanIPs() {
			fmt.Printf("  http://%s/?token=%s\n", net.JoinHostPort(ip, port), tok)
		}
		fmt.Println("\n  ! listening on every interface: anyone with the token can drive your agents.")
		fmt.Println("    Prefer a private network (Tailscale, WireGuard) over exposing this port.")
	}
}

func lanIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
			out = append(out, n.IP.String())
		}
	}
	return out
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	_ = cmd.Start()
}

// cmdAgentShortcut handles `optimus claude [dir] [-p prompt]`: start that
// agent in the multiplexer and attach to it.
func cmdAgentShortcut(a *app.App, agent string, args []string) error {
	fs := flag.NewFlagSet(agent, flag.ExitOnError)
	prompt := fs.String("p", "", "first message")
	name := fs.String("name", "", "window name")
	detach := fs.Bool("d", false, "start in the background, don't attach")
	pos := parse(fs, args)
	dir := ""
	if len(pos) > 0 {
		dir = pos[0]
	}
	if len(pos) > 1 && *prompt == "" {
		*prompt = strings.Join(pos[1:], " ")
	}
	id, err := a.Launch(agent, absDir(dir), *prompt, *name)
	if err != nil {
		return err
	}
	if *detach {
		fmt.Printf("started %s in window %s\n", agent, id)
		return nil
	}
	return mux.AttachCmd(id).Run()
}

package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

func treeOf(q string) (mux.Window, error) {
	w, err := mux.Resolve(q)
	if err != nil {
		return w, err
	}
	if w.Worktree == "" {
		return w, fmt.Errorf("%s doesn't run in its own worktree (start agents with --worktree)", w.Name)
	}
	return w, nil
}

// cmdWorktree: optimus wt ls | diff W | merge W [--commit] | rm W [--force]
func cmdWorktree(args []string) error {
	sub := first(args)
	rest := args
	if len(rest) > 0 {
		rest = rest[1:]
	}
	switch sub {
	case "", "ls", "list":
		return cmdCompare(nil)
	case "diff":
		w, err := treeOf(first(rest))
		if err != nil {
			return err
		}
		t, _ := app.Tree(w)
		d, err := t.Diff()
		if err != nil {
			return err
		}
		return page(d)
	case "merge":
		fs := flag.NewFlagSet("merge", flag.ExitOnError)
		commit := fs.Bool("commit", false, "commit the agent's uncommitted changes first")
		pos := parse(fs, rest)
		w, err := treeOf(first(pos))
		if err != nil {
			return err
		}
		t, _ := app.Tree(w)
		if err := t.Merge(*commit, "optimus: work from "+w.Name+" ("+w.Agent+")"); err != nil {
			return err
		}
		fmt.Printf("merged %s into %s\n", t.Branch, sp(t.Repo))
		return nil
	case "rm", "remove":
		fs := flag.NewFlagSet("rm", flag.ExitOnError)
		force := fs.Bool("force", false, "discard unmerged or uncommitted work")
		pos := parse(fs, rest)
		w, err := treeOf(first(pos))
		if err != nil {
			return err
		}
		t, _ := app.Tree(w)
		// remove first: if it's refused, the agent keeps running untouched
		if err := t.Remove(*force); err != nil {
			return err
		}
		if !w.Dead {
			_ = mux.Kill(w.ID)
		}
		fmt.Printf("removed %s and branch %s\n", sp(t.Path), t.Branch)
		return nil
	}
	return fmt.Errorf("usage: optimus wt [ls | diff W | merge W [--commit] | rm W [--force]]")
}

// cmdCompare lists agents working in worktrees side by side.
func cmdCompare(args []string) error {
	ws, err := mux.List()
	if err != nil {
		return err
	}
	only := map[string]bool{}
	for _, a := range args {
		if w, err := mux.Resolve(a); err == nil {
			only[w.ID] = true
		}
	}
	w := table()
	fmt.Fprintln(w, "WIN\tNAME\tAGENT\tSTATE\tCHANGES\tBRANCH\tREPO")
	n := 0
	for _, x := range ws {
		t, ok := app.Tree(x)
		if !ok || (len(only) > 0 && !only[x.ID]) {
			continue
		}
		n++
		screen, _ := mux.Capture(x.ID, 30)
		st, _ := t.Stats()
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", x.Index, x.Name, x.Agent, agentstate.Resolve(x, screen).State, st, t.Branch, sp(t.Repo))
	}
	w.Flush()
	if n == 0 {
		fmt.Println("(no agents in worktrees — start one with `optimus new codex . --worktree` or `optimus fanout`)")
	}
	return nil
}

// cmdFanout starts the same task in several agents, each in its own worktree.
func cmdFanout(a *app.App, args []string) error {
	fs := flag.NewFlagSet("fanout", flag.ExitOnError)
	agents := fs.String("agents", "claude,codex", "comma-separated agents")
	dir := fs.String("dir", "", "repository (default: current dir)")
	pos := parse(fs, args)
	prompt := strings.Join(pos, " ")
	if prompt == "" {
		return fmt.Errorf(`usage: optimus fanout [--agents claude,codex] [--dir REPO] "task"`)
	}
	ids, err := a.Fanout(strings.Split(*agents, ","), absDir(*dir), prompt)
	for _, id := range ids {
		if w, e := mux.Resolve(id); e == nil {
			fmt.Printf("started %s (%s) in %s\n", w.Name, w.Agent, sp(w.Worktree))
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("\ncompare with `optimus compare`, review with `optimus wt diff <window>`, keep the best with `optimus wt merge <window> --commit`\n")
	return nil
}

// page shows text through $PAGER (less) when stdout is a terminal.
func page(s string) error {
	if st, _ := os.Stdout.Stat(); st.Mode()&os.ModeCharDevice == 0 {
		fmt.Print(s)
		return nil
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -R"
	}
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(s), os.Stdout, os.Stderr
	return cmd.Run()
}

package main

import (
	"fmt"
	"os"

	"github.com/Unchained-Labs/optimus/internal/cli"
	"github.com/Unchained-Labs/optimus/internal/tui"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "ui" {
		if err := tui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "optimus:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(cli.Run(os.Args[1:]))
}

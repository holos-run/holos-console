package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/holos-run/holos-console/cli"
	"github.com/holos-run/holos-console/console"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Handle version command early
	if len(os.Args) >= 2 && os.Args[1] == "version" {
		fmt.Println(console.GetVersion())
		return 0
	}

	ctx := context.Background()
	cmd := cli.Command()

	if err := cmd.ExecuteContext(ctx); err != nil {
		slog.Error(err.Error(), "err", err)
		return 1
	}
	return 0
}

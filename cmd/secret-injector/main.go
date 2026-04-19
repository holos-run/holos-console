package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/holos-run/holos-console/console"
	"github.com/holos-run/holos-console/internal/secretinjector/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Handle version command early so `holos-secret-injector version`
	// prints the version without constructing a controller-runtime
	// manager or contacting the API server.
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

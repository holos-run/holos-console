package main

import (
	"fmt"
	"log/slog"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

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

	// Install the controller-runtime signal handler in main() so SIGTERM
	// and SIGINT drive a graceful shutdown of the manager. Without this,
	// Kubernetes rollouts and pod deletions would force-kill the binary
	// after the grace period instead of letting the manager unwind its
	// caches cleanly.
	ctx := ctrl.SetupSignalHandler()
	cmd := cli.Command()

	if err := cmd.ExecuteContext(ctx); err != nil {
		slog.Error(err.Error(), "err", err)
		return 1
	}
	return 0
}

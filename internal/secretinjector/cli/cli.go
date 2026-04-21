/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/holos-run/holos-console/console"
	sicontroller "github.com/holos-run/holos-console/internal/secretinjector/controller"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	logLevel            string
	controllerNamespace string
)

// Command returns the root cobra command for the holos-secret-injector CLI.
//
// The shape mirrors cli.Command() in cli/cli.go: a single root command with a
// persistent --log-level flag, a PersistentPreRunE that wires slog from that
// flag, a version template, and a default RunE that starts the
// controller-runtime manager stub. No reconcilers are registered here — M2
// (HOL-695 and HOL-696 in the Secret Injection Service MVP plan) adds them.
//
// See docs/adrs/031-secret-injection-service.md for the architectural boundary
// this package owns: the secret-injector Cobra tree is disjoint from the
// console's cli/ package so the two binaries can diverge without a rewrite.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "holos-secret-injector",
		Short:   "holos-secret-injector reconciles secrets.holos.run APIs and serves ext_authz",
		Version: console.GetVersion(),
		Args:    cobra.NoArgs,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level, err := parseLogLevel(logLevel)
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: level,
			}))
			slog.SetDefault(logger)
			return nil
		},
		RunE: Run,
	}

	cmd.SetVersionTemplate("{{.Version}}\n")

	// Hide the help command.
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cmd.PersistentFlags().BoolP("help", "h", false, "Print usage")
	cmd.PersistentFlags().Lookup("help").Hidden = true

	// Logging flag. Additional flags (metrics bind, health probe, ext_authz
	// listener, etc.) land in later phases.
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// Controller namespace. The pepper bootstrap (HOL-749) writes the
	// pinned pepper v1.Secret into this namespace on first manager
	// boot. Production deployments typically set POD_NAMESPACE via the
	// downward API on the controller's Deployment manifest and leave
	// this flag unset; the CLI flag is the escape hatch for local
	// debugging and envtest-style suites where the downward API is
	// unavailable. See [sicrypto.PodNamespaceEnv] for the env contract.
	cmd.PersistentFlags().StringVar(&controllerNamespace, "controller-namespace", "",
		"Namespace the controller treats as its own (pepper bootstrap target). Defaults to $POD_NAMESPACE.")

	return cmd
}

// Run is the RunE for the holos-secret-injector root command. It constructs a
// controller-runtime manager stub and blocks on Start() until the context
// provided by cmd.ExecuteContext is cancelled — the cmd/secret-injector main
// passes a ctrl.SetupSignalHandler context so SIGINT/SIGTERM drive a graceful
// shutdown.
//
// No reconcilers are registered yet — that is M2 (see ADR 031 §4). Running
// this binary today produces a manager that successfully stands up its
// informer caches (none, since no reconcilers are registered) and returns
// when the signal arrives.
func Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		// Callers that invoke the root via cmd.Execute() or cmd.Run()
		// directly (notably tests) don't set a context. Fall back to a
		// plain Background context so mgr.Start(ctx) doesn't panic on
		// the first controller-runtime lookup. The cmd/secret-injector
		// main wires ctrl.SetupSignalHandler() into the context, so
		// graceful shutdown still flows correctly in the real binary.
		ctx = context.Background()
	}

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	mgr, err := sicontroller.NewManager(cfg, sicontroller.Options{
		Logger:              slog.Default(),
		ControllerNamespace: controllerNamespace,
	})
	if err != nil {
		return fmt.Errorf("constructing manager: %w", err)
	}

	slog.Info("starting holos-secret-injector",
		"version", console.GetVersion())
	return mgr.Start(ctx)
}

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q: must be debug, info, warn, or error", level)
	}
}

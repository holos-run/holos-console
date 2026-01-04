package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/holos-run/holos-console/console"
)

var (
	listenAddr string
	certFile   string
	keyFile    string
	issuer     string
	clientID   string
)

// Command returns the root cobra command for the CLI.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "holos-console",
		Short:   "holos-console serves the Holos web console",
		Version: console.GetVersion(),
		Args:    cobra.NoArgs,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)
			return nil
		},
		RunE: Run,
	}

	cmd.SetVersionTemplate("{{.Version}}\n")

	// Hide the help command
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cmd.PersistentFlags().BoolP("help", "h", false, "Print usage")
	cmd.PersistentFlags().Lookup("help").Hidden = true

	// Server flags
	cmd.Flags().StringVar(&listenAddr, "listen", ":8443", "Address to listen on")
	cmd.Flags().StringVar(&certFile, "cert", "", "TLS certificate file (auto-generated if empty)")
	cmd.Flags().StringVar(&keyFile, "key", "", "TLS key file (auto-generated if empty)")

	// OIDC flags
	cmd.Flags().StringVar(&issuer, "issuer", "https://localhost:8443/dex", "OIDC issuer URL for token validation")
	cmd.Flags().StringVar(&clientID, "client-id", "holos-console", "Expected audience for tokens")

	return cmd
}

// Run serves as the Cobra run function for the root command.
func Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := console.Config{
		ListenAddr: listenAddr,
		CertFile:   certFile,
		KeyFile:    keyFile,
		Issuer:     issuer,
		ClientID:   clientID,
	}

	server := console.New(cfg)
	return server.Serve(ctx)
}

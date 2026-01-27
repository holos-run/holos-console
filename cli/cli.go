package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/holos-run/holos-console/console"
)

var (
	listenAddr      string
	certFile        string
	keyFile         string
	issuer          string
	clientID        string
	idTokenTTL      string
	refreshTokenTTL string
	namespace       string
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
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL (defaults to https://localhost:<port>/dex based on --listen)")
	cmd.Flags().StringVar(&clientID, "client-id", "holos-console", "Expected audience for tokens")

	// Token TTL flags
	cmd.Flags().StringVar(&idTokenTTL, "id-token-ttl", "15m", "ID token lifetime (e.g., 15m, 1h, 30s for testing)")
	cmd.Flags().StringVar(&refreshTokenTTL, "refresh-token-ttl", "12h", "Refresh token absolute lifetime - forces re-authentication")

	// Kubernetes flags
	cmd.Flags().StringVar(&namespace, "namespace", "holos-console", "Kubernetes namespace for secrets")

	return cmd
}

// deriveIssuer returns the issuer URL based on the listen address.
// If issuer is already set, returns it unchanged.
// Otherwise, derives from listen address using https and /dex path.
func deriveIssuer(listenAddr, issuer string) string {
	if issuer != "" {
		return issuer
	}

	// Parse listen address to extract host and port
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// Fallback if parsing fails
		return "https://localhost:8443/dex"
	}

	// Use localhost if host is empty or 0.0.0.0
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}

	return fmt.Sprintf("https://%s:%s/dex", host, port)
}

// Run serves as the Cobra run function for the root command.
func Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Parse token TTL durations
	idTTL, err := time.ParseDuration(idTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid --id-token-ttl: %w", err)
	}
	refreshTTL, err := time.ParseDuration(refreshTokenTTL)
	if err != nil {
		return fmt.Errorf("invalid --refresh-token-ttl: %w", err)
	}

	// Derive issuer from listen address if not explicitly set
	derivedIssuer := deriveIssuer(listenAddr, issuer)

	cfg := console.Config{
		ListenAddr:      listenAddr,
		CertFile:        certFile,
		KeyFile:         keyFile,
		Issuer:          derivedIssuer,
		ClientID:        clientID,
		IDTokenTTL:      idTTL,
		RefreshTokenTTL: refreshTTL,
		Namespace:       namespace,
	}

	server := console.New(cfg)
	return server.Serve(ctx)
}

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/holos-run/holos-console/console"
)

var (
	listenAddr      string
	certFile        string
	keyFile         string
	caCertFile      string
	plainHTTP       bool
	origin          string
	issuer          string
	clientID        string
	idTokenTTL      string
	refreshTokenTTL string
	namespacePrefix    string
	organizationPrefix string
	projectPrefix      string
	disableOrgCreation bool
	orgCreatorUsers    string
	orgCreatorGroups   string
	logHealthChecks  bool
	logLevel         string
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

	// Hide the help command
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cmd.PersistentFlags().BoolP("help", "h", false, "Print usage")
	cmd.PersistentFlags().Lookup("help").Hidden = true

	// Server flags
	cmd.Flags().StringVar(&listenAddr, "listen", ":8443", "Address to listen on")
	cmd.Flags().StringVar(&certFile, "cert", "", "TLS certificate file (auto-generated if empty)")
	cmd.Flags().StringVar(&keyFile, "key", "", "TLS key file (auto-generated if empty)")
	cmd.Flags().StringVar(&caCertFile, "ca-cert", "", "PEM-encoded CA certificate file to trust (e.g., mkcert CA root)")
	cmd.Flags().BoolVar(&plainHTTP, "plain-http", false, "Listen on plain HTTP instead of HTTPS")

	// OIDC flags
	cmd.Flags().StringVar(&origin, "origin", "", "Public-facing base URL of the console for OIDC redirect URIs (e.g., https://holos-console.example.com)")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL (defaults to https://localhost:<port>/dex based on --listen)")
	cmd.Flags().StringVar(&clientID, "client-id", "holos-console", "Expected audience for tokens")

	// Token TTL flags
	cmd.Flags().StringVar(&idTokenTTL, "id-token-ttl", "15m", "ID token lifetime (e.g., 15m, 1h, 30s for testing)")
	cmd.Flags().StringVar(&refreshTokenTTL, "refresh-token-ttl", "12h", "Refresh token absolute lifetime - forces re-authentication")

	// Namespace prefix flags
	cmd.Flags().StringVar(&namespacePrefix, "namespace-prefix", "holos-", "Global prefix for all namespace names (default \"holos-\"), enabling multi-instance isolation (e.g., prod-, ci-)")
	cmd.Flags().StringVar(&organizationPrefix, "organization-prefix", "org-", "Prefix for organization namespace names")
	cmd.Flags().StringVar(&projectPrefix, "project-prefix", "prj-", "Prefix for project namespace names")

	// Organization creation permission flags
	cmd.Flags().BoolVar(&disableOrgCreation, "disable-org-creation", false, "Unconditionally block organization creation regardless of creator lists")
	cmd.Flags().StringVar(&orgCreatorUsers, "org-creator-users", "", "Comma-separated email addresses allowed to create organizations")
	cmd.Flags().StringVar(&orgCreatorGroups, "org-creator-groups", "owner", "Comma-separated OIDC group names allowed to create organizations")

	// Logging flags
	cmd.Flags().BoolVar(&logHealthChecks, "log-health-checks", false, "Log /healthz and /readyz requests (suppressed by default)")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return cmd
}

// deriveOrigin returns the public-facing base URL of the console.
// If origin is already set, returns it unchanged.
// Otherwise, derives from the listen address.
// The scheme is http when plainHTTP is true, https otherwise.
func deriveOrigin(listenAddr, origin string, plainHTTP bool) string {
	if origin != "" {
		return origin
	}

	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		if plainHTTP {
			return "http://localhost:8080"
		}
		return "https://localhost:8443"
	}

	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}

	scheme := "https"
	if plainHTTP {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// deriveIssuer returns the issuer URL based on the listen address.
// If issuer is already set, returns it unchanged.
// Otherwise, derives from listen address using the /dex path.
// The scheme is http when plainHTTP is true, https otherwise.
func deriveIssuer(listenAddr, issuer string, plainHTTP bool) string {
	if issuer != "" {
		return issuer
	}

	// Parse listen address to extract host and port
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// Fallback if parsing fails
		if plainHTTP {
			return "http://localhost:8080/dex"
		}
		return "https://localhost:8443/dex"
	}

	// Use localhost if host is empty or 0.0.0.0
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}

	scheme := "https"
	if plainHTTP {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%s/dex", scheme, host, port)
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

// splitCSV splits a comma-separated string into a slice, trimming whitespace
// and omitting empty entries.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
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

	// Derive origin and issuer from listen address if not explicitly set
	derivedOrigin := deriveOrigin(listenAddr, origin, plainHTTP)
	derivedIssuer := deriveIssuer(listenAddr, issuer, plainHTTP)

	cfg := console.Config{
		ListenAddr:       listenAddr,
		CertFile:         certFile,
		KeyFile:          keyFile,
		CACertFile:       caCertFile,
		PlainHTTP:        plainHTTP,
		Origin:           derivedOrigin,
		Issuer:           derivedIssuer,
		ClientID:         clientID,
		IDTokenTTL:       idTTL,
		RefreshTokenTTL:  refreshTTL,
		NamespacePrefix:     namespacePrefix,
		OrganizationPrefix:  organizationPrefix,
		ProjectPrefix:       projectPrefix,
		DisableOrgCreation:  disableOrgCreation,
		OrgCreatorUsers:     splitCSV(orgCreatorUsers),
		OrgCreatorGroups:    splitCSV(orgCreatorGroups),
		LogHealthChecks:     logHealthChecks,
	}

	server := console.New(cfg)
	return server.Serve(ctx)
}

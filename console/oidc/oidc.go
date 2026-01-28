package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dexidp/dex/server"
	"github.com/dexidp/dex/storage"
	"github.com/dexidp/dex/storage/memory"
)

// Config holds configuration for the embedded OIDC identity provider.
type Config struct {
	// Issuer is the full OIDC issuer URL including mount path.
	// Example: "https://localhost:8443/dex"
	Issuer string

	// ClientID is the OAuth2 client ID for the SPA.
	ClientID string

	// RedirectURIs are the allowed OAuth2 redirect URIs.
	RedirectURIs []string

	// Logger for operations.
	Logger *slog.Logger

	// IDTokenTTL is the lifetime of ID tokens.
	// Default: 15 minutes
	IDTokenTTL time.Duration

	// RefreshTokenTTL is the absolute lifetime of refresh tokens.
	// After this duration, users must re-authenticate.
	// Default: 12 hours
	RefreshTokenTTL time.Duration
}

func init() {
	// Register our custom password connector with Dex.
	// This connector supports groups, unlike the built-in mockPassword connector.
	server.ConnectorsConfig["holosPassword"] = func() server.ConnectorConfig {
		return new(PasswordConnectorConfig)
	}

	// Register the auto-login connector for development.
	// This connector bypasses the login form entirely.
	server.ConnectorsConfig["holosAuto"] = func() server.ConnectorConfig {
		return new(AutoConnectorConfig)
	}
}

// NewHandler creates an http.Handler for the embedded OIDC identity provider.
// The issuer must include the full URL with the mount path (e.g., "https://localhost:8443/dex").
// The handler should be mounted at the path suffix of the issuer URL.
func NewHandler(ctx context.Context, cfg Config) (http.Handler, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("issuer is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("clientID is required")
	}
	if len(cfg.RedirectURIs) == 0 {
		return nil, fmt.Errorf("at least one redirectURI is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Create in-memory storage
	store := memory.New(logger)

	// Add static client for holos-console SPA
	store = storage.WithStaticClients(store, []storage.Client{
		{
			ID:           cfg.ClientID,
			RedirectURIs: cfg.RedirectURIs,
			Name:         "Holos Console",
			Public:       true, // SPA = public client, no secret
		},
	})

	// Configure auto-login connector for development.
	// This connector bypasses the login form entirely and immediately authenticates
	// users as the configured username with the configured groups.
	connectorConfig, err := json.Marshal(AutoConnectorConfig{
		Username: GetUsername(),
		Groups:   []string{"owner"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connector config: %w", err)
	}

	// Add auto-login connector that skips the password form
	store = storage.WithStaticConnectors(store, []storage.Connector{
		{
			ID:     "holos",
			Type:   "holosAuto",
			Name:   "Development Auto-Login",
			Config: connectorConfig,
		},
	})

	// Create Dex server config
	serverConfig := server.Config{
		Issuer:                 cfg.Issuer,
		Storage:                store,
		SkipApprovalScreen:     true,
		Logger:                 logger,
		SupportedResponseTypes: []string{"code"},
		AllowedOrigins:         []string{"*"}, // Allow all origins for development
	}

	// Configure ID token lifetime if specified
	if cfg.IDTokenTTL > 0 {
		serverConfig.IDTokensValidFor = cfg.IDTokenTTL
	}

	// Configure refresh token policy with absolute lifetime if specified
	if cfg.RefreshTokenTTL > 0 {
		refreshPolicy, err := server.NewRefreshTokenPolicy(
			logger,
			true,                         // rotation enabled
			"",                           // validIfNotUsedFor (empty = no limit)
			cfg.RefreshTokenTTL.String(), // absoluteLifetime
			"3s",                         // reuseInterval (handle network retries)
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create refresh token policy: %w", err)
		}
		serverConfig.RefreshTokenPolicy = refreshPolicy
	}

	// Create Dex server
	dexServer, err := server.NewServer(ctx, serverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dex server: %w", err)
	}

	logger.Info("embedded OIDC provider initialized",
		"issuer", cfg.Issuer,
		"clientID", cfg.ClientID,
		"username", GetUsername(),
	)

	return dexServer, nil
}

package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dexidp/dex/connector/mock"
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

	// Configure mock password connector
	connectorConfig, err := json.Marshal(mock.PasswordConfig{
		Username: GetUsername(),
		Password: GetPassword(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connector config: %w", err)
	}

	// Add mock password connector
	store = storage.WithStaticConnectors(store, []storage.Connector{
		{
			ID:     "mock",
			Type:   "mockPassword",
			Name:   "Development Login",
			Config: connectorConfig,
		},
	})

	// Create Dex server
	dexServer, err := server.NewServer(ctx, server.Config{
		Issuer:                 cfg.Issuer,
		Storage:                store,
		SkipApprovalScreen:     true,
		Logger:                 logger,
		SupportedResponseTypes: []string{"code"},
		AllowedOrigins:         []string{"*"}, // Allow all origins for development
	})
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

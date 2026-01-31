package oidc

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/dexidp/dex/connector"
)

// AutoConnectorConfig configures an auto-login connector that immediately
// authenticates users without requiring credentials.
// This is intended for development use only.
type AutoConnectorConfig struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

// Open returns a callback connector that auto-authenticates users.
func (c *AutoConnectorConfig) Open(id string, logger *slog.Logger) (connector.Connector, error) {
	username := c.Username
	if username == "" {
		username = DefaultUsername
	}
	return &autoConnector{
		username: username,
		groups:   c.Groups,
		logger:   logger,
	}, nil
}

// autoConnector implements connector.CallbackConnector for automatic authentication.
// It redirects users back immediately without showing a login form.
type autoConnector struct {
	username string
	groups   []string
	logger   *slog.Logger
}

func (c *autoConnector) Close() error { return nil }

// LoginURL returns the URL that immediately redirects back to Dex with auto-login.
// The scopes parameter indicates what the client requested.
// The callbackURL is where to redirect after "authentication".
// The state is an opaque value that must be preserved through the redirect.
func (c *autoConnector) LoginURL(scopes connector.Scopes, callbackURL, state string) (string, error) {
	// Build callback URL with state parameter
	u, err := url.Parse(callbackURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("state", state)
	u.RawQuery = q.Encode()

	c.logger.Info("auto connector login redirect",
		"callbackURL", callbackURL,
		"scopes.Groups", scopes.Groups,
	)

	return u.String(), nil
}

// HandleCallback processes the redirect from "authentication".
// Since we auto-authenticate, this just returns the configured identity.
func (c *autoConnector) HandleCallback(scopes connector.Scopes, r *http.Request) (identity connector.Identity, err error) {
	c.logger.Info("auto connector callback",
		"username", c.username,
		"groups", c.groups,
		"scopes.Groups", scopes.Groups,
	)

	return connector.Identity{
		UserID:        "dev-user-001",
		Username:      c.username,
		Email:         c.username + "@localhost",
		EmailVerified: true,
		Groups:        c.groups,
	}, nil
}

// Refresh preserves the identity on token refresh.
func (c *autoConnector) Refresh(_ context.Context, _ connector.Scopes, identity connector.Identity) (connector.Identity, error) {
	return identity, nil
}

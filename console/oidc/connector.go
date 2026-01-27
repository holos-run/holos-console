package oidc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/dexidp/dex/connector"
)

// PasswordConnectorConfig configures a password connector that supports groups.
// Unlike the mock.PasswordConfig, this connector allows specifying groups
// that will be included in the user's identity.
type PasswordConnectorConfig struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Groups   []string `json:"groups"`
}

// Open returns a password connector that includes the configured groups.
func (c *PasswordConnectorConfig) Open(id string, logger *slog.Logger) (connector.Connector, error) {
	if c.Username == "" {
		return nil, errors.New("no username supplied")
	}
	if c.Password == "" {
		return nil, errors.New("no password supplied")
	}
	return &passwordConnector{
		username: c.Username,
		password: c.Password,
		groups:   c.Groups,
		logger:   logger,
	}, nil
}

// passwordConnector implements a password connector with groups support.
type passwordConnector struct {
	username string
	password string
	groups   []string
	logger   *slog.Logger
}

func (p *passwordConnector) Close() error { return nil }

func (p *passwordConnector) Login(ctx context.Context, s connector.Scopes, username, password string) (identity connector.Identity, validPassword bool, err error) {
	if username == p.username && password == p.password {
		return connector.Identity{
			UserID:        "0-385-28089-0",
			Username:      p.username,
			Email:         p.username,
			EmailVerified: true,
			Groups:        p.groups,
		}, true, nil
	}
	return identity, false, nil
}

func (p *passwordConnector) Prompt() string { return "" }

func (p *passwordConnector) Refresh(_ context.Context, _ connector.Scopes, identity connector.Identity) (connector.Identity, error) {
	return identity, nil
}

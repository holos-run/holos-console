package oidc

import (
	"github.com/dexidp/dex/connector"
)

// Compile-time check: autoConnector must implement connector.RefreshConnector
// so Dex issues refresh tokens when offline_access scope is requested.
// Without this, signinSilent() in oidc-client-ts falls back to iframe-based
// renewal, which fails with "IFrame timed out without a response".
var _ connector.RefreshConnector = (*autoConnector)(nil)

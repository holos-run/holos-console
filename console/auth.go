package console

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// NewIDTokenVerifier creates an OIDC ID token verifier for the given issuer and client ID.
// It fetches the OIDC discovery document from the issuer URL to configure the verifier.
func NewIDTokenVerifier(ctx context.Context, issuer, clientID string) (*oidc.IDTokenVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	return verifier, nil
}

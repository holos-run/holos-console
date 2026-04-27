package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4"
	"google.golang.org/protobuf/encoding/protowire"
)

// TokenExchangeRequest is the JSON body for POST /api/dev/token.
type TokenExchangeRequest struct {
	Email string `json:"email"`
}

// TokenExchangeResponse is the JSON response from POST /api/dev/token.
type TokenExchangeResponse struct {
	IDToken   string   `json:"id_token"`
	Email     string   `json:"email"`
	Groups    []string `json:"groups"`
	ExpiresIn int64    `json:"expires_in"`
}

// idTokenClaims mirrors the OIDC ID token claims structure used by Dex.
// We define our own copy because Dex's idTokenClaims type is unexported.
type idTokenClaims struct {
	Issuer        string   `json:"iss"`
	Subject       string   `json:"sub"`
	Audience      audience `json:"aud"`
	Expiry        int64    `json:"exp"`
	IssuedAt      int64    `json:"iat"`
	Email         string   `json:"email,omitempty"`
	EmailVerified *bool    `json:"email_verified,omitempty"`
	Groups        []string `json:"groups,omitempty"`
	Name          string   `json:"name,omitempty"`
}

// audience implements custom JSON marshalling to match Dex's behavior:
// a single-element audience is serialized as a string, not an array.
type audience []string

func (a audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

// HandleTokenExchange returns an http.HandlerFunc that mints OIDC ID tokens
// for registered test users. The tokens are signed using Dex's signing keys
// retrieved from the Dex storage, so they pass the LazyAuthInterceptor JWT
// verification.
//
// If state is nil (Dex not enabled), the handler returns 404.
func HandleTokenExchange(state *DexState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if state == nil {
			http.Error(w, "dev token endpoint not available (Dex not enabled)", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit request body to 1KB to prevent abuse.
		r.Body = http.MaxBytesReader(w, r.Body, 1024)

		var req TokenExchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		if req.Email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}

		// Look up the user in the test users registry.
		var user *TestUser
		for i := range TestUsers {
			if TestUsers[i].Email == req.Email {
				user = &TestUsers[i]
				break
			}
		}
		if user == nil {
			http.Error(w, fmt.Sprintf("unknown test user email: %q", req.Email), http.StatusBadRequest)
			return
		}

		// Mint an ID token signed with Dex's signing keys.
		token, expiresIn, err := mintIDToken(r.Context(), state, user)
		if err != nil {
			slog.Error("failed to mint dev token", "email", req.Email, "error", err)
			http.Error(w, "failed to mint token", http.StatusInternalServerError)
			return
		}

		resp := TokenExchangeResponse{
			IDToken:   token,
			Email:     user.Email,
			Groups:    user.Groups,
			ExpiresIn: expiresIn,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// mintIDToken creates a signed OIDC ID token for the given test user using
// the signing keys from Dex's storage. The token structure matches what Dex
// produces for the openid, email, groups, and profile scopes.
func mintIDToken(ctx context.Context, state *DexState, user *TestUser) (string, int64, error) {
	keys, err := state.Storage.GetKeys(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get signing keys: %w", err)
	}

	signingKey := keys.SigningKey
	if signingKey == nil {
		return "", 0, fmt.Errorf("no signing key available (Dex may not have initialized yet)")
	}

	alg, err := signatureAlgorithm(signingKey)
	if err != nil {
		return "", 0, fmt.Errorf("failed to determine signature algorithm: %w", err)
	}

	now := time.Now()
	expiresIn := int64(3600) // 1 hour
	expiry := now.Add(time.Duration(expiresIn) * time.Second)

	// Encode the subject the same way Dex does: a protobuf-encoded
	// IDTokenSubject{user_id, conn_id} that is base64-RawURL-encoded.
	// The connector ID is "holos" (our auto connector).
	subject, err := encodeSubject(user.UserID, "holos")
	if err != nil {
		return "", 0, fmt.Errorf("failed to encode subject: %w", err)
	}

	emailVerified := true
	claims := idTokenClaims{
		Issuer:        state.Issuer,
		Subject:       subject,
		Audience:      audience{state.ClientID},
		Expiry:        expiry.Unix(),
		IssuedAt:      now.Unix(),
		Email:         user.Email,
		EmailVerified: &emailVerified,
		Groups:        user.Groups,
		Name:          user.DisplayName,
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal claims: %w", err)
	}

	token, err := signPayload(signingKey, alg, payload)
	if err != nil {
		return "", 0, fmt.Errorf("failed to sign token: %w", err)
	}

	return token, expiresIn, nil
}

// encodeSubject encodes a userID and connectorID into the Dex subject format.
// Dex uses a protobuf-encoded IDTokenSubject message that is then
// base64-RawURL-encoded. The proto message has two string fields:
//
//	message IDTokenSubject {
//	    string user_id = 1;
//	    string conn_id = 2;
//	}
//
// We manually encode the protobuf wire format to avoid importing
// dex/server/internal (which is a Go internal package).
func encodeSubject(userID, connID string) (string, error) {
	// Proto wire format: field 1 (tag=1, wiretype=2) + length + bytes,
	// then field 2 (tag=2, wiretype=2) + length + bytes.
	var buf []byte
	buf = protowire.AppendTag(buf, 1, protowire.BytesType)
	buf = protowire.AppendString(buf, userID)
	buf = protowire.AppendTag(buf, 2, protowire.BytesType)
	buf = protowire.AppendString(buf, connID)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// TestUserSubjectForEmail returns the OIDC sub claim minted by the embedded
// Dex dev-token endpoint for a static test user email.
func TestUserSubjectForEmail(email string) (string, bool) {
	for i := range TestUsers {
		if TestUsers[i].Email != email {
			continue
		}
		subject, err := encodeSubject(TestUsers[i].UserID, "holos")
		if err != nil {
			return "", false
		}
		return subject, true
	}
	return "", false
}

// signatureAlgorithm determines the JWS algorithm for the given key.
// This mirrors Dex's signatureAlgorithm function.
func signatureAlgorithm(jwk *jose.JSONWebKey) (jose.SignatureAlgorithm, error) {
	if jwk.Key == nil {
		return "", fmt.Errorf("no signing key")
	}
	switch key := jwk.Key.(type) {
	case *rsa.PrivateKey:
		return jose.RS256, nil
	case *ecdsa.PrivateKey:
		switch key.Params() {
		case elliptic.P256().Params():
			return jose.ES256, nil
		case elliptic.P384().Params():
			return jose.ES384, nil
		case elliptic.P521().Params():
			return jose.ES512, nil
		default:
			return "", fmt.Errorf("unsupported ecdsa curve")
		}
	default:
		return "", fmt.Errorf("unsupported signing key type %T", jwk.Key)
	}
}

// signPayload signs a JSON payload using the given JWK and algorithm.
// This mirrors Dex's signPayload function.
func signPayload(key *jose.JSONWebKey, alg jose.SignatureAlgorithm, payload []byte) (string, error) {
	signingKey := jose.SigningKey{Key: key, Algorithm: alg}
	signer, err := jose.NewSigner(signingKey, &jose.SignerOptions{})
	if err != nil {
		return "", fmt.Errorf("new signer: %w", err)
	}
	sig, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("signing payload: %w", err)
	}
	return sig.CompactSerialize()
}

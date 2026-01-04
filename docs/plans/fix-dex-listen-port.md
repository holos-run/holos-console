# Plan: Fix Dex Issuer Port Mismatch

> **Status:** UNREVIEWED / UNAPPROVED
>
> This plan has not been reviewed. Do not implement until approved.

## Overview

When the server is started on a non-default port (e.g., `--listen :4443`), the OIDC sign-in flow fails because the `--issuer` flag defaults to `https://localhost:8443/dex` regardless of the listen address. This causes a mismatch between:

1. The frontend's OIDC authority URL (derived from `window.location.origin`)
2. The backend's Dex issuer configuration

## Problem Analysis

### Current Behavior

1. **CLI default** ([cli/cli.go:56](cli/cli.go#L56)): `--issuer` defaults to `https://localhost:8443/dex`
2. **Frontend OIDC config** ([ui/src/auth/config.ts:29-31](ui/src/auth/config.ts#L29-L31)): Uses `window.location.origin` to derive authority:
   ```typescript
   const origin = window.location.origin
   return {
     authority: `${origin}/dex`,
     ...
   }
   ```
3. **Dex issuer** ([console/oidc/oidc.go:84-86](console/oidc/oidc.go#L84-L86)): Uses the `--issuer` value verbatim

### Failure Scenario

```bash
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443
```

- Server listens on port 4443
- User opens `https://localhost:4443/ui/profile`
- Frontend calculates authority as `https://localhost:4443/dex`
- Dex is configured with issuer `https://localhost:8443/dex`
- OIDC discovery URL mismatch causes redirect to wrong port

### Production Context

In production, the server may run behind a reverse proxy (e.g., Kubernetes Ingress, nginx). The external URL (`https://console.example.com`) differs from the internal listen address (`:8443`). The `--issuer` flag must be explicitly set to the external URL in this case.

## Goal

1. When `--issuer` is not specified, derive it from `--listen` address
2. When `--issuer` is explicitly specified, use that value (supports reverse proxy deployments)
3. Frontend continues to use `window.location.origin` - no changes needed on frontend

## Design Decisions

| Topic | Decision | Rationale |
| ----- | -------- | --------- |
| Default issuer | Derive from `--listen` | Match frontend behavior that uses `window.location.origin` |
| Explicit issuer | User-provided value wins | Required for reverse proxy/production deployments |
| Port parsing | Use `net.SplitHostPort` | Handle various listen formats (`:8443`, `0.0.0.0:8443`, `localhost:8443`) |
| Default host | `localhost` | When listen is `:port` format, use localhost as host |
| Scheme | Always `https` | Server always uses TLS |

## Changes Required

### Modify (Existing Files)

- [cli/cli.go](cli/cli.go) - Remove default value from `--issuer`, add logic to derive issuer from listen address

### Add (New Files)

None required (all changes in cli.go)

## Implementation

### Phase 1: Update CLI Flag Handling

#### 1.1 Remove hardcoded default from --issuer flag

Change the `--issuer` flag definition to have an empty default:

```go
cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL (defaults to https://localhost:<port>/dex)")
```

#### 1.2 Add issuer derivation logic

Add a function to derive the issuer URL from the listen address:

```go
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
```

#### 1.3 Apply derivation in Run function

Update the `Run` function to derive the issuer:

```go
func Run(cmd *cobra.Command, args []string) error {
    // ... existing code ...

    cfg := console.Config{
        ListenAddr: listenAddr,
        CertFile:   certFile,
        KeyFile:    keyFile,
        Issuer:     deriveIssuer(listenAddr, issuer),
        ClientID:   clientID,
    }

    // ... rest of function ...
}
```

### Phase 2: Testing

#### 2.1 Add unit tests for deriveIssuer

Add test cases in `cli/cli_test.go`:

```go
func TestDeriveIssuer(t *testing.T) {
    tests := []struct {
        name       string
        listenAddr string
        issuer     string
        want       string
    }{
        {
            name:       "explicit issuer takes precedence",
            listenAddr: ":8443",
            issuer:     "https://console.example.com/dex",
            want:       "https://console.example.com/dex",
        },
        {
            name:       "derive from port-only listen",
            listenAddr: ":4443",
            issuer:     "",
            want:       "https://localhost:4443/dex",
        },
        {
            name:       "derive from full listen address",
            listenAddr: "localhost:9000",
            issuer:     "",
            want:       "https://localhost:9000/dex",
        },
        {
            name:       "0.0.0.0 becomes localhost",
            listenAddr: "0.0.0.0:8443",
            issuer:     "",
            want:       "https://localhost:8443/dex",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := deriveIssuer(tt.listenAddr, tt.issuer)
            if got != tt.want {
                t.Errorf("deriveIssuer() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

#### 2.2 Manual E2E verification

Test the fix manually:

```bash
# Start server on non-default port
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443

# Verify log shows correct issuer
# Should see: "issuer": "https://localhost:4443/dex"

# Open https://localhost:4443/ui/profile and sign in
# Should redirect to Dex at port 4443, not 8443
```

### Phase 3: Documentation

#### 3.1 Update help text

Update the `--issuer` flag help text to clarify the default behavior:

```go
cmd.Flags().StringVar(&issuer, "issuer", "",
    "OIDC issuer URL for token validation. Defaults to https://localhost:<port>/dex based on --listen address. Set explicitly for reverse proxy deployments.")
```

---

## TODO (Implementation Checklist)

### Phase 1: Update CLI Flag Handling
- [ ] 1.1: Remove hardcoded default from --issuer flag
- [ ] 1.2: Add deriveIssuer function
- [ ] 1.3: Apply derivation in Run function

### Phase 2: Testing
- [ ] 2.1: Add unit tests for deriveIssuer
- [ ] 2.2: Manual E2E verification

### Phase 3: Documentation
- [ ] 3.1: Update --issuer flag help text

---

## Testing

After implementation, verify with:

```bash
# Build
make build

# Test on non-default port
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443

# Check logs for correct issuer URL
# Open https://localhost:4443/ui/profile
# Sign in should work without port mismatch

# Test explicit issuer still works
./bin/holos-console --cert certs/tls.crt --key certs/tls.key --listen :4443 --issuer https://external.example.com/dex
# Logs should show the explicit issuer URL
```

Run unit tests:
```bash
make test
```

## Security Considerations

- No new attack surface - this only changes how the default issuer is computed
- Explicit `--issuer` flag continues to work for production deployments with reverse proxies
- Token validation continues to use the configured issuer URL

## Alternatives Considered

### Alternative 1: Inject issuer into frontend at runtime

Instead of deriving the issuer on the backend, the server could inject the issuer URL into the HTML served to the frontend (via `window.__OIDC_CONFIG__`).

**Rejected:** The current frontend behavior of using `window.location.origin` is correct and simple. The problem is the backend's hardcoded default, not the frontend's discovery mechanism.

### Alternative 2: Always require explicit --issuer flag

Remove the default entirely and require users to specify `--issuer` in all cases.

**Rejected:** Poor developer experience. The common case (local development) should work without extra flags.

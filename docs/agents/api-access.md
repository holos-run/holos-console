# API Access

Users and agents can call any RPC from the command line using `curl` (Connect protocol — recommended) or `grpcurl` (gRPC backward compatibility). See `docs/api-access.md` for the canonical reference covering:

- How to obtain an ID token from the profile page (`/profile`)
- Shell-history-safe `export HOLOS_ID_TOKEN=...` workflow
- `curl` Connect-protocol invocation (`-H "Connect-Protocol-Version: 1"`)
- `grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem"` invocation (never `-plaintext` or `-insecure` — the listener is TLS-only and always presents a valid cert)
- gRPC reflection (`grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" localhost:8443 list`)
- Troubleshooting the `first record does not look like a TLS handshake` error

## Related

- [Adding New RPCs](adding-rpcs.md) — How to create RPC endpoints
- [Authentication](authentication.md) — How to obtain tokens
- [TLS Command Guardrail](guardrail-tls-commands.md) — Never bypass TLS verification in examples

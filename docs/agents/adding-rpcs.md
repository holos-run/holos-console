# Adding New RPCs

Steps to add a new RPC endpoint:

1. Define RPC and messages in `proto/holos/console/v1/*.proto`
2. Run `make generate`
3. Implement handler method in `console/rpc/` (embed `Unimplemented*Handler` for forward compatibility)
4. Handler is auto-wired when service is registered in `console/console.go`

See `docs/rpc-service-definitions.md` for detailed examples. See `docs/permissions-guide.md` for permission design guidelines including narrow scoping, multi-level grantability, and the cascade table pattern.

## Related

- [Code Generation](code-generation.md) — Protobuf generation pipeline
- [API Access](api-access.md) — How to call RPCs from the command line
- [Package Structure](package-structure.md) — Where handlers are registered

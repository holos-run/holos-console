# Code Generation

Protobuf code is generated using buf. The `generate.go` file contains the `//go:generate buf generate` directive. After modifying `.proto` files in `proto/`, run:

```bash
make generate   # or: go generate ./...
```

This produces:

- `gen/**/*.pb.go` — Go structs for messages
- `gen/**/consolev1connect/*.connect.go` — ConnectRPC client/server bindings
- `frontend/src/gen/**/*_pb.ts` — TypeScript message classes and service definitions (protobuf-es v2)

## Related

- [Adding New RPCs](adding-rpcs.md) — End-to-end steps for adding a new RPC
- [Pre-Commit Workflow](pre-commit.md) — Always run `make generate` before committing
- [Package Structure](package-structure.md) — Where generated code lives
- [Tool Dependencies](tool-dependencies.md) — buf is a pinned tool dependency

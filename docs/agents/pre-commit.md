# Pre-Commit Workflow

**Always run `make generate` before committing changes.** This command:

1. Regenerates protobuf code (Go and TypeScript)
2. Rebuilds the UI (runs `npm run build` which includes TypeScript type checking)

If `make generate` fails, fix the errors before committing. Common issues:

- TypeScript type errors in test mocks (cast mock responses with `as unknown as ...`)
- Missing protobuf imports after adding new message types

## Related

- [Build Commands](build-commands.md) — Full list of make targets
- [Code Generation](code-generation.md) — What `make generate` produces

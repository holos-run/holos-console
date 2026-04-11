# Tool Dependencies

Tool versions are pinned in `tools.go` using the Go tools pattern. Install with `make tools`. Currently pins: buf.

CUE is used at runtime (not as a pinned tool) by the `console/templates/` package to parse and validate deployment template source. The `cuelang.org/go` module is a regular Go dependency listed in `go.mod`. See `docs/cue-template-guide.md` for the full template interface, including the structured `projectResources`/`platformResources` output format (ADR 016). CUE schema definitions for template types (`#ProjectInput`, `#PlatformInput`, etc.) are generated from `api/v1alpha2` Go types via `cue get go` and prepended by the renderer.

## Related

- [Code Generation](code-generation.md) — buf generates protobuf code
- [Template Service](template-service.md) — CUE runtime used for template parsing
- [Build Commands](build-commands.md) — `make tools` installs pinned dependencies

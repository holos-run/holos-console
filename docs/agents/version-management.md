# Version Management

Version is determined by:

1. `console/version/{major,minor,patch}` files (embedded at compile time)
2. `GitDescribe` ldflags override (set by Makefile during build)

Build metadata (commit, tree state, date) injected via ldflags in Makefile.

See `docs/versioning.md` for the complete versioning workflow including bump and tag procedures.

## Container Builds

Trigger container image builds using the `container.yaml` GitHub workflow. The workflow runs from `main` and accepts a `git_ref` input specifying what to check out and build:

```bash
gh workflow run container.yaml --ref main -f git_ref=refs/heads/<branch-name>
gh workflow run container.yaml --ref main -f git_ref=refs/tags/v1.2.3
```

## Related

- [Build Commands](build-commands.md) — `make bump-*` and `make tag` targets
- [Implementing Plans](implementing-plans.md) — Merge commits preserve screenshot SHA references

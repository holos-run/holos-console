# Versioning

## Version Files

The version is stored in three plain-text files under `console/version/`:

- `major` - Major version number
- `minor` - Minor version number
- `patch` - Patch version number

These files are embedded into the Go binary at compile time via `//go:embed` in
`console/version.go`. The `GitDescribe` ldflag (set by the Makefile from
`git describe --tags`) takes precedence at runtime when available.

## Bumping Versions

Use Make targets to bump version numbers. Never edit the version files by hand.

```bash
make bump-major   # Increment major, reset minor and patch to 0
make bump-minor   # Increment minor, reset patch to 0
make bump-patch   # Increment patch
make show-version # Display current version
```

After bumping, commit the changed version files, then create a tag:

```bash
make bump-minor
git add console/version/
git commit -m "Bump version to $(make show-version)"
make tag
```

## Creating Tags

**Never use `git tag` directly.** Always use `make tag`, which:

1. Reads the version from the committed version files
2. Validates that the tag does not already exist
3. Validates that the working tree is clean
4. Creates an annotated tag (`git tag -a`) with a release message

The version in the tag must match the version committed in the code. Using
`make tag` enforces this by deriving the tag name from the version files.

## Release Workflow

1. Bump the version: `make bump-minor` (or `bump-major` / `bump-patch`)
2. Commit the version files on the release branch
3. Merge the PR
4. Check out the merge commit on `main`
5. Run `make tag` to create the annotated tag
6. Push the tag: `git push origin v$(make show-version)`

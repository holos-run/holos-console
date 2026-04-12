# Guardrail: CI Check Names

**Rule**: When polling or waiting for GitHub Actions CI checks on PRs in this repository, use the actual workflow job names — not shorthand or lowercase variants.

| Actual check name | Common mistake |
|-------------------|----------------|
| `Unit Tests` | `test` |
| `Lint` | `lint` |
| `E2E Tests` | `e2e` |

**Triggers**: Apply when writing `gh pr checks` polling loops, parsing CI status JSON, or filtering checks by name. Always run `gh pr checks <PR> --json name,bucket` once to discover names before entering a poll loop.

## Example

```bash
# Correct: use actual check names
TEST_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "Unit Tests") | .bucket')
LINT_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "Lint") | .bucket')
E2E_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "E2E Tests") | .bucket')

# Wrong: these return empty strings
TEST_BUCKET=$(echo "$CHECKS" | jq -r '.[] | select(.name == "test") | .bucket')
```

## Related

- [Build Commands](build-commands.md) — Make targets that correspond to these CI checks
- [Testing Patterns](testing-patterns.md) — Test structure and tooling

---
name: codex-review
description: Run a cross-model code review using the OpenAI Codex CLI. Use this skill after implementing changes to get a second opinion before opening a PR. Triggers on phrases like "codex review", "cross-model review", "review my changes", "get a codex review", or "/codex-review". Designed for implement-review-fix loops where Claude implements and Codex reviews.
version: 1.0.0
---

# Codex Review

Cross-model code review using the OpenAI Codex CLI. Reviews your branch changes against project conventions, writes findings to a file, and reports a structured verdict. Designed for fix-review loops: invoke repeatedly until clean.

## Arguments

`{{ARGUMENTS}}` is optional and controls the review scope:

- **No argument or `branch`** -- Review the current branch diff against `main` (default)
- **`uncommitted`** -- Review only uncommitted changes
- **`quick`** -- Quick review using scope flag only (no custom prompt, faster but less targeted)

## Workflow

### 1. Preflight

Verify codex is available and determine review scope:

```bash
CODEX="/Users/u6136576/.nvm/versions/node/v24.12.0/bin/codex"
if ! [ -x "$CODEX" ]; then
  echo "ERROR: codex CLI not found at $CODEX"
  echo "Install with: npm install -g @openai/codex"
  # Stop here
fi
```

Check the current branch:
```bash
CURRENT_BRANCH=$(git branch --show-current)
```

### 2. Determine Review Round

```bash
mkdir -p tmp/codex-review
ROUND=$(ls tmp/codex-review/round-*.md 2>/dev/null | wc -l | tr -d ' ')
ROUND=$((ROUND + 1))
OUTPUT_FILE="tmp/codex-review/round-${ROUND}.md"
```

### 3. Determine Invocation Mode

The `codex exec review` CLI has a constraint: `--base <BRANCH>` and `[PROMPT]` are mutually exclusive. This means you cannot provide both a custom review prompt and a branch scope flag in the same invocation.

**Two invocation modes** depending on the argument:

| Argument | Mode | Command |
|----------|------|---------|
| *(empty)* or `branch` | **Prompted** | `codex exec review --ephemeral -o $OUTPUT_FILE "prompt with scope instructions"` |
| `uncommitted` | **Prompted** | `codex exec review --ephemeral -o $OUTPUT_FILE "prompt about uncommitted changes"` |
| `quick` | **Scoped** | `codex exec review --base main --ephemeral -o $OUTPUT_FILE` |

**Prompted mode** (default) embeds project conventions in the prompt and tells codex to review the branch diff. This is more thorough because codex knows exactly what to look for.

**Scoped mode** (`quick`) uses the `--base main` flag for automatic diff scoping but relies on codex's default review criteria. Faster but less targeted.

### 4. Run the Review

**Prompted mode** (default — includes project conventions):

```bash
CODEX="/Users/u6136576/.nvm/versions/node/v24.12.0/bin/codex"

# Determine scope instruction based on argument
# Default/branch: "Review the changes on this branch compared to the main branch"
# Uncommitted: "Review the uncommitted changes in the working directory"

timeout 300 $CODEX exec review \
  --ephemeral \
  -o "${OUTPUT_FILE}" \
  "$(cat <<'REVIEW_PROMPT'
Review the changes on this branch compared to the main branch. Run `git diff main...HEAD` to see the diff.

You are reviewing changes to holos-console, a Go HTTPS server with a React frontend that serves a web console UI and exposes ConnectRPC services. The built UI is embedded into the Go binary via go:embed.

## Review Checklist

Check each convention below. Report ONLY violations you actually find in the diff. Do not report passing checks.

### Critical (must fix before merge)

1. **TLS example guardrail**: No `curl -k`, `curl --insecure`, or `grpcurl -insecure` anywhere in code, tests, docs, or comments. The server always uses valid TLS certs via mkcert.
2. **Security**: No hardcoded credentials, secrets in code, command injection, XSS, SQL injection, or insecure defaults.
3. **Generated code not edited**: Files under `gen/` and `frontend/src/gen/` must not be manually edited -- they are produced by `buf generate`.
4. **Template field guardrail**: New fields on `CreateDeploymentRequest`, `DeploymentDefaults`, or template proto messages must also appear in `api/v1alpha2/types.go`, `console/deployments/render.go`, the frontend template editor preview, and `console/templates/defaults.go`.

### Important (should fix)

5. **Terminology**: Use "platform template" not "system template" for org-level or folder-level CUE templates.
6. **Code generation consistency**: If proto files or CUE schemas changed, `gen/` and `frontend/src/gen/` should have corresponding changes (i.e. `make generate` was run).
7. **Test coverage**: New behavior should have tests. Prefer unit tests (Vitest + RTL for frontend, table-driven Go tests with fake K8s client) over E2E. E2E only for OIDC login and full-stack K8s CRUD.
8. **RED GREEN pattern**: Tests should define expected behavior. Assertions should match the implementation. Look for tests that always pass (tautologies) or never exercise the new code path.
9. **No backwards compatibility noise**: This code is unreleased. Do not flag breaking changes, removed exports, or renamed APIs as issues. They are expected.

### Style (optional, nice to have)

10. **UI conventions**: Semantic CSS tokens (`bg-background`, `text-foreground`), not hardcoded colors. `Combobox` for dynamic collections, basic `Select` only for small static enumerations (2-4 fixed choices).
11. **Dead code**: Unused imports, unreachable code paths, stale comments that reference removed behavior.
12. **Error handling**: Validate at system boundaries (user input, external APIs). Do not add defensive checks for internal code that cannot produce the error.

## Output Format

Structure your review as markdown:

## Review Summary

**Round**: <round number if known, else 1>
**Scope**: <what was reviewed -- branch diff, uncommitted changes, etc.>
**Verdict**: CLEAN | HAS_ISSUES

## Findings

### [CRITICAL] <title>
- **File**: <path>:<line>
- **Issue**: <what is wrong>
- **Fix**: <concrete suggestion>

### [IMPORTANT] <title>
...

### [STYLE] <title>
...

If no issues found at any severity, output:

## Review Summary

**Round**: 1
**Scope**: <what was reviewed>
**Verdict**: CLEAN

No issues found. The changes follow project conventions.

IMPORTANT: Be specific and concise. Cite exact file paths and line numbers. Give concrete fix suggestions. Only report actual issues found in the diff, not hypothetical concerns.
REVIEW_PROMPT
)"
```

**Quick mode** (scope flag only, no custom prompt):

```bash
timeout 300 $CODEX exec review \
  --base main \
  --ephemeral \
  -o "${OUTPUT_FILE}"
```

The `--ephemeral` flag prevents codex from persisting session files. The `-o` flag writes only the final review message to the output file (no progress noise). The `timeout 300` caps execution at 5 minutes.

### 5. Read and Present Results

After codex finishes, read the output file with the Read tool.

If the output file is empty or missing, codex may have failed (API error, timeout, auth issue). Report the error and suggest the user check:
- `codex login status` -- authentication
- Network connectivity to OpenAI API
- The stderr output from the command for specific error messages

If the output file has content, look for the **Verdict** line:
- `**Verdict**: CLEAN` -- No blocking issues found
- `**Verdict**: HAS_ISSUES` -- Issues to address

Count findings by severity by searching for `### [CRITICAL]`, `### [IMPORTANT]`, and `### [STYLE]` headings.

### 6. Report Summary

Print a concise summary for the calling agent or user:

```
Codex review round N complete.
- Output: tmp/codex-review/round-N.md
- Verdict: CLEAN | HAS_ISSUES
- Critical: <count>
- Important: <count>
- Style: <count>
```

Then based on the verdict:

| Verdict | Action |
|---------|--------|
| CLEAN | "Review is clean. Ready to open PR." |
| HAS_ISSUES (CRITICAL or IMPORTANT) | "Review found issues. Fix and re-run /codex-review." |
| HAS_ISSUES (STYLE only) | "Minor style suggestions only. Optionally fix, then proceed." |

## Fix-Review Loop Pattern

When used during implementation (e.g., between step 5 and step 7 of `/implement-issue`):

```
1. Implement changes and commit
2. Run /codex-review
3. If CLEAN → proceed to PR
4. If HAS_ISSUES:
   a. Read tmp/codex-review/round-N.md
   b. Fix each CRITICAL and IMPORTANT finding
   c. Commit fixes with message: "fix: address codex review round N findings"
   d. Run /codex-review again (round N+1)
   e. Repeat until CLEAN or only STYLE findings remain
5. Maximum 3 rounds -- after round 3, proceed to PR
   and note any remaining issues in the PR description
```

### Integrating with implement-issue

Insert codex review between implementation and PR creation:

```
/implement-issue steps 1-5: fetch, branch, comment, explore, implement
  ↓
/codex-review (loop until clean, max 3 rounds)
  ↓
/implement-issue steps 6-10: cleanup, PR, CI, screenshots, merge
```

### Resetting Review Rounds

To start fresh (e.g., new feature, different branch):

```bash
rm -f tmp/codex-review/round-*.md
```

## Prerequisites

- **Codex CLI**: Install with `npm install -g @openai/codex`
- **Authentication**: Run `codex login` once to authenticate with OpenAI
- **Git repository**: Must be inside a git repo

## Technical Notes

- Uses `codex exec review` (not `codex review`) to access `-o` and `--ephemeral` flags
- The `--base` flag and `[PROMPT]` are mutually exclusive in codex CLI — the default mode embeds scope instructions in the prompt instead
- Output captured via `-o` flag writes only the final agent message (no progress noise)
- The `tmp/` directory is gitignored — review artifacts are not committed
- Each round creates a separate file for cross-round comparison
- The review runs in read-only sandbox mode — codex does not modify files
- Cross-model review (GPT reviewing Claude's work) provides independent perspective
- Override the model with `-c model="gpt-5.4"` if needed

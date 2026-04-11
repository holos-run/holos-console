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

Verify codex is available and determine review scope.

```bash
CODEX="/Users/u6136576/.nvm/versions/node/v24.12.0/bin/codex"
if ! [ -x "$CODEX" ]; then
  echo "ERROR: codex CLI not found at $CODEX"
  echo "Install with: npm install -g @openai/codex"
  # Stop here -- do not proceed without codex
fi
```

Check the current branch:
```bash
CURRENT_BRANCH=$(git branch --show-current)
```

If on `main` with no argument, fall back to `uncommitted` scope.

### 2. Determine Review Round

Check for existing review output to track the iteration count:

```bash
mkdir -p tmp/codex-review
ROUND=$(ls tmp/codex-review/round-*.md 2>/dev/null | wc -l | tr -d ' ')
ROUND=$((ROUND + 1))
OUTPUT_FILE="tmp/codex-review/round-${ROUND}.md"
```

### 3. Determine Invocation Mode

The `codex exec review` CLI has a constraint: `--base <BRANCH>` and `[PROMPT]` are **mutually exclusive**. You cannot provide both a custom review prompt and a branch scope flag in the same invocation.

**Two invocation modes:**

| Argument | Mode | What happens |
|----------|------|-------------|
| *(empty)*, `branch`, or `uncommitted` | **Prompted** | Custom prompt includes project conventions AND scope instructions. More thorough. |
| `quick` | **Scoped** | Uses `--base main` flag, relies on codex default review criteria. Faster. |

### 4. Run the Review

#### Prompted mode (default)

Includes project conventions in the prompt. The prompt tells codex how to find the diff.

```bash
CODEX="/Users/u6136576/.nvm/versions/node/v24.12.0/bin/codex"

# For branch scope (default):
#   "Run `git diff main...HEAD` to see the changes to review."
# For uncommitted scope:
#   "Run `git diff` and `git diff --cached` to see the uncommitted changes to review."

timeout 300 $CODEX exec review \
  --ephemeral \
  -o "${OUTPUT_FILE}" \
  "$(cat <<'REVIEW_PROMPT'
Review the changes on this branch compared to the main branch. Run `git diff main...HEAD` to see the diff, and `git diff main...HEAD --name-only` to list changed files.

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

#### Quick mode

Uses the `--base main` flag for automatic diff scoping, no custom prompt:

```bash
timeout 300 $CODEX exec review \
  --base main \
  --ephemeral \
  -o "${OUTPUT_FILE}"
```

The `--ephemeral` flag prevents codex from persisting session files. The `-o` flag writes only the final review message to the output file. The `timeout 300` caps execution at 5 minutes.

### 5. Handle Errors

If the output file is empty or missing after codex exits, the review failed. Common causes:
- **Auth error**: Run `codex login` to authenticate
- **API error**: OpenAI API may be unavailable (500 errors)
- **Timeout**: Review took longer than 5 minutes
- **No diff**: No changes to review

Check stderr from the codex command for specific error messages. Report the failure and suggest the user retry or check `codex login status`.

### 6. Read and Present Results

Read the output file with the Read tool. Parse the review:

1. Find the **Verdict** line: `CLEAN` or `HAS_ISSUES`
2. Count findings by severity: grep for `### [CRITICAL]`, `### [IMPORTANT]`, `### [STYLE]` headings

Print a concise summary:

```
Codex review round N complete.
- Output: tmp/codex-review/round-N.md
- Verdict: CLEAN | HAS_ISSUES
- Critical: <count>
- Important: <count>
- Style: <count>
```

| Verdict | Next step |
|---------|-----------|
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

The implementing agent should:
1. Complete the implementation and commit all changes
2. Invoke `/codex-review` (default: review branch diff against main)
3. Read the review output from `tmp/codex-review/round-N.md`
4. Fix CRITICAL and IMPORTANT findings, commit
5. Re-invoke `/codex-review` if needed
6. Proceed to cleanup and PR once clean

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

- Uses `codex exec review` (not bare `codex review`) to access `-o` and `--ephemeral` flags
- The `--base` flag and `[PROMPT]` are mutually exclusive in codex CLI — the default "prompted" mode embeds scope instructions in the prompt instead
- The `-o` flag writes only the final agent message (clean output, no progress noise)
- The `tmp/` directory is gitignored — review artifacts are not committed
- Each round creates a separate numbered file for cross-round comparison
- The review runs in codex's default read-only sandbox — it does not modify files
- Cross-model review (GPT reviewing Claude's work) provides an independent perspective
- Override the model with `-c model="gpt-5.4"` if needed

---
name: implement-issue
description: Implement a GitHub issue end-to-end. Use this skill when the user provides a GitHub issue URL and asks to implement it, work on it, fix it, or resolve it. Triggers on phrases like "implement issue", "work on this issue", "fix this issue", or when given a GitHub issue URL alone. Handles the full workflow: fetch, branch, comment, implement, open PR, fix CI, capture screenshots, and merge.
version: 5.0.0
---

# Implement Issue

Full workflow for implementing a GitHub issue: fetch the issue, create a feature branch, announce on the issue, implement using repository conventions, open a PR, loop on CI until green, capture screenshots for frontend changes, and merge.

## Workflow

### 1. Fetch the Issue

Fetch the issue details using the gh CLI:

```bash
gh issue view <issue-number> --repo <owner/repo> --json number,title,body,labels
```

Parse the issue URL to extract the repo and issue number. URL formats:
- `https://github.com/owner/repo/issues/123` → repo=`owner/repo`, number=`123`

### 1b. Check for Sub-Issues

After fetching the issue, check its body for sub-issues. Sub-issues are indicated by task-list lines referencing other issues in the same repo, matching patterns like:

- `- [ ] #123 — description`
- `- [x] #123 — description`
- `- [ ] #123`

Extract all referenced issue numbers from these patterns using a regex like `#(\d+)`.

**If sub-issues are found**, implement only the **first unchecked sub-issue**, then stop:

1. Find the first sub-issue that is not checked off (i.e., `- [ ] #NNN`, not `- [x] #NNN`).
2. Launch an Agent tool call with `subagent_type: "general-purpose"` and `model: "opus"`.
   - The agent prompt must instruct it to invoke the `/implement-issue` skill with that sub-issue number as the argument. Include the repo context (owner/repo) and working directory so the agent has full context.
3. **Wait for the agent to complete.**
4. **Stop here** — do not proceed to the next sub-issue or continue to step 2. Only one sub-issue is implemented per invocation. This prevents stacked PRs while code review is being integrated into the workflow.

If all sub-issues are already checked off, comment on the parent issue that all work is complete and stop.

**If no sub-issues are found**, continue with step 2 below (normal single-issue workflow).

### 2. Fetch Origin and Create Branch

```bash
git fetch origin
git checkout main
git pull origin main
git checkout -b <branch-name>
```

**Branch naming**: `feat/<number>-<slug>` where `<slug>` is the issue title converted to lowercase with spaces replaced by hyphens, truncated to ~40 chars. Examples:
- Issue #42 "Add Playwright E2E test infrastructure" → `feat/42-add-playwright-e2e-test-infrastructure`
- Issue #7 "Fix token expiry handling" → `feat/7-fix-token-expiry-handling`
- Issue #173 "Add role-based access control for secrets" → `feat/173-add-role-based-access-control-for-secrets`

Strip special characters from the slug (keep only alphanumeric and hyphens).

### 3. Comment on the Issue

Post a comment announcing which agent is working on this issue:

```bash
gh issue comment <number> --repo <owner/repo> --body "$(cat <<'EOF'
Working on this issue.

- **Agent path**: <absolute path of working directory>
- **Hostname**: <hostname from `hostname` command>
- **Branch**: <branch-name>
EOF
)"
```

Get the values:
```bash
pwd          # working directory
hostname     # machine hostname
```

### 4. Read and Understand the Codebase

Before implementing:
1. Read `AGENTS.md` (or `CLAUDE.md`) for project conventions
2. Read `CONTRIBUTING.md` if it exists for commit message requirements
3. Explore the relevant code areas mentioned in the issue
4. Understand existing patterns before writing new code

### 5. Implement Using RED GREEN Approach

Follow the RED GREEN pattern from the repository's conventions:

1. **RED** — Write failing tests first that define the expected behavior
2. **GREEN** — Write the minimum implementation to make the tests pass
3. Make regular commits as you work — each commit should be a logical unit

Commit message format (check CONTRIBUTING.md for the specific format):
```
<type>(<scope>): <short description>

<longer explanation if needed>
```

Run the relevant test commands to verify your implementation:
- `make test` — all tests
- `make test-go` — Go tests
- `make test-ui` — UI unit tests
- `make generate` — regenerate code if proto/schema files changed

**Always run `make generate` before committing** if any generated files might be affected.

### 6. Final Cleanup Phase

Before opening the PR, scan for:
- Dead code introduced or made stale by the implementation
- Obsolete comments or outdated references
- Unused imports
- Stale documentation

Commit cleanup separately with a message explaining what was removed and why.

### 7. Open the PR

```bash
gh pr create --title "<concise title under 70 chars>" --body "$(cat <<'EOF'
## Summary
- <bullet points describing changes>

Closes: #<issue-number>

## Test plan
- [ ] <specific things to verify>

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

The `Closes: #<number>` line automatically closes the issue when the PR is merged.

**Stop here.** Do not loop on CI checks, capture screenshots, or merge. The PR is open and ready for a human or another agent to review, fix CI, and merge. This boundary exists because code review (e.g., `/review-pr`) is being integrated into the workflow — until that integration is complete, each PR should be reviewed before further work proceeds.

## Key Conventions

- **No backwards compatibility**: This code is not yet released; breaking changes are fine
- **RED GREEN**: Write tests before implementation
- **Regular commits**: Commit logical units as you go, not one giant commit at the end
- **make generate**: Always run before committing if proto or generated files are involved
- **Cleanup phase**: Every implementation ends with a cleanup commit
- **Stop at PR**: Do not loop on CI, capture screenshots, or merge — stop after opening the PR
- **One sub-issue at a time**: When a parent issue has sub-issues, implement only the first unchecked one per invocation
- **Close the right issue**: PRs close the specific issue being worked on (`Closes #<sub-issue>` for sub-issues, not the parent)

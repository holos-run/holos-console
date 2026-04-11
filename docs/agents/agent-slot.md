# Agent Slot Identification

Agents run in worktrees whose path encodes the agent slot. Identify your slot from your working directory — for example, if `pwd` is `/path/to/worktrees/holos-run/agent-2/holos-console`, your slot is `agent-2`.

**Issue title**: Use a conventional commit prefix (`feat:`, `fix:`, `docs:`, `build:`, `refactor:`, `test:`). Do **not** include the agent slot.

**PR title**: Use a conventional commit prefix (`feat:`, `fix:`, `docs:`, `build:`, `refactor:`, `test:`). Do **not** include the agent slot.

**PR description**: Include the slot in the footer so reviewers know which agent produced the work.

## Example Workflow

```bash
git checkout -b feat/add-playwright-config
# ... implement changes, committing as you go ...
git commit -m "Add webServer configuration to playwright.config.ts

Configure Playwright to automatically start Go backend and Vite dev
server before running E2E tests."

# Determine agent slot from working directory
SLOT=$(pwd | grep -oP 'agent-\d+' || echo "agent-0")

# Open a PR that closes the plan issue
# Note: PR title uses conventional commit format — no agent slot prefix
gh pr create --title "feat: add Playwright E2E test infrastructure" --body "$(cat <<'EOF'
## Summary
- Configure Playwright to start Go backend and Vite dev server
- Add E2E test for the login flow

Closes: #42

## Test plan
- [ ] `make test-e2e` passes

🤖 Generated with [Claude Code](https://claude.com/claude-code) · ${SLOT}
EOF
)"
```

## Related

- [Implementing Plans](implementing-plans.md) — Full plan-to-PR workflow

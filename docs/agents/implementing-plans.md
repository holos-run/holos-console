# Implementing Plans

Plans are recorded as GitHub issues. Implement each plan on a feature branch with regular commits in a single PR that references the issue.

1. **Create a feature branch** from `main` for the plan.
2. **Make regular commits** as you work. Each commit should be a logical unit of change.
3. **Open a PR** when the work is complete. Include `Closes: #NN` (where NN is the issue number) in the PR description so the issue is automatically closed when the PR is merged.
4. **Loop on PR checks**: after pushing, watch CI checks (`gh pr checks <N> --watch`) and fix any failures. Iterate until all checks pass.
5. **Merge via merge commit** once all checks pass: `gh pr merge <N> --merge`. Do not squash or rebase — the project uses merge commits so that commit SHAs referenced in screenshot URLs remain reachable in `main` history.

## Related

- [Agent Slot Identification](agent-slot.md) — PR title and description conventions for agents
- [Red-Green Implementation](red-green.md) — TDD approach for each phase
- [Dispatching Plans](dispatching-plans.md) — How to dispatch issues to agent worktrees
- [Cleanup Phase](cleanup-phase.md) — Required final phase for every plan
- [Tracking Progress](tracking-progress.md) — Keeping GitHub issues up to date

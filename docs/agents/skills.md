# Skills

Skills live in `.claude/skills/<name>/SKILL.md` using the **directory-based layout**. Do not create single-file skills (`.claude/skills/<name>.md`). The directory layout allows supporting files (reference docs, templates, scripts) alongside the skill definition.

Use the `/skill-creator` skill to create and manage skills when available. If `/skill-creator` is not available, create the directory and `SKILL.md` manually following the standard layout:

```
.claude/skills/<name>/
├── SKILL.md          # Required — YAML frontmatter + markdown instructions
├── reference.md      # Optional — detailed reference material
├── examples.md       # Optional — usage examples
├── template.md       # Optional — template for Claude to fill in
└── scripts/          # Optional — executable helpers
```

## Current Skills

| Skill | Purpose |
|-------|---------|
| `agent-browser` | Browser automation for visual verification and E2E workflows |
| `implement-issue` | Implement a single GitHub issue end-to-end (branch, code, PR) |
| `implement-plan` | Execute a full plan: iterate sub-issues, implement (Opus), review (Codex), fix, merge or escalate |
| `plan-issue` | Create implementation plans as GitHub issue hierarchies |
| `reset-agent` | Reset the current agent worktree to a clean state on origin/main |
| `review-pr` | Cross-model PR review (currently Codex CLI backend) |

## Related

- [Implementing Plans](implementing-plans.md) — Plans that skills help execute
- [Browser Automation](browser-automation.md) — The agent-browser skill in detail

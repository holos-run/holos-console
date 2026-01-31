# Slack Agents Integration: Prompt named Claude Code agents from Slack channel

> **GitHub Issue** — Copy this content to create an issue at holos-run/holos-console

## Summary

Wire up Slack integration so team members can prompt specific named Claude Code agents from within a Slack channel. Agents execute in sandbox VMs (Debian 13) and respond in Slack threads. This is "multiplayer mode" — Jeff has one VM with multiple named agents, Andy has a separate VM with multiple named agents, and everyone can prompt any agent.

Slack thread reference: https://openinfrastructure.slack.com/archives/C0AC6KFAKKL/p1769884931671709?thread_ts=1769884109.352349&cid=C0AC6KFAKKL

## Context & Existing Infrastructure

- **Claude Code** already running in Debian 13 sandbox VMs
- **GitHub CLI (`gh`)** working in the VMs
- **Paid Slack org** (openinfrastructure.slack.com)
- **Public-facing Kubernetes cluster** available for webhook routing
- All working in the **holos-run** GitHub org

## Research Findings

### Approach: Claude Agent SDK + Slack Socket Mode Bot

The recommended architecture combines three components:

1. **[Claude Agent SDK](https://platform.claude.com/docs/en/agent-sdk/overview)** — the official SDK that exposes Claude Code as a programmable library. Supports Python and TypeScript, built-in tools (Read, Edit, Bash, Glob, Grep, etc.), session management, subagents, MCP servers, and hooks.

2. **Slack Bot via Socket Mode** — WebSocket-based connection to Slack (no public endpoint needed per VM, though we have K8s available). Requires `SLACK_BOT_TOKEN` (xoxb-), `SLACK_APP_TOKEN` (xapp-), and `SLACK_SIGNING_SECRET`.

3. **Agent Router/Dispatcher** — a lightweight daemon running on each VM that routes Slack mentions to the correct named agent.

### Prior Art

| Project | Description | Relevance |
|---------|-------------|-----------|
| [mpociot/claude-code-slack-bot](https://github.com/mpociot/claude-code-slack-bot) | TypeScript Slack bot using Claude Code SDK via Socket Mode | Closest to what we need; single-agent, single-VM |
| [sleepless-agent](https://github.com/context-machine-lab/sleepless-agent) | 24/7 daemon with Slack integration, multi-agent workflow, task queue | Good architectural reference for daemon + queue pattern |
| [OpenClaw](https://openclaw.ai/) (formerly Clawdbot) | Open-source personal AI assistant with Slack skill | Demonstrates Slack as a chat interface for autonomous agents |
| [claude-slack (theo-nash)](https://github.com/theo-nash/claude-slack) | MCP-based agent-to-agent communication protocol | Relevant for inter-agent coordination |
| [Official Claude Code in Slack](https://code.claude.com/docs/en/slack) | Anthropic's built-in Slack integration | Routes to Claude Code on the web, not self-hosted VMs |

## Proposed Architecture

```
┌─────────────────────────────────────────────────────┐
│                    Slack Channel                     │
│  @jeff-console "fix the auth bug in holos-console"   │
│  @andy-reviewer "review PR #42"                      │
└──────────────┬───────────────────────┬───────────────┘
               │ Socket Mode           │ Socket Mode
               ▼                       ▼
┌──────────────────────┐  ┌──────────────────────┐
│   Jeff's VM (Debian) │  │  Andy's VM (Debian)  │
│                      │  │                      │
│  ┌────────────────┐  │  │  ┌────────────────┐  │
│  │ Agent Daemon   │  │  │  │ Agent Daemon   │  │
│  │ (dispatcher)   │  │  │  │ (dispatcher)   │  │
│  └───┬────────┬───┘  │  │  └───┬────────┬───┘  │
│      │        │      │  │      │        │      │
│  ┌───▼──┐ ┌──▼───┐  │  │  ┌───▼──┐ ┌──▼───┐  │
│  │agent │ │agent │  │  │  │agent │ │agent │  │
│  │"con- │ │"docs"│  │  │  │"re-  │ │"test"│  │
│  │sole" │ │      │  │  │  │view" │ │      │  │
│  └──────┘ └──────┘  │  │  └──────┘ └──────┘  │
│                      │  │                      │
│  Claude Agent SDK    │  │  Claude Agent SDK    │
│  + gh CLI            │  │  + gh CLI            │
└──────────────────────┘  └──────────────────────┘
```

### Components

#### 1. Slack App Configuration

Create a single Slack App with:
- **Bot scopes**: `app_mentions:read`, `chat:write`, `channels:history`, `groups:history`, `im:history`, `files:read`, `files:write`
- **Socket Mode** enabled (generates `xapp-` app-level token)
- **Event subscriptions**: `app_mention`, `message.im`
- Bot user name serves as the router entry point (e.g., `@holos-agents`)

#### 2. Agent Daemon (per VM)

A lightweight Python or TypeScript process running on each VM:

```python
# Pseudocode for the agent daemon
from claude_agent_sdk import query, ClaudeAgentOptions, AgentDefinition
from slack_bolt import App
from slack_bolt.adapter.socket_mode import SocketModeHandler

app = App(token=SLACK_BOT_TOKEN)

# Named agents configured per-VM
AGENTS = {
    "console": AgentDefinition(
        description="Works on holos-console codebase",
        prompt="You are a developer working on holos-console...",
        tools=["Read", "Edit", "Bash", "Glob", "Grep", "Task"]
    ),
    "docs": AgentDefinition(
        description="Writes and reviews documentation",
        prompt="You are a documentation specialist...",
        tools=["Read", "Edit", "Glob", "Grep", "WebSearch"]
    ),
}

@app.event("app_mention")
async def handle_mention(event, say):
    text = event["text"]
    agent_name, prompt = parse_agent_and_prompt(text)
    agent = AGENTS.get(agent_name)

    # Run agent with Claude Agent SDK
    async for message in query(
        prompt=prompt,
        options=ClaudeAgentOptions(
            allowed_tools=agent.tools,
            cwd="/path/to/repo",
        )
    ):
        # Stream responses back to Slack thread
        update_slack_thread(event, message)

SocketModeHandler(app, SLACK_APP_TOKEN).start()
```

Key daemon responsibilities:
- Parse `@bot agent-name <prompt>` from Slack messages
- Route to the correct named agent
- Manage Claude Agent SDK sessions (resumable per-thread)
- Stream responses back to Slack threads
- Handle concurrent requests via async/threading
- Graceful shutdown on SIGTERM/SIGINT

#### 3. Agent Registration & Discovery

Each VM registers its available agents. Options:

- **Option A (Simple)**: Each VM's daemon connects to Slack independently. Agent names include a VM prefix (e.g., `@bot jeff/console`, `@bot andy/reviewer`).
- **Option B (K8s Router)**: A central dispatcher runs on K8s, routes to VMs via webhook. More complex but cleaner UX.
- **Option C (Multiple Slack Bot Users)**: Each named agent is a separate Slack bot user. Cleanest UX (`@jeff-console`, `@andy-reviewer`) but requires more Slack app management.

**Recommendation**: Start with **Option A** for simplicity. Each VM runs its own Socket Mode connection and agents are addressed as `@holos-agents jeff/console fix the bug`. Migrate to Option C later if the UX warrants it.

#### 4. Session & Thread Management

- Map Slack thread IDs to Claude Agent SDK session IDs
- Replies in a thread resume the same session (context preserved)
- New top-level messages start fresh sessions
- Store session mappings in SQLite on each VM

## Implementation Plan

### Phase 1: Slack App + Single-Agent Bot

1. Create Slack App with required scopes and Socket Mode
2. Build minimal agent daemon using Claude Agent SDK + Slack Bolt (Python) or `@slack/bolt` (TypeScript)
3. Single named agent per VM, responds to `@bot <prompt>`
4. Deploy on Jeff's VM as proof of concept

### Phase 2: Multi-Agent Routing

5. Add agent name parsing (`@bot agent-name <prompt>`)
6. Agent configuration via YAML/JSON file per VM
7. Session persistence (SQLite) for thread-based context
8. Concurrent request handling

### Phase 3: Multi-VM Multiplayer

9. Second VM (Andy's) with its own agent daemon
10. Agent discovery/listing command (`@bot list-agents`)
11. Cross-VM agent awareness (optional: shared registry)

### Phase 4: Production Hardening

12. Structured logging and error reporting to Slack
13. Rate limiting and queue management
14. Agent health checks and auto-restart (systemd unit)
15. Sandbox security review (network, filesystem, credential access)

## Dependencies

- **Python**: `claude-agent-sdk`, `slack-bolt` (or TypeScript equivalents: `@anthropic-ai/claude-agent-sdk`, `@slack/bolt`)
- **Slack**: Paid workspace with ability to create apps
- **Claude**: `ANTHROPIC_API_KEY` (or Bedrock/Vertex credentials)
- **VM**: Debian 13 with Claude Code installed, `gh` CLI configured
- **Optional**: K8s ingress for webhook-based routing (Phase 3+)

## Security Considerations

- Each VM's agent runs in its existing sandbox with scoped filesystem access
- Slack tokens stored as environment variables or systemd credentials
- Anthropic API key per-VM (existing)
- Consider restricting which Slack users/channels can invoke agents
- Claude Agent SDK `permission_mode` and `allowed_tools` limit agent capabilities per-agent
- Audit logging via SDK hooks (PostToolUse) for all file modifications and bash commands

## Open Questions

1. **Slack App topology**: One shared app or one app per VM?
2. **Agent naming convention**: `vm/agent` vs flat namespace with unique names?
3. **Cost management**: Rate limiting per-user or per-channel?
4. **Git workflow**: Should agents auto-create branches and PRs, or wait for explicit instruction?
5. **Notification preferences**: Should agents post progress updates or only final results?

## References

- [Claude Agent SDK Overview](https://platform.claude.com/docs/en/agent-sdk/overview)
- [Claude Agent SDK Python](https://github.com/anthropics/claude-agent-sdk-python)
- [mpociot/claude-code-slack-bot](https://github.com/mpociot/claude-code-slack-bot)
- [sleepless-agent](https://github.com/context-machine-lab/sleepless-agent)
- [Slack Socket Mode docs](https://docs.slack.dev/apis/events-api/using-socket-mode/)
- [Slack Bolt for Python](https://slack.dev/bolt-python/)
- [OpenClaw](https://openclaw.ai/)
- [Building an agentic Slackbot with Claude Code](https://medium.com/@dotdc/building-an-agentic-slackbot-with-claude-code-eba0e472d8f4)
- [How Coding Agents Work in Slack](https://slack.com/blog/developers/coding-agents-in-slack)

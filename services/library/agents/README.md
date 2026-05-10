# agents/

Prompts for AI agents that operate Kura through its MCP tool surface.

This is **layer 3** in Kura's documentation stack:

1. **Per-tool MCP descriptions** (`internal/server/mcp/tool_*.md`) —
   shipped with the binary and served to any MCP client. What each
   tool does, parameter shape, return shape.
2. **Engineer-facing reference** ([../docs/](../docs/)) — concepts,
   lifecycle, on-disk formats. For people reading the repo.
3. **Agent orchestration prompts** (this directory) — how to chain
   the tools, when to escalate, how to disambiguate, how to talk to
   the user. For agents driving Kura on a user's behalf.

## Files

- [use-kura.md](use-kura.md) — unified guide: concepts, operating
  rules, the five recurring workflows (triage / adopt / add /
  stage / refresh), duplicate-slot ranking, common failures.

## How to use

### Generic agent (Cursor, Zed, Claude Desktop, ChatGPT, custom)

Paste the body of [use-kura.md](use-kura.md) into the agent's system
prompt. The YAML frontmatter at the top is harmless if left in place,
or strip it with `awk '/^---$/{n++; next} n>=2' use-kura.md`.

The agent also needs a working MCP connection to a `kura serve
--mcp-stdio` or `--mcp-http` instance — see
[../docs/mcp.md](../docs/mcp.md) and
[../docs/deployment.md](../docs/deployment.md).

### Claude Code

Install as a skill:

```sh
mkdir -p ~/.claude/skills
ln -s "$(pwd)/agents/use-kura.md" ~/.claude/skills/use-kura.md
```

Or copy the file in if you'd rather not symlink. The frontmatter's
`description` field tells Claude Code when to auto-trigger the skill;
adjust it to match how you phrase Kura tasks.

For project-scoped install (only active inside this repo), use
`.claude/skills/` instead of `~/.claude/skills/`.

# Future Work

Improvements to make blamebot a more complete provenance tool for AI-authored code.

## Shadow branch storage

**Status**: Exploratory

Currently, JSONL logs are committed directly into the repo under `.blamebot/log/`. This is simple and portable — anyone who clones gets the full history — but it pollutes commit diffs, inflates repo size over time, and creates merge surface area even with `merge=union`.

**Proposal**: Store JSONL logs on a dedicated `blamebot/v1` ref instead of in the working tree.

- `hook.py` writes to a staging area (`.git/blamebot/pending/`)
- A post-commit or pre-push hook commits pending records to the shadow branch via `git hash-object` / `git update-ref` (no checkout needed)
- `git blamebot` queries the shadow branch directly (read tree objects, or check out into a temp dir for SQLite indexing)
- The shadow branch is pushed/pulled alongside normal branches

**DX impact**: Slight. Developers no longer see `.blamebot/` in `git status` or diffs. The tradeoff is that cloning alone won't surface the data — you'd need to fetch the ref. A `git blamebot init --fetch` could handle that. Net positive for teams who find the committed logs noisy.

**Risk**: More moving parts. The current approach "just works" because JSONL files are regular committed files. Shadow branches need careful handling around shallow clones, CI environments, and force-pushes. Worth prototyping before committing to.

## Multi-agent support

**Status**: High value, moderate effort

The hooks currently assume Claude Code's specific payload structure (`session_id`, `transcript_path`, `tool_input.file_path`, `structuredPatch`, etc.). Supporting Codex, Gemini CLI, OpenCode, or Cursor would require:

**Phase 1 — Refactor hook layer into adapter pattern**:

- Define a common internal event schema: `{file, lines, old_content, new_content, prompt, session_id, agent}`
- Write an adapter per agent that normalizes its native hook payload into the common schema
- The core logic (JSONL writing, content hashing, change summarization) stays unchanged

**Phase 2 — Abstract transcript reading**:

- The `--fill-reasons` pipeline currently walks Claude's JSONL transcript format (message → content blocks → tool_use/text) and sends context to Haiku
- Each agent has a different transcript format (or none at all)
- Define a `TranscriptReader` interface: `get_reasoning_before(tool_use_id) -> str`
- Implement per-agent readers

**Phase 3 — Agent detection and setup**:

- `setup.sh` should detect which agents are installed and offer to hook into each
- Hook installation paths differ per agent (`.claude/settings.json`, `.gemini/settings.json`, `.cursor/hooks.json`)

This is worth doing even if we only add one more agent. The refactoring itself will make the Claude Code path more robust by separating payload parsing from business logic.

Note: The OpenAI Codex team are working on adding hooks: <https://github.com/openai/codex/issues/2109#issuecomment-3946505571>

## ~~Rewrite in Go~~

**Status**: Done

Rewritten as a single Go binary. Hooks, CLI, and setup are all handled by `git-blamebot` subcommands. JSONL and SQLite formats are backwards-compatible with existing data.

## VS Code extension

**Status**: Should be a separate repo

A VS Code extension that shows blamebot annotations inline, similar to GitLens for `git blame`.

**Core features**:

- Gutter annotations showing the prompt/reason for AI-edited lines
- Hover card with full detail: prompt, reason, change summary, timestamp, author
- Click-through to the full reasoning trace
- "AI-edited" highlighting (lines with blamebot records vs. human-authored lines)
- CodeLens above functions/blocks showing the most recent AI prompt that touched them

**Implementation**:

- Call `git blamebot -L {line} {file} --json`
- Cache results per file, invalidate on save
- Use VS Code's `DecorationProvider` API for gutter icons
- Use `HoverProvider` for detail cards

**Prerequisites from the CLI side**:

- Ensure the CLI is fast enough for per-line queries (current SQLite index should handle this)

Should live in a separate repo (`blamebot-vscode` or similar) to keep release cycles independent.

## Other improvements

### Git blame integration mode

`git blamebot --blame src/file.ts` could output interleaved git-blame + blamebot data for every line, showing both "who/when" and "why" in a single view. `-v` mode already does git blame cross-referencing per record, but a dedicated `--blame` mode would show every line of a file with both provenance sources side by side.

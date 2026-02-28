# Future Work

Improvements to make blamebot a more complete provenance tool for AI-authored code.

## Shadow branch storage

**Status**: Done

Provenance data is now stored on a dedicated `blamebot-provenance` branch. Manifests are written via `git hash-object` / `git update-ref` at commit time (no checkout needed). The branch is pushed/pulled alongside normal branches via the `pre-push` hook. `git-blamebot enable` auto-fetches the branch from origin if it exists.

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

Note: blamebot now uses a similar checkpoint approach to [Git AI](https://usegitai.com/docs/cli/how-git-ai-works) — snapshotting files before and after every edit. This makes multi-agent integration easier since any agent with pre/post-edit hooks can feed the same attribution pipeline.

Cursor hooks: <https://cursor.com/docs/agent/hooks>
Gemini CLI hooks: <https://geminicli.com/docs/hooks/>
OpenCode plugins and events: <https://opencode.ai/docs/plugins/#create-a-plugin>
Note: The OpenAI Codex team are working on adding hooks: <https://github.com/openai/codex/issues/2109#issuecomment-3946505571> and Git AI are using the Codex alpha hooks: <https://github.com/git-ai-project/git-ai/pull/504>

## Other improvements

### Git blame integration mode

`git blamebot --blame src/file.ts` could output interleaved git-blame + blamebot data for every line, showing both "who/when" and "why" in a single view. `-v` mode already does git blame cross-referencing per record, but a dedicated `--blame` mode would show every line of a file with both provenance sources side by side.

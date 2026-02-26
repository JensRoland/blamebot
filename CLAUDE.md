# CLAUDE.md

## Project overview

**blamebot** is a provenance tracking tool for AI-authored code. It captures the *why* behind every AI edit — the user's prompt, a generated reason, and a compact diff summary — and stores them as JSONL records that travel with the repo.

## Architecture

Two Claude Code hooks capture context, and a CLI queries it:

1. **`capture_prompt.py`** — `UserPromptSubmit` hook. Strips IDE metadata from the prompt, stashes it plus session info to `.git/blamebot/current_prompt.json`.
2. **`hook.py`** — `PostToolUse` hook (fires on Edit/Write/MultiEdit). Reads stashed prompt state, extracts file, lines, content hash, and change summary from the tool payload, appends a JSONL record to `.blamebot/log/<session>.jsonl`.
3. **`git-blamebot`** (CLI) — Python script installed to `~/.local/bin/`. Builds a SQLite index from JSONL files and supports querying by file, line, date, author, grep, and trace. Also handles `--fill-reasons` (sends session context to Haiku to generate one-sentence reasons at commit time), `--explain` (deep explanation via Sonnet), and `--trace` (shows full thinking/response blocks).

## Key files

| File                | Purpose                                                                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `git-blamebot`      | Main CLI (~800 lines Python). Querying, `--fill-reasons`, `--explain`, `--trace`, `--stats`, index management.      |
| `hook.py`           | PostToolUse hook. Extracts edits from Claude Code payloads, writes JSONL records.                                   |
| `capture_prompt.py` | UserPromptSubmit hook. Captures and cleans the user prompt, stashes session state.                                  |
| `setup.sh`          | Installer. `install` puts CLI + hooks globally; `init` sets up a repo with `.blamebot/` dir and pre-commit hook.    |

## Data layout

- **`.blamebot/log/*.jsonl`** — committed, travels with the repo. One file per prompt session.
- **`.blamebot/traces/`** — committed. Extracted thinking/response blocks for portability.
- **`.git/blamebot/`** — local only. SQLite index (`index.db`), ephemeral state (`current_prompt.json`), debug logs.

## Language and dependencies

- Python 3 (no third-party packages for hooks/CLI)
- Bash (setup.sh)
- SQLite (query index, built from JSONL at runtime)
- Anthropic API (called by `--fill-reasons` and `--explain` via subprocess to `claude` CLI)

## Common commands

```bash
sh setup.sh install          # global install (CLI + hooks + Claude Code config)
sh setup.sh init             # per-repo init (creates .blamebot/, installs pre-commit hook)
git blamebot <file>          # query reasons for a file
git blamebot --fill-reasons  # generate reasons for staged edits (runs automatically via pre-commit)
git blamebot --stats         # summary statistics
```

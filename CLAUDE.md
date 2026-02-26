# CLAUDE.md

## Project overview

**blamebot** is a provenance tracking tool for AI-authored code. It captures the *why* behind every AI edit — the user's prompt, a generated reason, and a compact diff summary — and stores them as JSONL records that travel with the repo.

## Architecture

A single Go binary (`git-blamebot`) handles everything: hooks, CLI queries, and setup.

1. **`git-blamebot hook prompt-submit`** — `UserPromptSubmit` hook. Strips IDE metadata from the prompt, stashes it plus session info to `.git/blamebot/current_prompt.json`.
2. **`git-blamebot hook post-tool-use`** — `PostToolUse` hook (fires on Edit/Write/MultiEdit). Reads stashed prompt state, extracts file, lines, content hash, and change summary from the tool payload, appends a JSONL record to `.blamebot/log/<session>.jsonl`.
3. **`git-blamebot`** (CLI) — Builds a SQLite index from JSONL files and supports querying by file, line, date, author, grep, and trace. Also handles `--fill-reasons` (sends session context to Haiku to generate one-sentence reasons at commit time), `--explain` (deep explanation via Sonnet), and `--trace` (shows full thinking/response blocks).

## Key files

```
main.go                          Entry point, subcommand dispatch
cmd/
  root.go                        CLI flag parsing, query dispatch
  hook.go                        hook subcommand wiring
  query.go                       file/grep/since/author queries
  trace.go                       --trace command
  explain.go                     --explain command (Sonnet)
  fill.go                        --fill-reasons command (Haiku)
  stats.go                       --stats command
  log.go                         --log, --dump-payload
  enable.go                      enable subcommand (repo init + global hooks)
  disable.go                     disable subcommand (remove tracking)
internal/
  project/project.go             Project root detection, path helpers
  record/record.go               JSONL record struct, content hashing
  hook/prompt.go                 UserPromptSubmit handler
  hook/tooluse.go                PostToolUse handler
  index/index.go                 SQLite index management
  transcript/transcript.go       Transcript reader (trace context, session prompts)
  llm/llm.go                    Claude CLI subprocess caller
  format/                        ANSI colors, reason formatting, diff/box rendering
  git/git.go                    Git helpers (blame, author, staging)
  debug/debug.go                Debug logging
```

## Data layout

- **`.blamebot/log/*.jsonl`** — committed, travels with the repo. One file per prompt session.
- **`.blamebot/traces/`** — committed. Extracted thinking/response blocks for portability.
- **`.git/blamebot/`** — local only. SQLite index (`index.db`), ephemeral state (`current_prompt.json`), debug logs.

## Language and dependencies

- Go (single static binary, no CGO)
- SQLite via `modernc.org/sqlite` (query index, built from JSONL at runtime)
- Anthropic API (called by `--fill-reasons` and `--explain` via subprocess to `claude` CLI)

## Common commands

```bash
make build                        # build binary to dist/git-blamebot
make install                      # build + install to ~/.local/bin/
make test                         # run all tests
git-blamebot enable --global      # global install (Claude Code hooks config)
git-blamebot enable               # per-repo init (creates .blamebot/, pre-commit hook)
git-blamebot disable              # remove tracking from repo
git blamebot <file>               # query reasons for a file
git blamebot --fill-reasons       # generate reasons for staged edits
git blamebot --stats              # summary statistics
```

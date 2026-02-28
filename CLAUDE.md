# CLAUDE.md

## Project overview

**blamebot** is a provenance tracking tool for AI-authored code. It captures the *why* behind every AI edit — the user's prompt, a generated reason, and a compact diff summary — and stores them on a dedicated `blamebot-provenance` branch that travels with the repo.

## Architecture

A single Go binary (`git-blamebot`) handles everything: hooks, CLI queries, and setup.

### Hook pipeline

1. **`git-blamebot hook pre-tool-use`** — `PreToolUse` hook (fires before Edit/Write/MultiEdit). Snapshots the file content before the edit as a checkpoint blob.
2. **`git-blamebot hook prompt-submit`** — `UserPromptSubmit` hook. Strips IDE metadata from the prompt, stashes it plus session info to `.git/blamebot/current_prompt.json`.
3. **`git-blamebot hook post-tool-use`** — `PostToolUse` hook (fires after Edit/Write/MultiEdit). Reads stashed prompt state, extracts file, lines, content hash, and change summary from the tool payload, writes a pending edit to `.git/blamebot/pending/`. Also snapshots the file after the edit as a post-edit checkpoint.

### Commit-time processing

4. **`git-blamebot hook commit-msg`** — `commit-msg` git hook. Bundles pending edits into a manifest, fills reasons via Haiku, computes checkpoint-based attribution (LCS diffing of pre/post snapshots), writes the manifest to the `blamebot-provenance` branch, and appends a `Blamebot-Ref` trailer.
5. **`git-blamebot hook post-commit`** — Backfills the commit SHA into the manifest.
6. **`git-blamebot hook pre-push`** — Pushes the `blamebot-provenance` branch alongside normal branches.

### Query layer

7. **`git-blamebot`** (CLI) — Builds a SQLite index from manifests on the provenance branch plus pending edits, and supports querying by file, line, date, author, grep, and trace. Uses checkpoint-based attribution for accurate line tracking, with fallback to forward simulation + content hash for older manifests. Also handles `--explain` (deep explanation via Sonnet) and `--trace` (shows full thinking/response blocks).

## Key files

```
main.go                          Entry point, subcommand dispatch
cmd/
  root.go                        CLI flag parsing, query dispatch
  hook.go                        Hook subcommand wiring
  query.go                       File/grep/since/author queries, attribution resolution
  debug.go                       Debug subcommand (simulated edits for testing)
  trace.go                       --trace command
  explain.go                     --explain command (Sonnet)
  fill.go                        --fill-reasons command (Haiku)
  stats.go                       --stats command
  log.go                         --log, --dump-payload
  enable.go                      Enable subcommand (repo init + global hooks)
  disable.go                     Disable subcommand (remove tracking)
internal/
  project/project.go             Project root detection, path helpers
  record/record.go               Record struct, content hashing
  provenance/provenance.go       Manifest types, pending edits, branch operations
  checkpoint/checkpoint.go       Checkpoint storage (pre/post-edit snapshots, blob dedup)
  checkpoint/attribution.go      Attribution engine (LCS-based per-line tracking)
  hook/pretooluse.go             PreToolUse handler (pre-edit file snapshots)
  hook/tooluse.go                PostToolUse handler (edit recording + post-edit snapshots)
  hook/prompt.go                 UserPromptSubmit handler
  hook/commitmsg.go              commit-msg handler (manifest creation, attribution, reasons)
  index/index.go                 SQLite index management (built from manifests + pending)
  transcript/transcript.go       Transcript reader (trace context, session prompts)
  llm/llm.go                     Claude CLI subprocess caller
  linemap/linemap.go             Forward simulation (legacy fallback)
  lineset/lineset.go             Line set operations (ranges, contains, overlap)
  format/                        ANSI colors, reason formatting, diff/box rendering
  git/git.go                     Git helpers (blame, author, staging, show)
  debug/debug.go                 Debug logging
```

## Data layout

- **`blamebot-provenance` branch** — committed, travels with the repo. Manifests and traces.
  - `manifests/<id>.json` — one manifest per commit with AI edits
  - `traces/<session>.json` — extracted thinking/response blocks (portable)
- **`.git/blamebot/`** — local only. SQLite index, ephemeral state, checkpoints.
  - `index.db` — SQLite query cache (auto-rebuilt from manifests)
  - `pending/<id>.json` — uncommitted edits (cleared at commit time)
  - `checkpoints/` — pre/post-edit file snapshots (cleared at commit time)
  - `current_prompt.json` — ephemeral state between hooks
  - `logs/` — debug logs

## Language and dependencies

- Go (single static binary, no CGO)
- SQLite via `modernc.org/sqlite` (query index, built from manifests at runtime)
- Anthropic API (called by `--fill-reasons` and `--explain` via subprocess to `claude` CLI)

## Common commands

```bash
make build                        # build binary to dist/git-blamebot
make install                      # build + install to ~/.local/bin/
make test                         # run all tests
git-blamebot enable --global      # global install (Claude Code hooks config)
git-blamebot enable               # per-repo init (provenance branch + git hooks)
git-blamebot disable              # remove tracking from repo
git blamebot <file>               # query current AI edits for a file
git blamebot --include-history    # include superseded/overwritten edits
git blamebot --fill-reasons       # generate reasons for pending edits
git blamebot --stats              # summary statistics
```

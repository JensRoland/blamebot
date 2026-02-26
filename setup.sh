#!/usr/bin/env bash
set -euo pipefail

# blamebot
#
# Usage:
#   ./setup.sh install          Install CLI globally + hook scripts to ~/.blamebot/
#   ./setup.sh init             Initialize the current git repo for tracking
#   ./setup.sh install && ./setup.sh init   Full first-time setup

HOOKS_SRC="$(cd "$(dirname "$0")" && pwd)"
BLAMEBOT_HOME="$HOME/.blamebot"
HOOKS_DEST="$BLAMEBOT_HOME/hooks"
CLI_DEST="$HOME/.local/bin"

usage() {
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  install   Install CLI and hook scripts globally (~/.local/bin, ~/.blamebot/)"
    echo "  init      Initialize the current git repo for tracking"
    echo ""
    echo "First time:  $0 install && $0 init"
    echo "New repo:    $0 init"
    exit 1
}

cmd_install() {
    echo "Installing blamebot globally..."

    # 1. Install hook scripts to ~/.blamebot/hooks/
    mkdir -p "$HOOKS_DEST"
    cp "$HOOKS_SRC/capture_prompt.py" "$HOOKS_DEST/capture_prompt.py"
    cp "$HOOKS_SRC/hook.py" "$HOOKS_DEST/hook.py"
    chmod +x "$HOOKS_DEST/capture_prompt.py"
    chmod +x "$HOOKS_DEST/hook.py"
    echo "  ✓ Hook scripts installed to $HOOKS_DEST/"

    # 2. Install CLI to ~/.local/bin/
    mkdir -p "$CLI_DEST"
    cp "$HOOKS_SRC/git-blamebot" "$CLI_DEST/git-blamebot"
    chmod +x "$CLI_DEST/git-blamebot"
    echo "  ✓ CLI installed to $CLI_DEST/git-blamebot"

    # 3. Check PATH
    if ! echo "$PATH" | tr ':' '\n' | grep -qx "$CLI_DEST"; then
        echo ""
        echo "  ⚠  $CLI_DEST is not on your PATH."
        echo "  Add to your shell config:"
        echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    else
        echo "  ✓ git-blamebot is on PATH (try: git blamebot --stats)"
    fi

    # 4. Configure Claude Code hooks in ~/.claude/settings.json
    SETTINGS_FILE="$HOME/.claude/settings.json"
    mkdir -p "$HOME/.claude"

    python3 - "$SETTINGS_FILE" "$HOOKS_DEST" <<'PYEOF'
import json, sys

settings_path = sys.argv[1]
hooks_dest = sys.argv[2]

# Load existing settings or start fresh
try:
    with open(settings_path) as f:
        settings = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    settings = {}

hooks = settings.setdefault("hooks", {})

hook_cmd = f"python3 {hooks_dest}/hook.py"
capture_cmd = f"python3 {hooks_dest}/capture_prompt.py"

# PostToolUse — add/replace blamebot entry
post_tool = hooks.get("PostToolUse", [])
post_tool = [e for e in post_tool
             if not any(hooks_dest in h.get("command", "")
                        for h in e.get("hooks", []))]
post_tool.append({
    "matcher": "Edit|Write|MultiEdit",
    "hooks": [{"type": "command", "command": hook_cmd}],
})
hooks["PostToolUse"] = post_tool

# UserPromptSubmit — add/replace blamebot entry
user_prompt = hooks.get("UserPromptSubmit", [])
user_prompt = [e for e in user_prompt
               if not any(hooks_dest in h.get("command", "")
                          for h in e.get("hooks", []))]
user_prompt.append({
    "hooks": [{"type": "command", "command": capture_cmd}],
})
hooks["UserPromptSubmit"] = user_prompt

with open(settings_path, "w") as f:
    json.dump(settings, f, indent=2)
    f.write("\n")
PYEOF

    echo "  ✓ Claude Code hooks configured in $SETTINGS_FILE"
}

cmd_init() {
    PROJ_DIR="$(git rev-parse --show-toplevel 2>/dev/null)" || {
        echo "Error: not inside a git repository" >&2
        exit 1
    }

    echo "Initializing blamebot in $PROJ_DIR"

    # 1. Create committed directory for JSONL logs
    REASONS_LOG="$PROJ_DIR/.blamebot/log"
    mkdir -p "$REASONS_LOG"

    # Add .gitattributes for clean merging (union merge = append both sides)
    GITATTR="$PROJ_DIR/.blamebot/.gitattributes"
    if [ ! -f "$GITATTR" ]; then
        echo '*.jsonl merge=union' > "$GITATTR"
        echo "  ✓ Created .blamebot/ with merge=union strategy"
    else
        echo "  ✓ .blamebot/ already exists"
    fi

    # Add a README so the directory's purpose is clear to other devs
    README="$PROJ_DIR/.blamebot/README"
    if [ ! -f "$README" ]; then
        cat > "$README" <<'README_CONTENT'
This directory is maintained by blamebot.
It tracks the prompts and reasoning behind AI-authored code edits.
See: https://github.com/TODO/blamebot

JSONL files in log/ are append-only and merge cleanly across branches.
Do not edit these files manually.
README_CONTENT
    fi

    # 2. Create local cache directory (inside .git, not committed)
    CACHE_DIR="$PROJ_DIR/.git/blamebot"
    mkdir -p "$CACHE_DIR/logs"
    echo "  ✓ Local cache at .git/blamebot/"

    # 3. Install pre-commit hook for automatic reason filling
    HOOK_DIR="$PROJ_DIR/.git/hooks"
    PRE_COMMIT="$HOOK_DIR/pre-commit"
    FILL_MARKER="# blamebot: fill reasons"

    if [ -f "$PRE_COMMIT" ] && grep -q "$FILL_MARKER" "$PRE_COMMIT"; then
        echo "  ✓ Pre-commit hook already installed"
    else
        mkdir -p "$HOOK_DIR"
        if [ -f "$PRE_COMMIT" ]; then
            # Append to existing pre-commit hook
            cat >> "$PRE_COMMIT" <<'HOOK'

# blamebot: fill reasons
# Auto-generate reasons for AI edits using Claude Haiku
if git diff --cached --name-only -- '.blamebot/log/*.jsonl' | grep -q .; then
    git-blamebot --fill-reasons
fi
HOOK
            echo "  ✓ Appended to existing pre-commit hook"
        else
            cat > "$PRE_COMMIT" <<'HOOK'
#!/usr/bin/env bash
# blamebot: fill reasons
# Auto-generate reasons for AI edits using Claude Haiku
if git diff --cached --name-only -- '.blamebot/log/*.jsonl' | grep -q .; then
    git-blamebot --fill-reasons
fi
HOOK
            chmod +x "$PRE_COMMIT"
            echo "  ✓ Installed pre-commit hook"
        fi
    fi

    # 4. Check global install exists
    if ! command -v git-blamebot &>/dev/null; then
        echo ""
        echo "  ⚠  'git-blamebot' CLI not found on PATH."
        echo "  Run '$0 install' first."
    fi

    echo ""
    echo "  Ready! Commit .blamebot/ to share reasoning with your team:"
    echo "    git add .blamebot && git commit -m 'Initialize blamebot tracking'"
}

# ── Command dispatch ─────────────────────────────────────────────────

case "${1:-}" in
    install) cmd_install ;;
    init)    cmd_init ;;
    "")      usage ;;
    *)       echo "Unknown command: $1" >&2; usage ;;
esac

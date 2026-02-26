#!/usr/bin/env python3
"""
blamebot: UserPromptSubmit hook

Captures the user's prompt and prepares a session file that
the PostToolUse hook (hook.py) will append edit records to.

Real Claude Code payload structure (from debug logs):
  - prompt: str (may contain <ide_opened_file> wrappers)
  - session_id: str (UUID)
  - transcript_path: str (full path to session JSONL)
  - cwd: str
  - permission_mode: str
  - hook_event_name: "UserPromptSubmit"
"""

import json
import sys
import os
import re
import time
import random
import string
import subprocess
from pathlib import Path


def get_project_dir() -> Path:
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR", "")
    if not project_dir:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True, text=True
        )
        project_dir = result.stdout.strip()
    return Path(project_dir)


def log_debug(cache_dir: Path, message: str, data=None):
    log_file = cache_dir / "logs" / "capture_prompt.log"
    log_file.parent.mkdir(parents=True, exist_ok=True)
    timestamp = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(log_file, "a") as f:
        f.write(f"\n{'='*60}\n")
        f.write(f"[{timestamp}] {message}\n")
        if data is not None:
            f.write(json.dumps(data, indent=2, default=str))
            f.write("\n")


def get_git_author() -> str:
    try:
        result = subprocess.run(
            ["git", "config", "user.name"],
            capture_output=True, text=True
        )
        return result.stdout.strip() or "unknown"
    except Exception:
        return "unknown"


def clean_prompt(raw_prompt: str) -> str:
    """Strip IDE metadata tags from the prompt, keeping only the user's actual request."""
    cleaned = re.sub(r'<ide_opened_file>.*?</ide_opened_file>\s*', '', raw_prompt, flags=re.DOTALL)
    cleaned = re.sub(r'<ide_\w+>.*?</ide_\w+>\s*', '', cleaned, flags=re.DOTALL)
    return cleaned.strip()


def main():
    project_dir = get_project_dir()

    # Not initialized — exit silently
    if not (project_dir / ".blamebot").is_dir():
        return

    cache_dir = project_dir / ".git" / "blamebot"
    log_dir = project_dir / ".blamebot" / "log"

    try:
        raw = sys.stdin.read()
        log_debug(cache_dir, "Raw stdin received", {
            "raw_length": len(raw),
            "raw_preview": raw[:2000],
        })
        data = json.loads(raw) if raw.strip() else {}
    except Exception as e:
        log_debug(cache_dir, f"Failed to read/parse stdin: {e}")
        data = {}

    log_debug(cache_dir, "Parsed payload", {
        "keys": list(data.keys()) if isinstance(data, dict) else f"type={type(data).__name__}",
        "full_payload": data,
    })

    # Extract and clean prompt
    raw_prompt = data.get("prompt", "")
    prompt = clean_prompt(raw_prompt)

    # Session ID and transcript path are in the payload (not env vars)
    session_id = data.get("session_id", "")
    transcript_path = data.get("transcript_path", "")

    # Generate session file name
    ts = time.strftime("%Y%m%dT%H%M%SZ")
    rand = ''.join(random.choices(string.ascii_lowercase + string.digits, k=6))
    session_filename = f"{ts}-{rand}.jsonl"

    state = {
        "prompt": prompt,
        "prompt_raw": raw_prompt,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "session_file": session_filename,
        "author": get_git_author(),
        "session_id": session_id,
        "transcript_path": transcript_path,
    }

    cache_dir.mkdir(parents=True, exist_ok=True)
    log_dir.mkdir(parents=True, exist_ok=True)

    state_file = cache_dir / "current_prompt.json"
    with open(state_file, "w") as f:
        json.dump(state, f, indent=2)

    log_debug(cache_dir, "Stashed prompt state", state)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass  # Hook must never block prompt submission

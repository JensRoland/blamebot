#!/usr/bin/env python3
"""
blamebot: PostToolUse hook for Edit/Write/MultiEdit

Real Claude Code Edit payload structure (from debug logs):

  Top level:
    session_id, transcript_path, cwd, permission_mode,
    hook_event_name, tool_name, tool_input, tool_response, tool_use_id

  tool_input (Edit):
    file_path (absolute!), old_string, new_string, replace_all

  tool_response (Edit):
    filePath, oldString, newString, originalFile,
    structuredPatch: [{ oldStart, oldLines, newStart, newLines, lines }],
    userModified, replaceAll

  Key findings:
    - NO description/reason field in tool_input
    - Line numbers are in tool_response.structuredPatch, not tool_input
    - session_id and transcript_path are in the top-level payload
    - tool_use_id can be used as offset pointer into the transcript
    - file_path is absolute, needs relativizing
"""

import json
import sys
import os
import time
import hashlib
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
    log_file = cache_dir / "logs" / "hook.log"
    log_file.parent.mkdir(parents=True, exist_ok=True)
    timestamp = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(log_file, "a") as f:
        f.write(f"\n{'='*60}\n")
        f.write(f"[{timestamp}] {message}\n")
        if data is not None:
            f.write(json.dumps(data, indent=2, default=str))
            f.write("\n")


def content_hash(text: str) -> str:
    if not text:
        return ""
    normalized = " ".join(text.split())
    return hashlib.sha256(normalized.encode()).hexdigest()[:16]


def relativize_path(abs_path: str, project_dir: str) -> str:
    """Convert absolute file path to project-relative."""
    try:
        return str(Path(abs_path).relative_to(project_dir))
    except ValueError:
        return abs_path


def compact_change_summary(old_str: str, new_str: str) -> str:
    """
    Generate a compact human-readable summary of what changed.
    Since Claude Code's Edit tool has no description field, this is
    our best approximation of "what happened" from the diff itself.

    When old and new share a long common prefix, we skip past it
    to show where they actually diverge — otherwise the truncated
    previews can look identical and mislead the reason generator.
    """
    MAX_LEN = 200

    if not old_str and new_str:
        # New file or insertion
        preview = new_str[:MAX_LEN].replace('\n', ' ')
        return f"added: {preview}"

    if old_str and not new_str:
        preview = old_str[:MAX_LEN].replace('\n', ' ')
        return f"removed: {preview}"

    # Normalize to single-line for display
    old_flat = old_str.replace('\n', ' ').strip()
    new_flat = new_str.replace('\n', ' ').strip()

    # Find common prefix length
    common = 0
    for a, b in zip(old_flat, new_flat):
        if a == b:
            common += 1
        else:
            break

    # If long common prefix, skip past it to show where they diverge
    if common > 20:
        offset = max(common - 10, 0)
        old_display = "…" + old_flat[offset:]
        new_display = "…" + new_flat[offset:]
    else:
        old_display = old_flat
        new_display = new_flat

    # Truncate if still too long
    if len(old_display) > MAX_LEN:
        old_display = old_display[:MAX_LEN] + "…"
    if len(new_display) > MAX_LEN:
        new_display = new_display[:MAX_LEN] + "…"

    return f"{old_display} → {new_display}"


def extract_line_numbers(data: dict) -> tuple[int | None, int | None]:
    """
    Extract line numbers from tool_response.structuredPatch.
    Returns (start_line, end_line) of the new content.
    """
    tool_response = data.get("tool_response", {})
    patches = tool_response.get("structuredPatch", [])

    if not patches:
        return None, None

    # Use the first patch (Edit usually has one)
    patch = patches[0]
    start = patch.get("newStart")
    lines = patch.get("newLines", 0)

    if start is not None:
        end = start + max(lines - 1, 0)
        return start, end

    return None, None


def extract_edits(data: dict, project_dir: str) -> list[dict]:
    """Extract file edit details from the real Claude Code payload."""
    tool_name = data.get("tool_name", "")
    tool_input = data.get("tool_input", {})
    tool_response = data.get("tool_response", {})

    edits = []

    # file_path is absolute in Claude Code — relativize it
    file_path = relativize_path(
        tool_input.get("file_path", "") or tool_input.get("path", ""),
        project_dir
    )

    if tool_name == "Edit":
        old_str = tool_input.get("old_string", "")
        new_str = tool_input.get("new_string", "")
        line_start, line_end = extract_line_numbers(data)

        edits.append({
            "file": file_path,
            "line_start": line_start,
            "line_end": line_end,
            "content_hash": content_hash(new_str),
            "change": compact_change_summary(old_str, new_str),
        })

    elif tool_name == "Write":
        content = (
            tool_input.get("content")
            or tool_input.get("file_text")
            or ""
        )
        n_lines = content.count("\n") + 1 if content else None
        edits.append({
            "file": file_path,
            "line_start": 1,
            "line_end": n_lines,
            "content_hash": content_hash(content[:500]),
            "change": f"created file ({n_lines} lines)" if n_lines else "created file",
        })

    elif tool_name == "MultiEdit":
        # MultiEdit may have multiple structuredPatch entries
        sub_edits = tool_input.get("edits") or tool_input.get("changes") or []
        patches = tool_response.get("structuredPatch", [])

        for i, edit in enumerate(sub_edits):
            new_str = edit.get("new_string", "")
            old_str = edit.get("old_string", "")

            start, end = None, None
            if i < len(patches):
                p = patches[i]
                start = p.get("newStart")
                if start is not None:
                    end = start + max(p.get("newLines", 1) - 1, 0)

            edits.append({
                "file": relativize_path(
                    edit.get("file_path", "") or file_path,
                    project_dir
                ),
                "line_start": start,
                "line_end": end,
                "content_hash": content_hash(new_str),
                "change": compact_change_summary(old_str, new_str),
            })

    else:
        edits.append({
            "file": file_path or f"unknown:{tool_name}",
            "line_start": None,
            "line_end": None,
            "content_hash": "",
            "change": f"unknown tool: {tool_name}",
        })

    return edits


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
            "raw_preview": raw[:3000],
        })
        data = json.loads(raw) if raw.strip() else {}
    except Exception as e:
        log_debug(cache_dir, f"Failed to read/parse stdin: {e}")
        return

    log_debug(cache_dir, "Parsed payload", {
        "top_level_keys": list(data.keys()) if isinstance(data, dict) else None,
        "tool_name": data.get("tool_name"),
        "tool_input_keys": list(data.get("tool_input", {}).keys()),
        "tool_response_keys": list(data.get("tool_response", {}).keys()) if isinstance(data.get("tool_response"), dict) else type(data.get("tool_response")).__name__,
    })

    # Load stashed prompt state
    state_file = cache_dir / "current_prompt.json"
    prompt_state = {}
    if state_file.exists():
        try:
            with open(state_file) as f:
                prompt_state = json.load(f)
        except Exception:
            pass
    log_debug(cache_dir, "Loaded prompt state", prompt_state)

    # Determine session file
    session_filename = prompt_state.get("session_file")
    if not session_filename:
        import random, string
        ts = time.strftime("%Y%m%dT%H%M%SZ")
        rand = ''.join(random.choices(string.ascii_lowercase + string.digits, k=6))
        session_filename = f"{ts}-{rand}-orphan.jsonl"
        log_debug(cache_dir, f"No session file in prompt state, fallback: {session_filename}")

    session_path = log_dir / session_filename

    # Extract edit records
    edits = extract_edits(data, str(project_dir))
    log_debug(cache_dir, f"Extracted {len(edits)} edit(s)", edits)

    # Trace reference for later LLM-based reason filling
    transcript_path = (
        data.get("transcript_path")
        or prompt_state.get("transcript_path")
        or ""
    )
    tool_use_id = data.get("tool_use_id", "")
    trace_ref = transcript_path
    if tool_use_id:
        trace_ref = f"{trace_ref}#{tool_use_id}"

    # Session ID from payload (preferred) or prompt state
    session_id = (
        data.get("session_id")
        or prompt_state.get("session_id")
        or ""
    )

    now = time.strftime("%Y-%m-%dT%H:%M:%SZ")
    tool_name = data.get("tool_name", "")
    author = prompt_state.get("author", "unknown")

    log_dir.mkdir(parents=True, exist_ok=True)

    records_written = 0
    try:
        with open(session_path, "a") as f:
            for edit in edits:
                record = {
                    "ts": now,
                    "file": edit["file"],
                    "lines": [edit["line_start"], edit["line_end"]],
                    "content_hash": edit["content_hash"],
                    "prompt": prompt_state.get("prompt", ""),
                    "reason": "",
                    "change": edit["change"],
                    "tool": tool_name,
                    "author": author,
                    "session": session_id,
                    "trace": trace_ref,
                }
                f.write(json.dumps(record, separators=(",", ":")) + "\n")
                records_written += 1

        log_debug(cache_dir, f"Wrote {records_written} record(s) to {session_path}")

    except Exception as e:
        log_debug(cache_dir, f"Failed to write JSONL: {e}")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass  # PostToolUse hook must never cause the tool to fail

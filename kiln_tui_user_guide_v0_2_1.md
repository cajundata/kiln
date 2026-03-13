# Kiln TUI User Guide (v0.2.1)

## Quick-Start Cheat Sheet

```bash
# 1. Build kiln (if not already built)
make bin/kiln

# 2. Launch the TUI in one terminal
kiln tui

# 3. Run tasks in another terminal
make all -j4
```

**Essential keybindings:**

| Key | What it does |
|-----|-------------|
| `j` / `k` | Move up/down in task list |
| `Enter` | Drill into task details (errors, verify gates, duration) |
| `l` | View captured log events from Claude's execution |
| `Escape` | Go back to main view |
| `r` | Force immediate refresh |
| `q` | Quit |

**What the colors mean:** green = complete, yellow = running, red = failed, magenta = blocked, gray = pending, orange = not complete.

**Task failed?** Check the error class in the detail view (`Enter`), then see [Section 18](#18-recovery-workflow-retry-reset-resume) for the retry/reset/resume decision tree.

For the full guide, read on.

---

## Table of Contents

[[# Quick-Start Cheat Sheet]]
[[# Table of Contents]]
[[# 1. Overview]]
- [[# When to use the TUI]]
[[# 2. Prerequisites]]
[[# 3. The .kiln/ Directory Layout]]
- [[# How the TUI uses each file]]
[[# 4. Launching the TUI]]
[[# 5. Multi-Terminal Layout Tips]]
- [[# tmux (recommended)]]
- [[# Terminal emulator tabs/splits]]
- [[# Layout suggestion]]
[[# 6. Walkthrough: New Project from Scratch]]
- [[# Step 1: Initialize the project]]
- [[# Step 2: Write your PRD]]
- [[# Step 3: Generate the task graph]]
- [[# Step 4: Launch the TUI in one terminal]]
- [[# Step 5: Start execution in a second terminal]]
- [[# Step 6: Watch tasks progress in the TUI]]
- [[# Step 7: Investigate failures]]
- [[# Step 8: After execution completes]]
[[# 7. Walkthrough: Pre-Existing Project]]
- [[# Step 1: Check current state]]
- [[# Step 2: Understand what's left]]
- [[# Step 3: Resume execution]]
- [[# Step 4: Use grouping for large projects]]
[[# 8. The Main View (Task Graph)]]
- [[# Summary Bar (top)]]
- [[# Task Table (middle)]]
- [[# Sort Order]]
- [[# Keybinding Footer (bottom)]]
[[# 9. The Summary Panel]]
[[# 10. The Task Detail View]]
- [[# What you'll see]]
- [[# Navigation]]
[[# 11. The Log Events View]]
- [[# Scrolling]]
- [[# Live updates]]
[[# 12. The Help Overlay]]
[[# 13. Grouping Tasks]]
- [[# Group by Phase]]
- [[# Group by Milestone]]
- [[# Notes on grouping]]
[[# 14. Understanding Task Status]]
- [[# Priority order (highest to lowest)]]
- [[# Status color reference]]
[[# 15. Error Classes and Retryability]]
- [[# Error class reference]]
- [[# How retryability works]]
[[# 16. Exit Codes]]
- [[# Reading exit codes in the TUI]]
[[# 17. Blocked Tasks and Dependencies]]
- [[# In the main view]]
- [[# In the detail view]]
- [[# What to do about blocked tasks]]
[[# 18. Recovery Workflow: retry, reset, resume]]
- [[# Decision tree]]
- [[# Command reference]]
- [[# Watching recovery in the TUI]]
[[# 19. Polling and Live Refresh]]
- [[# What gets refreshed on each tick]]
- [[# Configuring the refresh interval]]
- [[# Force refresh]]
- [[# Performance considerations]]
[[# 20. Models and Profiles]]
- [[# Model selection hierarchy]]
- [[# Profiles: speed vs reliable]]
- [[# What the TUI shows]]
[[# 21. Anatomy of a tasks.yaml Entry]]
- [[# Minimal entry]]
- [[# Full entry with all optional fields]]
- [[# Real-world example from this project]]
- [[# How changes to tasks.yaml appear in the TUI]]
[[# 22. Interpreting the Attempts Column]]
- [[# Reading attempts + status together]]
- [[# What drives the attempt count]]
- [[# Examples from a real project]]
- [[# When to worry]]
[[# 23. Keyboard Reference]]
- [[# Main View]]
- [[# Detail View]]
- [[# Log Events View]]
- [[# Help Overlay]]
[[# 24. CLI Flags Reference]]
- [[# Examples]]
[[# 25. Common Workflows]]
- [[# Monitor a full build]]
- [[# Investigate a failure]]
- [[# Check progress after stepping away]]
- [[# Run a specific dev-phase]]
- [[# Batch retry all transient failures]]
[[# 26. Companion Commands: status and report]]
- [[# `kiln status` — One-shot task overview]]
- [[# `kiln report` — Post-run analysis]]
[[# 27. Troubleshooting]]
- [[# TUI shows no tasks]]
- [[# All tasks show as "pending" even though some completed]]
- [[# Colors look wrong or missing]]
- [[# TUI exits immediately]]
- [[# Refresh feels sluggish]]
- [[# Task status doesn't match what I expect]]
- [[# A task shows "failed" but I fixed the code]]
- [[# Lock conflict error]]
- [[# Terminal is garbled after a crash]]
[[# 28. Glossary]]

---

## 1. Overview

`kiln tui` is an interactive terminal dashboard for monitoring and inspecting kiln task execution in real time. It replaces the need to repeatedly run `kiln status` or manually inspect log files and done markers — instead, you get a live, color-coded view of your entire task graph that auto-refreshes as tasks complete, fail, or start running.

The TUI is read-only. It does not trigger, retry, or skip tasks — those operations are handled by `make all`, `kiln retry`, and `kiln reset`. The TUI's job is to give you full situational awareness of what's happening across your task graph while Make drives execution in a separate terminal.

### When to use the TUI

- During `make all -j4` to watch tasks execute in parallel
- After a partial run to quickly identify which tasks failed and why
- To inspect detailed error messages, verify gate results, or browse captured log events
- To check dependency chains when tasks are blocked

---

## 2. Prerequisites

Before using `kiln tui`, ensure:

1. **Kiln is built.** Run `make bin/kiln` or `go build -o bin/kiln ./cmd/kiln` from the project root.
2. **A `tasks.yaml` exists.** Either generate one via `make plan` or write one manually at `.kiln/tasks.yaml`.
3. **Terminal supports 256 colors.** The TUI uses ANSI color codes for status indicators. Most modern terminals (iTerm2, Ghostty, Alacritty, Windows Terminal, Kitty) work out of the box.

The TUI gracefully degrades when optional files are missing — it will launch and display tasks even without `.kiln/state.json`, `.kiln/logs/`, or `.kiln/done/` directories.

---

## 3. The .kiln/ Directory Layout

Understanding the `.kiln/` directory helps you make sense of what the TUI is reading and displaying. Here is the full layout with annotations showing which files the TUI consumes:

```
.kiln/
├── tasks.yaml              # [TUI reads] Task graph: IDs, dependencies, prompts, metadata
├── state.json              # [TUI reads] Per-task execution state (status, attempts, errors, duration)
├── targets.mk              # Generated Make include file (not read by TUI)
├── config.yaml             # Optional profile/model config (not read by TUI directly)
├── done/                   # [TUI reads] Make idempotency markers
│   ├── auth-module.done
│   ├── database-schema.done
│   └── ...
├── logs/                   # [TUI reads] Per-task execution logs (one JSON file per task)
│   ├── auth-module.json
│   ├── database-schema.json
│   └── ...
├── locks/                  # Ephemeral lock files for concurrent exec safety (not read by TUI)
├── unify/                  # [TUI reads] Closure artifacts from kiln unify
│   ├── auth-module.md
│   └── ...
├── prompts/
│   ├── 00_extract_tasks.md # PRD parsing prompt template
│   └── tasks/              # Per-task prompt files
│       ├── auth-module.md
│       ├── database-schema.md
│       └── ...
├── decisions.log           # Append-only decision log (not read by TUI)
└── references/             # Reference materials (not read by TUI)
```

### How the TUI uses each file

| File | TUI Purpose |
|------|-------------|
| `tasks.yaml` | Loads all task definitions (ID, needs, kind, phase, milestone, lane) |
| `state.json` | Primary source of task status, attempt count, duration, model, errors |
| `done/<id>.done` | Fallback completeness check when `state.json` has no entry |
| `logs/<id>.json` | Log entry details: timestamps, exit code, events, verify gates, errors |
| `unify/<id>.md` | Checks for existence to display "Closure: available" in detail view |

---

## 4. Launching the TUI

```bash
# Default: reads .kiln/tasks.yaml, refreshes every 2 seconds
kiln tui

# Custom tasks file and refresh interval
kiln tui --tasks path/to/tasks.yaml --refresh 500ms

# Refresh every 5 seconds (for large projects with many tasks)
kiln tui --refresh 5s
```

The TUI launches in the alternate screen buffer (full-screen mode). Your previous terminal content is preserved and restored when you quit.

**Quitting:** Press `q` or `Ctrl+C` at any time to exit.

---

## 5. Multi-Terminal Layout Tips

The core kiln workflow uses two terminals: one for the TUI (monitoring) and one for execution (`make all`). Here are practical ways to set this up:

### tmux (recommended)

```bash
# Start a new tmux session with a vertical split
tmux new-session -s kiln \; split-window -h \; send-keys 'kiln tui' C-m \; select-pane -L

# Left pane: your shell (run make all -j4 here)
# Right pane: kiln tui (auto-started)
```

Or from within an existing tmux session:

```bash
# Split vertically (side by side)
Ctrl+b %

# In the new pane:
kiln tui

# Switch back to the original pane:
Ctrl+b ←
```

### Terminal emulator tabs/splits

Most modern terminal emulators support built-in splits:

| Terminal | Split shortcut |
|----------|---------------|
| **iTerm2** | Cmd+D (vertical), Cmd+Shift+D (horizontal) |
| **Ghostty** | Ctrl+Shift+Enter (new split) |
| **Kitty** | Ctrl+Shift+Enter (new window in layout) |
| **Windows Terminal** | Alt+Shift+D (duplicate pane) |
| **VS Code Terminal** | Click the split icon or Ctrl+Shift+5 |

### Layout suggestion

For most work, a **vertical split** (side by side) works best:

```
┌──────────────────────┬──────────────────────┐
│                      │                      │
│   Shell              │   kiln tui           │
│   (make all -j4)     │   (live dashboard)   │
│                      │                      │
│                      │                      │
└──────────────────────┴──────────────────────┘
```

For projects with many tasks or when you need to read long error messages, a **horizontal split** (stacked) gives the TUI full width:

```
┌─────────────────────────────────────────────┐
│  Shell (make all -j4)                       │
├─────────────────────────────────────────────┤
│  kiln tui (full-width dashboard)            │
│                                             │
│                                             │
└─────────────────────────────────────────────┘
```

---

## 6. Walkthrough: New Project from Scratch

This section walks through a complete workflow from project initialization to monitoring task execution with the TUI.

### Step 1: Initialize the project

```bash
mkdir my-project && cd my-project
git init

# Scaffold the .kiln/ directory and Makefile
kiln init --profile go
```

This creates the directory structure shown in [Section 3](#3-the-kiln-directory-layout). Available profiles: `go`, `python`, `node`, `generic`.

### Step 2: Write your PRD

Create a `PRD.md` describing the work you want done. This is the input to kiln's planning step.

```bash
# Write or paste your PRD
vim PRD.md
```

### Step 3: Generate the task graph

```bash
# Parse PRD.md into .kiln/tasks.yaml (uses Claude)
make plan

# Validate and generate Make targets
make graph
```

At this point, `.kiln/tasks.yaml` contains your task definitions and `.kiln/targets.mk` contains the generated Make rules.

### Step 4: Launch the TUI in one terminal

```bash
kiln tui
```

You'll see all tasks listed as **pending** (gray) or **blocked** (magenta, if they have unmet dependencies). The summary panel shows something like:

```
Total: 8 | Complete: 0 | Running: 0 | Failed: 0 | Blocked: 3 | Pending: 5
```

### Step 5: Start execution in a second terminal

Open a new terminal tab/pane (see [Section 5](#5-multi-terminal-layout-tips)) and run:

```bash
# Execute all tasks (with up to 4 parallel jobs)
make all -j4
```

### Step 6: Watch tasks progress in the TUI

Switch back to the TUI terminal. Within 2 seconds (the default refresh interval), you'll see tasks transition:

1. **Pending** (gray) tasks that had no blockers start **running** (yellow) — these jump to the top of the list.
2. As tasks complete, they turn **complete** (green) and move to the bottom.
3. Blocked tasks (magenta) automatically become pending once their dependencies complete, then start running when Make picks them up.
4. If a task fails, it turns **failed** (red) and its error message appears in the Last Error column.

### Step 7: Investigate failures

If you see a red **failed** task:

1. Use `j`/`k` to navigate to the failed task row.
2. Press `Enter` to open the **detail view** — see the full error message, error class, exit code, and verify gate results.
3. Press `l` to open the **log events view** — browse the captured stdout/stderr from Claude's execution.
4. Press `Escape` to return to the main view.

See [Section 15](#15-error-classes-and-retryability) for what each error class means and [Section 18](#18-recovery-workflow-retry-reset-resume) for how to recover.

### Step 8: After execution completes

When all tasks are done (or some have failed), the summary panel gives you a clear picture:

```
Total: 8 | Complete: 7 | Running: 0 | Failed: 1 | Blocked: 0 | Pending: 0
```

Press `q` to exit the TUI, then address any failures with the recovery workflow described in [Section 18](#18-recovery-workflow-retry-reset-resume).

---

## 7. Walkthrough: Pre-Existing Project

For a project that's already partially through execution — perhaps you ran `make all` yesterday, some tasks completed, and you want to pick up where you left off.

### Step 1: Check current state

```bash
kiln tui
```

The TUI reads `.kiln/state.json`, `.kiln/done/*.done` markers, and `.kiln/logs/*.json` files to reconstruct the current state. You'll immediately see which tasks completed, which failed, and which are still pending.

### Step 2: Understand what's left

In the main view, tasks are sorted by urgency:
- **Running** tasks (if any are still going) appear at the top
- **Blocked** tasks come next — press `Enter` on one to see exactly which dependencies are missing
- **Failed** tasks show their last error — drill in with `Enter` for the full story
- **Pending** tasks are ready to run but haven't been picked up yet
- **Complete** tasks are at the bottom, out of the way

### Step 3: Resume execution

In a separate terminal:

```bash
# Retry failed tasks
kiln retry --task-id <failed-task-id>

# Or re-run the full graph (Make skips completed tasks via .done markers)
make all -j4
```

Switch to the TUI to watch progress. The polling cycle picks up state changes automatically.

### Step 4: Use grouping for large projects

For projects with many tasks, use grouping to organize the view:

- Press `p` to group tasks by their **phase** (plan, build, verify, docs)
- Press `m` to group tasks by their **milestone**
- Press the same key again to toggle grouping off

Grouping inserts section headers between task groups while preserving the status-based sort within each group.

---

## 8. The Main View (Task Graph)

The main view is what you see when the TUI launches. It consists of three areas:

### Summary Bar (top)

A single line showing aggregate counts and timing information. See [Section 9](#9-the-summary-panel) for details.

### Task Table (middle)

A full-width table with these columns:

| Column | Description |
|--------|-------------|
| **ID** | The task's kebab-case identifier from `tasks.yaml` |
| **Status** | Color-coded current status (see [Section 14](#14-understanding-task-status)) |
| **Attempts** | Number of execution attempts (from `state.json`) |
| **Last Error** | Truncated error message or blocked reason. `-` if none |
| **Duration** | Execution time from `state.json` (e.g. `45.3s`, `1461ms`). `-` if not available |
| **Kind** | Task kind from `tasks.yaml` (feature, fix, research, docs). `-` if not set |
| **Phase** | Task phase from `tasks.yaml` (plan, build, verify, docs). `-` if not set |
| **Lane** | Concurrency lane from `tasks.yaml`. `-` if not set |

The table adapts to your terminal width. The ID column expands to fill available space; other columns use fixed widths. Long values are truncated with `...`.

### Sort Order

Tasks are always sorted by status urgency:

1. **Running** — actively executing right now
2. **Blocked** — waiting on dependencies
3. **Failed** — most recent attempt failed
4. **Not Complete** — Claude reported work is not complete
5. **Pending** — ready to run, waiting for Make
6. **Complete** — finished successfully

Within each status group, tasks appear in their original `tasks.yaml` definition order.

### Keybinding Footer (bottom)

A dimmed line reminding you of the most common keybindings.

---

## 9. The Summary Panel

The summary panel is a single line always visible at the top of the main view:

```
Total: 16 | Complete: 14 | Running: 1 | Failed: 0 | Blocked: 0 | Pending: 1 | Elapsed: 5m32s | Refreshed: 2s ago
```

| Field | Description |
|-------|-------------|
| **Total** | Total number of tasks in `tasks.yaml` |
| **Complete** | Tasks with "complete" status (green) |
| **Running** | Tasks currently executing (yellow) |
| **Failed** | Tasks whose last attempt failed (red) |
| **Blocked** | Tasks waiting on incomplete dependencies (magenta) |
| **Pending** | Tasks ready to run but not yet started (gray) |
| **Elapsed** | Wall-clock time since the TUI was launched |
| **Refreshed** | Time since the last data refresh |

Each status label is rendered in its corresponding color for quick visual scanning.

---

## 10. The Task Detail View

Press `Enter` on any task in the main view to open the detail view.

### What you'll see

**Task metadata:**
```
ID:       error-taxonomy
Status:   complete
Attempts: 2
Kind:     feature
Phase:    build
Duration: 57149ms
Model:    claude-sonnet-4-6
Completed: 2026-03-07T02:50:35Z
```

**Last error (if present):**
```
Last Error (full):
  claude exited with code 1
Error Class: ClaudeExitError
```

See [Section 15](#15-error-classes-and-retryability) for a full reference of error classes.

**Log entry details (from `.kiln/logs/<id>.json`):**
```
── Log Entry ──
Started:   2026-03-07T02:49:38Z
Ended:     2026-03-07T02:50:35Z
Duration:  57149ms
Model:     claude-sonnet-4-6
Exit Code: 0
Status:    complete
Events:    247
```

See [Section 16](#16-exit-codes) for what each exit code means.

**Verify gate results (if the task has verification gates):**
```
── Verify Gates ──
All Passed: false
  ✓ go-test (exit 0)
  ✗ go-vet (exit 1)
```

Verify gates are post-execution checks defined in `tasks.yaml` (see [Glossary](#28-glossary)). Each gate runs a command and reports pass/fail. If any gate fails, the task is marked as failed even if Claude reported success.

**Closure artifact indicator (if `.kiln/unify/<id>.md` exists):**
```
Closure: available
```

This means `kiln unify` has been run for this task and produced a closure artifact — a reconciliation document comparing what was intended vs. what was actually built (see [Glossary](#28-glossary)).

**Blocked dependency listing (if task is blocked):**
```
── Dependencies ──
  ✓ auth-module
  ✗ database-schema
  ✗ config-loader
```

Each dependency shows whether it's complete (green check) or not yet done (red cross).

### Navigation

Press `Escape` or `q` to return to the main view.

---

## 11. The Log Events View

Press `l` on any task in the main view to open the log events view.

This view renders the `events` array from the task's execution log (`.kiln/logs/<id>.json`). Each event is a line of captured stdout or stderr from the Claude Code invocation, with a timestamp:

```
── Log Events: error-taxonomy ──

[02:49:40] I'll start by reading the existing error handling code...
[02:49:42] Looking at the timeoutError and claudeExitError types...
[02:49:45] Now I'll implement the error taxonomy...
[02:50:30] All tests pass. Writing the JSON footer.

[247/247 lines]
```

### Scrolling

The view is scrollable. When opened, it auto-scrolls to the most recent events (bottom).

| Key | Action |
|-----|--------|
| `j` / `Down` | Scroll down one line |
| `k` / `Up` | Scroll up one line |
| `g` | Jump to the first line (top) |
| `G` | Jump to the last line (bottom) |
| `Escape` / `q` | Return to the main view |

### Live updates

The log view refreshes on each polling tick. If you're watching a **running** task, new events appear as Claude produces output. The view auto-scrolls to keep the newest content visible.

If no log file exists for the selected task, the view shows:
```
(no log events)
```

---

## 12. The Help Overlay

Press `?` from the main view to display the keybinding reference overlay:

```
── Kiln TUI Help ──

Navigation:
  j / ↓       Move cursor down
  k / ↑       Move cursor up
  g           Jump to top
  G           Jump to bottom

Views:
  Enter       Open task detail view
  l           Open log events view
  Escape / q  Return to main view

Actions:
  r           Force immediate refresh
  p           Toggle phase grouping
  m           Toggle milestone grouping
  ?           Toggle this help overlay

Global:
  q / Ctrl+C  Quit

Press any key to close help.
```

Press any key to dismiss the overlay and return to the main view.

---

## 13. Grouping Tasks

For projects with many tasks, grouping helps organize the view by logical categories.

### Group by Phase

Press `p` to toggle phase grouping. Tasks are grouped under headers like:

```
── Phase: plan ──
  planning-task-1       pending    0   -         ...
  planning-task-2       complete   1   -         ...

── Phase: build ──
  auth-module           running    1   -         ...
  database-schema       blocked    0   blocked: auth-module  ...

── Phase: (none) ──
  misc-task             pending    0   -         ...
```

Press `p` again to toggle grouping off.

### Group by Milestone

Press `m` to toggle milestone grouping. Tasks are grouped under their milestone value:

```
── Milestone: v1-mvp ──
  ...

── Milestone: v1-polish ──
  ...
```

Press `m` again to toggle off.

### Notes on grouping

- Only one grouping mode is active at a time. Pressing `p` while milestone grouping is active switches to phase grouping (and vice versa — pressing `m` clears phase grouping and activates milestone grouping).
- Tasks are sorted by **group first, then by status urgency within each group**. This ensures all tasks in the same phase/milestone stay together — a group header never appears more than once.
- Groups are sorted alphabetically. Tasks with an empty group key (no phase/milestone set) appear last under a `(none)` header.
- Within each group, the standard status sort applies: running first, then blocked, failed, not_complete, pending, complete. Within the same status, tasks preserve their `tasks.yaml` definition order.
- If no tasks have the relevant field set (e.g., no tasks have a `phase` value), the toggle is a no-op — all tasks appear under a single `(none)` header.
- Group headers count toward the visible row limit, so on very small terminals some tasks may be clipped.

---

## 14. Understanding Task Status

The TUI derives task status using the same logic as `kiln status`, with a clear priority chain:

### Priority order (highest to lowest)

1. **`state.json`** — If `.kiln/state.json` has an entry for the task with a non-empty status, that status is authoritative. The status `"completed"` is normalized to `"complete"` for display.

2. **Done marker** — If `.kiln/done/<id>.done` exists and `state.json` has no entry, the task is `complete`.

3. **Log file** — If `.kiln/logs/<id>.json` exists, the log's status field is used. Log statuses `"timeout"` and `"error"` are normalized to `"failed"`.

4. **Dependency check** — If none of the above apply: if the task has `needs` and any dependency is not yet complete, status is `blocked`. Otherwise, status is `pending`.

### Status color reference

| Status | Color | Meaning |
|--------|-------|---------|
| `complete` | Green | Task finished successfully |
| `running` | Yellow | Task is currently executing |
| `failed` | Red | Most recent attempt failed (timeout, error, or bad exit code) |
| `blocked` | Magenta | Waiting on one or more incomplete dependencies |
| `pending` | Dim gray | Ready to run, no blockers, just hasn't started |
| `not_complete` | Orange | Claude reported the task is not yet complete |

---

## 15. Error Classes and Retryability

When a task fails, kiln classifies the error into a canonical error class. You'll see this in the TUI detail view as `Error Class:`. Understanding these classes helps you decide what to do next.

### Error class reference

| Error Class | Retryable? | Cause | What to do |
|-------------|-----------|-------|------------|
| `timeout` | Yes | Claude exceeded the configured `--timeout` duration (default 15m). The process was killed. | Increase the task timeout in `tasks.yaml`, or simplify the task prompt. Kiln auto-retries up to the configured retry count. |
| `claude_exit` | Yes | Claude Code exited with a non-zero exit code (crash, API error, rate limit). | Usually transient. Kiln auto-retries. If persistent, check Claude API status or your API key. |
| `footer_parse` | No | Claude's output did not end with the required JSON footer, or the footer was malformed JSON. | Review the task prompt — ensure it includes the footer contract instructions. Check the log events view (`l`) to see what Claude actually output. |
| `footer_validation` | No | The JSON footer was valid JSON but had incorrect content (wrong task_id, missing required fields, invalid status value). | Check that the prompt's TASK ID matches the task-id in `tasks.yaml`. Review the footer contract section of the prompt. |
| `lock_conflict` | No | Another `kiln exec` process is already running this task (detected via `.kiln/locks/<id>.lock`). | Wait for the other process to finish, or use `kiln exec --force-unlock` if the lock is stale (e.g., from a killed process). |
| `schema_validation` | No | The task definition in `tasks.yaml` failed schema validation (invalid ID format, missing prompt file, unknown dependency). | Fix `tasks.yaml` — run `kiln validate-schema --tasks .kiln/tasks.yaml` for detailed errors. |
| `unknown` | No | An error that doesn't match any known category. | Check the full error message in the detail view. Review log events for context. |

### How retryability works

When kiln encounters a **retryable** error during `kiln exec`, it automatically retries the task up to `--retries` times (configurable per task in `tasks.yaml` or via the profile). Retries use backoff (fixed or exponential) to avoid hammering the API.

When kiln encounters a **non-retryable** error, it stops immediately — retrying wouldn't help because the problem is in the prompt, the schema, or the configuration, not in a transient service issue.

In the TUI detail view, you can see both the error class and whether it was retryable:
```
Error Class: timeout
```
The attempt count tells you how many times kiln tried before giving up:
```
Attempts: 3
```

---

## 16. Exit Codes

The detail view shows an `Exit Code` for each log entry. These exit codes are part of kiln's contract with Make:

| Exit Code | Meaning | Make behavior |
|-----------|---------|---------------|
| **0** | Successful run. Task status may be `complete`, `not_complete`, or `blocked` depending on the footer. | If footer status is `complete`, Make creates the `.done` marker. |
| **2** | Successful run with footer status `complete`. (Legacy — same as 0 with complete status.) | Make creates the `.done` marker. |
| **10** | Permanent failure. Non-retryable error: invalid arguments, missing prompt file, footer parse/validation error, lock conflict. | Make stops the recipe. No `.done` marker. Task cannot proceed without manual intervention. |
| **20** | Transient failure — all retries exhausted. The task hit a retryable error (timeout or claude_exit) but failed on every attempt. | Make stops the recipe. No `.done` marker. Can be retried with `kiln retry`. |

### Reading exit codes in the TUI

When you see `Exit Code: 10` in the detail view, it means the task hit a permanent failure that won't be fixed by retrying — you need to fix the prompt, schema, or configuration. When you see `Exit Code: 20`, kiln tried multiple times but the transient errors persisted — waiting and retrying later may help.

---

## 17. Blocked Tasks and Dependencies

When a task shows as **blocked** (magenta), it means one or more of its `needs` dependencies haven't completed yet.

### In the main view

The Last Error column shows which dependencies are unmet:

```
prompt-chaining     blocked    0   blocked: unify-closure,richer-schema  ...
```

### In the detail view

Press `Enter` on a blocked task to see the full dependency breakdown:

```
── Dependencies ──
  ✓ unify-closure        (complete)
  ✗ richer-schema        (not complete)
```

This tells you exactly which upstream tasks need to finish before this task can proceed.

### What to do about blocked tasks

Blocked tasks resolve automatically. When their dependencies complete (via `make all` or manual `kiln exec`), the next TUI refresh cycle reclassifies them as `pending`, and Make picks them up for execution.

If a dependency is **failed**, the blocked task stays blocked until you fix the upstream failure (via `kiln retry` or manual intervention) and it completes successfully.

---

## 18. Recovery Workflow: retry, reset, resume

When you see a failed or incomplete task in the TUI, here's your decision tree for recovery. Use these commands in a separate terminal while the TUI is running — the TUI will pick up the state changes on the next refresh.

### Decision tree

```
Task shows as FAILED in TUI
├── Error class is retryable (timeout, claude_exit)?
│   ├── Yes → kiln retry --task-id <id>
│   │         (re-runs the task, honors retry/backoff settings)
│   └── No (footer_parse, footer_validation, schema_validation, lock_conflict)
│       → Fix the underlying issue first, then:
│         kiln reset --task-id <id>
│         make all  (or kiln exec directly)
│
Task shows as NOT_COMPLETE in TUI
├── Claude made partial progress?
│   ├── Yes → kiln resume --task-id <id>
│   │         (re-runs with dependency context, Claude continues from where it left off)
│   └── No  → kiln reset --task-id <id>
│             make all
│
Task shows as BLOCKED in TUI
└── Fix the upstream dependency first (it's failed or incomplete)
    └── Then the blocked task will automatically become pending
```

### Command reference

**`kiln retry`** — Re-execute a task that previously failed.

```bash
# Retry a specific failed task
kiln retry --task-id error-taxonomy

# Retry all failed tasks at once
kiln retry --failed

# Retry only tasks with transient (retryable) errors
kiln retry --failed --transient-only
```

**`kiln reset`** — Clear a task's state so it can be re-run from scratch. Removes the done marker, clears state.json entry, and deletes the lock file.

```bash
# Reset a specific task
kiln reset --task-id auth-module

# Reset ALL tasks (requires confirmation on stdin)
kiln reset --all
```

**`kiln resume`** — Re-run a task that reported `not_complete`. Kiln injects dependency context from completed upstream tasks so Claude can pick up where it left off.

```bash
kiln resume --task-id database-schema
```

### Watching recovery in the TUI

After running any recovery command, the TUI picks up the state change on its next refresh tick (or press `r` for an immediate refresh). You'll see:

1. After `kiln retry` → task goes from **failed** to **running** (yellow), then either **complete** (green) or **failed** (red) again.
2. After `kiln reset` → task goes from whatever state it was in to **pending** (gray). It will be picked up by the next `make all` run.
3. After `kiln resume` → task goes to **running** (yellow), then either **complete** or **not_complete** again.

---

## 19. Polling and Live Refresh

The TUI polls the filesystem for state changes at a configurable interval.

### What gets refreshed on each tick

1. **`.kiln/tasks.yaml`** — re-read to pick up any task definition changes
2. **`.kiln/state.json`** — re-read for status, attempt counts, timestamps, errors
3. **`.kiln/done/*.done`** — re-stat all done markers
4. **`.kiln/logs/*.json`** — re-read log files for status derivation

### Configuring the refresh interval

```bash
# Faster refresh for real-time monitoring
kiln tui --refresh 500ms

# Slower refresh to reduce filesystem I/O
kiln tui --refresh 10s

# Default: 2 seconds
kiln tui
```

### Force refresh

Press `r` at any time to trigger an immediate refresh without waiting for the next tick. Useful when you know a task just completed and want to see the update right away.

### Performance considerations

Each refresh cycle stats one file per task (done markers), reads `state.json`, and reads one log file per task. For a typical project with 10-20 tasks, this is negligible. For very large projects (100+ tasks), consider increasing the `--refresh` interval to 5-10 seconds.

---

## 20. Models and Profiles

The TUI detail view shows which Claude model was used for each task (e.g., `Model: claude-sonnet-4-6`). Understanding how models are selected helps you interpret execution results.

### Model selection hierarchy

Kiln resolves the model for each task in this order (highest priority first):

1. **`--model` flag** on `kiln exec` — explicit per-invocation override
2. **`model:` field** in `tasks.yaml` — per-task override
3. **`KILN_MODEL` environment variable** — project-wide default
4. **Built-in default** — `claude-sonnet-4-6`

### Profiles: speed vs reliable

Kiln supports workflow profiles that adjust retry behavior, backoff strategy, and other execution parameters. These don't change which model is used, but they affect how aggressively kiln retries failures.

```bash
# Show current profile settings
kiln profile

# Override profile for a run
kiln profile --profile speed
kiln profile --profile reliable
```

| Setting | `speed` profile | `reliable` profile |
|---------|----------------|-------------------|
| Retries | Fewer (get results fast) | More (tolerate transient errors) |
| Backoff | Shorter delays | Longer delays with jitter |
| Timeout | Shorter | Longer |

Profiles are configured in `.kiln/config.yaml` and can be overridden per-invocation with `kiln exec --profile speed`.

### What the TUI shows

In the detail view, the **Model** field tells you which model was actually used for the most recent attempt. The **Attempts** count tells you how many times kiln tried. If you see a task with `Attempts: 3` and a `timeout` error class, the reliable profile may help by giving each attempt more time.

---

## 21. Anatomy of a tasks.yaml Entry

The TUI displays data from `.kiln/tasks.yaml`. Understanding the file format helps you interpret what you see in the dashboard and make changes when needed.

### Minimal entry

Every task needs just three fields:

```yaml
- id: auth-module
  prompt: .kiln/prompts/tasks/auth-module.md
  needs: []
```

| Field | TUI column | Description |
|-------|-----------|-------------|
| `id` | **ID** | Unique kebab-case identifier. Must match `^[a-z0-9]+(-[a-z0-9]+)*$`. |
| `prompt` | *(not shown)* | Path to the markdown prompt file that Claude receives. |
| `needs` | *(blocked status)* | List of task IDs that must complete first. Empty list = no dependencies. |

### Full entry with all optional fields

```yaml
- id: error-taxonomy
  prompt: .kiln/prompts/tasks/error-taxonomy.md
  needs:
    - state-resumability
  timeout: 90m
  model: claude-sonnet-4-6
  description: Implement canonical error classification
  kind: feature
  phase: build
  milestone: v1-mvp
  lane: core
  dev-phase: 2
  retries: 3
  tags: [error-handling, reliability]
  validation:
    - go test ./cmd/kiln -v
    - go vet ./cmd/kiln
  env:
    KILN_MODEL: claude-sonnet-4-6
```

| Field | TUI column | Description |
|-------|-----------|-------------|
| `timeout` | *(detail view: Duration context)* | Maximum execution time (e.g., `15m`, `90m`, `180m`). Default: 15 minutes. |
| `model` | **Model** (detail view) | Override the Claude model for this task. |
| `description` | *(not shown)* | Human-readable description. Used by `kiln status` and `kiln report`. |
| `kind` | **Kind** | Task type: `feature`, `fix`, `research`, `docs`, `refactor`, etc. |
| `phase` | **Phase** | Lifecycle stage: `plan`, `build`, `verify`, `docs`. Used by `p` grouping. |
| `milestone` | *(grouping only)* | Project milestone: `v1-mvp`, `v2-polish`, etc. Used by `m` grouping. |
| `lane` | **Lane** | Concurrency group. Tasks in the same lane can be mutually exclusive. Shows `-` if not set. |
| `dev-phase` | *(not shown)* | Numeric phase for `make graph DEV_PHASE=N` filtering. |
| `retries` | *(detail view: Attempts context)* | Override retry count for this task. |
| `tags` | *(not shown)* | Arbitrary tags for filtering and organization. |
| `validation` | *(detail view: Verify Gates)* | Shell commands run after Claude finishes. Failures override Claude's status. |
| `env` | *(not shown)* | Environment variables injected into the Claude invocation. |

### Real-world example from this project

```yaml
- id: interactive-tui           # ← Shows as ID in the TUI table
  prompt: .kiln/prompts/tasks/interactive-tui.md
  timeout: 180m                 # ← 3 hours (complex task)
  dev-phase: 5                  # ← Only included when make graph DEV_PHASE=5
  needs:
    - state-resumability        # ← If not complete, TUI shows "blocked: state-resumability"
```

### How changes to tasks.yaml appear in the TUI

The TUI re-reads `tasks.yaml` on every refresh tick. If you edit the file while the TUI is running:
- Adding a new task: it appears on the next refresh
- Changing a task's `kind` or `phase`: the table columns update
- Adding a `needs` dependency: the task may become blocked if the new dependency isn't complete
- Removing a task: it disappears from the table

No restart required — the TUI picks up changes automatically.

---

## 22. Interpreting the Attempts Column

The **Attempts** column in the TUI main view and detail view shows how many times kiln has tried to execute a task. Understanding this number in context with the task's status tells you the full story.

### Reading attempts + status together

| Attempts | Status | What happened |
|----------|--------|---------------|
| 0 | pending | Task hasn't been run yet. |
| 0 | blocked | Task can't run — dependencies aren't complete. |
| 1 | complete | Succeeded on the first try. The ideal outcome. |
| 1 | failed | Failed on the first (and only) attempt. Either non-retryable error, or retries are set to 0. |
| 2 | complete | Failed once, succeeded on the retry. Check the error class to understand what was transient. |
| 3 | complete | Failed twice, succeeded on the third attempt. The task may be flaky — consider increasing its timeout or simplifying the prompt. |
| 3 | failed | Failed on all 3 attempts (retries exhausted). Exit code is likely 20. Needs manual intervention. |
| 1 | not_complete | Claude ran but reported the work isn't finished. Use `kiln resume` to continue. |

### What drives the attempt count

- **Retryable errors** (timeout, claude_exit) trigger automatic retries up to the configured limit. Each retry increments the attempt count.
- **Non-retryable errors** (footer_parse, schema_validation) do NOT trigger retries. Attempts will be 1.
- The **profile** (speed vs reliable) and per-task **retries** field control the maximum number of attempts.

### Examples from a real project

Looking at kiln's own tasks:

```
error-taxonomy     complete    2    ← Failed once (likely timeout), succeeded on retry
verify-plan        complete    3    ← Took 3 attempts to get right
json-output        complete    2    ← One transient failure
profile-strategy   complete    1    ← Clean first-try success
```

### When to worry

- **Attempts > 1 + complete**: Not necessarily a problem. Transient failures (API rate limits, timeouts on complex tasks) are normal. If the same task consistently needs retries across multiple runs, consider increasing its timeout or simplifying the prompt.
- **Attempts = max retries + failed**: The task is consistently failing. Check the error class in the detail view. If it's `timeout`, the task may need a longer timeout. If it's `claude_exit`, check Claude API status.
- **Attempts = 1 + failed with non-retryable error**: The problem is in the configuration, not transient. Fix the prompt, schema, or footer contract — retrying won't help.

---

## 23. Keyboard Reference

### Main View

| Key | Action |
|-----|--------|
| `j` / `Down Arrow` | Move cursor down one row |
| `k` / `Up Arrow` | Move cursor up one row |
| `g` | Jump to the first task (top) |
| `G` | Jump to the last task (bottom) |
| `Enter` | Open task detail view for selected task |
| `l` | Open log events view for selected task |
| `r` | Force immediate state refresh |
| `p` | Toggle grouping by phase |
| `m` | Toggle grouping by milestone |
| `?` | Show help overlay |
| `q` | Quit the TUI |
| `Ctrl+C` | Quit the TUI (works from any view) |

### Detail View

| Key | Action |
|-----|--------|
| `Escape` | Return to main view |
| `q` | Return to main view |
| `Ctrl+C` | Quit the TUI |

### Log Events View

| Key | Action |
|-----|--------|
| `j` / `Down Arrow` | Scroll down one line |
| `k` / `Up Arrow` | Scroll up one line |
| `g` | Jump to top of log |
| `G` | Jump to bottom of log |
| `Escape` | Return to main view |
| `q` | Return to main view |
| `Ctrl+C` | Quit the TUI |

### Help Overlay

| Key | Action |
|-----|--------|
| Any key | Close help and return to main view |

---

## 24. CLI Flags Reference

```
Usage: kiln tui [flags]

Flags:
  --tasks <path>       Path to tasks.yaml (default: .kiln/tasks.yaml)
  --refresh <duration> Polling interval (default: 2s)
                       Accepts Go duration strings: 500ms, 1s, 2s, 5s, 1m
```

### Examples

```bash
# Monitor a non-standard tasks file
kiln tui --tasks projects/api/tasks.yaml

# Fast refresh for interactive debugging
kiln tui --refresh 200ms

# Slow refresh for long-running batch jobs
kiln tui --refresh 30s
```

---

## 25. Common Workflows

### Monitor a full build

```bash
# Terminal 1: Launch the TUI
kiln tui

# Terminal 2: Run all tasks with parallelism
make all -j4
```

Watch tasks flow from pending → running → complete in real time.

### Investigate a failure

1. Launch `kiln tui`
2. Navigate to the red **failed** task with `j`/`k`
3. Press `Enter` — read the full error message, error class ([Section 15](#15-error-classes-and-retryability)), and exit code ([Section 16](#16-exit-codes))
4. Press `Escape`, then `l` — browse the captured Claude output to understand what went wrong
5. Press `q` to exit
6. Follow the recovery decision tree in [Section 18](#18-recovery-workflow-retry-reset-resume)

### Check progress after stepping away

```bash
kiln tui
```

The TUI instantly reconstructs state from disk. No need to parse log files or check done markers manually — the summary panel gives you the full picture in one line.

### Run a specific dev-phase

```bash
# Generate targets for phase 1 only
make graph DEV_PHASE=1

# Monitor in the TUI
kiln tui

# Execute in another terminal
make all -j4
```

### Batch retry all transient failures

```bash
# In one terminal, retry all tasks with retryable errors
kiln retry --failed --transient-only

# In another terminal, watch the retries in the TUI
kiln tui --refresh 1s
```

---

## 26. Companion Commands: status and report

The TUI is the interactive way to monitor tasks, but kiln also provides non-interactive commands for scripting, CI, and post-run analysis.

### `kiln status` — One-shot task overview

```bash
# Human-readable table (same data as the TUI main view, but static)
kiln status --tasks .kiln/tasks.yaml

# JSON output for scripting
kiln status --tasks .kiln/tasks.yaml --format json
```

**When to use `kiln status` instead of `kiln tui`:**
- In CI/CD pipelines where there's no interactive terminal
- In scripts that parse task state (use `--format json`)
- For a quick one-shot check without entering full-screen mode
- When piping output to other tools (`jq`, `grep`, etc.)

**JSON output example:**

```bash
# Count failed tasks
kiln status --tasks .kiln/tasks.yaml --format json | jq '.summary.failed'

# List IDs of all failed tasks
kiln status --tasks .kiln/tasks.yaml --format json | jq -r '.tasks[] | select(.status == "failed") | .id'

# Check if all tasks are complete
kiln status --tasks .kiln/tasks.yaml --format json | jq '.summary.complete == .summary.total'
```

### `kiln report` — Post-run analysis

```bash
# Table format summary of all log files
kiln report

# JSON format for further processing
kiln report --format json

# Custom log directory
kiln report --log-dir .kiln/logs
```

**When to use `kiln report`:**
- After a full run to generate a summary for stakeholders
- To analyze execution patterns (which tasks took longest, which had retries)
- To feed into dashboards or tracking systems (use `--format json`)

| Feature | `kiln tui` | `kiln status` | `kiln report` |
|---------|-----------|--------------|---------------|
| Interactive | Yes | No | No |
| Live refresh | Yes | No | No |
| Detail drill-down | Yes | No | No |
| JSON output | No | Yes | Yes |
| CI-friendly | No | Yes | Yes |
| Shows log analysis | Yes (per-task) | No | Yes (aggregate) |
| Scriptable | No | Yes | Yes |

---

## 27. Troubleshooting

### TUI shows no tasks

**Cause:** The `tasks.yaml` file is missing or empty.

**Fix:** Ensure `.kiln/tasks.yaml` exists and contains at least one task, or pass the correct path with `--tasks`.

```bash
kiln tui --tasks path/to/tasks.yaml
```

### All tasks show as "pending" even though some completed

**Cause:** The `.kiln/done/` directory or `.kiln/state.json` is missing or was deleted.

**Fix:** Check that `.kiln/done/` exists and contains `.done` marker files. If you're using `state.json` (the default since the state-resumability feature), check that it hasn't been accidentally deleted.

```bash
ls .kiln/done/
cat .kiln/state.json | python3 -m json.tool
```

### Colors look wrong or missing

**Cause:** Terminal doesn't support 256 colors, or `TERM` environment variable is misconfigured.

**Fix:** Ensure your terminal emulator supports 256 colors and that `TERM` is set appropriately (e.g., `xterm-256color`).

```bash
echo $TERM
# Should be xterm-256color, screen-256color, or similar
```

### TUI exits immediately

**Cause:** The terminal doesn't support the alternate screen buffer, or there's a TTY issue (e.g., running inside a pipe or non-interactive shell).

**Fix:** `kiln tui` requires a real terminal. It cannot run inside `script`, piped to another command, or in a non-interactive CI environment. Run it directly in your terminal emulator. For CI environments, use `kiln status --format json` instead.

### Refresh feels sluggish

**Cause:** Default 2-second refresh interval.

**Fix:** Decrease the refresh interval:

```bash
kiln tui --refresh 500ms
```

### Task status doesn't match what I expect

The TUI uses the same priority chain as `kiln status`. Debug by checking the raw data:

```bash
# Check state.json for a specific task
cat .kiln/state.json | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps(d['tasks'].get('TASK-ID','not found'), indent=2))"

# Check if done marker exists
ls -la .kiln/done/TASK-ID.done

# Check log file status
cat .kiln/logs/TASK-ID.json | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['status'], d.get('error_class',''))"
```

The priority order is: `state.json` > `.done` marker > log file > dependency check > pending.

### A task shows "failed" but I fixed the code

**Cause:** The state files still reflect the old failure. Kiln doesn't automatically detect code changes.

**Fix:** Reset the task and re-run:

```bash
kiln reset --task-id <id>
make all
```

### Lock conflict error

**Cause:** Another `kiln exec` process is still running for this task, or a previous process was killed and left a stale lock file.

**Fix:** If you're sure no other process is running:

```bash
kiln exec --task-id <id> --force-unlock ...
```

Or manually remove the lock:

```bash
rm .kiln/locks/<id>.lock
```

### Terminal is garbled after a crash

If the TUI exits abnormally (kill -9, terminal close), the alternate screen buffer may not be cleaned up.

**Fix:** Run `reset` or `tput rmcup` to restore your terminal:

```bash
reset
```

---

## 28. Glossary

Terms you'll encounter in the TUI and throughout kiln's documentation.

| Term | Definition |
|------|-----------|
| **Alternate screen buffer** | A terminal feature that provides a separate screen for full-screen applications. The TUI uses this so your shell history is preserved when you quit. |
| **Backoff** | The delay between retry attempts. Can be `fixed` (same delay each time) or `exponential` (delay doubles each attempt, with jitter to avoid thundering herd). |
| **Closure artifact** | A markdown file produced by `kiln unify` that reconciles what a task was supposed to do (from the prompt) with what it actually did (from the code changes). Stored in `.kiln/unify/<id>.md`. The TUI shows "Closure: available" in the detail view when one exists. |
| **Done marker** | A zero-byte file at `.kiln/done/<id>.done` that tells Make a task has completed. Make uses these for idempotency — if the marker exists, the task is skipped on subsequent runs. |
| **Error class** | A canonical category for task failures (e.g., `timeout`, `claude_exit`, `footer_parse`). See [Section 15](#15-error-classes-and-retryability) for the full reference. |
| **Footer contract** | The JSON object that Claude must output as the last line of its response: `{"kiln":{"status":"complete","task_id":"<id>"}}`. Kiln parses this to determine task outcome. A missing or malformed footer results in a `footer_parse` error. |
| **Idempotency** | The property that running `make all` multiple times produces the same result. Achieved via `.done` markers — completed tasks are never re-run unless explicitly reset. |
| **Lane** | An optional task field for concurrency grouping. Tasks in the same lane can be configured for mutual exclusion (only one runs at a time). Shown in the TUI table when set. |
| **Log events** | Captured stdout/stderr lines from a Claude Code invocation. Each event has a timestamp, type (`stdout` or `stderr`), and the text content. Viewable in the TUI via the `l` key. |
| **Milestone** | An optional task field for grouping tasks by project milestone (e.g., `v1-mvp`, `v2-polish`). Used by the TUI's `m` grouping mode. |
| **Needs** | The dependency list for a task in `tasks.yaml`. A task with `needs: [auth-module, config]` cannot start until both `auth-module` and `config` are complete. |
| **Phase** | An optional task field indicating lifecycle stage: `plan`, `build`, `verify`, or `docs`. Used by the TUI's `p` grouping mode. |
| **Profile** | A named configuration preset (`speed` or `reliable`) that adjusts retry counts, timeouts, and backoff strategy. Configured in `.kiln/config.yaml`. |
| **Prompt chaining** | The mechanism by which `kiln exec` injects context from completed upstream tasks into a downstream task's prompt. This gives Claude awareness of what was already built by dependencies. |
| **Ralph Wiggum pattern** | The workflow pattern kiln implements: one task per fresh Claude Code invocation, orchestrated by Make. Named for the principle that each invocation starts with a clean context, preventing the quality degradation (context rot) that occurs in long sequential sessions. |
| **Retryable error** | An error caused by a transient issue (timeout, API error) that may succeed on a subsequent attempt. Kiln automatically retries these. Non-retryable errors (bad prompt, invalid schema) require manual intervention. |
| **State manifest** | The `.kiln/state.json` file that tracks per-task execution state: status, attempt count, timestamps, errors, duration, and model used. This is the TUI's primary data source. |
| **Task graph** | The directed acyclic graph (DAG) of tasks defined in `tasks.yaml`. Edges are the `needs` dependencies. Make uses this graph to determine execution order and parallelism. |
| **Verify gate** | A post-execution check defined in `tasks.yaml` (e.g., `go test ./...`, `go vet ./...`). After Claude completes a task, kiln runs each gate command. If any gate fails, the task is marked as failed regardless of Claude's footer status. Results are shown in the TUI detail view. |

# Task: interactive-tui — Interactive TUI Dashboard

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
interactive-tui

## SCOPE
Implement ONLY the interactive TUI dashboard described below. Do not work on other backlog items (UNIFY, engine abstraction, prompt chaining, validation hooks, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln writes per-task JSON logs to `.kiln/logs/<task-id>.json` via `execRunLog` entries (one entry per attempt).
- Kiln creates `.kiln/done/<task-id>.done` markers for Make idempotency on successful completion.
- Tasks are defined in `.kiln/tasks.yaml` with fields: id, prompt, needs, timeout (and possibly richer schema fields if #7 has landed).
- If `.kiln/state.json` exists (from backlog item #1), use it for live status data (pending/running/completed/failed per task, attempt counts, timestamps, last error). If it does not exist, derive status from `.done` markers and log files.
- If error taxonomy fields (`error_class`, `retryable`) exist in log entries (from backlog item #9), display them in the error column. If not, fall back to raw error messages.
- If `kiln status` command exists (from backlog item #13), reuse its status-derivation logic rather than duplicating it.
- If richer schema fields exist (kind, phase, milestone, lane from backlog item #7), use them for grouping views. If not, skip grouping gracefully.

## REQUIREMENTS

1. **Add Bubble Tea dependency** — Add `github.com/charmbracelet/bubbletea` and `github.com/charmbracelet/lipgloss` (for styling) to the project via `go get`. These are the Go-native TUI framework and styling library.

2. **`kiln tui` command** — Register a new `tui` subcommand alongside existing subcommands (`exec`, `gen-make`, etc.) using the same CLI pattern (flag set, argument parsing).
   - `kiln tui` launches the interactive terminal dashboard
   - `kiln tui --refresh <duration>` sets the polling interval (default: `2s`)
   - `kiln tui --tasks <path>` overrides the tasks.yaml path (default: `.kiln/tasks.yaml`)

3. **Task graph view (main view)** — Display all tasks from `tasks.yaml` in a table:
   - Columns: Task ID, Status, Attempts, Last Error, Duration (if available from logs/state)
   - Color-coded status indicators:
     - `complete` = green
     - `running` = yellow/amber
     - `failed` = red
     - `blocked` = magenta
     - `pending` = dim/gray
     - `not_complete` = orange/yellow
   - Status is derived using the same logic as `kiln status` (if available) or independently:
     - `.kiln/state.json` per-task status (if state file exists and has "running" status)
     - `.kiln/done/<id>.done` exists -> `complete`
     - `.kiln/logs/<id>.json` exists with attempts -> check last attempt status
     - No log file and no done marker -> `pending`
   - Sort tasks by status category: running first, then blocked, failed, not_complete, pending, complete
   - Highlight the currently selected task row with a cursor

4. **Summary panel** — Show an always-visible summary bar at the top or bottom:
   - `Total: N | Complete: N | Running: N | Failed: N | Blocked: N | Pending: N`
   - Elapsed time since TUI launch
   - Last refresh timestamp

5. **Task detail view** — When user presses Enter on a selected task, show a detail panel:
   - Task ID, status, attempt count
   - Full last error message (not truncated)
   - Log entries from `.kiln/logs/<id>.json` (show last 5 attempts with timestamps and status)
   - If `.kiln/unify/<id>.md` closure artifact exists, show a "Closure: available" indicator
   - Press Escape or `q` to return to the main task graph view

6. **Live log tail view** — When user presses `l` on a selected task:
   - Stream/tail the task's log file content (`.kiln/logs/<id>.json`)
   - Auto-scroll to the bottom as new entries appear
   - Press Escape or `q` to return to the main view

7. **Polling and refresh** — The TUI must poll for state changes:
   - On each tick (configurable via `--refresh`), re-read `.kiln/state.json`, `.done` markers, and log files
   - Update the task table and summary panel with fresh data
   - Use Bubble Tea's `tea.Tick` command for periodic refresh

8. **Keyboard navigation**:
   - `j` / `k` or arrow keys: move cursor up/down in task list
   - `Enter`: open task detail view
   - `l`: open log tail view for selected task
   - `q` or `Ctrl+C`: quit the TUI
   - `r`: force immediate refresh
   - `?`: toggle help overlay showing keybindings
   - `g` / `G`: jump to top/bottom of task list

9. **Grouping views (optional, if richer schema is available)**:
   - If tasks have `phase` field, allow `p` key to toggle grouping by phase
   - If tasks have `milestone` field, allow `m` key to toggle grouping by milestone
   - If tasks have `lane` field, show lane info in the table
   - If none of these fields exist, these keys are no-ops (no error)

10. **Blocked-reason display** — If a task is blocked:
    - In the main table, show a truncated blocked reason in the Last Error column
    - In the detail view, show the full blocked reason and list which `needs` dependencies are not yet complete

11. **Graceful degradation** — The TUI must work with minimal data:
    - Works with only `tasks.yaml` and `.done` markers (no state file, no logs)
    - Works with empty `.kiln/logs/` directory
    - Works if `.kiln/state.json` does not exist
    - Handles malformed or missing log files without crashing

## Tests

- TUI model initializes correctly from a tasks.yaml fixture
- Status derivation produces correct status for each scenario: pending, complete (from .done), failed (from log), running (from state.json)
- Summary counts are correct for a mix of task statuses
- Polling tick triggers state refresh and updates the model
- Keyboard navigation moves cursor correctly (j/k, g/G)
- Enter key transitions to detail view, Escape returns to main view
- Task detail view shows correct attempt history from log entries
- Blocked task display shows unmet dependencies from `needs` field
- Graceful handling when log directory is missing
- Graceful handling when state.json is missing
- Graceful handling when log file is malformed JSON
- Color-coding maps correct colors to each status
- `--refresh` flag is parsed and used for tick interval
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln tui` launches an interactive terminal dashboard using Bubble Tea
- Task graph view displays all tasks with color-coded status, attempts, and last error
- Summary panel shows counts by status category and elapsed time
- Task detail view (Enter) shows full error, attempt history, and closure indicator
- Log tail view (l) streams the selected task's log file
- Keyboard navigation works: j/k, arrows, Enter, Escape, q, r, g/G, ?
- Polling refreshes state on a configurable interval (default 2s)
- TUI gracefully degrades when state.json, logs, or richer schema fields are missing
- Blocked tasks show unmet dependency information
- `kiln tui` subcommand is registered alongside existing CLI subcommands
- Bubble Tea and Lipgloss dependencies are added to go.mod
- Existing `kiln exec` and `kiln gen-make` behavior is unchanged
- `go test ./...` passes
- No large refactors unrelated to the TUI

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"interactive-tui"}}

Allowed values for status:
- "complete"     (all acceptance criteria met)
- "not_complete" (work attempted but acceptance criteria not met)
- "blocked"      (cannot proceed due to missing info, permissions, dependencies, or unclear requirements)

STRICT RULES FOR THE JSON FOOTER
- The JSON object MUST be the final line of your response.
- Output EXACTLY one JSON object.
- No extra text after it.
- No code fences around it.
- The task_id must exactly match the TASK ID above.
- If you are unsure, choose "not_complete" or "blocked" rather than "complete".

If you finish successfully, the correct final line is:
{"kiln":{"status":"complete","task_id":"interactive-tui"}}

# Task: interactive-tui — Interactive TUI Dashboard

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
interactive-tui

## SCOPE
Implement ONLY the interactive TUI dashboard described below. Do not work on other backlog items (UNIFY, engine abstraction, prompt chaining, validation hooks, etc.), even if you notice related opportunities.

## FILE ORGANIZATION

**CRITICAL:** `cmd/kiln/main.go` is already 3,517 lines and `cmd/kiln/main_test.go` is 9,070 lines. Do NOT add TUI code to these files.

- Create **`cmd/kiln/tui.go`** for all TUI production code (model, update, view, styling, polling).
- Create **`cmd/kiln/tui_test.go`** for all TUI tests.
- These files are in the same `main` package — import and call shared types/functions from `main.go` directly (e.g., `loadTasks`, `loadState`, `deriveTaskStatus`, `hasUnfinishedDeps`).
- Wire the `tui` subcommand dispatch in `main.go`'s `run()` function as a single `case "tui":` line that calls `runTUI(args[1:])` defined in `tui.go`.
- **IMPORTANT**: The `run()` function signature is `func run(args []string, stdout, stderr io.Writer) int`. Bubble Tea's `tea.NewProgram` requires direct terminal access for input and rendering — it cannot work through an abstract `io.Writer`. Therefore `runTUI` should NOT accept `stdout`/`stderr` parameters. Let `tea.NewProgram` use `os.Stdout`/`os.Stdin` directly (the default).
- **Testing pattern**: Do NOT test through `tea.Program.Run()`. Instead, test the Bubble Tea `Model` directly by calling `Init()`, `Update(msg)`, and `View()` on the model struct. This is the standard Bubble Tea testing approach. Construct test models with fixture data and assert on state transitions and rendered output.

## CONTEXT — WHAT ALREADY EXISTS

The following features are **already implemented** in the codebase. Do NOT probe for their existence or build fallback paths — use them directly.

### `kiln status` command (main.go)
Reuse these exported-in-package functions:
- `loadTasks(path string) ([]Task, error)` — reads and parses tasks.yaml
- `loadState(path string) (*StateManifest, error)` — reads .kiln/state.json (returns empty manifest on missing file, not error)
- `deriveTaskStatus(t Task, state *StateManifest, logDir, doneDir string, doneSet map[string]bool) taskStatusInfo` — the single source of truth for status derivation
- `hasUnfinishedDeps(needs []string, doneSet map[string]bool) bool` — checks if any dependency is incomplete

### Data structures (main.go)
```go
type Task struct {
    ID          string            `yaml:"id"`
    Prompt      string            `yaml:"prompt"`
    Needs       []string          `yaml:"needs"`
    Timeout     string            `yaml:"timeout,omitempty"`
    Model       string            `yaml:"model,omitempty"`
    Description string            `yaml:"description,omitempty"`
    Kind        string            `yaml:"kind,omitempty"`
    Tags        []string          `yaml:"tags,omitempty"`
    Retries     int               `yaml:"retries,omitempty"`
    Validation  []string          `yaml:"validation,omitempty"`
    Engine      string            `yaml:"engine,omitempty"`
    Env         map[string]string `yaml:"env,omitempty"`
    DevPhase    int               `yaml:"dev-phase,omitempty"`
    Phase       string            `yaml:"phase,omitempty"`
    Milestone   string            `yaml:"milestone,omitempty"`
    Lane        string            `yaml:"lane,omitempty"`
    // Also has Acceptance, Verify, Exclusive fields
}

type StateManifest struct {
    Tasks       map[string]*TaskState `json:"tasks"`
    LastUpdated time.Time             `json:"last_updated"`
}

type TaskState struct {
    Status         string    `json:"status"`          // "completed", "running", "failed", "pending"
    Attempts       int       `json:"attempts"`
    LastAttemptAt  time.Time `json:"last_attempt_at,omitempty"`
    LastError      string    `json:"last_error,omitempty"`
    LastErrorClass string    `json:"last_error_class,omitempty"`
    CompletedAt    time.Time `json:"completed_at,omitempty"`
    DurationMs     int64     `json:"duration_ms,omitempty"`
    Model          string    `json:"model,omitempty"`
    Notes          string    `json:"notes,omitempty"`
}

type taskStatusInfo struct {
    ID       string `json:"id"`
    Status   string `json:"status"`    // normalized: "complete", "running", "failed", "blocked", "pending", "not_complete"
    Attempts int    `json:"attempts"`
    LastErr  string `json:"last_error,omitempty"`
    Kind     string `json:"kind,omitempty"`
    Phase    string `json:"phase,omitempty"`
}

type logEvent struct {
    TS   time.Time `json:"ts"`
    Type string    `json:"type"` // "stdout" or "stderr"
    Line string    `json:"line"`
}

type gateResult struct {
    Name       string `json:"name"`
    Cmd        string `json:"cmd"`
    Passed     bool   `json:"passed"`
    ExitCode   int    `json:"exit_code"`
    DurationMs int64  `json:"duration_ms"`
    Stderr     string `json:"stderr,omitempty"`
}

type verifyResults struct {
    Gates     []gateResult `json:"gates"`
    AllPassed bool         `json:"all_passed"`
    Skipped   bool         `json:"skipped"`
}

type execRunLog struct {
    TaskID       string         `json:"task_id"`
    StartedAt    time.Time      `json:"started_at"`
    EndedAt      time.Time      `json:"ended_at"`
    DurationMs   int64          `json:"duration_ms"`
    Model        string         `json:"model"`
    PromptFile   string         `json:"prompt_file"`
    ExitCode     int            `json:"exit_code"`
    Status       string         `json:"status"`       // "complete","not_complete","blocked","timeout","error"
    ErrorClass   string         `json:"error_class,omitempty"`
    ErrorMessage string         `json:"error_message,omitempty"`
    Retryable    bool           `json:"retryable,omitempty"`
    Events       []logEvent     `json:"events"`       // captured stdout/stderr lines from Claude
    Verify       *verifyResults `json:"verify,omitempty"`
}
```

### Status normalization (handled by `deriveTaskStatus`)
- `state.json` stores `"completed"` → normalized to `"complete"` for display
- Log status `"timeout"` or `"error"` → normalized to `"failed"`
- Priority order: state.json > .done marker > log file > dependency check (blocked) > pending
- Error taxonomy fields (`ErrorClass`, `Retryable`) exist on both `execRunLog` and `TaskState`
- Richer schema fields exist on `Task`: `Kind`, `Phase`, `Milestone`, `Lane`, `Tags`, `DevPhase`

### Log file format
Log files (`.kiln/logs/<id>.json`) contain a **single** `execRunLog` JSON object per task (overwritten on each run attempt). They are NOT arrays of attempts. Attempt counts come from `state.json`, not from parsing multiple log entries.

## REQUIREMENTS

### 1. Add Bubble Tea v2 dependencies

Add these dependencies via `go get`:
- `charm.land/bubbletea/v2` — the TUI framework (v2, NOT v1)
- `charm.land/lipgloss/v2` — styling library (v2, NOT v1)

Do NOT add `charm.land/x/term` or other terminal utilities — Bubble Tea v2 handles terminal size detection internally via `tea.WindowSizeMsg`.

**CRITICAL — Bubble Tea v2 API differences from v1:**
The v2 upgrade guide is available at `.kiln/references/bubble_tea/UPGRADE_GUIDE_V2.md`. Key changes:

- **Import paths**: `charm.land/bubbletea/v2` (NOT `github.com/charmbracelet/bubbletea`)
- **View() signature**: Returns `tea.View` NOT `string`. Use `tea.NewView(content)` or `v.SetContent(content)`.
- **Alt screen**: Set `view.AltScreen = true` in `View()` — NOT `tea.WithAltScreen()` option or `tea.EnterAltScreen` command.
- **Key events**: Use `tea.KeyPressMsg` (NOT `tea.KeyMsg` which is now an interface). Space is `"space"` not `" "`.
- **Ctrl+key**: Use `msg.String() == "ctrl+c"` or `msg.Code == 'c' && msg.Mod == tea.ModCtrl`.
- **Program creation**: `tea.NewProgram(model{})` — no more option flags for alt screen/mouse.
- **Window size**: `tea.RequestWindowSize` (NOT `tea.WindowSize()`).
- **Sequence**: `tea.Sequence(...)` (NOT `tea.Sequentially(...)`).
- **Testing**: `tea.WithWindowSize(w, h)` and `tea.WithColorProfile(p)` are useful for tests.

### 2. `kiln tui` command

Register a new `tui` subcommand in `main.go`'s `run()` dispatch (single case line). Implementation in `tui.go`.
- `kiln tui` launches the interactive terminal dashboard
- `kiln tui --refresh <duration>` sets the polling interval (default: `2s`)
- `kiln tui --tasks <path>` overrides the tasks.yaml path (default: `.kiln/tasks.yaml`)

### 3. Task graph view (main view)

Display all tasks from `tasks.yaml` in a table:
- Columns: Task ID, Status, Attempts, Last Error, Duration, Kind, Phase
- Color-coded status indicators:
  - `complete` = green
  - `running` = yellow/amber
  - `failed` = red
  - `blocked` = magenta
  - `pending` = dim/gray
  - `not_complete` = orange/yellow
- Status derived by calling `deriveTaskStatus()` for each task (do NOT reimplement)
- Sort tasks by status category: running first, then blocked, failed, not_complete, pending, complete. Within each status group, preserve `tasks.yaml` definition order.
- Highlight the currently selected task row with a cursor
- Adapt table width to terminal size (use `tea.WindowSizeMsg` from Bubble Tea). Truncate columns as needed, similar to how `kiln status` truncates `LastErr` at 38 chars.

### 4. Summary panel

Show an always-visible summary bar at the top or bottom:
- `Total: N | Complete: N | Running: N | Failed: N | Blocked: N | Pending: N`
- Elapsed time since TUI launch
- Last refresh timestamp

### 5. Task detail view

When user presses Enter on a selected task, show a detail panel:
- Task ID, status, attempt count, duration, model used
- Full last error message (not truncated) and error class (if present)
- Current log entry details: timestamp, status, exit code, error class, error message, event count, verify gate results (if present)
- If `.kiln/unify/<id>.md` closure artifact exists, show a "Closure: available" indicator
- If task is blocked, list which `needs` dependencies are not yet complete
- Press Escape or `q` to return to the main task graph view

### 6. Log events view

When user presses `l` on a selected task:
- Render the `events` array from the task's `execRunLog` JSON in a scrollable viewport. Each event has a timestamp and line of captured stdout/stderr from Claude.
- On refresh tick, re-read the file and update the viewport content.
- Auto-scroll to the bottom as new entries appear.
- This is **polling-based** (re-read on tick), not inotify/streaming.
- Press Escape or `q` to return to the main view.

### 7. Polling and refresh

The TUI must poll for state changes:
- On each tick (configurable via `--refresh`), re-read `.kiln/state.json`, `.done` markers, and log files
- Rebuild the task table and summary panel with fresh data using `deriveTaskStatus()`
- Use Bubble Tea's `tea.Tick` command for periodic refresh

### 8. Keyboard navigation

- `j` / `k` or arrow keys: move cursor up/down in task list
- `Enter`: open task detail view
- `l`: open log events view for selected task
- `q` or `Ctrl+C`: quit the TUI (remember: v2 uses `tea.KeyPressMsg` and `msg.String() == "ctrl+c"`)
- `r`: force immediate refresh
- `?`: toggle help overlay showing keybindings
- `g` / `G`: jump to top/bottom of task list

### 9. Grouping views

Tasks have `Phase` and `Milestone` fields on the `Task` struct. Use them:
- `p` key: toggle grouping by `Phase` (show phase headers between groups)
- `m` key: toggle grouping by `Milestone`
- If a task has `Lane` field set, show lane info in the table
- If a field is empty for all tasks, the toggle is a no-op (no error)

### 10. Blocked-reason display

If a task is blocked:
- In the main table, show "blocked: <unmet deps>" truncated in the Last Error column
- In the detail view, show the full blocked reason and list each `needs` dependency with its current status (complete/not complete)

### 11. Graceful degradation

The TUI must work with minimal data:
- Works with only `tasks.yaml` and `.done` markers (no state file, no logs)
- Works with empty `.kiln/logs/` directory
- Works if `.kiln/state.json` does not exist (loadState returns empty manifest)
- Handles malformed or missing log files without crashing
- Handles terminal resize events (`tea.WindowSizeMsg`) smoothly

## Tests

Write all tests in `cmd/kiln/tui_test.go`.

- TUI model initializes correctly from a tasks.yaml fixture
- Status derivation produces correct status for each scenario: pending, complete (from .done), failed (from log), running (from state.json) — test via `deriveTaskStatus()` integration
- Summary counts are correct for a mix of task statuses
- Polling tick triggers state refresh and updates the model
- Keyboard navigation moves cursor correctly (j/k, g/G)
- Enter key transitions to detail view, Escape returns to main view
- Task detail view shows correct log entry data (status, error class, error message, event count)
- Blocked task display shows unmet dependencies from `needs` field
- Graceful handling when log directory is missing
- Graceful handling when state.json is missing
- Graceful handling when log file is malformed JSON
- Color-coding maps correct colors to each status
- `--refresh` flag is parsed and used for tick interval
- Sort order: running > blocked > failed > not_complete > pending > complete, with definition-order preserved within groups
- Terminal resize updates table width
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln tui` launches an interactive terminal dashboard using Bubble Tea **v2**
- TUI code lives in `cmd/kiln/tui.go` and `cmd/kiln/tui_test.go` (NOT in main.go/main_test.go beyond the dispatch line)
- Task graph view displays all tasks with color-coded status, attempts, and last error
- Summary panel shows counts by status category and elapsed time
- Task detail view (Enter) shows full error, log entry details, and closure indicator
- Log events view (l) renders the events array from the task's log file in a scrollable viewport
- Keyboard navigation works: j/k, arrows, Enter, Escape, q, r, g/G, ?
- Polling refreshes state on a configurable interval (default 2s)
- TUI gracefully degrades when state.json, logs, or richer schema fields are missing
- Blocked tasks show unmet dependency information
- `kiln tui` subcommand is registered alongside existing CLI subcommands
- Bubble Tea v2 and Lipgloss v2 dependencies are added to go.mod (import path: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`)
- Uses `tea.View` return type (NOT `string`), `tea.KeyPressMsg` (NOT `tea.KeyMsg`), declarative view fields (NOT imperative options/commands)
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

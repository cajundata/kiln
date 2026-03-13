# TUI Interactive Control Backlog (PRD)

Owner: Weldon
Last updated: 2026-03-13
Status: Backlog (not started)

---

## 1. Problem Statement

The kiln TUI (`kiln tui`) is currently a read-only monitoring dashboard. Users must switch to a separate terminal to retry failed tasks, reset stale state, execute individual tasks, run unify, or perform any state-modifying operation. This context-switching breaks flow — the user sees a failed task in the TUI, identifies the problem, then must type a command from memory in another terminal, manually entering the task ID they were just looking at.

Adding interactive control directly to the TUI eliminates this friction. The user navigates to a task, presses a key, and the action runs — with the TUI showing progress in real time.

## 2. Design Principles

1. **Progressive disclosure.** The TUI should remain approachable. New keybindings are discoverable via the `?` help overlay. Dangerous operations require confirmation.
2. **No silent state mutation.** Every state-modifying action shows a confirmation modal before proceeding. The modal displays exactly what will happen (files deleted, commands run).
3. **Subprocess output capture.** Commands that invoke Claude (`exec`, `unify`, `gen-prompts`) run as background subprocesses. Their output is streamed into the log events view. The TUI remains responsive during execution.
4. **Keybinding consistency.** Vim-style navigation (j/k/g/G) is preserved. Action keys use mnemonics (x = execute, R = reset, etc.). Shift variants indicate "stronger" or batch operations.
5. **Graceful failure.** If a subprocess fails, the TUI displays the error inline — it never crashes or leaves the terminal in a broken state.

## 3. Architecture: Command Execution Model

All interactive commands follow the same pattern:

```
User presses key → Confirmation modal → Subprocess spawned → Output captured → State refreshed
```

### 3.1 Confirmation Modal

A centered overlay that describes the action and asks for confirmation:

```
┌─────────────────────────────────────────┐
│  Retry task: error-taxonomy             │
│                                         │
│  This will:                             │
│    - Remove .kiln/done/error-taxonomy   │
│    - Archive error-taxonomy.json log    │
│    - Clear state.json entry             │
│    - Re-execute via kiln exec           │
│                                         │
│  [Enter] Confirm    [Escape] Cancel     │
└─────────────────────────────────────────┘
```

Read-only actions (verify-plan, report) skip the confirmation modal.

### 3.2 Subprocess Execution

State-modifying commands run as child processes via `os/exec.Command`. The TUI:

1. Spawns the process with stdout/stderr piped.
2. Captures output lines as they arrive (same `logEvent` format).
3. Displays a "running" indicator in the summary bar.
4. On completion, triggers an immediate state refresh.
5. Shows success/failure in a brief toast notification (auto-dismissed after 3 seconds or on any keypress).

### 3.3 Keybinding Allocation

| Key | Action | Sprint | Scope |
|-----|--------|--------|-------|
| `x` | Execute selected task | 2 | Single task |
| `X` | Execute all pending tasks | 5 | Batch |
| `t` | Retry selected task | 1 | Single task |
| `T` | Retry all failed tasks | 4 | Batch |
| `R` | Reset selected task | 1 | Single task |
| `Ctrl+R` | Reset all tasks | 5 | Batch (with double-confirm) |
| `e` | Resume selected task | 1 | Single task |
| `u` | Unify selected task | 3 | Single task |
| `U` | Unify all completed tasks | 5 | Batch |
| `v` | Verify-plan overlay | 3 | Read-only |
| `s` | Report overlay | 3 | Read-only |
| `/` | Search/filter tasks | 4 | UX |
| `f` | Cycle status filter | 4 | UX |
| `n` / `N` | Jump to next/prev error | 4 | Navigation |

Current `r` (force refresh) is unchanged. `t` (retry) was chosen over `r` to avoid conflict.

---

## 4. Sprint 1: Single-Task Recovery (retry, reset, resume)

**Priority:** Highest. This is the #1 workflow after seeing a failure in the TUI.

### F1.1 Retry Selected Task (`t`)

**Description:** From the main view, pressing `t` on a selected task retries it — equivalent to `kiln retry --task-id <id>`.

**Preconditions:**
- Task status must be `failed`, `not_complete`, or `complete` (allow re-running completed tasks).
- If task is `pending` or `blocked`, show a brief error toast: "Cannot retry: task is <status>".
- If task is `running`, show: "Cannot retry: task is already running".

**Behavior:**
1. Show confirmation modal listing: task ID, current status, what will be removed (done marker, log archive, state entry).
2. On confirm: spawn `kiln retry --task-id <id> --tasks <tasksFile>` as subprocess.
3. Capture output. On completion, refresh state.
4. Show toast: "Retried: <id>" (green) or "Retry failed: <error>" (red).

**Keybinding:** `t` (mnemonic: re**t**ry)

**Tests:**
- Pressing `t` on a failed task shows confirmation modal.
- Pressing `t` on a pending task shows error toast.
- Confirming the modal transitions task to pending/running.
- Pressing Escape on the modal cancels without side effects.

---

### F1.2 Reset Selected Task (`R`)

**Description:** From the main view, pressing `R` (shift+r) on a selected task resets it — equivalent to `kiln reset --task-id <id>`.

**Preconditions:**
- Task can be in any state except `running`.
- If task is `running`, show: "Cannot reset: task is running".

**Behavior:**
1. Show confirmation modal listing: task ID, what will be removed (done marker, log archived to .bak, state entry cleared).
2. On confirm: spawn `kiln reset --task-id <id> --tasks <tasksFile>`.
3. Refresh state. Task should appear as `pending` or `blocked` (depending on deps).
4. Show toast: "Reset: <id>".

**Keybinding:** `R` (shift for destructive action)

**Tests:**
- Pressing `R` on a complete task shows confirmation modal.
- Confirming resets task to pending.
- Pressing `R` on a running task shows error toast.

---

### F1.3 Resume Selected Task (`e`)

**Description:** From the main view, pressing `e` on a `not_complete` task shows its prior execution context — equivalent to `kiln resume --task-id <id>`.

**Preconditions:**
- Task status must be `not_complete`. For other statuses, show error toast: "Cannot resume: task is <status>".

**Behavior:**
1. No confirmation needed (read-only display).
2. Run `kiln resume --task-id <id> --tasks <tasksFile>` and capture output.
3. Display output in a scrollable overlay (similar to the detail view but with resume context: attempt count, last status, last error, original prompt excerpt).
4. Press Escape or `q` to dismiss.

**Keybinding:** `e` (mnemonic: r**e**sume)

**Tests:**
- Pressing `e` on a not_complete task opens resume overlay.
- Pressing `e` on a pending task shows error toast.
- Resume overlay is scrollable and dismissible.

---

### Sprint 1 Acceptance Criteria

- [ ] Confirmation modal component implemented (reusable for all actions).
- [ ] Toast notification component implemented (auto-dismiss, color-coded).
- [ ] `t` retries selected task with confirmation.
- [ ] `R` resets selected task with confirmation.
- [ ] `e` shows resume context for not_complete tasks.
- [ ] Error toasts shown for invalid state transitions.
- [ ] Help overlay (`?`) updated with new keybindings.
- [ ] All existing tests pass (no regressions).
- [ ] New tests cover modal, toast, and each action.

**Estimated complexity:** Medium. Requires confirmation modal component, subprocess execution, and toast notifications — all new UI primitives that later sprints reuse.

---

## 5. Sprint 2: Execute Task from TUI

**Priority:** High. The second most common workflow — "I see a pending task, I want to run it now."

### F2.1 Execute Selected Task (`x`)

**Description:** From the main view, pressing `x` on a selected task executes it — equivalent to `kiln exec --task-id <id> --tasks <tasksFile>`.

**Preconditions:**
- Task status must be `pending`. Blocked tasks show: "Cannot execute: blocked by <unmet deps>". Running tasks show: "Already running". Complete tasks show: "Already complete (use R to reset first)".

**Behavior:**
1. Show confirmation modal: task ID, prompt file path, model (resolved via profile), timeout.
2. On confirm: spawn `kiln exec --task-id <id> --tasks <tasksFile>` as a background subprocess.
3. Task immediately transitions to `running` (yellow) on next refresh.
4. Output is captured and viewable via the log events view (`l`).
5. On completion: refresh state, show toast with outcome (complete/failed/not_complete).

**Keybinding:** `x` (mnemonic: e**x**ecute)

**Edge cases:**
- If another `kiln exec` is already running the same task (lock conflict), the subprocess fails with exit code 10. The TUI shows: "Lock conflict: another process is running <id>".
- If the user presses `x` again on a now-running task, show: "Already running".

**Tests:**
- Pressing `x` on a pending task shows confirmation modal.
- Pressing `x` on a blocked task shows error toast with unmet dep names.
- Confirming spawns a subprocess (mock via test helper pattern).
- Subprocess completion triggers state refresh.

---

### F2.2 Live Execution Tracking

**Description:** While a task is executing (spawned from the TUI), the log events view (`l`) live-streams subprocess output.

**Behavior:**
1. When the user presses `l` on a running task that was launched from this TUI session, show captured output in real time (not just polling the log file — direct pipe capture).
2. For tasks launched externally (via `make all`), fall back to the existing polling-based log file view.
3. A subtle indicator in the task row shows "launched from TUI" vs "external" (e.g., a `*` suffix on the status).

**Tests:**
- Log view for TUI-launched task shows live captured output.
- Log view for externally-launched task falls back to file-based polling.

---

### Sprint 2 Acceptance Criteria

- [ ] `x` executes selected pending task with confirmation.
- [ ] Subprocess runs in background; TUI remains responsive.
- [ ] Live output capture for TUI-launched tasks.
- [ ] Error toasts for invalid state transitions (blocked, running, complete).
- [ ] Lock conflict errors displayed gracefully.
- [ ] Help overlay updated.
- [ ] Tests cover execute flow, precondition checks, and output capture.

**Estimated complexity:** High. Background subprocess management, live output piping, and concurrent state updates require careful goroutine handling within the Bubble Tea event loop.

---

## 6. Sprint 3: Unify, Verify, and Report Overlays

**Priority:** Medium. These actions provide valuable context without the user leaving the TUI.

### F3.1 Unify Selected Task (`u`)

**Description:** From the main view, pressing `u` on a completed task generates a closure artifact — equivalent to `kiln unify --task-id <id>`.

**Preconditions:**
- Task must be `complete`. Otherwise show: "Cannot unify: task is <status>".
- If closure artifact already exists (`.kiln/unify/<id>.md`), show confirmation: "Closure artifact already exists. Overwrite?"

**Behavior:**
1. Confirmation modal: task ID, what will be created (`.kiln/unify/<id>.md`).
2. Spawn `kiln unify --task-id <id>` as subprocess.
3. On completion: refresh state. Detail view now shows "Closure: available".
4. Toast: "Closure generated: <id>" or "Unify failed: <error>".

**Keybinding:** `u` (mnemonic: **u**nify)

**Tests:**
- `u` on complete task shows confirmation and spawns unify.
- `u` on pending task shows error toast.
- Existing closure artifact triggers overwrite confirmation.

---

### F3.2 Verify-Plan Overlay (`v`)

**Description:** Pressing `v` from the main view runs `kiln verify-plan` and displays the results in a scrollable overlay.

**Preconditions:** None (always available).

**Behavior:**
1. No confirmation needed (read-only).
2. Run `kiln verify-plan --tasks <tasksFile> --format text` and capture output.
3. Display in a scrollable overlay. Color-code: red for ERROR, yellow for WARNING, green for PASS.
4. Press Escape to dismiss.

**Keybinding:** `v` (mnemonic: **v**erify)

**Tests:**
- `v` opens overlay with verify-plan results.
- Overlay is scrollable and dismissible.
- Error/warning lines are color-coded.

---

### F3.3 Report Overlay (`s`)

**Description:** Pressing `s` from the main view runs `kiln report` and displays an execution summary overlay.

**Preconditions:** None (always available).

**Behavior:**
1. No confirmation needed (read-only).
2. Run `kiln report --format table` and capture output.
3. Display in a scrollable overlay.
4. Press Escape to dismiss.

**Keybinding:** `s` (mnemonic: **s**ummary/report)

**Tests:**
- `s` opens overlay with report output.
- Overlay handles empty log directory gracefully.

---

### Sprint 3 Acceptance Criteria

- [ ] `u` generates closure artifact for completed tasks.
- [ ] `v` displays verify-plan results in scrollable overlay.
- [ ] `s` displays report summary in scrollable overlay.
- [ ] All overlays are dismissible with Escape.
- [ ] Help overlay updated with new keybindings.
- [ ] Tests cover each action, preconditions, and overlay rendering.

**Estimated complexity:** Medium. Reuses subprocess execution and overlay patterns from Sprints 1-2. The overlay component is new but simpler than the confirmation modal (read-only, no input handling beyond dismiss).

---

## 7. Sprint 4: Search, Filter, and Error Navigation

**Priority:** Medium. UX improvements for projects with many tasks (15+).

### F4.1 Search/Filter Tasks (`/`)

**Description:** Pressing `/` opens a search bar at the bottom of the main view. The user types a query; the task list filters in real time to show only matching tasks.

**Behavior:**
1. Search bar appears at the bottom of the screen (replaces keybinding footer).
2. User types a query string. The task list filters to show tasks whose ID, kind, phase, milestone, lane, or last error contains the query (case-insensitive substring match).
3. Summary panel updates to reflect only visible tasks.
4. `Enter` or `Escape` closes the search bar. `Enter` keeps the filter; `Escape` clears it.
5. When a filter is active, a `[filter: <query>]` indicator appears in the footer.

**Keybinding:** `/` (vim-style search)

**Clear filter:** `Escape` from search bar, or `/` followed by `Enter` (empty query).

**Tests:**
- `/` opens search bar, typing filters the list.
- Matching is case-insensitive and substring-based.
- Enter preserves filter; Escape clears it.
- Summary counts update to reflect filtered view.

---

### F4.2 Status Filter Cycling (`f`)

**Description:** Pressing `f` cycles through status filters: all → running → failed → blocked → not_complete → pending → complete → all.

**Behavior:**
1. Each press of `f` advances to the next status filter.
2. Only tasks matching the selected status are shown.
3. Summary panel shows: `[filter: failed]` indicator.
4. The filter persists across refresh ticks.
5. `f` on the last status ("complete") wraps back to "all".

**Keybinding:** `f` (mnemonic: **f**ilter)

**Tests:**
- `f` cycles through statuses.
- Task list shows only matching tasks.
- Filter persists across tick refreshes.

---

### F4.3 Jump to Next/Previous Error (`n` / `N`)

**Description:** Pressing `n` moves the cursor to the next task with status `failed` or `not_complete`. Pressing `N` (shift+n) moves to the previous one.

**Behavior:**
1. From the current cursor position, scan forward (or backward for `N`) through `displayOrder` for the next task with status `failed` or `not_complete`.
2. If found, move the cursor to that task.
3. If no more errors in that direction, show toast: "No more errors" and wrap around to the beginning (or end).

**Keybinding:** `n` / `N` (mnemonic: **n**ext error)

**Tests:**
- `n` jumps to next failed/not_complete task.
- `N` jumps to previous.
- Wraps around at list boundaries.
- Shows toast when no errors exist.

---

### F4.4 Retry All Failed Tasks (`T`)

**Description:** Pressing `T` (shift+t) retries all tasks with `failed` status — equivalent to `kiln retry --failed`.

**Preconditions:**
- At least one task must be `failed`. Otherwise show toast: "No failed tasks to retry".

**Behavior:**
1. Confirmation modal lists all failed task IDs and count.
2. On confirm: spawn `kiln retry --failed --tasks <tasksFile>`.
3. Refresh state. All previously-failed tasks become pending.
4. Toast: "Retried N failed tasks".

**Keybinding:** `T` (shift = batch variant of `t`)

**Tests:**
- `T` with failed tasks shows confirmation modal listing them.
- `T` with no failed tasks shows error toast.
- Confirming resets all failed tasks.

---

### Sprint 4 Acceptance Criteria

- [ ] `/` search bar filters tasks by ID, kind, phase, milestone, lane, last error.
- [ ] `f` cycles through status filters.
- [ ] `n` / `N` jumps to next/previous error task.
- [ ] `T` retries all failed tasks with confirmation.
- [ ] Filter indicators visible in footer/summary.
- [ ] Help overlay updated.
- [ ] Tests cover search, filter cycling, error navigation, and batch retry.

**Estimated complexity:** Medium-High. The search bar requires a text input component (could use `charm.land/bubbletea/v2` text input or a simple rune buffer). Filter state needs to integrate with `computeDisplayOrder`.

---

## 8. Sprint 5: Batch Operations and Advanced Commands

**Priority:** Lower. These are power-user features for large projects.

### F5.1 Execute All Pending Tasks (`X`)

**Description:** Pressing `X` (shift+x) executes all pending tasks — equivalent to `make all -j<parallelism_limit>`.

**Preconditions:**
- At least one task must be `pending`. Otherwise show toast: "No pending tasks".

**Behavior:**
1. Confirmation modal: list of pending task IDs, parallelism limit from profile.
2. On confirm: spawn `make all -j<N>` as subprocess (where N comes from profile's `parallelism_limit`, default 4).
3. Output captured. Tasks transition to `running` as Make starts them.
4. On completion: toast with summary (N complete, M failed).

**Keybinding:** `X` (shift = batch variant of `x`)

---

### F5.2 Unify All Completed Tasks (`U`)

**Description:** Pressing `U` (shift+u) generates closure artifacts for all completed tasks that don't already have one.

**Preconditions:**
- At least one completed task without a closure artifact.

**Behavior:**
1. Confirmation modal: list of tasks that will be unified.
2. Spawn `kiln unify` for each task sequentially (or with controlled parallelism).
3. Toast: "Generated N closure artifacts".

**Keybinding:** `U` (shift = batch variant of `u`)

---

### F5.3 Reset All Tasks (`Ctrl+R`)

**Description:** Pressing `Ctrl+R` resets all tasks — equivalent to `kiln reset --all`.

**Preconditions:** Always available (but requires double confirmation due to destructiveness).

**Behavior:**
1. First confirmation modal: "Reset ALL N tasks? This removes all done markers, archives all logs, and clears state.json."
2. Second confirmation: "Type 'RESET' to confirm." (text input required — prevents accidental double-Enter).
3. On confirm: spawn `kiln reset --all --tasks <tasksFile>`.
4. Refresh state. All tasks become pending/blocked.
5. Toast: "Reset all N tasks".

**Keybinding:** `Ctrl+R` (ctrl = dangerous global action, distinct from `r` refresh)

---

### F5.4 Regenerate Make Targets (`Ctrl+G`)

**Description:** Pressing `Ctrl+G` regenerates `.kiln/targets.mk` — equivalent to `make graph`.

**Preconditions:** None.

**Behavior:**
1. Confirmation modal: "Regenerate targets.mk from tasks.yaml?"
2. Spawn `kiln gen-make --tasks <tasksFile> --out .kiln/targets.mk`.
3. Toast: "Targets regenerated" or error.

**Keybinding:** `Ctrl+G` (mnemonic: **g**raph/**g**en-make)

---

### Sprint 5 Acceptance Criteria

- [ ] `X` executes all pending tasks via Make with confirmation.
- [ ] `U` unifies all completed tasks without existing closure artifacts.
- [ ] `Ctrl+R` resets all tasks with double confirmation (type "RESET").
- [ ] `Ctrl+G` regenerates Make targets.
- [ ] Help overlay updated with batch operations section.
- [ ] Tests cover batch operations, double confirmation, and subprocess management.

**Estimated complexity:** Medium. Reuses patterns from earlier sprints. The double-confirmation for `Ctrl+R` requires a text input within the modal component.

---

## 9. Sprint 6: Command Palette and Configuration

**Priority:** Lowest. Polish and power-user features.

### F6.1 Command Palette (`:`)

**Description:** Pressing `:` opens a command palette at the bottom of the screen (vim-like). The user types a command and presses Enter to execute it.

**Supported commands:**
```
:retry <id>              Retry a specific task
:reset <id>              Reset a specific task
:exec <id>               Execute a specific task
:unify <id>              Generate closure for a task
:filter <query>          Filter tasks (same as /)
:filter clear            Clear filter
:set refresh <duration>  Change refresh interval
:set profile <name>      Change active profile (speed/reliable)
:sort <field>            Sort by: status, id, attempts, duration, kind, phase
:export <path>           Export current view to file (status JSON)
:q                       Quit
```

**Behavior:**
1. `:` opens command input bar at bottom.
2. Tab completion for command names and task IDs.
3. Unknown commands show error toast.
4. Up arrow recalls command history (session-scoped).

**Keybinding:** `:` (vim command mode)

---

### F6.2 Multi-Select and Bulk Actions

**Description:** Allow selecting multiple tasks for batch operations.

**Behavior:**
1. `Space` toggles selection on the current task (visual indicator: `>` prefix or highlight).
2. `a` selects all visible tasks. `A` deselects all.
3. When tasks are selected, action keys (`t`, `R`, `x`, `u`) operate on all selected tasks instead of just the cursor task.
4. Confirmation modal lists all selected task IDs.
5. Selection is cleared after any action completes.

**Keybinding:** `Space` (toggle), `a` (select all), `A` (deselect all)

---

### F6.3 Custom Sort Order

**Description:** Pressing `o` opens a sort picker or cycles through sort modes.

**Sort modes:**
1. Status urgency (default — current behavior)
2. Alphabetical by ID
3. By attempt count (most retries first)
4. By duration (longest first)
5. By definition order (tasks.yaml order)

**Keybinding:** `o` (mnemonic: s**o**rt/**o**rder)

---

### Sprint 6 Acceptance Criteria

- [ ] `:` opens command palette with tab completion.
- [ ] Multi-select with Space, bulk actions on selected tasks.
- [ ] Custom sort modes via `o`.
- [ ] Help overlay updated with all new keybindings.
- [ ] Tests cover command palette parsing, multi-select state, and sort modes.

**Estimated complexity:** High. Command palette requires text input, parsing, tab completion, and command dispatch. Multi-select adds selection state that interacts with all existing action handlers.

---

## 10. Sprint Summary and Recommended Order

| Sprint | Theme | Features | Complexity | Dependencies |
|--------|-------|----------|-----------|--------------|
| **1** | Single-task recovery | Retry (`t`), Reset (`R`), Resume (`e`) | Medium | None — foundational UI primitives |
| **2** | Task execution | Execute (`x`), live output tracking | High | Sprint 1 (subprocess pattern, modal, toast) |
| **3** | Context overlays | Unify (`u`), Verify-plan (`v`), Report (`s`) | Medium | Sprint 1 (modal, subprocess, overlay) |
| **4** | UX and batch retry | Search (`/`), Filter (`f`), Error nav (`n`/`N`), Batch retry (`T`) | Medium-High | Sprint 1 (toast), Sprint 2 (subprocess) |
| **5** | Batch operations | Execute all (`X`), Unify all (`U`), Reset all (`Ctrl+R`), Gen-make (`Ctrl+G`) | Medium | Sprints 1-2 (modal, subprocess, batch pattern) |
| **6** | Power user | Command palette (`:`), Multi-select (`Space`), Sort modes (`o`) | High | Sprints 1-5 (all patterns) |

### Critical path

```
Sprint 1 ──→ Sprint 2 ──→ Sprint 3
   │                          │
   └──→ Sprint 4 ─────→ Sprint 5 ──→ Sprint 6
```

Sprint 1 is the foundation — it establishes the confirmation modal, toast notification, and subprocess execution patterns that every subsequent sprint reuses. Sprint 2 depends on Sprint 1. Sprints 3 and 4 can proceed in parallel after Sprint 1. Sprint 5 requires Sprints 1-2. Sprint 6 requires everything.

---

## 11. Keybinding Reference (Complete — Current + Planned)

### Main View

| Key | Current | Sprint | Action |
|-----|---------|--------|--------|
| `j` / `k` | Existing | — | Move cursor up/down |
| `g` / `G` | Existing | — | Jump to top/bottom |
| `Enter` | Existing | — | Task detail view |
| `l` | Existing | — | Log events view |
| `r` | Existing | — | Force refresh |
| `p` | Existing | — | Toggle phase grouping |
| `m` | Existing | — | Toggle milestone grouping |
| `?` | Existing | — | Help overlay |
| `q` | Existing | — | Quit |
| `Ctrl+C` | Existing | — | Quit (global) |
| `t` | — | **1** | Retry selected task |
| `R` | — | **1** | Reset selected task |
| `e` | — | **1** | Resume selected task |
| `x` | — | **2** | Execute selected task |
| `u` | — | **3** | Unify selected task |
| `v` | — | **3** | Verify-plan overlay |
| `s` | — | **3** | Report overlay |
| `/` | — | **4** | Search/filter tasks |
| `f` | — | **4** | Cycle status filter |
| `n` / `N` | — | **4** | Next/prev error task |
| `T` | — | **4** | Retry all failed |
| `X` | — | **5** | Execute all pending |
| `U` | — | **5** | Unify all completed |
| `Ctrl+R` | — | **5** | Reset all tasks |
| `Ctrl+G` | — | **5** | Regenerate Make targets |
| `:` | — | **6** | Command palette |
| `Space` | — | **6** | Toggle task selection |
| `a` / `A` | — | **6** | Select/deselect all |
| `o` | — | **6** | Cycle sort mode |

---

## 12. Non-Goals

- **`kiln plan` from TUI.** PRD parsing is a one-time setup step. Running it from the TUI adds complexity (needs PRD file path, model selection) with little benefit.
- **`kiln init` from TUI.** Project initialization happens before the TUI is useful. There's nothing to display.
- **`kiln gen-prompts` from TUI.** Prompt generation is a bulk operation typically done once after planning. It could be added in a future sprint if demand warrants.
- **Editing tasks.yaml from TUI.** The TUI is an execution dashboard, not an editor. Users should edit tasks.yaml in their editor and the TUI picks up changes on the next refresh.
- **Remote execution.** The TUI runs locally and manages local state. Remote orchestration is a different product.

---

## 13. Risk Mitigations

### Risk: Accidental state mutation

**Mitigation:** Every state-modifying action requires a confirmation modal. Destructive batch operations (`Ctrl+R` reset all) require typing a confirmation word. The modal clearly lists what will change.

### Risk: Subprocess hangs or crashes

**Mitigation:** All subprocesses inherit the task's timeout. If the subprocess doesn't complete within the timeout, it's killed with SIGTERM (then SIGKILL after 5s). The TUI shows "Timed out" in the toast. A hung subprocess never blocks the TUI's event loop — output capture runs in a separate goroutine.

### Risk: Concurrent state conflicts

**Mitigation:** Kiln's existing lock mechanism (`.kiln/locks/<id>.lock`) prevents two `kiln exec` invocations from running the same task. If the TUI launches an exec and the user also runs `make all` in another terminal, the lock prevents conflicts. The TUI shows "Lock conflict" errors clearly.

### Risk: Keybinding overload

**Mitigation:** Sprint 1 adds only 3 keys. Each subsequent sprint adds 3-5. The help overlay (`?`) is updated with each sprint. The command palette (Sprint 6) provides an alternative discovery mechanism for users who don't memorize keybindings.

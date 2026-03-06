# Task: recovery-ux — Recovery UX (status/resume/retry/reset)

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
recovery-ux

## SCOPE
Implement ONLY the recovery UX commands described below. Do not work on other backlog items (UNIFY, TUI, engine abstraction, prompt chaining, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln already writes per-task JSON logs to `.kiln/logs/<task-id>.json` via `execRunLog` entries (one entry per attempt).
- Kiln creates `.kiln/done/<task-id>.done` markers for Make idempotency on successful completion.
- Exit codes: 0 = success, 2 = not_complete/blocked, 10 = permanent failure, 20 = transient retries exhausted.
- Tasks are defined in `.kiln/tasks.yaml` with fields: id, prompt, needs, timeout (and possibly richer schema fields if #7 has landed).
- If `.kiln/state.json` exists (from backlog item #1), integrate with it for richer status data. If it does not exist, derive status from `.done` markers and log files.
- If error taxonomy fields (`error_class`, `retryable`) exist in log entries (from backlog item #9), use them in status display. If not, fall back to raw error messages.
- If `.kiln/unify/<id>.md` closure artifacts exist (from backlog item #12), `kiln resume` should reference them. If not, skip closure injection gracefully.

## REQUIREMENTS

1. **`kiln status` command** — Display a scoreboard of all tasks from `.kiln/tasks.yaml`:
   - For each task, show: task ID, status, attempt count, last error (if any)
   - Status is derived by checking (in order):
     - `.kiln/state.json` per-task status (if state file exists)
     - `.kiln/done/<id>.done` exists -> `complete`
     - `.kiln/logs/<id>.json` exists with attempts -> check last attempt: `failed`, `not_complete`, or `blocked` based on last log entry status
     - No log file and no done marker -> `pending`
   - Attempt count: number of entries in the task's log file (0 if no log file)
   - Last error: from the most recent log entry's error field (or `error_class` if available), `-` if none
   - Summary line at the bottom: `Total: N | Complete: N | Failed: N | Pending: N | Blocked: N`
   - Group tasks by status category for readability: runnable/pending first, then running, blocked, failed, complete
   - `--format json` flag outputs structured JSON to stdout

2. **`kiln retry` command** — Re-run failed or incomplete tasks:
   - `kiln retry` with no flags retries all tasks that are not `complete` and not `pending`
   - `kiln retry --task-id <id>` retries a specific task
   - `kiln retry --failed` retries only tasks whose last attempt status was `failed`
   - `kiln retry --transient-only` (used with `--failed`) retries only tasks whose last error was classified as retryable (requires error taxonomy fields; if not present, skip this filter and warn)
   - Retry works by invoking `kiln exec` for each matching task (reuse existing exec logic, do not duplicate it)
   - Before retrying, remove the task's `.done` marker if it exists (so Make doesn't skip it)
   - Print which tasks will be retried before executing, e.g.: `Retrying 2 task(s): auth-module, api-routes`
   - If no tasks match the filter, print: `No tasks match retry criteria.`

3. **`kiln reset` command** — Clear execution state for a task so it can be re-run cleanly:
   - `kiln reset --task-id <id>` (required flag, no bulk reset without explicit intent)
   - Removes `.kiln/done/<id>.done` if it exists
   - Removes or archives the task's log file `.kiln/logs/<id>.json` (move to `.kiln/logs/<id>.json.bak` to preserve history)
   - If `.kiln/state.json` exists, clear the task's entry from it
   - Print confirmation: `Reset task: <id> (done marker removed, logs archived)`
   - If the task ID is not found in `tasks.yaml`, print error: `Unknown task: <id>` and exit with code 1
   - `kiln reset --all` resets all tasks (requires confirmation prompt: `Reset all N tasks? [y/N]`)

4. **`kiln resume` command** — Generate a context-enriched prompt wrapper for resuming a previously attempted task:
   - `kiln resume --task-id <id>` (required flag)
   - Reads the task's original prompt from the path in `tasks.yaml`
   - Reads the task's last log entry to extract: last status, last error, attempt count
   - If `.kiln/unify/<id>.md` exists, includes closure artifact content as context
   - Outputs a combined prompt to stdout that includes:
     - A "RESUME CONTEXT" header with attempt history and last error
     - Closure artifact content (if available) under "PREVIOUS CLOSURE SUMMARY"
     - The original task prompt
   - The user can pipe this to Claude or redirect to a file: `kiln resume --task-id auth-module > resume-prompt.md`
   - If the task has no prior attempts (no log file), print: `No prior attempts found for task: <id>. Use 'kiln exec' instead.`
   - If the task ID is not found in `tasks.yaml`, print error: `Unknown task: <id>` and exit with code 1

5. **Wire all commands into the CLI** — Register `status`, `retry`, `reset`, and `resume` subcommands alongside existing `exec` and `gen-make` using the same pattern (flag set, argument parsing, etc.).

6. **Output format for `kiln status`** — Human-readable table example:
   ```
   Task               Status        Attempts  Last Error
   ----               ------        --------  ----------
   setup-db           pending       0         -
   auth-module        failed        3         timeout: context deadline exceeded
   api-routes         not_complete  2         claude_exit: exit code 1
   cache-layer        complete      1         -

   Summary
   -------
   Total: 4 | Complete: 1 | Failed: 1 | Not Complete: 1 | Pending: 1
   ```

## Tests

- `kiln status` reads `tasks.yaml` and displays all tasks with correct derived status
- `kiln status` derives `complete` from `.done` marker when no state file exists
- `kiln status` derives `pending` when no `.done` and no log file exist
- `kiln status` derives `failed`/`not_complete`/`blocked` from last log entry
- `kiln status` shows correct attempt counts from log file entries
- `kiln status` handles missing `.kiln/logs/` directory gracefully
- `kiln status --format json` produces valid structured JSON output
- `kiln status` summary line counts are correct
- `kiln retry` identifies and retries non-complete, non-pending tasks
- `kiln retry --task-id <id>` retries only the specified task
- `kiln retry --failed` retries only failed tasks
- `kiln retry` removes `.done` marker before retrying
- `kiln retry` prints "No tasks match retry criteria." when no tasks match
- `kiln reset --task-id <id>` removes `.done` marker and archives log file
- `kiln reset --task-id <id>` errors on unknown task ID
- `kiln reset --all` resets all tasks
- `kiln resume --task-id <id>` outputs combined prompt with resume context
- `kiln resume --task-id <id>` includes closure artifact when `.kiln/unify/<id>.md` exists
- `kiln resume --task-id <id>` works without closure artifact
- `kiln resume` errors when no prior attempts exist
- `kiln resume` errors on unknown task ID
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln status` displays a table of all tasks with status, attempts, and last error
- `kiln status` correctly derives status from `.done` markers, log files, and state file (if present)
- `kiln status --format json` outputs structured JSON
- `kiln status` shows a summary line with counts by status
- `kiln retry` re-runs tasks matching filter criteria via existing exec logic
- `kiln retry --failed` and `--transient-only` filters work correctly
- `kiln retry` removes `.done` markers before re-execution
- `kiln reset --task-id <id>` removes done marker and archives log file
- `kiln reset` validates task ID against `tasks.yaml`
- `kiln resume --task-id <id>` outputs a context-enriched prompt combining resume context, closure artifacts (if available), and the original prompt
- `kiln resume` errors gracefully when task has no prior attempts or is unknown
- All four subcommands are registered in the CLI alongside `exec` and `gen-make`
- Existing `kiln exec` and `kiln gen-make` behavior is unchanged
- `go test ./...` passes
- No large refactors unrelated to recovery UX

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"recovery-ux"}}

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
{"kiln":{"status":"complete","task_id":"recovery-ux"}}

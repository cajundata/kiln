# Task: error-taxonomy — Error Taxonomy & Reporting Beyond Exit Codes

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
error-taxonomy

## SCOPE
Implement ONLY the error taxonomy, classification, and reporting feature described below. Do not work on other backlog items (state resumability, UNIFY, TUI, recovery UX, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln already has error types: `timeoutError`, `claudeExitError`, `footerError` with an `isRetryable()` method that decides retry eligibility.
- Kiln already writes per-task JSON logs to `.kiln/logs/<task-id>.json` via `execRunLog` entries (one entry per attempt).
- Exit codes are already defined: 0 = success, 2 = not_complete/blocked, 10 = permanent failure, 20 = transient retries exhausted.
- If `.kiln/state.json` exists (from backlog item #1), it may contain per-task status — integrate with it if present, but do not create it if it doesn't exist.

## REQUIREMENTS

1. **Standardized error classification** — Define a canonical set of error classes as string constants:
   - `"timeout"` — task exceeded its time limit (maps to existing `timeoutError`)
   - `"claude_exit"` — Claude process exited with a non-zero code (maps to existing `claudeExitError`)
   - `"footer_parse"` — failed to parse the JSON footer from Claude output (maps to existing `footerError` parse failures)
   - `"footer_validation"` — footer parsed but contained invalid/unexpected values (maps to existing `footerError` validation failures)
   - `"lock_conflict"` — task lock already held (if concurrency safety feature exists)
   - `"schema_validation"` — task schema or config validation failure
   - `"unknown"` — catch-all for unclassified errors
   - Each class must have a `retryable` boolean property (timeout and claude_exit are retryable; footer_parse, footer_validation, lock_conflict, schema_validation are not).

2. **Classify errors in log entries** — Update `execRunLog` (or equivalent log struct) to include:
   - `error_class` (string): one of the canonical error classes above
   - `error_message` (string): human-readable error description (already exists as `error` field — keep backward compatibility)
   - `retryable` (bool): whether this error class is retryable
   - Populate these fields by adding a `classify(err error) (errorClass string, retryable bool)` function that inspects the error type and returns the appropriate class and retryability.

3. **`kiln report` command** — Add a new subcommand that summarizes execution across all tasks:
   - Reads all files in `.kiln/logs/` directory
   - For each task, report: task ID, final status (complete/failed/not_complete/blocked), attempt count, last error class (if any), last error message (if any)
   - Summary section at the bottom:
     - Total tasks, completed, failed, not_complete, blocked
     - Top error classes with counts (e.g., `timeout: 3, claude_exit: 2`)
     - Total attempts across all tasks
   - Default output is human-readable table format to stdout
   - `--format json` flag outputs the full report as structured JSON to stdout
   - If `.kiln/logs/` is empty or doesn't exist, print a message: `"No execution logs found in .kiln/logs/"`

4. **Consistent error classification in exec flow** — Update the existing `kiln exec` error handling:
   - Where errors are caught and logged, call `classify()` to populate the new log fields
   - Ensure every log entry written has `error_class` set (use `"unknown"` as fallback)
   - Successful attempts should have empty/omitted error fields (not "unknown")
   - Do not change existing exit code behavior — error classes are metadata, not flow control

5. **Report output format** — Human-readable table example:
   ```
   Task               Status        Attempts  Last Error Class  Last Error
   ----               ------        --------  ----------------  ----------
   setup-db           complete      1         -                 -
   auth-module        failed        3         timeout           context deadline exceeded
   api-routes         not_complete  2         claude_exit       exit code 1

   Summary
   -------
   Total: 3 | Complete: 1 | Failed: 1 | Not Complete: 1 | Blocked: 0
   Attempts: 6
   Top errors: timeout (1), claude_exit (1)
   ```

6. **Wire `kiln report` into the CLI** — Register the `report` subcommand alongside existing `exec` and `gen-make` subcommands using the same pattern (flag set, argument parsing, etc.).

## Tests

- `classify()` returns correct class and retryability for each error type (`timeoutError`, `claudeExitError`, `footerError`, generic error)
- `classify()` returns `"unknown", false` for unrecognized error types
- `classify()` distinguishes footer parse vs footer validation errors (if distinguishable in codebase)
- Log entries from `kiln exec` include `error_class` and `retryable` fields on failure
- Log entries from successful attempts do not include error classification fields (or they are empty)
- `kiln report` reads log files and produces correct summary
- `kiln report` handles empty/missing `.kiln/logs/` gracefully
- `kiln report --format json` produces valid structured JSON output
- `kiln report` correctly counts attempts per task
- `kiln report` correctly identifies top error classes
- `kiln report` human-readable output includes all required columns
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- Canonical error classes are defined as constants with retryable properties
- `classify()` function correctly maps existing error types to error classes
- `execRunLog` entries include `error_class` and `retryable` fields when errors occur
- `kiln report` subcommand exists and reads `.kiln/logs/` to produce a summary
- `kiln report` shows per-task status, attempt counts, and last error class
- `kiln report` shows aggregate summary (totals, top error classes, total attempts)
- `kiln report --format json` outputs structured JSON
- `kiln report` handles empty/missing log directory gracefully
- Existing exit code behavior is unchanged
- Existing log format is backward-compatible (new fields are additive)
- `go test ./...` passes
- No large refactors unrelated to error taxonomy

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"error-taxonomy"}}

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
{"kiln":{"status":"complete","task_id":"error-taxonomy"}}

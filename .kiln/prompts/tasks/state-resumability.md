# Task: state-resumability — Introduce .kiln/state.json as a first-class state manifest

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
state-resumability

## SCOPE
Implement ONLY the state manifest feature described below. Do not work on other backlog items (richer task schema, concurrency safety, error taxonomy, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- `execRunLog` already captures per-attempt data (task_id, started_at, ended_at, duration_ms, model, exit_code, status, footer, events).
- `.done` markers handle Make idempotency but don't track attempt history, failure reasons, or enable safe resumption.
- The state file aggregates across attempts and persists between runs, complementing (not replacing) `.done` markers.

## REQUIREMENTS

1. **Define the state schema** — Add Go structs for the state manifest:
   - `StateManifest`: top-level struct with a map of task ID to `TaskState`, plus a `LastUpdated` timestamp.
   - `TaskState`: per-task state with fields:
     - `Status` (string): one of `pending`, `running`, `completed`, `failed`, `blocked`
     - `Attempts` (int): total attempt count
     - `LastAttemptAt` (time.Time, omitempty): timestamp of most recent attempt
     - `LastError` (string, omitempty): error message from last failed attempt
     - `LastErrorClass` (string, omitempty): error classification (`timeout`, `claude_exit`, `footer_invalid`, `permanent`)
     - `CompletedAt` (time.Time, omitempty): timestamp of successful completion
     - `DurationMs` (int64, omitempty): duration of last successful execution in milliseconds
     - `Model` (string, omitempty): model used in last execution
     - `Notes` (string, omitempty): notes from kiln footer on completion

2. **State file I/O** — Implement functions:
   - `loadState(path string) (*StateManifest, error)` — Read and unmarshal `.kiln/state.json`. Return empty manifest (not error) if file does not exist.
   - `saveState(path string, state *StateManifest) error` — Marshal and atomically write state (write to `.tmp`, then rename). Update `LastUpdated` before writing.

3. **Integrate with `kiln exec`** — Update the exec flow to read/write state:
   - Before execution: load state, set task status to `running`, increment `Attempts`, set `LastAttemptAt`, save state.
   - After successful execution (status=complete): set status to `completed`, populate `CompletedAt`, `DurationMs`, `Model`, `Notes`, clear `LastError`/`LastErrorClass`, save state.
   - After failed execution: set status to `failed` (or `blocked` if footer says blocked), populate `LastError` and `LastErrorClass` based on error type, save state.
   - State path: `.kiln/state.json` (relative to working directory, same convention as `.kiln/logs/`).

4. **Error classification helper** — Add `classifyError(err error) string` that returns:
   - `"timeout"` for `*timeoutError`
   - `"claude_exit"` for `*claudeExitError`
   - `"footer_invalid"` for `*footerError`
   - `"permanent"` for all other errors

5. **`kiln status` enhancement** — If a `kiln status` command exists, update it to read from `.kiln/state.json` and display per-task state (status, attempts, last error). If `kiln status` does not exist yet, add a minimal version that:
   - Loads tasks.yaml and state.json
   - Prints a table: task ID | status | attempts | last error (truncated)
   - Exits 0

6. **Resume flags on `kiln exec`** (optional stretch, implement if time allows):
   - `--resume`: skip tasks already in `completed` state in state.json
   - `--retry-failed`: re-run only tasks in `failed` state
   - `--reset`: clear state.json before running

7. **Do not break `.done` markers** — Make still uses `.done` files for idempotency. State.json is complementary, not a replacement.

## Tests

- `loadState` returns empty manifest when file does not exist
- `loadState` correctly deserializes a valid state.json
- `loadState` returns error on malformed JSON
- `saveState` writes valid JSON and `LastUpdated` is populated
- `saveState` uses atomic write (tmp + rename)
- `classifyError` returns correct class for each error type
- State transitions during exec: pending -> running -> completed
- State transitions during exec: pending -> running -> failed (with error info)
- State transitions during exec: pending -> running -> blocked (footer says blocked)
- Attempt count increments across retries
- `kiln status` reads state.json and prints task states
- `kiln status` works with empty/missing state.json (shows all tasks as pending)
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `.kiln/state.json` is written after each `kiln exec` invocation with correct per-task state
- `loadState` and `saveState` functions exist and handle missing file, valid JSON, and malformed JSON
- `classifyError` correctly maps each error type to its classification string
- `kiln exec` updates state to `running` before execution and to `completed`/`failed`/`blocked` after
- Attempt count increments on each retry
- `kiln status` displays task states from state.json
- `.done` marker behavior is unchanged
- `go test ./...` passes
- No large refactors unrelated to state management

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"state-resumability"}}

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
{"kiln":{"status":"complete","task_id":"state-resumability"}}

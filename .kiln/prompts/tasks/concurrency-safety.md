# Task: concurrency-safety — Concurrency Safety & Duplicate-Execution Prevention

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
concurrency-safety

## SCOPE
Implement ONLY the concurrency safety and duplicate-execution prevention feature described below. Do not work on other backlog items (state resumability, richer task schema, error taxonomy, UNIFY, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln depends on Make parallelism (`make -jN`) but MVP has no guards against concurrent execution of the same task.
- Two `kiln exec --task-id foo` invocations running simultaneously could corrupt log files, produce race conditions on `.done` markers, or cause duplicate work.
- If `.kiln/state.json` exists (from backlog item #1), the state file is also a concurrency-sensitive resource.

## REQUIREMENTS

1. **Task-level lock file** — Implement a file-based locking mechanism:
   - Lock directory: `.kiln/locks/`
   - Lock file path: `.kiln/locks/<task-id>.lock`
   - Lock file contents: JSON with `pid` (int), `started_at` (RFC3339 timestamp), and `hostname` (string) for diagnostics.
   - Use exclusive file creation (`os.OpenFile` with `O_CREATE|O_EXCL`) for atomic lock acquisition — do not use `flock` or advisory locks.
   - Lock must be acquired before any execution begins (before retry loop).
   - Lock must be released (file removed) when execution completes, fails permanently, or is interrupted — use `defer` for cleanup.

2. **Lock acquisition function** — `acquireLock(locksDir string, taskID string) (func(), error)`:
   - Creates `.kiln/locks/` directory if it doesn't exist (`os.MkdirAll`).
   - Attempts to create the lock file atomically.
   - If lock file already exists: read its contents, report the PID/hostname/timestamp of the holder, and return a descriptive error (do NOT retry or wait — fail fast).
   - On success: return a cleanup function that removes the lock file, and nil error.
   - The cleanup function must be safe to call multiple times (idempotent).

3. **Stale lock detection** — Handle the case where a previous `kiln exec` crashed without releasing its lock:
   - `--force-unlock` flag on `kiln exec`: if set, remove an existing lock file before attempting acquisition. Log a warning when force-unlocking.
   - When a lock conflict is detected, the error message should include the PID from the lock file and suggest `--force-unlock` if the process is no longer running.

4. **Integrate with `kiln exec`** — Update the exec flow:
   - Acquire the task lock immediately after parsing flags and validating the task ID.
   - Defer the lock release function.
   - If lock acquisition fails, exit with a non-zero exit code and a clear error message (e.g., `"task foo is already locked by PID 12345 (started 2026-03-06T10:00:00Z on hostname). Use --force-unlock if the process is no longer running."`).
   - Exit code for lock conflict: use exit code 10 (permanent failure — not retryable).

5. **Atomic log file writes** — Ensure log file writes are safe under concurrency:
   - When appending to `.kiln/logs/<task-id>.json`, write to a temporary file first, then rename (atomic write) OR use `O_APPEND` with a single `Write` call containing the complete JSON entry.
   - If log entries are currently written incrementally across multiple writes, consolidate into a single atomic write per attempt.

6. **`.kiln/locks/` directory conventions**:
   - `kiln gen-make` should create the `.kiln/locks/` directory as part of its setup (alongside `.kiln/done/` and `.kiln/logs/`).
   - Lock files are ephemeral — they should NOT be committed to git. Add `.kiln/locks/` to the project's `.gitignore` if one exists, or document the convention.

7. **Signal handling for lock cleanup** — Register a signal handler for `SIGINT` and `SIGTERM` that ensures the lock file is removed on interrupt. Use `os/signal.Notify` with a goroutine that calls the cleanup function and then exits.

## Tests

- `acquireLock` creates a lock file with correct JSON contents (pid, started_at, hostname)
- `acquireLock` returns error when lock file already exists
- `acquireLock` error message includes PID and hostname from existing lock
- Cleanup function removes the lock file
- Cleanup function is idempotent (calling twice does not error)
- `--force-unlock` removes existing lock before acquisition
- Lock is acquired before execution begins in `kiln exec`
- Lock is released after successful execution
- Lock is released after failed execution
- Lock is released after timeout
- Lock conflict exits with code 10
- Lock file directory is created if it doesn't exist
- Atomic log writes: concurrent appends do not produce corrupted JSON
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `acquireLock` and its cleanup function exist and work correctly (atomic create, idempotent cleanup)
- Concurrent `kiln exec` invocations for the same task ID fail fast with a clear lock-conflict error
- Lock file contains diagnostic JSON (pid, started_at, hostname)
- `--force-unlock` flag allows overriding a stale lock with a warning
- Lock is always released on success, failure, timeout, and interrupt (signal handling)
- Log file writes are atomic (no partial/corrupted entries under concurrency)
- `.kiln/locks/` directory is created by `kiln gen-make` setup
- Exit code 10 is used for lock conflicts (permanent failure, not retryable)
- `.done` marker behavior is unchanged
- `go test ./...` passes
- No large refactors unrelated to concurrency safety

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"concurrency-safety"}}

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
{"kiln":{"status":"complete","task_id":"concurrency-safety"}}

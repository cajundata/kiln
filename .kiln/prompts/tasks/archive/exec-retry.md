# Task: Implement retry behavior inside `kiln exec`

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

---

## Context

We want retry to exist inside kiln (not as shell glue). Today, timeouts and non-zero exits can happen, and we want an ergonomic way to retry a task execution with bounded attempts.

This should work for:

- `kiln exec --prompt-file ...`
- `kiln exec --tasks ... --task-id ...`

---

## Requirements

### 1) CLI

Add flags to `kiln exec`:

- `--retries <n>` (default 0)
- `--retry-backoff <duration>` (default 0; e.g., "250ms", "1s")

Behavior:

- On retryable failures, retry up to N times (total attempts = 1 + retries)
- Sleep `retry-backoff` between attempts (constant backoff is fine for now)

### 2) Retryable vs non-retryable

Define retryable errors as:

- timeout errors (your existing timeout error type)
- claude invocation failed due to non-zero exit code

Non-retryable errors include:

- invalid flags / missing required args
- unable to read prompt file / tasks file
- YAML parse errors
- task-id not found in tasks.yaml

### 3) Logging

Continue to write `.kiln/logs/<task-id>.json` as you do now, but include a small header line or log entry per attempt indicating attempt number (1..N).

If you prefer to keep the log format untouched, you may instead write attempt notes to stderr, but tests should verify retries occurred.

### 4) Done markers

Only write `.kiln/done/<task-id>.done` if the final outcome is success.

### 5) Exit codes

- success: exit 0 (no change)
- failures: preserve your existing exit code conventions (e.g., timeout exit code 20) if already implemented.
- If retries exhausted, exit with the code that corresponds to the final failure type.

---

## Acceptance Criteria

- `kiln exec ... --retries 2` performs up to 3 attempts for retryable failures.
- Retries do not happen for non-retryable validation errors.
- Done marker only exists after a successful execution.
- Unit tests cover:
  - retries on timeout
  - retries on non-zero claude exit
  - no retries on missing files / parse errors
  - backoff respected (can be tested by injecting/simulating sleeper if needed)

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"exec-retry","notes":"Added retries/backoff to kiln exec for retryable failures; preserves logging and only writes done marker on success"}}
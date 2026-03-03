# Task: Add retry with exponential backoff to kiln exec

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln exec` can invoke the `claude` CLI with a `--timeout` flag. When a transient failure occurs (timeout, network error), we want automatic retries with exponential backoff and jitter.

---

## Requirements

### 1. Add --max-retries flag

- Add a `--max-retries` flag (default: `3`).
- Add a `--backoff` flag (default: `exponential`; only `exponential` is supported for MVP).

### 2. Retry logic

- On transient failure (exit code `20` from timeout, or claude process returning non-zero), retry up to `--max-retries` times.
- Use exponential backoff with jitter between retries.
- Each retry attempt should be logged to `.kiln/logs/<task-id>.json`.

### 3. Exit codes after retries

- If all retries are exhausted: exit `20`.
- If any attempt succeeds: use the success exit code from that attempt.

---

## Acceptance Criteria

- A transient failure triggers automatic retries up to the configured limit.
- Backoff increases exponentially between attempts.
- Each attempt is logged.
- Exit code `20` when all retries exhausted.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"exec-retry","notes":"added retry with exponential backoff and jitter"}}

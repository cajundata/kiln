# Task: Add --timeout flag to kiln exec

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln exec` currently invokes the `claude` CLI and streams output to stdout + a log file. It does not enforce any time limit on the Claude invocation.

---

## Requirements

### 1. Add --timeout flag

- Add a `--timeout` flag to the `exec` subcommand (default: `5m`).
- Accept Go duration strings (e.g., `5m`, `30s`, `2m30s`).

### 2. Enforce hard kill on timeout

- Use `context.WithTimeout` (or equivalent) to enforce the deadline.
- If the `claude` process exceeds the timeout, kill it and exit with status code `20` (transient failure).

### 3. Log timeout events

- When a timeout occurs, write a log entry to `.kiln/logs/<task-id>.json` indicating the timeout.

---

## Acceptance Criteria

- `kiln exec --task-id foo --prompt-file p.md --timeout 10s` kills the process after 10 seconds.
- Exit code is `20` on timeout.
- The log file records the timeout event.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"exec-timeout","notes":"added --timeout with hard kill and exit 20"}}

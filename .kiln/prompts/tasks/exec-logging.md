# Task: Structured logging for kiln exec

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln exec` writes raw claude output to `.kiln/logs/<task-id>.json`. We need structured logging that records all attempts, timing, and the final classification.

---

## Requirements

### 1. Structured log format

Each log file at `.kiln/logs/<task-id>.json` should contain a JSON object with:
- `task_id`: the task identifier
- `attempts`: array of attempt objects, each with:
  - `attempt`: attempt number (1-based)
  - `started_at`: ISO 8601 timestamp
  - `ended_at`: ISO 8601 timestamp
  - `exit_code`: the raw exit code from the claude process
  - `timed_out`: boolean
- `final_status`: the parsed footer status (or `"unknown"` if no footer)
- `final_exit_code`: the exit code kiln used

### 2. Always write logs

- Logs must be written on every run, even on failure.
- Each retry attempt appends to the attempts array.

---

## Acceptance Criteria

- `.kiln/logs/<task-id>.json` contains valid structured JSON after every run.
- All attempt details (timing, exit codes, timeout status) are recorded.
- The log includes the final classification.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"exec-logging","notes":"implemented structured JSON logging with attempt tracking"}}

# Task: status-command — kiln status for task progress visibility

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Implement `kiln status --tasks <path>` that reads tasks.yaml, checks done markers, and prints a table showing each task's status (done/runnable/blocked) with a summary line.

## CLI
```bash
kiln status --tasks .kiln/tasks.yaml
```

## Requirements

1. Parse `--tasks` flag (required).
2. Load tasks via existing `loadTasks()`.
3. Build done set by stat-ing `.kiln/done/{id}.done` for each task.
4. Classify each task:
   - **done**: `.done` marker exists
   - **blocked**: has unfinished dependency
   - **runnable**: not done and all deps satisfied
5. Print table with columns: TASK | STATUS | NEEDS
6. Print summary: "X/Y tasks done, Z runnable"

## Edge cases
- `.kiln/done/` directory doesn't exist → all tasks show as not-done
- `needs` references an ID not in tasks.yaml → that dep never appears in doneSet, task stays blocked

## Tests
- Missing `--tasks` flag → error
- Tasks file not found → error
- All done → correct counts
- None done with deps → correct runnable/blocked classification
- Partial done → correct classification
- Output contains TASK and STATUS headers
- Dispatch via `run(["status", ...])` returns 0

## Acceptance criteria
- `go test ./...` passes
- `kiln status --tasks .kiln/tasks.yaml` prints correct table
- Wire into `run()` dispatch

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"status-command","notes":"Implemented kiln status command with task progress visibility."}}

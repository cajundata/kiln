# Task: exec-logging — structured logs, footer capture, and status-friendly output

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Upgrade `kiln exec` logging so that:
- Each run produces a single JSON log file that is easy to consume.
- The run captures the final JSON footer and validates it.
- `kiln status` (later) can rely on `.kiln/done/*.done` and optionally cross-check logs.

This task assumes:
- A `.kiln/logs/<task-id>.json` log file already exists in minimal form.
- `.kiln/done/<task-id>.done` is created only when a valid footer is detected (per your policy).

## Log file format (v1)
Write JSON with this top-level shape:

```json
{
  "task_id": "exec-footer",
  "started_at": "RFC3339",
  "ended_at": "RFC3339",
  "duration_ms": 1234,
  "model": "claude-sonnet-4-6",
  "prompt_file": ".kiln/prompts/tasks/exec-footer.md",
  "exit_code": 0,
  "status": "success|timeout|error",
  "footer": { "kiln": { "status": "complete", "task_id": "exec-footer", "notes": "..." } },
  "footer_valid": true,
  "events": [
    { "ts": "RFC3339", "type": "stdout", "line": "..." },
    { "ts": "RFC3339", "type": "stderr", "line": "..." }
  ]
}
```

Notes:
- Keep it compact; limit event buffering if necessary (but don’t drop footer parsing).
- If you already store the raw Claude stream-json, you may store it under a separate key, but keep `events` usable.

## Footer handling rules
1. Footer is expected to be the final JSON object printed by the model to stdout.
2. Footer must parse as JSON and contain:
   - `kiln.status == "complete"`
   - `kiln.task_id` matches the current task id
3. If footer is missing/invalid:
   - `footer_valid=false`
   - do not create `.kiln/done/<task>.done` (ensure logging matches this)
4. If command times out:
   - `status="timeout"`, `exit_code` should reflect your timeout exit code policy
   - include an error message field if you already have one

## Implementation notes
- Capture stdout/stderr streams.
- Continue writing to console as today, but ensure log output is always written even on errors/timeouts.
- Make log writes atomic: write to temp then rename to `.kiln/logs/<task-id>.json`.

## Tests
Add tests covering:
- success run with valid footer -> footer_valid=true and log contains footer object
- success run but missing footer -> footer_valid=false
- footer task_id mismatch -> footer_valid=false
- timeout -> status=timeout with footer_valid=false
- log file always created

Use the existing fake helper process pattern if present.

## Acceptance criteria
- `go test ./...` passes
- logs are valid JSON and include footer parsing results
- behavior matches policy: `.done` only created when footer is valid

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"exec-logging","notes":"Implemented structured exec logs with validated footer capture."}}

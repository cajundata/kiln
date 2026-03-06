# Task: json-output — Machine-Readable Result Output Mode

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
json-output

## SCOPE
Implement ONLY the machine-readable output mode (`--format`) for `kiln exec` and `kiln gen-make`. Do not work on other backlog items (state resumability, richer task schema, TUI, error taxonomy, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln already writes structured JSON logs to `.kiln/logs/<task-id>.json`, but has no stdout-based structured output mode for CI/automation consumption.
- Current output is human-readable text via `fmt.Fprintf` to stdout/stderr.
- The `run()` function dispatches to `runExec()` and `runGenMake()` which accept `io.Writer` for stdout.
- `runExec` returns `(int, error)` where int is the exit code. `runGenMake` returns `error`.

## REQUIREMENTS

1. **`--format` flag for `kiln exec`** — Add a `--format` flag accepting `text` (default) or `json`:
   - `--format text` — current behavior, no changes to existing output.
   - `--format json` — on completion (success or failure), emit a single JSON object to stdout summarizing the execution result. No other text output to stdout in json mode (warnings/progress go to stderr only).
   - The JSON output schema for `kiln exec --format json` must include:
     - `task_id` (string)
     - `status` (string: "complete", "not_complete", "blocked", "timeout", "error")
     - `exit_code` (int)
     - `model` (string)
     - `prompt_file` (string)
     - `started_at` (string, RFC3339)
     - `ended_at` (string, RFC3339)
     - `duration_ms` (int64)
     - `attempts` (int — total attempts including retries)
     - `footer` (object or null — the parsed kiln footer if valid)
     - `footer_valid` (bool)
     - `error` (string or null — error message if failed)
   - The JSON must be a single line (compact, no pretty-printing) to make line-based parsing easy.
   - In json mode, stream claude's stdout/stderr output to stderr (so the user can still watch progress), and reserve stdout exclusively for the final JSON result.

2. **`--format` flag for `kiln gen-make`** — Add a `--format` flag accepting `text` (default) or `json`:
   - `--format text` — current behavior: writes `.kiln/targets.mk` and prints a success message.
   - `--format json` — emit a JSON object to stdout with:
     - `tasks_count` (int — number of tasks processed)
     - `output_file` (string — path to the generated targets.mk)
     - `targets` (array of objects, each with `task_id`, `target` path, and `depends_on` array of target paths)
   - The JSON must be compact (single line).

3. **Flag validation** — If `--format` is given a value other than `text` or `json`, return an error with a clear message (e.g., `invalid --format value "xml": must be text or json`).

4. **Stdout discipline in json mode** — In json mode for `kiln exec`:
   - Claude process stdout/stderr must be forwarded to stderr (not stdout), so the only stdout content is the final JSON result.
   - Warning messages (e.g., "failed to load state", "failed to save state") must go to stderr, not stdout.
   - This means the `stdout` writer passed to internal functions may need to be swapped to stderr when `--format json` is active, or warnings need to be directed to a separate writer.

5. **Backward compatibility** — When `--format` is not specified or is `text`, behavior must be identical to the current implementation. No changes to existing text output formatting.

6. **Reuse existing types** — The `execRunLog` struct already captures most of the data needed for the JSON output. Reuse or derive the output from it rather than building a parallel data structure. Add an `error` field if needed.

## Tests

- `kiln exec --format json` produces valid JSON on stdout with all required fields
- `kiln exec --format json` with a successful task has `status: "complete"` and `exit_code: 0`
- `kiln exec --format json` with a failed/timeout task has the appropriate status and non-zero exit code
- `kiln exec --format text` produces identical output to current behavior (no regression)
- `kiln exec --format xml` returns an error
- `kiln gen-make --format json` produces valid JSON with `tasks_count`, `output_file`, and `targets` array
- `kiln gen-make --format json` targets array has correct `task_id`, `target`, and `depends_on` per task
- `kiln gen-make --format text` produces identical output to current behavior
- `kiln gen-make --format xml` returns an error
- JSON output is compact (single line, no pretty-printing)
- In json mode, claude process output goes to stderr, not stdout
- In json mode, warning messages go to stderr, not stdout
- `go test ./cmd/kiln -v` passes with no regressions

## ACCEPTANCE CRITERIA
- `--format text|json` flag exists on both `kiln exec` and `kiln gen-make`
- Default format is `text` with no behavior change from current implementation
- `kiln exec --format json` emits a single compact JSON object to stdout with all specified fields (`task_id`, `status`, `exit_code`, `model`, `prompt_file`, `started_at`, `ended_at`, `duration_ms`, `attempts`, `footer`, `footer_valid`, `error`)
- `kiln gen-make --format json` emits a compact JSON object to stdout with `tasks_count`, `output_file`, and `targets` array
- Invalid `--format` values produce a clear error message
- In json mode, stdout contains ONLY the final JSON — no warnings, no claude output, no progress text
- Existing text-mode output is unchanged (backward compatible)
- `go test ./cmd/kiln -v` passes with no regressions

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"json-output"}}

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
{"kiln":{"status":"complete","task_id":"json-output"}}

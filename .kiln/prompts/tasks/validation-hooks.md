# Task: validation-hooks — Validation Hooks as Configurable Gates

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
validation-hooks

## SCOPE
Implement ONLY the validation hooks feature described below. Do not work on other backlog items (state resumability, UNIFY, error taxonomy, verification mapping, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln currently runs `kiln exec` for a task and writes a `.done` marker on success. There is no post-run validation — a task can report "complete" without any verification that the work is actually correct.
- This task depends on #7 Richer Task Schema (`richer-schema`). The task schema should already have optional `verify` fields on tasks. If the `verify` field is not yet present in the `Task` struct, add it as part of this task (a minimal addition — do not implement the full richer schema).
- Validation hooks are post-execution gates: shell commands that run after a task completes successfully and must all pass before the `.done` marker is written.

## REQUIREMENTS

1. **Task schema: `verify` field** — Ensure the `Task` struct supports an optional `verify` field:
   - Type: list of verify gate objects, each with:
     - `cmd` (string, required): the shell command to run (e.g., `go test ./...`, `golangci-lint run`)
     - `name` (string, optional): human-readable label for the gate (e.g., "unit tests", "lint")
     - `expect` (string, optional): expected behavior — default is `exit_code_zero`; future extensibility for `output_contains`, `output_matches`, etc. For now, only implement `exit_code_zero`.
   - If the `verify` field is already present from the `richer-schema` task, use it as-is. If not, add it minimally.
   - Tasks without `verify` gates behave exactly as they do today (no validation step).

2. **Project-level default validations** — Support project-wide default gates in `.kiln/config.yaml`:
   - Schema:
     ```yaml
     defaults:
       verify:
         - cmd: "go test ./..."
           name: "unit tests"
         - cmd: "go vet ./..."
           name: "go vet"
     ```
   - Default gates apply to ALL tasks unless a task explicitly sets `verify: []` (empty list) to opt out.
   - Per-task `verify` entries are MERGED with (appended to) project defaults. To skip defaults entirely, a task sets `verify: []`.
   - If `.kiln/config.yaml` does not exist or has no `defaults.verify`, no default gates apply.

3. **Gate execution engine** — Implement a `runVerifyGates` function:
   - Signature: `runVerifyGates(gates []VerifyGate, taskID string, workDir string) error`
   - Runs each gate command sequentially using `exec.Command` with `/bin/sh -c <cmd>`.
   - Working directory: the repository root (or the directory where `kiln exec` was invoked).
   - Each gate has a configurable timeout (default: 5 minutes). Use `context.WithTimeout`.
   - If a gate fails (non-zero exit code or timeout):
     - Log the gate name/command, exit code, and stderr output.
     - Stop executing remaining gates (fail-fast).
     - Return an error describing which gate failed and why.
   - If all gates pass, return nil.
   - Gate stdout/stderr should be captured and included in the task's log entry.

4. **Integration with `kiln exec` flow** — Update the execution flow:
   - After a task completes with status `"complete"` (from the Claude JSON footer), run the verify gates.
   - If ALL gates pass: write the `.done` marker as normal.
   - If ANY gate fails: do NOT write the `.done` marker. Exit with code 2 (not_complete — the task ran but isn't verified).
   - If the task itself failed (status != complete), skip verification entirely.
   - Log the verification results (pass/fail per gate, timing) as part of the task's structured log entry.

5. **`--skip-verify` flag** — Add a flag to `kiln exec` that skips all verification gates:
   - When set, behave exactly as today (no post-run validation).
   - Useful for debugging or when running tasks iteratively during development.
   - Log a warning when `--skip-verify` is used so it's visible in logs.

6. **Structured log entries for verification** — Extend the task log entry to include verification results:
   - Add a `verify` field to the log entry containing:
     - `gates`: list of objects with `name`, `cmd`, `passed` (bool), `exit_code` (int), `duration_ms` (int), `stderr` (string, truncated to 2000 chars)
     - `all_passed` (bool)
     - `skipped` (bool) — true if `--skip-verify` was used
   - If no gates are configured, omit the `verify` field entirely (or set to null).

7. **Config file loading** — Implement minimal `.kiln/config.yaml` loading:
   - Load the config file at startup if it exists.
   - Parse only the `defaults.verify` section for now. Ignore unknown fields gracefully.
   - If the file doesn't exist or is empty, proceed with no defaults.
   - Use `gopkg.in/yaml.v3` (already a dependency for tasks.yaml parsing).

## Tests

- Tasks with no `verify` field behave identically to current behavior (no gates run, `.done` written on success)
- `runVerifyGates` with all-passing gates returns nil
- `runVerifyGates` with a failing gate returns an error naming the failed gate
- `runVerifyGates` stops at first failure (fail-fast — subsequent gates are not run)
- `runVerifyGates` respects timeout (gate killed after timeout, error returned)
- Gate stdout/stderr is captured in results
- Project defaults from `.kiln/config.yaml` are merged with per-task gates
- Per-task `verify: []` (empty list) opts out of project defaults
- Per-task `verify` entries are appended to project defaults
- `--skip-verify` flag skips all gates and logs a warning
- Successful verification writes `.done` marker
- Failed verification does NOT write `.done` marker and exits with code 2
- Verification is skipped when task status is not "complete"
- Structured log entry includes verify results (gates, all_passed, skipped)
- Missing `.kiln/config.yaml` is handled gracefully (no error, no defaults)
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `Task` struct has an optional `verify` field (list of gate objects with cmd, name, expect)
- `runVerifyGates` function exists, runs gates sequentially, fails fast on first failure
- Gates run after successful task completion, before `.done` marker is written
- Failed gates prevent `.done` marker and exit with code 2
- Project-level default gates are loaded from `.kiln/config.yaml` and merged with per-task gates
- Per-task `verify: []` opts out of project defaults
- `--skip-verify` flag skips all verification with a logged warning
- Gate results (pass/fail, exit code, stderr, duration) are included in structured log entries
- Gate timeout is enforced (default 5 minutes)
- Tasks without verify gates behave identically to current behavior (no regression)
- `go test ./...` passes
- No large refactors unrelated to validation hooks

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"validation-hooks"}}

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
{"kiln":{"status":"complete","task_id":"validation-hooks"}}

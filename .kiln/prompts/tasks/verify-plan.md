# Task: verify-plan — Verification Mapping (verify-plan)

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
verify-plan

## SCOPE
Implement ONLY the `kiln verify-plan` command described below. Do not work on other backlog items (state resumability, UNIFY, error taxonomy, TUI, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- This task depends on #7 Richer Task Schema (`richer-schema`). The task schema should already have optional `acceptance` and `verify` fields on tasks. If these fields are not yet present in the `Task` struct, add them minimally (do not implement the full richer schema).
- This task depends on #3 Validation Hooks (`validation-hooks`). Verify gates should already be executable via `runVerifyGates` or similar. If that function does not exist yet, implement a minimal stub that runs shell commands and checks exit codes.
- `kiln verify-plan` is a pre-run planning check: it analyzes the task graph and reports coverage gaps between acceptance criteria and verification gates. It does NOT execute gates — it only checks that they are defined and plausibly runnable.

## REQUIREMENTS

1. **`kiln verify-plan` subcommand** — Add a new subcommand to the CLI:
   - Reads `.kiln/tasks.yaml` (or the file specified by `--tasks` flag, consistent with other subcommands).
   - Analyzes each task for coverage between `acceptance` criteria and `verify` gates.
   - Outputs a human-readable report to stdout.
   - Exits with code 0 if all tasks have adequate coverage, non-zero if gaps are found.

2. **Coverage analysis logic** — Implement the core verification mapping check:
   - **Tasks with `acceptance` but no `verify`**: Flag as "uncovered" — acceptance criteria exist but no gates to verify them.
   - **Tasks with `verify` but no `acceptance`**: Flag as "unanchored" (warning, not error) — gates exist but no documented criteria they verify.
   - **Tasks with neither**: Skip silently (no coverage requirement).
   - **Tasks with both**: Report as "covered" — acceptance criteria have corresponding gates.
   - Do NOT attempt semantic matching between acceptance text and gate commands. This is a structural check: "does a gate exist?", not "does the gate test the right thing?"

3. **Gate executability check** — For each `verify` gate, check that the command is plausibly runnable:
   - Parse the first token of `cmd` as the executable name.
   - Check if it exists on `$PATH` using `exec.LookPath`.
   - Flag missing executables as warnings (not errors) — the command might be installed later or in a different environment.
   - If the `cmd` field is empty or whitespace-only, flag as an error.

4. **Report output format** — The report should include:
   - A summary header: total tasks, covered count, uncovered count, warning count.
   - Per-task details for any task with issues:
     - Task ID
     - Issue type: "UNCOVERED" (acceptance without verify), "UNANCHORED" (verify without acceptance), "EMPTY_CMD" (verify gate with blank command), "CMD_NOT_FOUND" (executable not on PATH)
     - Relevant details (which acceptance criteria lack gates, which commands are missing)
   - A final summary line: "All tasks covered" or "N issue(s) found".
   - Use color/formatting only if stdout is a TTY (or skip color entirely — keep it simple).

5. **`--strict` flag** — When `--strict` is passed:
   - "UNANCHORED" warnings become errors (non-zero exit).
   - "CMD_NOT_FOUND" warnings become errors (non-zero exit).
   - Useful for CI environments where you want hard failures on any gap.

6. **`--format` flag** — Support `--format text` (default) and `--format json`:
   - `text`: human-readable report as described above.
   - `json`: structured JSON output with the same data for machine consumption:
     ```json
     {
       "summary": {
         "total_tasks": 10,
         "covered": 7,
         "uncovered": 2,
         "warnings": 1
       },
       "issues": [
         {
           "task_id": "auth-login",
           "type": "UNCOVERED",
           "message": "Task has 3 acceptance criteria but no verify gates"
         }
       ],
       "pass": false
     }
     ```

7. **Integration with project defaults** — If `.kiln/config.yaml` exists and defines `defaults.verify`, consider those default gates when evaluating coverage:
   - A task with `acceptance` criteria and no per-task `verify` but with project-default verify gates is considered "covered" (defaults apply).
   - A task that explicitly opts out with `verify: []` ignores defaults and is evaluated on its own.

## Tests

- `verify-plan` with all tasks having both `acceptance` and `verify` reports all covered, exits 0
- `verify-plan` with a task having `acceptance` but no `verify` reports "UNCOVERED", exits non-zero
- `verify-plan` with a task having `verify` but no `acceptance` reports "UNANCHORED" warning, exits 0
- `verify-plan` with `--strict` and "UNANCHORED" task exits non-zero
- `verify-plan` with a verify gate whose command is empty reports "EMPTY_CMD" error
- `verify-plan` with a verify gate whose executable is not on PATH reports "CMD_NOT_FOUND" warning
- `verify-plan` with `--strict` and "CMD_NOT_FOUND" exits non-zero
- `verify-plan` with tasks that have neither `acceptance` nor `verify` skips them silently
- `verify-plan` considers project default verify gates from `.kiln/config.yaml` for coverage
- `verify-plan` with task `verify: []` (opt-out) ignores defaults and evaluates independently
- `verify-plan` `--format json` outputs valid JSON matching the expected schema
- `verify-plan` `--format text` outputs human-readable report
- `verify-plan` with no tasks file exits with an error message
- `verify-plan` with an empty tasks file reports 0 tasks, exits 0
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln verify-plan` subcommand exists and reads `.kiln/tasks.yaml`
- Coverage analysis flags tasks with `acceptance` but no `verify` as "UNCOVERED" (error)
- Coverage analysis flags tasks with `verify` but no `acceptance` as "UNANCHORED" (warning)
- Gate executability check validates that gate commands reference existing executables
- Empty/blank `cmd` fields are flagged as errors
- Report includes summary header (total, covered, uncovered, warnings) and per-task details
- Exit code is 0 when all tasks are covered, non-zero when uncovered tasks exist
- `--strict` flag promotes warnings to errors
- `--format json` outputs structured JSON report
- Project default verify gates from `.kiln/config.yaml` are considered for coverage evaluation
- Tasks with `verify: []` opt out of defaults and are evaluated independently
- `go test ./...` passes
- No large refactors unrelated to the verify-plan command

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"verify-plan"}}

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
{"kiln":{"status":"complete","task_id":"verify-plan"}}

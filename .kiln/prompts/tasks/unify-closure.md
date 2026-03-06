# Task: unify-closure — UNIFY / Closure Artifacts

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
unify-closure

## SCOPE
Implement ONLY the UNIFY closure artifact feature described below. Do not work on other backlog items (prompt chaining, recovery UX, validation hooks, TUI, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- UNIFY is a post-completion reconciliation primitive. After a task completes, `kiln unify` invokes Claude Code in a fresh single-shot invocation to produce a semantic closure artifact summarizing what actually happened vs what was intended.
- If `.kiln/state.json` exists (from backlog item #1), read task status from it. Otherwise, fall back to checking `.kiln/done/<task-id>.done` markers for completion status.
- The closure artifact is the bridge between "task ran" and "task is reconciled" — it produces high-signal summaries that downstream tasks can consume (via future prompt chaining).

## REQUIREMENTS

1. **`kiln unify` subcommand** — Add a new subcommand to the CLI:
   - `kiln unify --task-id <id>` generates a closure artifact for a completed task.
   - Required flag: `--task-id` (kebab-case, validated with existing regex).
   - Optional flag: `--model` (override model, same behavior as `kiln exec --model`).
   - Optional flag: `--timeout` (default 10 minutes, same pattern as `kiln exec`).
   - The task must be completed before UNIFY can run. Check `.kiln/done/<task-id>.done` exists (and/or state.json status is "completed" if state file exists). If the task is not completed, exit with a clear error message and exit code 2.

2. **Closure artifact generation** — Invoke Claude Code with a structured prompt:
   - The prompt must instruct Claude to analyze the task and produce a closure summary covering:
     - **What changed**: files modified, functions added/removed, key code changes
     - **What's incomplete or deferred**: any known gaps, TODOs, or deferred work
     - **Decisions made**: key design decisions and their rationale
     - **Handoff notes**: what the next developer (or downstream task) needs to know
     - **Acceptance criteria coverage**: if the task prompt contains acceptance criteria, assess which are met/unmet
   - Input context for the prompt:
     - The original task prompt file (read from `.kiln/prompts/tasks/<task-id>.md`)
     - The task execution log (read from `.kiln/logs/<task-id>.json`)
     - Recent git diff for the task (use `git diff` or `git log` to capture changes — the prompt should instruct Claude to inspect the repo)
   - Invoke Claude Code using the same `commandBuilder` pattern as `kiln exec` (subprocess invocation with `--print`, `--model`, etc.).
   - Do NOT use the retry loop from `kiln exec` — UNIFY is a single-shot invocation. If it fails, exit with an error.

3. **Closure artifact output** — Write the closure artifact to:
   - `.kiln/unify/<task-id>.md` — The full closure summary in Markdown format.
   - Create `.kiln/unify/` directory if it doesn't exist (`os.MkdirAll`).
   - The artifact file is the raw Markdown output from Claude (after stripping any JSON footer if present).
   - Overwrite any existing artifact for the same task ID (re-running unify regenerates the artifact).

4. **Decision ledger** — Append decisions to a shared ledger:
   - `.kiln/decisions.log` — Append-only file, one entry per UNIFY invocation.
   - Each entry is a JSON object on its own line (JSON Lines format):
     ```json
     {"task_id":"<id>","timestamp":"<RFC3339>","artifact_path":".kiln/unify/<id>.md","model":"<model-used>"}
     ```
   - Use `O_APPEND|O_CREATE|O_WRONLY` for atomic append.

5. **JSON footer contract for UNIFY** — The UNIFY prompt must instruct Claude to end its response with:
   ```json
   {"kiln":{"status":"complete","task_id":"<task-id>"}}
   ```
   - Parse the footer using the existing `parseFooter` function.
   - If footer status is "complete", write the artifact and decision ledger entry.
   - If footer status is "not_complete" or "blocked", log the issue and exit with code 2.
   - If footer parsing fails, log the raw output and exit with code 10.

6. **UNIFY prompt construction** — Build the prompt as a string (not a file):
   - Include the original task prompt content inline.
   - Include a summary of the execution log (at minimum: number of attempts, final status, any errors).
   - Instruct Claude to inspect the current repo state (git status, recent commits) to determine what changed.
   - Instruct Claude to produce the closure summary sections listed in requirement 2.
   - Instruct Claude to end with the JSON footer.

7. **Directory conventions**:
   - `kiln gen-make` should create `.kiln/unify/` directory as part of its setup (alongside `.kiln/done/`, `.kiln/logs/`, and `.kiln/locks/` if present).
   - `.kiln/unify/` and `.kiln/decisions.log` should NOT be in `.gitignore` — these are intended to be committed as project artifacts.

## Tests

- `kiln unify --task-id` requires a valid kebab-case task ID
- `kiln unify` exits with error if task is not completed (no `.done` marker)
- UNIFY prompt includes the original task prompt content
- UNIFY prompt includes execution log summary
- UNIFY prompt instructs Claude to produce the required closure sections
- Closure artifact is written to `.kiln/unify/<task-id>.md`
- `.kiln/unify/` directory is created if it doesn't exist
- Decision ledger entry is appended to `.kiln/decisions.log` in JSON Lines format
- Decision ledger entry contains task_id, timestamp, artifact_path, and model
- Re-running unify overwrites the existing artifact file
- Footer parsing uses existing `parseFooter` function
- Exit code 2 when task is not completed
- Exit code 2 when footer status is not "complete"
- Exit code 10 when footer parsing fails
- `--model` flag overrides the default model
- `--timeout` flag sets the execution timeout
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln unify --task-id <id>` subcommand exists and is documented in `--help`
- UNIFY only runs for completed tasks (checks `.done` marker and/or state.json)
- Claude Code is invoked in a single-shot subprocess with a structured closure prompt
- Closure artifact is written to `.kiln/unify/<task-id>.md`
- Decision ledger entry is appended to `.kiln/decisions.log` (JSON Lines format)
- Footer is parsed using existing `parseFooter`; exit codes are correct (0/2/10)
- `kiln gen-make` creates `.kiln/unify/` directory
- `--model` and `--timeout` flags work correctly
- No retry loop — single-shot invocation only
- `go test ./...` passes
- No large refactors unrelated to UNIFY closure

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"unify-closure"}}

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
{"kiln":{"status":"complete","task_id":"unify-closure"}}

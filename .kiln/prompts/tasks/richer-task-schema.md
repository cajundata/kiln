# Task: richer-task-schema — Extend Task struct with optional metadata fields

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
richer-task-schema

## SCOPE
Implement ONLY the richer task schema feature described below. Do not work on other backlog items (state & resumability, concurrency safety, validation hooks, engine abstraction, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- The current `Task` struct has 5 fields: `ID`, `Prompt`, `Needs`, `Timeout`, `Model`.
- `loadTasks` uses `yaml.Decoder` with `KnownFields(true)` for strict schema validation — unknown YAML fields cause a parse error.
- Task IDs must match kebab-case regex: `^[a-z0-9]+(?:-[a-z0-9]+)*$`.
- This task unblocks future features: validation hooks (#3), engine abstraction (#2), and `kiln init` (#4).

## REQUIREMENTS

1. **Add optional fields to the `Task` struct** — Extend with these new optional YAML fields:
   - `Description` (string, omitempty): Human-readable title/summary separate from the task ID.
   - `Kind` (string, omitempty): Task classification (e.g., `backend`, `frontend`, `docs`, `infra`, `test`). Free-form string, no enum enforcement yet.
   - `Tags` ([]string, omitempty): Arbitrary tags for grouping, filtering, or parallel lane assignment.
   - `Retries` (int, omitempty): Per-task retry count override. When set, overrides the default retry count for this task. Must be >= 0.
   - `Validation` ([]string, omitempty): List of post-run validation commands to execute after task completion (e.g., `go test ./...`, `golangci-lint run`). This field is schema-only for now — execution logic will be added in a future task (#3 Validation Hooks).
   - `Engine` (string, omitempty): Engine identifier for future multi-engine support (e.g., `claude`, `codex`). Schema-only — engine selection logic will be added in a future task (#2 Engine Abstraction).
   - `Env` (map[string]string, omitempty): Per-task environment variable overrides.

2. **Preserve strict YAML parsing** — `KnownFields(true)` must remain active. The new fields must be recognized by the decoder. Any truly unknown fields must still cause a parse error.

3. **Validate new fields in `loadTasks`** — Add validation for the new optional fields:
   - If `Retries` is provided, it must be >= 0. Return an error if negative.
   - If `Kind` is provided, it must be non-empty (no whitespace-only values).
   - If `Tags` are provided, each tag must be non-empty and contain no whitespace.
   - If `Env` keys are provided, each key must be a valid environment variable name (letters, digits, underscores, not starting with a digit).
   - Existing validations (task ID format, prompt file existence, unique IDs, valid `needs` references) must remain unchanged.

4. **Wire `Retries` into exec** — If the task has a `Retries` field set, use it as the retry count instead of the default. Check how retries are currently handled (likely a flag or constant) and add logic: if `task.Retries > 0`, use that value; otherwise fall back to the existing default.

5. **Wire `Env` into exec** — If the task has an `Env` map, merge those environment variables into the command environment when running the claude process. Task-level env vars should override any existing env vars with the same name.

6. **Update `kiln gen-make`** — The generated Make targets should pass through any per-task `--timeout` and `--model` overrides that already exist. No changes needed for the new fields unless `gen-make` currently hardcodes assumptions about the Task struct fields. Verify and adjust if necessary.

7. **Do not break existing tasks.yaml files** — All new fields are optional with `omitempty`. Existing tasks.yaml files with only the original 5 fields must continue to parse and work identically.

## Tests

- `loadTasks` accepts tasks.yaml with only original fields (backward compatibility)
- `loadTasks` accepts tasks.yaml with all new optional fields populated
- `loadTasks` rejects negative `Retries` value
- `loadTasks` rejects whitespace-only `Kind`
- `loadTasks` rejects tags containing whitespace
- `loadTasks` rejects invalid env var key names (e.g., starting with digit, containing spaces)
- `loadTasks` still rejects unknown YAML fields (`KnownFields(true)` still active)
- Per-task `Retries` override is used when set, default is used when not set
- Per-task `Env` vars are merged into the command environment
- Per-task `Env` vars override existing environment variables
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `Task` struct has new optional fields: `Description`, `Kind`, `Tags`, `Retries`, `Validation`, `Engine`, `Env`
- All new fields use `omitempty` YAML tags
- `KnownFields(true)` is still active — unknown fields still cause parse errors
- Validation in `loadTasks` catches invalid values for `Retries`, `Kind`, `Tags`, and `Env` keys
- Existing tasks.yaml files without new fields parse and execute identically (no breaking changes)
- Per-task `Retries` overrides the default retry count during `kiln exec`
- Per-task `Env` variables are injected into the claude process environment
- `go test ./...` passes
- No large refactors unrelated to the richer task schema

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"richer-task-schema"}}

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
{"kiln":{"status":"complete","task_id":"richer-task-schema"}}

# Task: richer-schema — Extend the Task struct with optional metadata fields

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
richer-schema

## SCOPE
Implement ONLY the richer task schema feature described below. Do not work on other backlog items (state resumability, concurrency safety, UNIFY, validation hooks, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- The current `Task` struct has these fields: `ID`, `Prompt`, `Needs`, `Timeout`, `Model`, `Description`, `Kind`, `Tags`, `Retries`, `Validation`, `Engine`, `Env`.
- `loadTasks` uses `dec.KnownFields(true)` for strict YAML parsing — any new fields must be added to the struct or they will be rejected.
- `kiln gen-make` generates Make targets from the task graph. New metadata fields should extend the generated targets where appropriate (phase/milestone Make entrypoints).
- `kiln validate-schema` validates task definitions. New fields need validation rules.

## REQUIREMENTS

1. **Add new optional fields to the `Task` struct** — Extend the struct with:
   - `Phase` (string, omitempty): human-oriented lifecycle phase, e.g. `plan`, `build`, `verify`, `docs`. Free-form but validated as non-whitespace-only if present.
   - `Milestone` (string, omitempty): project milestone grouping, e.g. `M1-auth`, `M2-payments`. Must be kebab-case if present (same regex as task IDs).
   - `Acceptance` ([]string, omitempty): list of acceptance criteria (Given/When/Then or bullet AC). Each entry must be non-empty.
   - `Verify` ([]string, omitempty): list of gate commands to run post-completion, e.g. `["go test ./...", "go vet ./..."]`. Each entry must be non-empty.
   - `Lane` (string, omitempty): concurrency grouping identifier. Tasks in the same lane run serially. Must be kebab-case if present.
   - `Exclusive` (bool, omitempty): if true, this task must run with no other tasks in parallel.

2. **Validation in `loadTasks`** — Add validation rules for the new fields:
   - `Phase`: if present, must not be whitespace-only.
   - `Milestone`: if present, must match `taskIDRegexp` (kebab-case).
   - `Acceptance`: each entry must be non-empty string.
   - `Verify`: each entry must be non-empty string.
   - `Lane`: if present, must match `taskIDRegexp` (kebab-case).
   - `Exclusive`: no special validation needed (bool zero value is fine).

3. **Generate phase and milestone Make entrypoints in `kiln gen-make`** — Extend the generated `.kiln/targets.mk` to include:
   - `.PHONY: phase-<phase>` targets that depend on all `.kiln/done/<id>.done` targets for tasks with that phase value.
   - `.PHONY: milestone-<milestone>` targets that depend on all `.kiln/done/<id>.done` targets for tasks with that milestone value.
   - Only generate these targets when at least one task has the field set.
   - Collect unique phase and milestone values in stable (sorted) order.

4. **Pass `--model` and `--timeout` from task fields in gen-make recipes** — The current gen-make already passes `--timeout` when set. Ensure `--model` is also passed in the recipe when the task has a non-empty `Model` field.

5. **Display new metadata in `kiln status`** — If the `kiln status` command exists, update it to show `Kind` and `Phase` columns (or append them to existing output). Keep the output readable — truncate long values if needed.

6. **Research artifact output convention** — Document (via code comments) that tasks with `kind: research` are expected to produce artifacts at `.kiln/artifacts/research/<id>.md`. Do NOT implement artifact creation or enforcement — just add the convention as comments near the Kind field and in the gen-make output as a comment header.

## Tests

- `loadTasks` accepts tasks with all new optional fields populated
- `loadTasks` accepts tasks with no new optional fields (backward compatible)
- `loadTasks` rejects whitespace-only `Phase`
- `loadTasks` rejects non-kebab-case `Milestone`
- `loadTasks` rejects empty string in `Acceptance` list
- `loadTasks` rejects empty string in `Verify` list
- `loadTasks` rejects non-kebab-case `Lane`
- `loadTasks` accepts `Exclusive: true` without error
- `gen-make` generates `phase-<phase>` targets when tasks have Phase set
- `gen-make` generates `milestone-<milestone>` targets when tasks have Milestone set
- `gen-make` does not generate phase/milestone targets when no tasks have those fields
- `gen-make` passes `--model` in recipe when task has Model set
- `gen-make` sorts phase and milestone target names for deterministic output
- Existing tests continue to pass (`go test ./cmd/kiln -v`)

## ACCEPTANCE CRITERIA
- `Task` struct includes `Phase`, `Milestone`, `Acceptance`, `Verify`, `Lane`, and `Exclusive` fields with correct YAML tags
- `loadTasks` validates all new fields and rejects invalid values with clear error messages
- `loadTasks` remains backward compatible — existing tasks.yaml files without new fields still parse successfully
- `KnownFields(true)` still enforced — no unknown YAML keys are silently accepted
- `kiln gen-make` produces `.PHONY: phase-<phase>` and `.PHONY: milestone-<milestone>` targets
- `kiln gen-make` passes `--model` in recipe when task has a Model field set
- `kiln status` displays Kind and Phase metadata when present
- All new and existing tests pass (`go test ./cmd/kiln -v`)
- No large refactors unrelated to schema extension

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"richer-schema"}}

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
{"kiln":{"status":"complete","task_id":"richer-schema"}}

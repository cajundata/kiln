# Task: plan-command — kiln plan as a first-class command

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Add `kiln plan` as a first-class command that wraps the PRD→tasks.yaml workflow, replacing the raw `kiln exec` call currently in the Makefile's `plan` target.

## Context
Currently `make plan` runs:
```
$(KILN) exec --task-id extract-tasks --prompt-file $(PROMPT_DIR)/00_extract_tasks.md
```
This is unintuitive. `kiln plan` should be the user-facing command that:
1. Reads the PRD
2. Invokes Claude with the extract-tasks prompt
3. Writes `.kiln/tasks.yaml`

## Requirements

1. Add `kiln plan` command with flags:
   - `--prd <path>` (default: `PRD.md`) — path to the PRD file
   - `--prompt <path>` (default: `.kiln/prompts/00_extract_tasks.md`) — the extraction prompt
   - `--out <path>` (default: `.kiln/tasks.yaml`) — output path for generated tasks
   - `--model <model>` (optional) — model override
2. Implementation:
   - Read the PRD file
   - Read the extraction prompt
   - Combine: inject PRD content into the prompt (or pass both to claude)
   - Invoke claude via existing `commandBuilder`
   - Write output to `--out` path
   - Validate the output with `loadTasks()` before finalizing
3. Wire into `run()` dispatch
4. Update Makefile `plan` target to use `kiln plan`

## Tests
- Missing PRD file → error
- Missing prompt file → error
- Dispatch via `run(["plan", ...])` → correct exit code
- Default flag values are sensible
- Invalid output YAML → error with helpful message

## Acceptance criteria
- `go test ./...` passes
- `kiln plan` produces a valid `.kiln/tasks.yaml`
- `make plan` updated to use `kiln plan`
- Backward compat: users can still use `kiln exec` directly if preferred

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"plan-command","notes":"Implemented kiln plan as first-class command wrapping PRD-to-tasks workflow."}}

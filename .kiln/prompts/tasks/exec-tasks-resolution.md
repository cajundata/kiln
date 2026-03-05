# Task: exec-tasks-resolution — resolve prompt and model from tasks.yaml

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Add a `--tasks` flag to `kiln exec` so it can resolve the prompt file path and model from tasks.yaml instead of requiring `--prompt-file` on every invocation.

## Requirements

1. Add `--tasks <path>` optional flag to `kiln exec`
2. When `--tasks` is provided:
   - Call `loadTasks()` to parse the file
   - Find the task matching `--task-id`
   - Use its `prompt` field as the prompt file (unless `--prompt-file` is explicitly given)
   - Extract its `model` field for model resolution
3. Error if task not found: `task "X" not found in <path>`
4. Error if task has empty `prompt` and no `--prompt-file`: `task "X" has no prompt field`
5. After resolution: require prompt file non-empty (error message hints at both `--prompt-file` and `--tasks`)
6. Backward compat: `--task-id X --prompt-file Y` (no `--tasks`) works exactly as before

## Tests
- `--tasks` + `--task-id` resolves prompt → success
- Task not found → error mentioning task ID
- `--prompt-file` takes precedence over task prompt
- Task model passed to claude
- `--model` flag overrides task model
- Neither `--tasks` nor `--prompt-file` → error mentioning both options
- `--tasks` with bad path → "failed to read tasks file"
- Task with empty prompt field → error

## Acceptance criteria
- `go test ./...` passes
- Existing `--prompt-file` usage unaffected
- `kiln exec --task-id X --tasks .kiln/tasks.yaml` works

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"exec-tasks-resolution","notes":"Added --tasks flag to exec for prompt and model resolution from tasks.yaml."}}

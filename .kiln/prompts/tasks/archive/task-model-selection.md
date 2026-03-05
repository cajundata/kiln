# Task: task-model-selection — per-task model override in tasks.yaml

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Support a `model` field in tasks.yaml so each task can specify which Claude model to use, enabling cost-efficient development (e.g. haiku for simple tasks, sonnet for complex ones).

## Requirements

1. Add `Model string \`yaml:"model,omitempty"\`` to the Task struct
2. Update `resolveModel` to 4-tier precedence:
   - `--model` flag (highest)
   - task `model:` from tasks.yaml
   - `KILN_MODEL` env var
   - `defaultModel` constant (lowest)
3. When `--tasks` is used in exec, pass the task's model to `resolveModel`
4. `gen-make` does not need to emit `--model` in recipes (exec resolves it from tasks.yaml)

## tasks.yaml example
```yaml
- id: simple-task
  prompt: .kiln/prompts/tasks/simple-task.md
  needs: []
  model: claude-haiku-4-5-20251001

- id: complex-task
  prompt: .kiln/prompts/tasks/complex-task.md
  needs:
    - simple-task
  model: claude-sonnet-4-6
```

## Tests
- `resolveModel("", "task-model")` → "task-model"
- `resolveModel("flag", "task")` → "flag" (flag wins)
- `resolveModel("", "task")` with KILN_MODEL set → "task" (task wins over env)
- `resolveModel("", "")` with KILN_MODEL set → env value
- `resolveModel("", "")` with no env → defaultModel
- exec with `--tasks` passes task model to claude command
- exec with `--tasks` + `--model` flag overrides task model

## Acceptance criteria
- `go test ./...` passes
- `kiln exec --task-id X --tasks .kiln/tasks.yaml` uses the task's model
- `--model` flag still overrides everything

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"task-model-selection","notes":"Implemented per-task model selection with 4-tier precedence."}}

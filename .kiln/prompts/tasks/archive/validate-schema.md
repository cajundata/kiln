# Task: validate-schema — parse tasks.yaml, validate shape, and normalize

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Implement `kiln validate-schema` that validates `.kiln/tasks.yaml` and provides a stable normalized representation for downstream commands (`validate-cycles`, `gen-make`, `exec --tasks`, `status`).

## CLI
```bash
kiln validate-schema --tasks .kiln/tasks.yaml
```

## Tasks.yaml schema (v1)
Each task item:
- `id` (string, required, non-empty, unique)
- `prompt` (string, required, non-empty)
- `needs` (list of string, optional; default empty)
- `model` (string, optional; if empty treat as unset)

Notes:
- Preserve backward compatibility: existing files without `model` must still validate.
- Unknown fields are an error (strict schema), unless you already have an established policy in the codebase.

## Validation requirements
1. YAML parse errors -> return non-zero / error with message `failed to parse tasks file`.
2. Empty list -> error `no tasks found`.
3. Unique IDs -> error if duplicates; message should mention duplicate id.
4. Required fields -> error if `id` or `prompt` missing/empty.
5. Needs type -> if present, must be list of strings; no nulls.
6. Model type -> if present, must be string (allow empty string but treat as unset).
7. Path sanity (lightweight):
   - `prompt` must be a relative path (no absolute paths), OR document and enforce existing project convention.
   - Do not require the prompt file to exist here (that is `exec`’s job).

## Output behavior
- On success: print a concise success line to stdout, e.g. `validate-schema: OK (<N> tasks)`.
- Do not rewrite the file.

## Code structure
- Introduce a `Task` struct representing the YAML shape.
- Create a loader function (e.g. `loadTasks(path) ([]Task, error)`) that both validate-schema and other commands can reuse.

## Tests
Add tests covering:
- invalid YAML
- empty tasks
- missing id/prompt
- duplicate ids
- needs wrong type
- model wrong type
- success case with model present and absent

Prefer table-driven tests.

## Acceptance criteria
- `go test ./...` passes
- `kiln validate-schema --tasks .kiln/tasks.yaml` succeeds for the current repository tasks file
- Error messages match expectations in tests

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"validate-schema","notes":"Implemented strict tasks.yaml schema validation and reusable loader."}}

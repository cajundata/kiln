# Task: Implement tasks.yaml schema validation

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln gen-make` needs to read and validate `.kiln/tasks.yaml` before generating Make targets. This task implements the schema validation step.

The tasks.yaml file is a YAML sequence (list) of task objects:

```yaml
- id: exec-timeout
  prompt: .kiln/prompts/tasks/exec-timeout.md
  needs: []
```

---

## Requirements

### 1. Parse tasks.yaml

- Read and parse the YAML file specified by the `--tasks` flag.
- Deserialize into Go structs.

### 2. Validate schema

- The root must be a YAML sequence (list).
- Each task must have:
  - `id`: string matching `^[a-z0-9]+(-[a-z0-9]+)*$`
  - `prompt`: string path
  - `needs`: optional list of strings (default to empty)
- No duplicate task IDs.
- All `needs` entries must reference existing task IDs.
- Prompt files must exist on disk (unless `--allow-missing-prompts` is passed).

### 3. Error reporting

- On validation failure, print a clear error including:
  - The offending task id (if applicable)
  - The field name
  - Expected format (with example)
- Exit with non-zero status on validation failure.

---

## Acceptance Criteria

- Valid tasks.yaml files pass validation.
- Invalid files produce clear, actionable error messages.
- Missing fields, bad IDs, and dangling needs references are caught.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"validate-schema","notes":"implemented tasks.yaml schema validation with clear error messages"}}

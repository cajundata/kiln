# Task: Parse JSON completion footer from Claude output

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln exec` invokes `claude` and captures its stream-json output. The model's final response text must end with a JSON footer:

```json
{"kiln":{"status":"complete","task_id":"<task-id>"}}
```

Valid statuses: `complete`, `not_complete`, `blocked`.

---

## Requirements

### 1. Parse the JSON footer

- After the claude process completes, scan the captured output for the JSON footer.
- Extract `status` and `task_id` from the footer.
- Validate that `task_id` matches the `--task-id` flag.

### 2. Exit code mapping

- `status: complete` → exit code `2`
- `status: not_complete` → exit code `0`
- `status: blocked` → exit code `0`
- Missing or invalid footer → exit code `10` (permanent failure) with a helpful schema error.

### 3. Error messages

- If the footer is missing: print a clear message explaining the expected format.
- If `task_id` mismatches: print a warning but still use the status-based exit code.

---

## Acceptance Criteria

- `kiln exec` exits `2` when Claude output contains a `complete` footer.
- `kiln exec` exits `10` when the footer is missing or malformed.
- A helpful error message is printed for missing/invalid footers.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"exec-footer","notes":"implemented JSON footer parsing with exit code mapping"}}

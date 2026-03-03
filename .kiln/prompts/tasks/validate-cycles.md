# Task: Detect cyclic dependencies in tasks.yaml

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

After schema validation passes, we need to verify that the dependency graph in tasks.yaml is acyclic. Cyclic dependencies would cause Make to hang or error.

---

## Requirements

### 1. Cycle detection

- After schema validation, build a directed graph from task `needs`.
- Detect cycles using topological sort or DFS-based cycle detection.

### 2. Error reporting

- If a cycle is detected, print a clear error showing the cycle path (e.g., `a -> b -> c -> a`).
- Exit with non-zero status on cycle detection.

### 3. Integration

- This check runs as part of `kiln gen-make`, after schema validation and before target generation.

---

## Acceptance Criteria

- Acyclic graphs pass validation.
- Cyclic graphs produce a clear error showing the cycle path.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"validate-cycles","notes":"implemented cycle detection with clear error reporting"}}

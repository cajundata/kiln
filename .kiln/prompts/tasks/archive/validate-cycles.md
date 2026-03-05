# Task: validate-cycles — dependency validation & ordering

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Implement `kiln validate-cycles` that validates dependency references and detects cycles in `.kiln/tasks.yaml`.

## CLI
```bash
kiln validate-cycles --tasks .kiln/tasks.yaml
```

## Requirements
Using the same loader/schema as `validate-schema`:

1. Unknown dependency ids
- Every entry in `needs` must reference an existing task id.
- If not found, error message should include the task id and the missing dep id.

2. Cycle detection
- Detect any cycle in the dependency graph.
- Error should report at least one cycle path in a human-readable way (e.g., `a -> b -> c -> a`).

3. Self-dependency
- If a task lists itself in `needs`, treat as an error (can be a specific cycle case).

4. Deterministic traversal
- The cycle detection and error reporting should be deterministic for stable tests.

## Output behavior
- On success: print a concise success line to stdout: `validate-cycles: OK`.
- Do not rewrite the file.

## Suggested algorithm
- Build adjacency list from tasks in definition order.
- Validate deps exist.
- DFS with color marking (white/gray/black) or Kahn’s algorithm for cycle detection.
- Keep parent pointers to reconstruct the cycle path.

## Tests
Add tests covering:
- missing dependency reference
- simple cycle (a->b->a)
- longer cycle (a->b->c->a)
- self dependency
- acyclic graph success

## Acceptance criteria
- `go test ./...` passes
- `kiln validate-cycles --tasks .kiln/tasks.yaml` succeeds for current tasks file
- Cycle/missing-dep errors are clear and deterministic

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"validate-cycles","notes":"Implemented dependency validation and deterministic cycle detection."}}

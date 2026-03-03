# Task: Generate Make targets from tasks.yaml

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

`kiln gen-make` reads a validated tasks.yaml and produces `.kiln/targets.mk`, a Make include file that drives task execution.

---

## Requirements

### 1. CLI

```bash
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk
```

### 2. Target generation

For each task, generate a Make target:
- Target: `.kiln/done/<id>.done`
- Prerequisites: `.kiln/done/<dep>.done` for each entry in `needs`
- Recipe: `$(KILN) exec --task-id <id> --prompt-file <prompt> && touch $@`

### 3. All target

- Generate a phony `all` target that depends on all `.done` files.

### 4. Output requirements

- Output must be deterministic (stable ordering by task ID or definition order).
- Output must be a valid Make include file.

---

## Acceptance Criteria

- `kiln gen-make` produces a valid `.kiln/targets.mk`.
- `make <task-id>` runs prerequisites first.
- `make -jN` parallelizes independent tasks.
- A failed `kiln exec` prevents `.done` creation and stops Make.
- The code builds with `go build -o kiln ./cmd/kiln`.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"gen-make","notes":"implemented Make target generation from tasks.yaml"}}

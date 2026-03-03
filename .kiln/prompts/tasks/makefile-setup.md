# Task: Finalize Makefile for plan/graph/all workflow

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to implement focused, minimal changes.

---

## Context

The project Makefile defines `plan`, `graph`, and `all` targets. This task ensures the Makefile is complete and correct for the MVP workflow.

---

## Requirements

### 1. Verify existing targets

- `plan`: runs `kiln exec` against the PRD parsing prompt
- `graph`: runs `kiln gen-make` to produce `.kiln/targets.mk`
- `all`: includes `.kiln/targets.mk` and runs all generated targets

### 2. Include generated targets

- Use `-include .kiln/targets.mk` to conditionally include generated targets.
- If `.kiln/targets.mk` does not exist, `make all` should fail with a clear message to run `make graph` first.

### 3. Clean target

- Add a `clean` target that removes `.kiln/done/`, `.kiln/logs/`, and `.kiln/targets.mk`.

---

## Acceptance Criteria

- `make plan` produces `.kiln/tasks.yaml`.
- `make graph` produces `.kiln/targets.mk`.
- `make all` runs the full task graph.
- `make clean` removes generated artifacts.
- The Makefile is correct and complete for the MVP workflow.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"makefile-setup","notes":"finalized Makefile with plan/graph/all/clean targets"}}

# Task: Implement `kiln gen-make` to generate Make targets from `tasks.yaml`

## Role

You are an assistant developer working inside the existing Go CLI project `kiln`. Your job in this task is to implement a **new subcommand**:

- `kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk`

This command will read the task graph from `tasks.yaml` and emit a small Makefile fragment that knows how to call `kiln exec` for each task in the graph.

You are allowed to:

- Modify existing Go files (especially in `cmd/kiln/`)
- Create new Go files if helpful (e.g., for task struct parsing)
- Add minimal dependencies to `go.mod` (e.g., a YAML library)
- Update the root `Makefile` if needed to align with `gen-make` behavior
- Run `go build`, `go test`, and `make` commands

You should keep the implementation **simple and explicit**. Avoid premature abstraction.

---

## Context

The high-level kiln workflow:

1. `make plan`
   - Uses `kiln exec` with `.kiln/prompts/00_extract_tasks.md`
   - Claude reads `PRD.md` and writes `.kiln/tasks.yaml`

2. `make graph`
   - Calls `kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk`
   - Generates Make targets for each task

3. `make all`
   - Runs all tasks by invoking the generated targets

We have already:

- Implemented `kiln exec`:
  - Reads a prompt file
  - Calls `claude --output-format stream-json -p "<prompt>"`
  - Streams output to stdout and `.kiln/logs/<task-id>.json`
  - Uses exit code 0 on success, 1 on failure
- Established a minimal schema for `.kiln/tasks.yaml`:

```yaml
- id: exec-01
  prompt: .kiln/prompts/tasks/exec-01.md
  needs: []

- id: genmake-01
  prompt: .kiln/prompts/tasks/genmake-01.md
  needs:
    - exec-01
```

### Current Makefile expectations

The root `Makefile` already has:

```make
KILN := ./kiln
PROMPT_DIR := .kiln/prompts
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

.PHONY: plan graph all

plan:
	$(KILN) exec \
		--task-id extract-tasks \
		--prompt-file $(PROMPT_DIR)/00_extract_tasks.md

graph:
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE)

all: graph
	$(MAKE) -f $(TARGETS_FILE) all

-include $(TARGETS_FILE)
```

Your job is to make `gen-make` real, and to ensure that the generated `.kiln/targets.mk` works with this `Makefile`.

---

## Requirements

### 1. Define the `gen-make` subcommand interface

In `cmd/kiln`:

- Extend the CLI dispatcher so that:

  ```bash
  kiln gen-make --tasks <path> --out <path>
  ```

  is supported.

- Interface requirements:
  - Subcommand name: `gen-make`
  - Flags:
    - `--tasks` (string, required) – path to `tasks.yaml`
    - `--out`   (string, required) – path to the generated Makefile fragment
  - Behavior on missing/empty flags:
    - Print a helpful error via `stderr`
    - Exit with status code 1 (i.e., `run()` should return non-zero)

You can mirror the style of `exec` for flag parsing and error handling.

### 2. Parse `.kiln/tasks.yaml` into a Go struct

- Define a Go struct that matches the schema:

  ```go
  type TaskDef struct {
      ID     string   `yaml:"id"`
      Prompt string   `yaml:"prompt"`
      Needs  []string `yaml:"needs"`
  }
  ```

- `tasks.yaml` is a YAML sequence of `TaskDef`.

- Requirements:
  - If the file cannot be read → error
  - If the YAML is invalid → error
  - If any task is missing `id` or `prompt` → error
  - If any `needs` entry references a non-existent `id` → error

Use a standard Go YAML library. You may add a single YAML dependency to `go.mod` (for example, a commonly used `yaml` package).

### 3. Generate `.kiln/targets.mk`

Given the parsed tasks, write a Makefile fragment to `--out`. The fragment should:

1. Define variables:

   ```make
   KILN ?= ./kiln
   ```

2. Define a pattern for `.done` files and an `all` target:

   - Every task should be represented by a `.done` file, e.g.:

     ```make
     .PHONY: all
     all: <space-separated list of all task .done targets>
     ```

   - For each task with `id = <ID>`:

     - The “done marker” is `.kiln/done/<ID>.done`
     - The prompt file is the `prompt` path from YAML

3. For each task, emit a rule in the form:

   ```make
   .kiln/done/<ID>.done: <dependency .done targets>
   	$(KILN) exec    		--task-id <ID>    		--prompt-file <PROMPT_PATH>
   	@mkdir -p .kiln/done
   	@touch $@
   ```

   Where:

   - `<ID>` comes from `id`
   - `<PROMPT_PATH>` comes from `prompt`
   - `<dependency .done targets>` is a list of `.kiln/done/<dep>.done` derived from `needs`

4. The generated Makefile should be:

   - Deterministic (same `tasks.yaml` → same `targets.mk`)
   - Readable enough to debug by hand

5. Overwrite `--out` if it already exists. Ensure parent directories exist (`os.MkdirAll`).

### 4. Error handling and exit codes

`kiln gen-make` should:

- Exit with **0** (success) only if:
  - `tasks.yaml` was read and parsed successfully
  - All tasks are valid
  - All `needs` references resolve
  - The `--out` file was written successfully

- Exit with **1** on any failure, including:
  - Missing flags
  - File I/O errors
  - YAML parsing errors
  - Invalid tasks (missing `id`/`prompt`)
  - Invalid dependencies (`needs` referencing unknown IDs)

Print concise, actionable error messages to `stderr` when failing.

### 5. Tests

Add tests under `cmd/kiln` to cover at least:

1. **Happy path:**

   - Given a small, valid `tasks.yaml` in a temp directory:
     - `gen-make` produces a `targets.mk`
     - The file contains rules for each task
     - `all` depends on all `.done` targets

2. **Invalid YAML / malformed tasks.yaml:**

   - `gen-make` returns an error and does not panic

3. **Missing IDs / prompts:**

   - A task with empty `id` or `prompt` triggers an error

4. **Bad dependency:**

   - `needs` referencing a non-existent task ID triggers an error

You may structure tests similar to how `runExec` and `run()` are tested.

---

## Acceptance Criteria

This task is complete when:

1. `kiln gen-make --help` (or incorrect usage) clearly indicates required flags.

2. Running, from the project root:

   ```bash
   go test ./cmd/kiln/...
   ```

   passes with all tests (including your new `gen-make` tests).

3. With a valid `.kiln/tasks.yaml` in place (from a prior `make plan`), running:

   ```bash
   ./kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk
   ```

   - Produces `.kiln/targets.mk`
   - The file contains:
     - An `all` target that depends on all `.kiln/done/<ID>.done` targets
     - A rule for each task that:
       - Calls `$(KILN) exec --task-id <ID> --prompt-file <PROMPT>`
       - Creates `.kiln/done/<ID>.done` on success

4. Running:

   ```bash
   make graph
   make -f .kiln/targets.mk -n all
   ```

   shows a sane plan for executing each task exactly once, in an order consistent with `needs`.

---

## Final JSON Status Footer

At the end of your work, emit exactly one JSON object on a single line:

**Successful completion:**

```json
{"kiln":{"status":"complete","task_id":"genmake-01","notes":"implemented kiln gen-make to read tasks.yaml and generate .kiln/targets.mk with .done targets and dependencies"}}
```

**Blocked:**

```json
{"kiln":{"status":"blocked","task_id":"genmake-01","notes":"<brief explanation of what is blocking you>"}}
```
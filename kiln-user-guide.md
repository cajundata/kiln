# Kiln User Guide

Kiln is a Go CLI tool that prevents Claude Code context rot by running one task per fresh invocation, orchestrated by Make for dependency resolution and parallel execution.

The core idea: break a project into small, focused tasks defined in a YAML graph, then let Make run each task as an independent Claude Code session with timeouts, retries, and structured logging.

## Quick Start

```bash
# 1. Build the binary
go build -o kiln ./cmd/kiln

# 2. Write your PRD
vim PRD.md

# 3. Generate the task graph from your PRD
kiln plan

# 4. Review and edit .kiln/tasks.yaml (optional but recommended)
vim .kiln/tasks.yaml

# 5. Generate prompt files for each task
kiln gen-prompts

# 6. Review and edit prompt files (optional but recommended)
ls .kiln/prompts/tasks/

# 7. Generate Make targets from the task graph
make graph

# 8. Run all tasks (respecting dependencies)
make all
```

## Execution Flow

```
PRD.md
  |  (kiln plan)
  v
.kiln/tasks.yaml          <-- review/edit
  |  (kiln gen-prompts)
  v
.kiln/prompts/tasks/*.md   <-- review/edit
  |  (make graph / kiln gen-make)
  v
.kiln/targets.mk
  |  (make all)
  v
kiln exec per task --> .kiln/logs/<id>.json + .kiln/done/<id>.done
```

## Commands

### `kiln plan`

Parses a PRD file and generates `.kiln/tasks.yaml` by invoking Claude with an extraction prompt. After generation, the output is validated against the tasks.yaml schema.

| Flag | Default | Description |
|------|---------|-------------|
| `--prd` | `PRD.md` | Path to the PRD file |
| `--prompt` | `.kiln/prompts/00_extract_tasks.md` | Path to the extraction prompt |
| `--out` | `.kiln/tasks.yaml` | Output path for the generated tasks file |
| `--model` | See [Model Selection](#model-selection) | Claude model to use |
| `--timeout` | `15m` | Maximum duration for the Claude invocation |

```bash
kiln plan
kiln plan --prd docs/my-prd.md --out .kiln/tasks.yaml
```

### `kiln exec`

Runs a single Claude Code invocation for one task. This is the core command -- Make calls it once per task in the dependency graph.

| Flag | Default | Description |
|------|---------|-------------|
| `--task-id` | *(required)* | Task identifier (kebab-case) |
| `--prompt-file` | *(resolved from tasks.yaml)* | Path to prompt file (overrides tasks.yaml lookup) |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml for prompt/model resolution |
| `--model` | See [Model Selection](#model-selection) | Claude model to use |
| `--timeout` | `15m` | Maximum duration for the Claude invocation |
| `--retries` | `0` | Number of additional attempts on retryable failures |
| `--retry-backoff` | `0s` | Base sleep duration between retry attempts |
| `--backoff` | `fixed` | Backoff strategy: `fixed` or `exponential` |

**Minimal usage** (resolves prompt from `.kiln/tasks.yaml`):

```bash
kiln exec --task-id my-task
```

**With explicit prompt file** (skips tasks.yaml resolution):

```bash
kiln exec --task-id my-task --prompt-file path/to/prompt.md
```

**With retries and exponential backoff**:

```bash
kiln exec --task-id my-task --retries 3 --retry-backoff 10s --backoff exponential
```

### `kiln gen-prompts`

Reads `.kiln/tasks.yaml` and generates prompt files (`.kiln/prompts/tasks/<id>.md`) for tasks that don't already have one. Uses Claude (opus by default) to produce task-specific prompts based on the PRD and the prompt template.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | `.kiln/tasks.yaml` | Path to the tasks file |
| `--prd` | `PRD.md` | Path to the PRD file (provides context for prompt generation) |
| `--template` | `.kiln/templates/<id>.md` | Path to the prompt template |
| `--model` | See [Model Selection](#model-selection) | Claude model to use (default: `claude-opus-4-6`) |
| `--timeout` | `15m` | Timeout per Claude invocation |
| `--overwrite` | `false` | Regenerate prompts even when the file already exists |

```bash
# Generate missing prompt files
kiln gen-prompts

# Use a different PRD as context
kiln gen-prompts --prd BACKLOG.md

# Regenerate all prompt files (including existing ones)
kiln gen-prompts --overwrite
```

### `kiln gen-make`

Reads `.kiln/tasks.yaml` and generates `.kiln/targets.mk` with Make targets that respect the dependency graph.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | *(required)* | Path to tasks.yaml |
| `--out` | *(required)* | Output path for the generated Makefile include |

```bash
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk
```

The generated file defines one Make target per task. Each target depends on its `needs` and calls `kiln exec --task-id <id>`. Tasks with a `timeout` field get a `--timeout` flag appended.

### `kiln status`

Displays the current state of all tasks: done, runnable, or blocked.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | *(required)* | Path to tasks.yaml |

```bash
kiln status --tasks .kiln/tasks.yaml
```

Example output:

```
TASK                           STATUS     NEEDS
----                           ------     -----
exec-timeout                   done       -
exec-retry                     runnable   exec-timeout
gen-make                       blocked    validate-cycles

2/3 tasks done, 1 runnable
```

### `kiln validate-schema`

Validates tasks.yaml against the strict schema. Rejects unknown fields, ensures kebab-case IDs, relative prompt paths, and no duplicates.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | *(required)* | Path to tasks.yaml |

```bash
kiln validate-schema --tasks .kiln/tasks.yaml
```

### `kiln validate-cycles`

Checks the dependency graph for unknown references and cycles.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | *(required)* | Path to tasks.yaml |

```bash
kiln validate-cycles --tasks .kiln/tasks.yaml
```

## tasks.yaml Schema

The file is a YAML sequence of task objects:

```yaml
- id: build-api
  prompt: .kiln/prompts/tasks/build-api.md
  needs: []

- id: build-ui
  prompt: .kiln/prompts/tasks/build-ui.md
  needs:
    - build-api
  timeout: 20m
  model: claude-opus-4-6
```

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Kebab-case identifier matching `^[a-z0-9]+(?:-[a-z0-9]+)*$` |
| `prompt` | Yes | Relative path to the task's prompt file |
| `needs` | No | List of task IDs that must complete first |
| `timeout` | No | Per-task timeout (overrides the default 15m when passed via gen-make) |
| `model` | No | Per-task model override |

Unknown fields are rejected during validation (strict schema).

## Model Selection

Model is resolved in this order of precedence:

1. `--model` flag (highest priority)
2. `model` field in tasks.yaml for the current task (exec only)
3. `KILN_MODEL` environment variable
4. Command default: `claude-opus-4-6` for `plan` and `gen-prompts`, `claude-sonnet-4-6` for everything else

```bash
# Use env var for all tasks
export KILN_MODEL=claude-opus-4-6
make all

# Override for a single task
kiln exec --task-id my-task --model claude-haiku-4-5-20251001
```

## Retry and Backoff

Retryable errors include timeouts and non-zero Claude exit codes. Parse/validation errors (footer errors) are **not** retryable.

**Fixed backoff** (default): sleeps the same `--retry-backoff` duration between each attempt.

**Exponential backoff**: sleeps `base * 2^(attempt-1)` with 0-50% random jitter, capped at 5 minutes.

```bash
# 3 retries with 10s fixed backoff
kiln exec --task-id my-task --retries 3 --retry-backoff 10s

# 3 retries with exponential backoff starting at 5s
kiln exec --task-id my-task --retries 3 --retry-backoff 5s --backoff exponential
```

## JSON Footer Contract

Every Claude invocation must end its output with a JSON footer:

```json
{"kiln":{"status":"complete","task_id":"<id>"}}
```

Valid `status` values:
- `complete` -- task finished successfully
- `not_complete` -- task could not finish (non-fatal)
- `blocked` -- task is blocked by an external dependency

A `.kiln/done/<id>.done` marker is only created when status is `complete` **and** the `task_id` matches the expected task ID. This marker is what Make uses for idempotency.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (footer parsed, status was `complete`, `not_complete`, or `blocked`) |
| 1 | General error (missing flags, invalid config, etc.) |
| 2 | Success with done marker created (footer valid, `complete` with matching task_id) |
| 10 | Permanent failure (missing/invalid footer, bad footer status) |
| 20 | Transient failure (timeout, retries exhausted) |

## .kiln/ Directory Structure

```
.kiln/
  tasks.yaml              # Task dependency graph
  targets.mk              # Generated Make include file
  templates/
    <id>.md               # Prompt template used by gen-prompts
  prompts/
    00_extract_tasks.md    # Plan extraction prompt
    tasks/
      <task-id>.md         # Per-task prompt files (generated or hand-written)
  logs/
    <task-id>.json         # Per-task execution logs (one per attempt)
  done/
    <task-id>.done         # Idempotency markers (empty files)
```

## Structured Logs

Each `kiln exec` writes a JSON log to `.kiln/logs/<task-id>.json`:

```json
{
  "task_id": "build-api",
  "started_at": "2026-03-05T10:00:00Z",
  "ended_at": "2026-03-05T10:02:30Z",
  "duration_ms": 150000,
  "model": "claude-sonnet-4-6",
  "prompt_file": ".kiln/prompts/tasks/build-api.md",
  "exit_code": 0,
  "status": "complete",
  "footer": {"kiln": {"status": "complete", "task_id": "build-api"}},
  "footer_valid": true,
  "events": [
    {"ts": "2026-03-05T10:00:01Z", "type": "stdout", "line": "..."},
    {"ts": "2026-03-05T10:00:02Z", "type": "stderr", "line": "..."}
  ]
}
```

## Makefile Targets

The provided `Makefile` exposes three workflow targets:

| Target | Command | Description |
|--------|---------|-------------|
| `make plan` | `kiln plan` | Parse PRD.md into .kiln/tasks.yaml |
| `make graph` | `kiln gen-make` | Generate .kiln/targets.mk from tasks.yaml |
| `make all` | *(generated)* | Run all tasks respecting dependency order |
| `make clean` | `rm -rf` | Remove .kiln/done, .kiln/logs, and targets.mk |

Make handles parallelism natively. Use `make -j4 all` to run up to 4 independent tasks concurrently.

## End-to-End Workflow

This section walks through using kiln to implement features from a PRD or backlog document.

### Step 1: Generate the task graph

```bash
kiln plan --prd BACKLOG.md
```

Review `.kiln/tasks.yaml` after generation. You may want to:
- Adjust task granularity (one Claude session per task)
- Tighten or loosen dependencies
- Scope to a single phase rather than the entire backlog

### Step 2: Generate prompt files

```bash
kiln gen-prompts --prd BACKLOG.md
```

This scaffolds `.kiln/prompts/tasks/<id>.md` for each task that doesn't have a prompt file yet. Review the generated prompts — they're a starting point. Tighten acceptance criteria or add codebase-specific context as needed.

### Step 3: Generate Make targets and run

```bash
make graph
make -j4 all
```

Make resolves the dependency graph, runs independent tasks in parallel, and calls `kiln exec --task-id <id>` for each. Tasks that complete successfully get `.done` markers; Make skips them on subsequent runs.

### Step 4: Monitor and iterate

```bash
kiln status --tasks .kiln/tasks.yaml
```

Re-run `make all` to retry failed or incomplete tasks. The `.done` markers make this idempotent — only unfinished work gets re-executed.

### Phased execution

For large backlogs, scope each run to one phase:

1. Edit `tasks.yaml` to include only Phase 1 tasks
2. Run `make graph && make all`
3. Verify results, rebuild if needed (`go build -o kiln ./cmd/kiln`)
4. Add Phase 2 tasks to `tasks.yaml`, generate their prompts
5. Repeat

This is especially important when kiln is building itself — changes to `cmd/kiln/main.go` during a task run could affect subsequent tasks. Rebuilding the binary between phases ensures each phase uses stable tooling.

### Running a single task manually

```bash
kiln exec --task-id my-task
kiln exec --task-id my-task --retries 2 --retry-backoff 10s --backoff exponential
```

### Resetting a completed task

Delete its done marker and re-run:

```bash
rm .kiln/done/my-task.done
make all
```

### Full reset

```bash
make clean        # removes .kiln/done/, .kiln/logs/, and targets.mk
make graph        # regenerate targets
make all          # re-run everything
```

## Build and Test

```bash
# Build
go build -o kiln ./cmd/kiln

# Run all tests
go test ./cmd/kiln -v

# Run a single test
go test ./cmd/kiln -v -run TestExecTimeout

# Test coverage
go test ./cmd/kiln -v -coverprofile=cover.out && go tool cover -func=cover.out
```

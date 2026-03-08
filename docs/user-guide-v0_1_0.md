# Kiln User Guide

> This guide covers Kiln version `v0.1.0-dirty`

Kiln is a Go CLI tool for running AI coding tasks one at a time, each in a fresh Claude Code invocation, orchestrated by Make for dependency resolution and parallel execution.

---

## Table of Contents

1. [Part 1: Getting Started Tutorial](#part-1-getting-started-tutorial)
2. [Part 2: Command Reference](#part-2-command-reference)
3. [Part 3: Task Schema Reference](#part-3-task-schema-reference)
4. [Part 4: Configuration & Environment](#part-4-configuration--environment)
5. [Part 5: Features Coming Soon](#part-5-features-coming-soon)

---

## Part 1: Getting Started Tutorial

### What is Kiln?

Kiln prevents **context rot** — the gradual degradation in AI output quality that happens when a long conversation accumulates too much state, noise, and stale context. Instead of running all your tasks in one long Claude session, Kiln orchestrates them as independent invocations: one task, one fresh context, one result.

This is the **"Ralph Wiggum" pattern**: give each agent a single, well-scoped task and a clear prompt. The workflow goes from product requirements (a `PRD.md` file) to a task graph (`tasks.yaml`) to generated Make targets (`targets.mk`) to executed tasks — each running in isolation with timeouts, retries, and structured logging.

Kiln uses Make as its orchestration engine, which means you get dependency resolution, parallel execution (`make -jN`), and idempotency for free. The `.done` markers that Kiln writes are Make targets — if a task completed successfully, Make skips it on the next run.

### Prerequisites

- **Go toolchain** — Go 1.21 or later (`go version` to verify)
- **Claude Code CLI** — installed and authenticated (`claude --version` to verify)
- **GNU Make** — available as `make` in your PATH

### Installation

Clone the repository and build the binary:

```bash
git clone https://github.com/your-org/kiln.git
cd kiln

# Build with version injection (recommended)
make bin/kiln

# Verify the binary works
./bin/kiln version
```

Add `./bin/kiln` to your PATH, or copy it to a location already on your PATH:

```bash
cp bin/kiln /usr/local/bin/kiln
kiln version
```

Building with bare `go build` (without the Makefile) produces a binary that prints `dev` for `kiln version`:

```bash
go build -o kiln ./cmd/kiln
./kiln version  # prints: dev
```

### Project Setup

Kiln stores all its runtime state in a `.kiln/` directory at the root of your project. After running `make graph`, the directory looks like this:

```
.kiln/
├── tasks.yaml              # Task graph: IDs, prompts, dependencies
├── targets.mk              # Generated Make targets (do not edit manually)
├── config.yaml             # Optional project-wide configuration
├── state.json              # Per-task execution state (auto-managed)
├── decisions.log           # Append-only record of UNIFY artifact generation
├── prompts/
│   ├── 00_extract_tasks.md # Extraction prompt used by `kiln plan`
│   └── tasks/
│       ├── my-task.md      # Per-task prompt files
│       └── other-task.md
├── templates/
│   └── <id>.md             # Template used by `kiln gen-prompts`
├── done/
│   ├── my-task.done        # Idempotency markers (Make targets)
│   └── other-task.done
├── logs/
│   ├── my-task.json        # Execution log for each task (last attempt)
│   └── other-task.json
├── locks/
│   └── my-task.lock        # Concurrency lock files (transient)
├── unify/
│   └── my-task.md          # UNIFY closure artifacts
└── artifacts/
    └── research/
        └── my-task.md      # Research task outputs (by convention)
```

The Makefile includes `targets.mk` automatically once it exists:

```makefile
-include .kiln/targets.mk
```

### The Three-Step Workflow

#### Step 1: `make plan` — Extract Tasks from PRD

Write your product requirements in `PRD.md`, then run:

```bash
make plan
```

This calls `kiln plan`, which reads `PRD.md` and the extraction prompt at `.kiln/prompts/00_extract_tasks.md`, then invokes Claude (using the `claude-opus-4-6` model by default) to produce `.kiln/tasks.yaml`.

The resulting `tasks.yaml` contains a list of tasks, each with an ID, a prompt file path, and optional dependency declarations:

```yaml
- id: setup-database
  prompt: .kiln/prompts/tasks/setup-database.md
  needs: []

- id: implement-auth
  prompt: .kiln/prompts/tasks/implement-auth.md
  needs:
    - setup-database

- id: write-api-tests
  prompt: .kiln/prompts/tasks/write-api-tests.md
  needs:
    - implement-auth
```

After `make plan` completes, validate that the schema is correct:

```bash
kiln validate-schema --tasks .kiln/tasks.yaml
kiln validate-cycles --tasks .kiln/tasks.yaml
```

#### Step 2: `make graph` — Generate Make Targets

```bash
make graph
```

This calls `kiln gen-make`, which reads `tasks.yaml` and writes `.kiln/targets.mk`. The generated file contains Make targets for each task, respecting the `needs` dependency graph. Example output:

```makefile
.PHONY: all
all: .kiln/done/setup-database.done .kiln/done/implement-auth.done .kiln/done/write-api-tests.done

.kiln/done/setup-database.done:
	$(KILN) exec --task-id setup-database

.kiln/done/implement-auth.done: .kiln/done/setup-database.done
	$(KILN) exec --task-id implement-auth

.kiln/done/write-api-tests.done: .kiln/done/implement-auth.done
	$(KILN) exec --task-id write-api-tests
```

To run only tasks in a specific development phase (e.g., phase 1):

```bash
make graph DEV_PHASE=1
```

This filters `targets.mk` to include only tasks with `dev-phase: 1` in `tasks.yaml`.

#### Step 3: `make all` — Execute the Task Graph

```bash
# Run all tasks sequentially (safe default)
make all

# Run up to 3 tasks in parallel
make -j3 all
```

Make respects the dependency graph encoded in `targets.mk`. Tasks with no pending dependencies run immediately; tasks with unsatisfied dependencies wait. When a task completes successfully, `kiln exec` writes a `.done` marker file, and Make considers that target satisfied.

If you rerun `make all` after a partial failure, Make skips tasks that already have `.done` markers — only the incomplete tasks run again.

### Understanding Task Results

#### Log Files

Every `kiln exec` invocation writes a structured JSON log to `.kiln/logs/<task-id>.json`. To inspect the last run of a task:

```bash
cat .kiln/logs/implement-auth.json | python3 -m json.tool
```

Key fields in the log:

| Field | Description |
|---|---|
| `task_id` | The task identifier |
| `status` | `complete`, `not_complete`, `blocked`, `timeout`, or `error` |
| `started_at` / `ended_at` | Timestamps |
| `duration_ms` | Wall-clock duration in milliseconds |
| `model` | Claude model used |
| `exit_code` | Process exit code |
| `footer_valid` | Whether the JSON footer was valid |
| `error_class` | Canonical error class (e.g., `timeout`, `claude_exit`) |
| `error_message` | Human-readable error description |
| `retryable` | Whether the error is eligible for retry |
| `verify` | Gate results (if verify gates are configured) |
| `events` | Per-line log events with timestamps |

#### Exit Codes

| Code | Meaning |
|---|---|
| `0` | Task reported `complete` and all verify gates passed |
| `2` | Task reported `not_complete` or `blocked`; or verify gate failed |
| `10` | Permanent failure (bad footer, schema error, lock conflict) |
| `20` | Transient retries exhausted (timeout or Claude exit) |

#### The JSON Footer Contract

Every Claude invocation must end with a valid JSON footer on its own line:

```json
{"kiln":{"status":"complete","task_id":"implement-auth"}}
```

Valid status values:

- `complete` — task finished successfully
- `not_complete` — task attempted work but did not finish
- `blocked` — task cannot proceed (missing dependency, permission, etc.)

Kiln scans the output bottom-up for this footer. If no valid footer is found, the task fails with exit code `10` and `error_class: footer_parse`.

#### Checking Overall Status

```bash
kiln status --tasks .kiln/tasks.yaml
```

This prints a scoreboard showing every task's current status, attempt count, kind, phase, last error, and dependencies.

### Prompt Generation

After `make plan` creates `tasks.yaml`, you can auto-generate per-task prompt files using the template at `.kiln/templates/<id>.md`:

```bash
kiln gen-prompts \
  --tasks .kiln/tasks.yaml \
  --prd PRD.md \
  --template .kiln/templates/\<id\>.md
```

This invokes Claude once per task (skipping tasks that already have a prompt file) to fill in the template with task-specific instructions derived from the PRD.

To regenerate all prompts, even those that already exist:

```bash
kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md --overwrite
```

Customize the template at `.kiln/templates/<id>.md` to control the structure of generated prompt files. The template uses `<task-id>` as a placeholder that gets replaced with each task's actual ID.

### What To Do When Things Fail

When a task fails, use this recovery flow:

1. **Check the scoreboard** to understand which tasks failed and why:
   ```bash
   kiln status --tasks .kiln/tasks.yaml
   ```

2. **Read the full report** for error class aggregation and duration stats:
   ```bash
   kiln report
   ```

3. **Generate a resume prompt** to re-run a specific task with prior context:
   ```bash
   kiln resume --task-id implement-auth --tasks .kiln/tasks.yaml
   ```

4. **Retry failed tasks** automatically:
   ```bash
   # Retry all failed tasks
   kiln retry --tasks .kiln/tasks.yaml --failed

   # Retry only tasks that failed due to transient errors (timeouts, crashes)
   kiln retry --tasks .kiln/tasks.yaml --failed --transient-only

   # Retry a specific task
   kiln retry --tasks .kiln/tasks.yaml --task-id implement-auth
   ```

5. **Reset a task** to re-run it from scratch:
   ```bash
   kiln reset --task-id implement-auth --tasks .kiln/tasks.yaml
   ```

6. **After fixing the underlying issue**, run `make all` again to pick up where you left off.

---

## Part 2: Command Reference

### `kiln version`

Prints the current Kiln version string.

```bash
kiln version
# Output: 6c0641d-dirty
```

The version is injected at build time via `-ldflags "-X main.version=$(VERSION)"` where `VERSION` comes from `git describe --tags --always --dirty`. Building with bare `go build` (without ldflags) prints `dev`.

---

### `kiln exec`

Single-task execution engine. Runs a Claude Code invocation for one task, handles retries with backoff, writes structured logs, runs verify gates, updates `state.json`, and writes the `.done` marker on success.

**Execution lifecycle:**

1. Acquire task lock (`.kiln/locks/<task-id>.lock`)
2. Load task definition from `tasks.yaml`
3. Augment prompt with context from completed dependencies (prompt chaining)
4. Load project-wide config (`.kiln/config.yaml`)
5. Invoke Claude via `claude --model <model> --dangerously-skip-permissions --verbose --output-format stream-json -p <prompt>`
6. Parse the JSON footer from output
7. Run verify gates (if configured and footer is valid)
8. Update `state.json`
9. Write `.done` marker (if complete and all gates passed)
10. Write execution log to `.kiln/logs/<task-id>.json`
11. Release lock

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--task-id` | *(required)* | Task identifier (kebab-case) |
| `--prompt-file` | *(from tasks.yaml)* | Path to prompt file. If omitted, resolved from the task's `prompt` field |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml for auto-resolving prompt, model, retries, and verify gates |
| `--model` | *(from KILN_MODEL or default)* | Claude model to use. Overrides `KILN_MODEL` env var and task-level model |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |
| `--retries` | `0` | Number of additional attempts on retryable failures |
| `--retry-backoff` | `0s` | Sleep duration between retry attempts |
| `--backoff` | `fixed` | Backoff strategy: `fixed` or `exponential`. Exponential adds jitter (0–50% of delay), capped at 5 minutes |
| `--force-unlock` | `false` | Remove the existing lock file before acquiring (use for stale locks from crashed processes) |
| `--skip-verify` | `false` | Skip all verify gates. Logs a warning. Useful for debugging |
| `--no-chain` | `false` | Disable prompt chaining (skip injecting context from completed dependencies) |
| `--max-context-bytes` | `50000` | Maximum bytes of injected dependency context (~50KB). Oldest contexts are truncated first |

**Exit codes:**

| Code | Meaning |
|---|---|
| `0` | Claude reported `complete` and all verify gates passed |
| `2` | Claude reported `not_complete` or `blocked`; or a verify gate failed |
| `10` | Permanent failure: missing/invalid footer, schema error, lock conflict |
| `20` | Transient retries exhausted (timeout or Claude process crash) |

**Examples:**

```bash
# Basic execution (prompt resolved from tasks.yaml)
kiln exec --task-id implement-auth

# With custom timeout and retries
kiln exec --task-id implement-auth --timeout 30m --retries 2 --retry-backoff 30s

# Exponential backoff: 30s, ~60s, ~120s (with jitter)
kiln exec --task-id implement-auth --retries 3 --retry-backoff 30s --backoff exponential

# Force-unlock a stale lock from a previously crashed process
kiln exec --task-id implement-auth --force-unlock

# Skip verify gates for debugging
kiln exec --task-id implement-auth --skip-verify

# Disable prompt chaining (run without injecting dependency context)
kiln exec --task-id implement-auth --no-chain

# Use a specific model
kiln exec --task-id implement-auth --model claude-opus-4-6
```

---

### `kiln plan`

Reads `PRD.md` and an extraction prompt, then invokes Claude to produce `.kiln/tasks.yaml`. The generated file is validated against the task schema before `kiln plan` returns.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--prd` | `PRD.md` | Path to the PRD file |
| `--prompt` | `.kiln/prompts/00_extract_tasks.md` | Path to the extraction prompt |
| `--out` | `.kiln/tasks.yaml` | Output path for the generated tasks.yaml |
| `--model` | `claude-opus-4-6` | Claude model to use |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |

**Example:**

```bash
# Use defaults
kiln plan

# Custom PRD file and output location
kiln plan --prd docs/REQUIREMENTS.md --out .kiln/tasks.yaml

# Use a specific model
kiln plan --prd PRD.md --model claude-sonnet-4-6
```

---

### `kiln gen-make`

Reads `tasks.yaml` and generates `.kiln/targets.mk` — a Make include file containing targets for each task respecting the dependency graph. Also creates runtime subdirectories (`done/`, `logs/`, `locks/`, `unify/`, `artifacts/research/`).

Tasks that have a `phase` field get corresponding `phase-<name>` Make targets. Tasks that have a `milestone` field get corresponding `milestone-<name>` Make targets.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | *(required)* | Path to tasks.yaml |
| `--out` | *(required)* | Output path for targets.mk |
| `--dev-phase` | `0` | Filter to a specific dev-phase number. `0` means include all tasks |

**Examples:**

```bash
# Generate targets for all tasks
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk

# Generate targets for phase 1 tasks only
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk --dev-phase 1
```

---

### `kiln gen-prompts`

Generates per-task prompt files from a template. For each task in `tasks.yaml`, invokes Claude (using `claude-opus-4-6` by default) to fill in the template with task-specific instructions derived from the PRD. Skips tasks that already have a prompt file unless `--overwrite` is set.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--prd` | `PRD.md` | Path to PRD file |
| `--template` | `.kiln/templates/<id>.md` | Path to the prompt template file |
| `--model` | `claude-opus-4-6` | Claude model to use |
| `--timeout` | `60m` | Timeout per Claude invocation |
| `--overwrite` | `false` | Regenerate prompts even when the file already exists |

**Examples:**

```bash
# Generate prompts for tasks that don't yet have one
kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md

# Regenerate all prompts
kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md --overwrite

# Use a custom template
kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md --template .kiln/templates/custom.md
```

---

### `kiln status`

Prints a task scoreboard showing the current status of every task in `tasks.yaml`. Status is derived by consulting `state.json`, done markers, log files, and the dependency graph — in that priority order.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | *(required)* | Path to tasks.yaml |
| `--format` | *(table)* | Output format: omit for a human-readable table, or `json` for machine-readable output |

**Table columns:**

| Column | Description |
|---|---|
| `TASK` | Task ID |
| `STATUS` | `complete`, `failed`, `not_complete`, `blocked`, `pending`, or `running` |
| `ATTEMPTS` | Number of execution attempts recorded in state.json |
| `KIND` | Task kind (e.g., `feature`, `fix`, `research`, `docs`) |
| `PHASE` | Task phase (e.g., `plan`, `build`, `verify`, `docs`) |
| `LAST ERROR` | Most recent error class or message |
| `NEEDS` | Comma-separated list of dependency task IDs |

The table is followed by a summary line showing completed vs. total tasks and a count of runnable (pending) tasks.

**Examples:**

```bash
# Human-readable table
kiln status --tasks .kiln/tasks.yaml

# Machine-readable JSON
kiln status --tasks .kiln/tasks.yaml --format json
```

**JSON output structure:**

```json
{
  "tasks": [
    {
      "id": "implement-auth",
      "status": "complete",
      "attempts": 1,
      "kind": "feature",
      "phase": "build"
    }
  ],
  "summary": {
    "total": 5,
    "complete": 3,
    "failed": 1,
    "not_complete": 0,
    "blocked": 0,
    "pending": 1,
    "running": 0
  }
}
```

---

### `kiln report`

Reads all log files from `.kiln/logs/` and produces an execution summary report. Shows per-task status, attempt counts, error class aggregation, and totals.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--format` | `table` | Output format: `table` or `json` |
| `--log-dir` | `.kiln/logs` | Path to the logs directory |

**Examples:**

```bash
# Human-readable table report
kiln report

# JSON report for CI processing
kiln report --format json

# Report from a non-default log directory
kiln report --log-dir /tmp/kiln-logs
```

The summary section includes:
- Total tasks, complete, failed, not_complete, blocked counts
- Total attempt count across all tasks
- Top error classes and their frequencies (e.g., `timeout (3), claude_exit (1)`)

---

### `kiln unify`

Generates a **closure artifact** for a completed task. A closure artifact is a semantic summary of what actually happened during task execution — bridging the gap between "task ran" and "task is fully reconciled."

Kiln invokes Claude with a structured prompt that asks it to inspect the git history and repository state, then produce a Markdown document covering:

- **What Changed** — files modified, functions added/removed, key code changes
- **What's Incomplete or Deferred** — known gaps, TODOs, explicitly deferred work
- **Decisions Made** — key design decisions and rationale
- **Handoff Notes** — context, caveats, and gotchas for downstream tasks
- **Acceptance Criteria Coverage** — MET/UNMET assessment (if criteria were defined)

The artifact is written to `.kiln/unify/<task-id>.md` and an entry is appended to `.kiln/decisions.log`. These artifacts are the primary context source for prompt chaining — when a downstream task runs, Kiln injects UNIFY summaries from completed dependencies into the prompt automatically.

The task must be marked complete (either in `state.json` or via a `.done` marker) before `kiln unify` will run.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--task-id` | *(required)* | Task identifier (kebab-case) |
| `--model` | *(from KILN_MODEL or default)* | Claude model to use |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |

**Examples:**

```bash
# Generate closure artifact for a completed task
kiln unify --task-id implement-auth

# Use a more capable model for the summary
kiln unify --task-id implement-auth --model claude-opus-4-6
```

---

### `kiln retry`

Re-runs tasks by removing their `.done` markers and invoking `kiln exec` again. Can target a specific task, all failed tasks, or only tasks that failed due to transient (retryable) errors.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--task-id` | *(empty)* | Retry a specific task by ID |
| `--failed` | `false` | Retry only tasks with `failed` status |
| `--transient-only` | `false` | With `--failed`, retry only tasks whose log shows `retryable: true` |

**Examples:**

```bash
# Retry all non-complete, non-pending tasks
kiln retry --tasks .kiln/tasks.yaml

# Retry only failed tasks
kiln retry --tasks .kiln/tasks.yaml --failed

# Retry only tasks that failed due to transient errors (timeouts, crashes)
kiln retry --tasks .kiln/tasks.yaml --failed --transient-only

# Retry a specific task regardless of status
kiln retry --tasks .kiln/tasks.yaml --task-id implement-auth
```

---

### `kiln reset`

Clears the done marker and archives the log file for one or all tasks, and removes the task's entry from `state.json`. Use this to re-run a task from scratch, as if it had never been executed.

Log files are archived (renamed to `.json.bak`) rather than deleted, so you can inspect the previous execution history.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--task-id` | *(empty)* | Task ID to reset (mutually exclusive with `--all`) |
| `--all` | `false` | Reset all tasks. Prompts for confirmation before proceeding |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml (used to validate task IDs and enumerate tasks for `--all`) |

**Examples:**

```bash
# Reset a single task
kiln reset --task-id implement-auth --tasks .kiln/tasks.yaml

# Reset all tasks (prompts: "Reset all 5 tasks? [y/N]")
kiln reset --all --tasks .kiln/tasks.yaml
```

---

### `kiln resume`

Generates a **resume prompt** with prior execution context for a task that has already been attempted. Prints to stdout. The output includes:

- Prior attempt count and last status/error
- The UNIFY closure artifact (if one was generated via `kiln unify`)
- The original task prompt

Pipe the output back to Claude to resume work with full context, without re-reading logs manually.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--task-id` | *(required)* | Task ID to resume |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |

**Example:**

```bash
# Print resume context to stdout
kiln resume --task-id implement-auth --tasks .kiln/tasks.yaml

# Pipe directly into a new Claude session
kiln resume --task-id implement-auth | claude -p -
```

If no prior attempts are found for the task, `kiln resume` prints a message and exits cleanly. Use `kiln exec` instead for a first-time run.

---

### `kiln verify-plan`

Checks the task graph for verification coverage gaps: tasks that have `acceptance` criteria but no `verify` gates. Also flags tasks with gates but no acceptance criteria (`UNANCHORED`). Checks that gate commands are plausibly executable.

In `--strict` mode (CI usage), warnings are treated as errors and the command exits non-zero if any issues are found.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--config` | `.kiln/config.yaml` | Path to project config (for default gates) |
| `--strict` | `false` | Treat warnings as errors (CI mode). Exits non-zero if any issues found |
| `--format` | `text` | Output format: `text` or `json` |

**Issue types:**

| Type | Meaning |
|---|---|
| `UNCOVERED` | Task has acceptance criteria but no verify gates |
| `UNANCHORED` | Task has verify gates but no acceptance criteria |

**Examples:**

```bash
# Check coverage, text output
kiln verify-plan --tasks .kiln/tasks.yaml

# CI mode — fail on any issue
kiln verify-plan --tasks .kiln/tasks.yaml --strict

# JSON output for automation
kiln verify-plan --tasks .kiln/tasks.yaml --format json
```

**JSON output structure:**

```json
{
  "summary": {
    "total_tasks": 5,
    "covered": 3,
    "uncovered": 1,
    "warnings": 1
  },
  "issues": [
    {
      "task_id": "implement-auth",
      "type": "UNCOVERED",
      "message": "Task has 3 acceptance criteria but no verify gates"
    }
  ],
  "pass": false
}
```

---

### `kiln validate-schema`

Validates that `tasks.yaml` is well-formed: all required fields present, IDs are kebab-case, no unknown fields, `verify[].expect` values are supported, etc.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | *(required)* | Path to tasks.yaml |

**Example:**

```bash
kiln validate-schema --tasks .kiln/tasks.yaml
# Output: validate-schema: OK (12 tasks)
```

Exits non-zero and prints an error if the schema is invalid.

---

### `kiln validate-cycles`

Checks the task dependency graph for cycles. Also verifies that all task IDs referenced in `needs` lists actually exist in `tasks.yaml`.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--tasks` | *(required)* | Path to tasks.yaml |

**Example:**

```bash
kiln validate-cycles --tasks .kiln/tasks.yaml
# Output: validate-cycles: OK
```

On a cycle:
```
cycle detected: implement-auth -> write-api-tests -> implement-auth
```

---

## Part 3: Task Schema Reference

Tasks are defined in `tasks.yaml` as a YAML list. Unknown fields are rejected (strict schema). The schema uses `KnownFields(true)` validation — any unrecognized field causes a parse error.

### Fields

#### Required

| Field | Type | Description |
|---|---|---|
| `id` | string | Task identifier. Must be **kebab-case**: `^[a-z0-9]+(?:-[a-z0-9]+)*$` |
| `prompt` | string | **Relative** path to the task's prompt file (e.g., `.kiln/prompts/tasks/my-task.md`) |

#### Optional

| Field | Type | Description |
|---|---|---|
| `needs` | `[]string` | List of task IDs that must complete before this task runs. Each entry must be non-empty and reference a valid task ID |
| `timeout` | string | Per-task timeout override (e.g., `30m`, `2h`). Overrides the `--timeout` flag for this task when invoked via generated Make targets |
| `model` | string | Per-task model override. Takes precedence over `KILN_MODEL` env var |
| `description` | string | Human-readable description of the task (not used at runtime) |
| `kind` | string | Task classification: `feature`, `fix`, `research`, `docs`, or any non-whitespace string. Tasks with `kind: research` are expected (by convention) to produce an artifact at `.kiln/artifacts/research/<id>.md` |
| `tags` | `[]string` | Arbitrary tags. Each tag must be non-empty and contain no whitespace |
| `retries` | int | Number of additional retry attempts on transient failures. Must be `>= 0`. Overrides `--retries` flag |
| `validation` | `[]string` | Freeform validation notes (not enforced at runtime; informational only) |
| `engine` | string | Engine identifier (reserved for future multi-engine support) |
| `env` | `map[string]string` | Extra environment variables injected into the Claude process. Keys must match `^[A-Za-z_][A-Za-z0-9_]*$` |
| `dev-phase` | int | Numeric phase for phased rollouts. Used with `kiln gen-make --dev-phase N` to filter targets |
| `phase` | string | Human-oriented lifecycle phase (e.g., `plan`, `build`, `verify`, `docs`). Must be non-whitespace if present. Tasks with a `phase` get a `phase-<name>` Make target |
| `milestone` | string | Project milestone grouping (e.g., `m1-auth`). Must be kebab-case if present. Tasks with a `milestone` get a `milestone-<name>` Make target |
| `acceptance` | `[]string` | List of acceptance criteria (e.g., Given/When/Then or bullet ACs). Each entry must be non-empty. Used by `kiln verify-plan` |
| `verify` | `[]VerifyGate` | List of verification gates to run post-completion before the `.done` marker is written. Set to `[]` (empty list) to explicitly opt out of project-level defaults |
| `lane` | string | Concurrency grouping identifier. Must be kebab-case if present. Tasks in the same lane run serially (informational — not currently enforced by kiln) |
| `exclusive` | bool | If `true`, this task should run with no other tasks in parallel (informational — not currently enforced by kiln) |

### VerifyGate Fields

Each entry in the `verify` list is a gate object:

| Field | Type | Required | Description |
|---|---|---|---|
| `cmd` | string | yes | Shell command to run (executed via `/bin/sh -c`). Exit code `0` = pass |
| `name` | string | no | Human-readable label for the gate. Defaults to `cmd` if omitted |
| `expect` | string | no | Expected outcome. Currently only `exit_code_zero` is supported. Defaults to exit code zero check if omitted |

Gates run sequentially and fail-fast: the first failed gate stops execution. Output (stdout+stderr combined) is captured and included in the log (truncated to 2000 characters).

### Complete Annotated Example

```yaml
- id: implement-auth
  prompt: .kiln/prompts/tasks/implement-auth.md

  # Dependencies: this task waits for setup-database to complete
  needs:
    - setup-database

  # Override timeout for this task only (default: 60m from --timeout)
  timeout: 90m

  # Override model for this task only
  model: claude-opus-4-6

  # Human-readable description (not used at runtime)
  description: "Implement JWT authentication with refresh token support"

  # Task classification
  kind: feature

  # Lifecycle phase (generates `make phase-build` target)
  phase: build

  # Milestone grouping (generates `make milestone-m1-auth` target)
  milestone: m1-auth

  # Dev phase (used with `make graph DEV_PHASE=1`)
  dev-phase: 1

  # Tags for filtering/grouping (informational)
  tags:
    - security
    - backend

  # Number of retry attempts on transient failures
  retries: 2

  # Extra environment variables injected into the Claude process
  env:
    DATABASE_URL: postgres://localhost:5432/myapp
    NODE_ENV: test

  # Acceptance criteria (used by `kiln verify-plan` for coverage checking)
  acceptance:
    - "Given a valid username and password, the user receives a JWT token"
    - "Given an expired token, the refresh endpoint returns a new token"
    - "Given an invalid token, the API returns 401"

  # Verify gates: run post-completion before `.done` is written
  # Set to `[]` to opt out of project-level defaults
  verify:
    - name: "Unit tests pass"
      cmd: "go test ./internal/auth/..."
      expect: exit_code_zero

    - name: "Integration tests pass"
      cmd: "go test -tags integration ./test/..."

    - name: "No secrets in diff"
      cmd: "git diff HEAD~1 | grep -qv 'password\\|secret\\|api_key'"

  # Concurrency grouping (informational)
  lane: backend-services

  # Must run alone (informational)
  exclusive: false
```

---

## Part 4: Configuration & Environment

### Versioning

Kiln uses `ldflags` + git tags for version injection. The `version` variable in `main.go` defaults to `"dev"` and is overridden at build time.

**Tagging a release:**

```bash
git tag v0.1.0
git push origin v0.1.0
make bin/kiln     # produces binary with version "v0.1.0"
kiln version      # prints: v0.1.0
```

**Version string semantics:**

| Scenario | Version string |
|---|---|
| Clean tagged commit | `v0.1.0` |
| Tagged commit with local changes | `v0.1.0-dirty` |
| Untagged commit | `abc1234` (short SHA) |
| Untagged commit with local changes | `abc1234-dirty` |
| Built without ldflags | `dev` |

**Semver conventions:**
- **Major** (`v1.0.0`) — breaking changes to CLI flags, task schema, or exit codes
- **Minor** (`v0.2.0`) — new commands, new optional schema fields, new flags
- **Patch** (`v0.1.1`) — bug fixes with no interface changes

### Environment Variables

| Variable | Description |
|---|---|
| `KILN_MODEL` | Default Claude model for all commands. Falls back to `claude-sonnet-4-6` if not set |

**Model resolution precedence** (highest to lowest):

1. `--model` flag (if provided)
2. Per-task `model` field in `tasks.yaml` (for `kiln exec`)
3. `KILN_MODEL` environment variable
4. Built-in default: `claude-sonnet-4-6`

Note: `kiln plan` and `kiln gen-prompts` default to `claude-opus-4-6` instead of `claude-sonnet-4-6` when no model flag or env var is set.

### `.kiln/config.yaml`

Optional project-level configuration file. Currently used to set default verify gates that apply to all tasks (unless a task explicitly opts out with `verify: []`).

```yaml
defaults:
  verify:
    - name: "Build passes"
      cmd: "go build ./..."
      expect: exit_code_zero

    - name: "Tests pass"
      cmd: "go test ./..."
      expect: exit_code_zero
```

The config is loaded by `kiln exec` and `kiln verify-plan`. If `.kiln/config.yaml` does not exist, no defaults are applied.

**Gate merging rules:**

| Task `verify` field | Behavior |
|---|---|
| Absent (not set) | Project defaults are used |
| `null` | Project defaults are used |
| `[]` (empty list) | Project defaults are **not** used (explicit opt-out) |
| Non-empty list | Project defaults prepended to task gates |

### Makefile Integration

The standard Makefile pattern for a Kiln project:

```makefile
# ---- Config ----
KILN := ./bin/kiln
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# ---- Version injection ----
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# ---- Build binary when source changes ----
$(KILN): cmd/kiln/main.go
	go build -ldflags "-X main.version=$(VERSION)" -o $(KILN) ./cmd/kiln

# ---- Include generated targets ----
-include $(TARGETS_FILE)

# ---- Workflow targets ----
.PHONY: plan graph clean

plan: $(KILN)
	$(KILN) plan

graph: $(KILN)
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE) \
		$(if $(DEV_PHASE),--dev-phase $(DEV_PHASE))

.PHONY: clean
clean:
	rm -rf .kiln/done .kiln/logs $(TARGETS_FILE)
```

**Key patterns:**

- `-include $(TARGETS_FILE)` — uses `-include` (not `include`) so the Makefile doesn't error if `targets.mk` doesn't exist yet
- `$(if $(DEV_PHASE),--dev-phase $(DEV_PHASE))` — passes `--dev-phase` only when `DEV_PHASE` is set: `make graph DEV_PHASE=1`
- `$(KILN): cmd/kiln/main.go` — rebuilds the binary automatically when source changes
- `make -j3 all` — runs up to 3 tasks in parallel, respecting the dependency graph

---

## Part 5: Features Coming Soon

### Machine-Readable Output Mode

A `--format json|text` flag for `kiln exec` and `kiln gen-make` stdout output, enabling structured consumption by CI/CD pipelines, dashboards, and automation tooling. Today, Kiln writes structured JSON to log files; this feature exposes a stable stdout contract for immediate consumption without needing to read log files after the fact.

### `kiln init` Scaffolding

A command to bootstrap a new Kiln project from scratch. Running `kiln init` will scaffold the `.kiln/` directory structure, install prompt templates, create or patch a `Makefile`, and generate example `PRD.md` and `tasks.yaml` templates. Profile support (e.g., `kiln init --profile go`) will tailor the scaffold for specific language ecosystems, reducing adoption friction to near-zero for new repositories.

### Interactive TUI Dashboard

A full-screen terminal UI for real-time monitoring and control of Kiln task graph execution. The dashboard will display a live task graph with color-coded status indicators, stream log output from the currently executing task, and provide keyboard controls to trigger, retry, skip, or reset individual tasks. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), the TUI will make `make -jN` output readable by consolidating interleaved logs from parallel tasks into a coherent, navigable view.

### Git Automation

Optional git integration controlled by flags or config. Features include verifying that a commit was made before allowing task completion, auto-committing with templated messages derived from the task's closure artifact, branch-per-task mode for isolated development, and PR creation hooks. All git automation is opt-in — the default behavior remains unchanged.

### Engine Abstraction

Multi-engine support beyond Claude Code. Kiln's `commandBuilder` and footer parsing will be abstracted behind an `Engine` interface, allowing per-task engine selection via a `kiln exec --engine codex` flag or an `engine` field in `tasks.yaml`. Each engine will have its own output parser, error classifier, and result schema normalization, enabling cost/quality tradeoffs per task type and providing fallbacks when one engine is rate-limited or unavailable.

### Profile Strategy

Selectable workflow profiles that control default behavior across the full task lifecycle. A `speed` profile minimizes gates, maximizes parallelism, and makes UNIFY optional — for prototype sprints where shipping fast matters most. A `reliable` profile requires UNIFY closure artifacts, enforces verify gates before `.done` creation, and uses conservative retry settings — for production releases where correctness gates matter. Profiles are implemented as default-value presets in `.kiln/config.yaml`, with individual task overrides always taking precedence.

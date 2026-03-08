# Kiln User Guide

**Version: v0.1.0-dirty**

This guide covers Kiln version v0.1.0. It is self-contained: a developer should be able to use Kiln end-to-end using only this document.

---

## Table of Contents

1. [Part 1: Getting Started Tutorial](#part-1-getting-started-tutorial)
   - [What is Kiln?](#what-is-kiln)
   - [Prerequisites](#prerequisites)
   - [Installation](#installation)
   - [Project Setup](#project-setup)
   - [The Three-Step Workflow](#the-three-step-workflow)
   - [Understanding Task Results](#understanding-task-results)
   - [Prompt Generation](#prompt-generation)
   - [When Things Fail](#when-things-fail)
2. [Part 2: Command Reference](#part-2-command-reference)
3. [Part 3: Task Schema Reference](#part-3-task-schema-reference)
4. [Part 4: Configuration & Environment](#part-4-configuration--environment)
5. [Part 5: Features Coming Soon](#part-5-features-coming-soon)

---

## Part 1: Getting Started Tutorial

### What is Kiln?

Kiln is a Go CLI tool that prevents **context rot** in AI-assisted development. Context rot happens when a Claude Code session grows stale — the AI assistant accumulates unrelated context from earlier in a long session, leading to degraded output quality, forgotten instructions, and increasingly unreliable behavior.

Kiln's answer is the **Ralph Wiggum pattern**: each Claude Code task runs in a fresh invocation with only the context it needs for that one job. Tasks are defined as nodes in a dependency graph, each with their own prompt file. Make handles parallel execution and dependency ordering. Kiln handles the invocation lifecycle, structured logging, retry logic, and post-completion verification.

The result is a workflow where AI agents operate predictably at scale: PRD → `tasks.yaml` → generated Make targets → executed tasks with timeouts, retries, verify gates, and structured logs. Large codebases that would overwhelm a single session become a set of focused, composable units.

---

### Prerequisites

Before using Kiln, ensure you have:

- **Go 1.21+** — for building from source
- **Claude Code CLI** — installed and authenticated (`claude --version` should work)
- **GNU Make** — for orchestrating the task graph
- **Git** — for version tagging and the `git describe`-based version string

---

### Installation

Build from source in the project root:

```bash
# Builds with version injection from git tags
make bin/kiln

# Or build without version injection (prints "dev")
go build -o bin/kiln ./cmd/kiln
```

Verify the binary works:

```bash
./bin/kiln version
# v0.1.0
```

You can also put the binary on your `PATH`:

```bash
cp bin/kiln /usr/local/bin/kiln
kiln version
```

---

### Project Setup

Kiln stores all its runtime data in a `.kiln/` directory at the project root. The fastest way to get started is:

```bash
kiln init --profile go
```

This scaffolds the complete directory structure, a starter `tasks.yaml`, prompt templates, and a `Makefile`. Use `--profile python`, `--profile node`, or `--profile generic` for other project types.

If you prefer to set up manually, here is the full directory layout:

```
.kiln/
├── tasks.yaml              # Task graph definition (you author this)
├── targets.mk              # Generated Make targets (do not hand-edit)
├── config.yaml             # Optional project-wide configuration
├── state.json              # Runtime state across exec invocations
├── decisions.log           # Append-only UNIFY decision ledger (JSONL)
├── prompts/
│   ├── 00_extract_tasks.md # PRD extraction prompt used by `kiln plan`
│   └── tasks/
│       ├── my-task.md      # Per-task prompt files
│       └── another-task.md
├── done/
│   └── my-task.done        # Idempotency marker written on completion
├── logs/
│   └── my-task.json        # Structured execution log (one per task)
├── locks/
│   └── my-task.lock        # Held during execution; prevents duplicate runs
├── unify/
│   └── my-task.md          # UNIFY closure artifact
└── artifacts/
    └── research/
        └── research-task.md  # Artifacts from tasks with kind: research
```

---

### The Three-Step Workflow

#### Step 1: Plan — `make plan`

`make plan` invokes `kiln plan`, which uses Claude to read your `PRD.md` and generate a structured `tasks.yaml`.

```bash
# Edit your PRD first
vi PRD.md

# Then generate tasks.yaml
make plan
```

Under the hood, Kiln reads the extraction prompt at `.kiln/prompts/00_extract_tasks.md`, combines it with the PRD content, and asks Claude to produce a valid task graph. The result is validated on the way out — if Claude produces invalid YAML or missing required fields, `kiln plan` exits with an error.

You can also run `kiln plan` directly with custom flags:

```bash
kiln plan \
  --prd PRD.md \
  --prompt .kiln/prompts/00_extract_tasks.md \
  --out .kiln/tasks.yaml \
  --model claude-opus-4-6 \
  --timeout 60m
```

After `make plan` completes, review `.kiln/tasks.yaml` and edit it manually to add verify gates, adjust timeouts, or tweak dependencies before proceeding.

#### Step 2: Graph — `make graph`

`make graph` runs `kiln gen-make`, which reads `tasks.yaml` and writes `targets.mk` — a Makefile include file that defines a Make target for every task.

```bash
make graph
```

Each target in `targets.mk` looks like this:

```makefile
.kiln/done/my-feature.done: .kiln/done/setup.done
	$(KILN) exec --task-id my-feature
```

The `--dev-phase` flag lets you limit which tasks are included in the generated Makefile, useful for phased rollouts:

```bash
# Only include phase-1 tasks in targets.mk
make graph DEV_PHASE=1
```

The Makefile also automatically creates subdirectories (`done/`, `logs/`, `locks/`, `unify/`, `artifacts/research/`) if they don't exist.

#### Step 3: Execute — `make all`

`make all` runs all tasks in the dependency graph. Tasks with no unmet dependencies run immediately; tasks with dependencies wait until their dependencies have `.done` markers.

```bash
# Run all tasks sequentially
make all

# Run up to 4 tasks in parallel (Make handles ordering)
make -j4 all
```

Make reads `targets.mk` (included via `-include .kiln/targets.mk` in the Makefile) and calls `kiln exec --task-id <id>` for each task. When a task completes successfully, Kiln writes a `.kiln/done/<id>.done` marker. Make uses these markers for idempotency — if you run `make all` again, completed tasks are skipped.

You can run a single task directly:

```bash
kiln exec --task-id my-feature
```

Or run all tasks for a specific phase or milestone:

```bash
make phase-build
make milestone-m1-auth
```

---

### Understanding Task Results

**Done markers** — When a task finishes with `status: complete` and all verify gates pass, Kiln writes `.kiln/done/<task-id>.done`. Make uses this file as the target, so re-running `make all` skips already-complete tasks.

**Structured logs** — Every execution attempt writes a JSON log to `.kiln/logs/<task-id>.json`:

```json
{
  "task_id": "my-feature",
  "started_at": "2026-03-08T10:00:00Z",
  "ended_at": "2026-03-08T10:05:32Z",
  "duration_ms": 332000,
  "model": "claude-sonnet-4-6",
  "prompt_file": ".kiln/prompts/tasks/my-feature.md",
  "exit_code": 0,
  "status": "complete",
  "footer_valid": true,
  "verify": {
    "gates": [{"name": "tests", "cmd": "go test ./...", "passed": true, "exit_code": 0}],
    "all_passed": true,
    "skipped": false
  }
}
```

**Exit codes**:

| Code | Meaning |
|------|---------|
| `0`  | Task completed successfully (`status: complete`, all verify gates passed) |
| `2`  | Task returned `not_complete` or `blocked` (Claude could not finish; retry manually) |
| `10` | Permanent failure — footer parse error, validation error, or lock conflict (do not auto-retry) |
| `20` | Transient failure — timeout or Claude process crash (retryable) |

**The JSON footer contract** — Claude's output must end with a structured footer that Kiln parses to determine task status:

```json
{"kiln":{"status":"complete","task_id":"my-feature"}}
```

Allowed `status` values:
- `complete` — task finished; Kiln writes the `.done` marker
- `not_complete` — task made progress but couldn't finish; no `.done` marker written
- `blocked` — task cannot proceed due to a dependency or missing information

If Claude's output does not contain a valid footer, `kiln exec` returns exit code 10.

**State file** — `.kiln/state.json` tracks per-task status across invocations, including attempt count, last error class, timestamps, and completion metadata. Use `kiln status` to read it in a human-friendly table.

---

### Prompt Generation

Instead of writing per-task prompts by hand, use `kiln gen-prompts` to auto-generate them from a template:

```bash
kiln gen-prompts \
  --tasks .kiln/tasks.yaml \
  --prd PRD.md \
  --template .kiln/templates/<id>.md \
  --overwrite
```

The template at `.kiln/templates/<id>.md` is a prompt skeleton with placeholder sections for task ID, scope, requirements, acceptance criteria, and the required JSON footer. Kiln sends the template plus the PRD content to Claude, which fills in the task-specific details and writes each prompt file to the path specified in `tasks.yaml`.

If a prompt file already exists and `--overwrite` is not set, Kiln skips it.

---

### When Things Fail

1. **Check the scoreboard**: `kiln status --tasks .kiln/tasks.yaml` shows every task's status, attempt count, kind, phase, and last error.

2. **Read the log**: Open `.kiln/logs/<task-id>.json` for detailed execution data, or run `kiln report` for an aggregate summary across all tasks.

3. **Retry failed tasks**:
   ```bash
   # Retry a single task
   kiln retry --task-id my-feature --tasks .kiln/tasks.yaml

   # Retry all failed tasks
   kiln retry --failed --tasks .kiln/tasks.yaml

   # Retry only transiently-failed tasks (timeouts, claude crashes)
   kiln retry --failed --transient-only --tasks .kiln/tasks.yaml
   ```

4. **Reset a task** to clear its done marker and state so it can be re-run:
   ```bash
   kiln reset --task-id my-feature --tasks .kiln/tasks.yaml
   ```

5. **Generate a resume prompt** for manual re-invocation with prior context:
   ```bash
   kiln resume --task-id my-feature --tasks .kiln/tasks.yaml
   ```

6. **Generate a UNIFY closure artifact** after a task completes to capture what changed, what's deferred, and decisions made — useful context for downstream tasks:
   ```bash
   kiln unify --task-id my-feature
   ```

---

## Part 2: Command Reference

### `kiln version`

Prints the current Kiln version string.

```bash
kiln version
# v0.1.0
```

The version is injected at build time via `ldflags` using `git describe --tags --always --dirty`. Building via `make bin/kiln` automatically injects the version. Building with bare `go build` without ldflags prints `dev`.

---

### `kiln exec`

Runs a single Claude Code invocation for one task. This is the core execution engine.

**Usage:**

```bash
kiln exec --task-id my-feature [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--task-id` | (required) | Kebab-case task identifier |
| `--prompt-file` | (from tasks.yaml) | Path to the prompt file; if omitted, resolved from `tasks.yaml` |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml; used to resolve prompt, model, retries, and verify gates |
| `--model` | `KILN_MODEL` or `claude-sonnet-4-6` | Claude model to use; overrides `KILN_MODEL` env var |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |
| `--retries` | `0` (or from profile) | Number of additional attempts on retryable failures |
| `--retry-backoff` | `0s` (or from profile) | Sleep duration between retry attempts |
| `--backoff` | `fixed` | Backoff strategy: `fixed` or `exponential` |
| `--force-unlock` | `false` | Remove existing stale lock file before acquiring a new one |
| `--skip-verify` | `false` | Skip all verify gates (logs a warning; useful for debugging) |
| `--no-chain` | `false` | Disable prompt chaining (skip dependency context injection) |
| `--max-context-bytes` | `50000` | Maximum bytes of injected dependency context (~50 KB) |
| `--profile` | (from config.yaml) | Workflow profile: `speed` or `reliable` |
| `--format` | `text` | Output format: `text` (progress to stdout) or `json` (progress to stderr, final result to stdout) |

**Execution lifecycle:**

1. **Lock acquisition** — Creates `.kiln/locks/<task-id>.lock` to prevent duplicate parallel execution. If a lock already exists, returns exit code 10 with a `lockConflictError`.
2. **Context injection (prompt chaining)** — If the task has `needs` and `--no-chain` is not set, Kiln prepends dependency context from UNIFY artifacts, research artifacts, or execution logs (in that priority order), capped at `--max-context-bytes`.
3. **Claude invocation** — Runs `claude --model <model> --dangerously-skip-permissions --verbose --output-format stream-json -p <prompt>`.
4. **Footer parsing** — Scans Claude's output (last-first) for a valid `{"kiln":{...}}` JSON footer.
5. **Verify gates** — If `status: complete` and the task has verify gates configured, runs each gate sequentially. A gate failure prevents the `.done` marker from being written.
6. **State update** — Writes attempt count, status, timestamps, and error metadata to `.kiln/state.json`.
7. **Done marker** — Writes `.kiln/done/<task-id>.done` on success.

**Exit codes:**

| Code | Meaning |
|------|---------|
| `0`  | `status: complete`, all verify gates passed |
| `2`  | `status: not_complete` or `blocked`, or verify gate failure |
| `10` | Permanent failure (footer parse/validation error, lock conflict, schema error) |
| `20` | Transient failure — retries exhausted (timeout, Claude process crash) |

**Examples:**

```bash
# Basic execution
kiln exec --task-id add-auth

# With explicit model and timeout
kiln exec --task-id add-auth --model claude-opus-4-6 --timeout 30m

# With retries and exponential backoff
kiln exec --task-id add-auth --retries 3 --retry-backoff 10s --backoff exponential

# Bypass stale lock from a previous crashed run
kiln exec --task-id add-auth --force-unlock

# JSON output for CI integration
kiln exec --task-id add-auth --format json
```

**JSON output format** (`--format json`):

```json
{
  "task_id": "add-auth",
  "status": "complete",
  "exit_code": 0,
  "model": "claude-sonnet-4-6",
  "prompt_file": ".kiln/prompts/tasks/add-auth.md",
  "started_at": "2026-03-08T10:00:00Z",
  "ended_at": "2026-03-08T10:05:32Z",
  "duration_ms": 332000,
  "attempts": 1,
  "footer": {"kiln": {"status": "complete", "task_id": "add-auth"}},
  "footer_valid": true,
  "error": null
}
```

---

### `kiln plan`

Parses a PRD into a `tasks.yaml` file by invoking Claude with an extraction prompt.

**Usage:**

```bash
kiln plan [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--prd` | `PRD.md` | Path to the PRD file |
| `--prompt` | `.kiln/prompts/00_extract_tasks.md` | Path to the extraction prompt |
| `--out` | `.kiln/tasks.yaml` | Output path for the generated tasks.yaml |
| `--model` | `claude-opus-4-6` | Claude model to use |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |

**Example:**

```bash
kiln plan --prd PRD.md --out .kiln/tasks.yaml
```

After generating `tasks.yaml`, Kiln validates it against the task schema. If validation fails, it reports the error without writing an invalid file.

---

### `kiln gen-make`

Reads `tasks.yaml` and generates `targets.mk`, a Makefile include file that defines one Make target per task.

**Usage:**

```bash
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | (required) | Path to tasks.yaml |
| `--out` | (required) | Output path for targets.mk |
| `--dev-phase` | `0` (all) | Filter tasks to a specific `dev-phase` value; 0 includes all tasks |
| `--format` | `text` | Output format: `text` (no stdout) or `json` (prints target summary to stdout) |
| `--profile` | (from config.yaml) | Workflow profile: `speed` or `reliable`; affects `MAKEFLAGS` parallelism cap in output |

Also creates runtime subdirectories (`done/`, `logs/`, `locks/`, `unify/`, `artifacts/research/`) alongside the output file.

Generates `.PHONY` targets for each unique `phase` value (`phase-build`, `phase-verify`, etc.) and each unique `milestone` value (`milestone-m1-auth`, etc.).

**Example:**

```bash
# Generate for all tasks
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk

# Generate only phase-1 tasks
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk --dev-phase 1

# JSON output for CI
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk --format json
```

---

### `kiln gen-prompts`

Generates per-task prompt files from a template by invoking Claude once per task.

**Usage:**

```bash
kiln gen-prompts [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--prd` | `PRD.md` | Path to PRD file (injected into the meta-prompt) |
| `--template` | `.kiln/templates/<id>.md` | Path to the prompt template file |
| `--model` | `claude-opus-4-6` | Claude model to use |
| `--timeout` | `60m` | Timeout per Claude invocation |
| `--overwrite` | `false` | Regenerate prompts even when the file already exists |

For each task in `tasks.yaml`, Kiln checks whether the prompt file already exists. If it does and `--overwrite` is false, the task is skipped. Otherwise, it sends a meta-prompt (template + PRD + task ID) to Claude and expects Claude to write the prompt file to disk.

**Example:**

```bash
kiln gen-prompts \
  --tasks .kiln/tasks.yaml \
  --prd PRD.md \
  --template .kiln/templates/<id>.md \
  --overwrite
```

---

### `kiln status`

Displays a task scoreboard: status, attempt count, kind, phase, last error, and dependencies for every task.

**Usage:**

```bash
kiln status --tasks .kiln/tasks.yaml [--format json]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | (required) | Path to tasks.yaml |
| `--format` | (table) | Output format: omit for human-readable table, or `json` for machine-readable JSON |

**Table columns:**

| Column | Description |
|--------|-------------|
| TASK | Task ID |
| STATUS | `complete`, `failed`, `not_complete`, `blocked`, `pending`, `running` |
| ATTEMPTS | Number of execution attempts recorded in state.json |
| KIND | Task kind (feature, fix, research, docs) or `-` |
| PHASE | Lifecycle phase (plan, build, verify, docs) or `-` |
| LAST ERROR | Error class or message from last failed attempt |
| NEEDS | Comma-separated dependency task IDs |

Status is derived from (in priority order): `state.json` entry → `.done` marker → log file → dependency graph.

**Example:**

```bash
kiln status --tasks .kiln/tasks.yaml

TASK                           STATUS       ATTEMPTS KIND       PHASE      LAST ERROR                               NEEDS
----                           ------       -------- ----       -----      ----------                               -----
add-auth                       complete            1 feature    build      -                                        setup
add-tests                      pending             0 -          -          -                                        add-auth

2/3 tasks done, 1 runnable

Summary
-------
Total: 3 | Complete: 2 | Failed: 0 | Not Complete: 0 | Pending: 1 | Blocked: 0
```

---

### `kiln report`

Generates an execution summary report aggregating data from `.kiln/logs/` and `.kiln/state.json`.

**Usage:**

```bash
kiln report [--format table|json] [--log-dir .kiln/logs]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `table` | Output format: `table` (human-readable) or `json` (machine-readable) |
| `--log-dir` | `.kiln/logs` | Directory containing execution log files |

**Report sections:**

- Per-task table: task ID, status, attempt count, last error class, last error message
- Summary: total tasks, complete, failed, not_complete, blocked counts, total attempts
- Top errors: error class aggregation with counts (e.g., `timeout (2), footer_parse (1)`)

**Example:**

```bash
kiln report

Task                 Status        Attempts  Last Error Class   Last Error
----                 ------        --------  ----------------   ----------
add-auth             complete             1  -                  -
add-tests            failed               2  timeout            task add-tests timed out after 60m…

Summary
-------
Total: 2 | Complete: 1 | Failed: 1 | Not Complete: 0 | Blocked: 0
Attempts: 3
Top errors: timeout (1)
```

---

### `kiln unify`

Generates a UNIFY closure artifact for a completed task. The artifact captures what actually happened: what changed, what's incomplete, decisions made, and handoff notes.

**Usage:**

```bash
kiln unify --task-id my-feature [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--task-id` | (required) | Kebab-case task identifier |
| `--model` | `KILN_MODEL` or `claude-sonnet-4-6` | Claude model to use |
| `--timeout` | `60m` | Maximum duration for the Claude invocation |

**Behavior:**

1. Verifies the task is marked complete (via `state.json` or `.done` marker).
2. Reads the task's prompt file and last execution log.
3. Invokes Claude with a structured meta-prompt asking it to inspect git history and produce a closure summary.
4. Writes the result to `.kiln/unify/<task-id>.md`.
5. Appends an entry to `.kiln/decisions.log` (append-only JSONL decision ledger).

The closure artifact includes these sections: **What Changed**, **What's Incomplete or Deferred**, **Decisions Made**, **Handoff Notes**, and **Acceptance Criteria Coverage**.

When downstream tasks are run, `kiln exec` automatically injects the UNIFY artifact as context via prompt chaining (highest priority source).

**Example:**

```bash
kiln unify --task-id add-auth
# unify: closure artifact written to .kiln/unify/add-auth.md
```

---

### `kiln retry`

Re-runs tasks that did not complete successfully, optionally filtering by failure type.

**Usage:**

```bash
kiln retry [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--task-id` | (all) | Retry a specific task by ID |
| `--failed` | `false` | Retry only tasks with `failed` status |
| `--transient-only` | `false` | With `--failed`, retry only tasks with retryable error classes (timeout, claude_exit) |

For each selected task, `kiln retry` removes the `.done` marker and calls `kiln exec`.

**Examples:**

```bash
# Retry a single task
kiln retry --task-id add-auth --tasks .kiln/tasks.yaml

# Retry all failed tasks
kiln retry --failed --tasks .kiln/tasks.yaml

# Retry only timeout/crash failures (skip permanent failures)
kiln retry --failed --transient-only --tasks .kiln/tasks.yaml
```

---

### `kiln reset`

Clears a task's done marker and state, making it eligible to run again. The log file is archived (renamed to `<task-id>.json.bak`).

**Usage:**

```bash
kiln reset [--task-id my-feature | --all] [--tasks .kiln/tasks.yaml]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--task-id` | — | Reset a specific task (required unless `--all`) |
| `--all` | `false` | Reset all tasks; prompts for confirmation before proceeding |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |

**Examples:**

```bash
# Reset a single task
kiln reset --task-id add-auth --tasks .kiln/tasks.yaml

# Reset all tasks (interactive confirmation required)
kiln reset --all --tasks .kiln/tasks.yaml
```

---

### `kiln resume`

Generates a resume prompt for a task that has prior execution history. The prompt includes previous attempt metadata and any available UNIFY closure summary, so you can pass it to Claude manually for recovery.

**Usage:**

```bash
kiln resume --task-id my-feature [--tasks .kiln/tasks.yaml]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--task-id` | (required) | Task ID to resume |
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |

**Output sections:**

1. **RESUME CONTEXT** — Task ID, prior attempt count, last status, last error
2. **PREVIOUS CLOSURE SUMMARY** — Contents of `.kiln/unify/<task-id>.md` (if available)
3. **ORIGINAL TASK PROMPT** — Full prompt file contents

**Example:**

```bash
kiln resume --task-id add-auth > /tmp/resume-add-auth.md
# Then pipe the prompt to Claude manually or use it with kiln exec --prompt-file
```

---

### `kiln verify-plan`

Checks that every task with acceptance criteria has corresponding verify gates. Reports coverage gaps and executability issues.

**Usage:**

```bash
kiln verify-plan [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | `.kiln/tasks.yaml` | Path to tasks.yaml |
| `--config` | `.kiln/config.yaml` | Path to config.yaml (reads project-level default gates) |
| `--strict` | `false` | Treat warnings as errors; exits 1 on any issue (CI mode) |
| `--format` | `text` | Output format: `text` (human-readable) or `json` (machine-readable) |

**Issue types:**

| Type | Severity | Description |
|------|----------|-------------|
| `UNCOVERED` | Error | Task has acceptance criteria but no verify gates |
| `EMPTY_CMD` | Error | A verify gate has an empty or blank command |
| `UNANCHORED` | Warning (`--strict`: Error) | Task has verify gates but no acceptance criteria |
| `CMD_NOT_FOUND` | Warning (`--strict`: Error) | Gate executable not found on PATH |

Returns exit code 0 when all checks pass, 1 on any error-level issue.

**Example:**

```bash
# Check coverage in CI (fail on any issue)
kiln verify-plan --tasks .kiln/tasks.yaml --strict

# JSON output for automation
kiln verify-plan --tasks .kiln/tasks.yaml --format json
```

---

### `kiln validate-schema`

Validates a `tasks.yaml` file against the full task schema. Reports the task count on success, or a detailed error on failure.

**Usage:**

```bash
kiln validate-schema --tasks .kiln/tasks.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | (required) | Path to tasks.yaml |

Unknown fields in the YAML are rejected (strict mode). Returns exit code 0 on success, 1 on any validation error.

---

### `kiln validate-cycles`

Checks the task dependency graph for cycles. Also validates that all `needs` references point to existing task IDs.

**Usage:**

```bash
kiln validate-cycles --tasks .kiln/tasks.yaml
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks` | (required) | Path to tasks.yaml |

Returns `validate-cycles: OK` and exit code 0 when the graph is acyclic and all references are valid. On a cycle, it prints the cycle path (e.g., `cycle detected: task-a -> task-b -> task-a`) and exits 1.

---

### `kiln init`

Scaffolds a complete `.kiln/` directory structure, starter `tasks.yaml`, prompt templates, and a `Makefile` for a new project.

**Usage:**

```bash
kiln init [--profile go|python|node|generic] [--force]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--profile` | `generic` | Project profile: `go`, `python`, `node`, or `generic` |
| `--force` | `false` | Overwrite existing files |

**Created files:**

- `.kiln/tasks.yaml` — starter task graph with `hello-world` and `follow-up` tasks
- `.kiln/prompts/tasks/hello-world.md` — profile-specific hello-world prompt
- `.kiln/prompts/tasks/follow-up.md` — follow-up verification prompt
- `Makefile` — project Makefile with `plan`, `graph`, and `all` targets
- `PRD.md` — starter PRD template

Existing files are skipped unless `--force` is passed.

**Example:**

```bash
cd my-new-project
kiln init --profile go
# Next steps printed on completion:
#   1. Edit PRD.md to describe your project goals and tasks.
#   2. Run `make plan` to generate .kiln/tasks.yaml from your PRD.
#   3. Run `make all` to execute all tasks.
```

---

### `kiln profile`

Prints the active workflow profile and its resolved settings, after applying config file and override.

**Usage:**

```bash
kiln profile [--profile speed|reliable] [--config .kiln/config.yaml]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--profile` | (from config.yaml) | Workflow profile override: `speed` or `reliable` |
| `--config` | `.kiln/config.yaml` | Path to config.yaml |

**Example:**

```bash
kiln profile

profile: speed
require_unify: false
require_verify_gates: false
parallelism_limit: 0
retry_max: 2
retry_backoff_base: 5s

kiln profile --profile reliable

profile: reliable
require_unify: true
require_verify_gates: true
parallelism_limit: 2
retry_max: 4
retry_backoff_base: 10s
```

---

## Part 3: Task Schema Reference

Tasks are defined as YAML sequences in `.kiln/tasks.yaml`. The schema is strict: unknown fields produce a parse error.

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique kebab-case identifier. Must match `^[a-z0-9]+(?:-[a-z0-9]+)*$` |
| `prompt` | string | Relative path to the prompt file (e.g., `.kiln/prompts/tasks/my-task.md`) |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `needs` | list of strings | `[]` | Task IDs that must complete before this task runs |
| `timeout` | string | `60m` | Max duration for the Claude invocation (e.g., `30m`, `2h`) |
| `model` | string | `KILN_MODEL` or `claude-sonnet-4-6` | Claude model for this task; overrides env var |
| `description` | string | — | Human-readable description (not used by the CLI) |
| `kind` | string | — | Task type: `feature`, `fix`, `research`, `docs`. Tasks with `kind: research` conventionally write artifacts to `.kiln/artifacts/research/<id>.md` |
| `tags` | list of strings | `[]` | Free-form tags; no whitespace allowed in tag values |
| `retries` | int | `0` | Additional retry attempts on retryable failures |
| `validation` | list of strings | `[]` | Reserved; currently not enforced by the CLI |
| `engine` | string | — | Reserved; currently not enforced by the CLI (see Engine Abstraction in Coming Soon) |
| `env` | map | — | Extra environment variables injected for this task's Claude invocation. Keys must be valid env var names |
| `dev-phase` | int | `0` | Development phase for phased rollouts via `kiln gen-make --dev-phase` |
| `phase` | string | — | Human-oriented lifecycle phase (e.g., `plan`, `build`, `verify`, `docs`). Generates a `phase-<value>` Make target |
| `milestone` | string | — | Project milestone grouping in kebab-case. Generates a `milestone-<value>` Make target |
| `acceptance` | list of strings | `[]` | Acceptance criteria (e.g., Given/When/Then or bullet AC). Used by `kiln verify-plan` |
| `verify` | list of gate objects | (project defaults) | Post-completion validation gates. `[]` to opt out of project defaults |
| `lane` | string | — | Concurrency grouping identifier in kebab-case (field exists; Make-level enforcement is a coming-soon feature) |
| `exclusive` | bool | `false` | If true, intended to prevent parallel execution with other tasks (field exists; Make-level enforcement is a coming-soon feature) |

### Verify Gate Schema

Each entry in the `verify` list is an object:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cmd` | string | Yes | Shell command to run (passed to `/bin/sh -c`) |
| `name` | string | No | Human-readable label; defaults to `cmd` if omitted |
| `expect` | string | No | Expected outcome: only `exit_code_zero` is currently supported |

Gates run sequentially after `status: complete`. A gate failure prevents the `.done` marker and triggers exit code 2. Gate output (combined stdout+stderr, truncated to 2000 chars) is included in the execution log.

### Complete Annotated Example

```yaml
- id: add-authentication
  prompt: .kiln/prompts/tasks/add-authentication.md
  needs:
    - setup-database
    - design-api
  timeout: 45m
  model: claude-sonnet-4-6
  description: "Implement JWT-based authentication endpoints"
  kind: feature
  tags:
    - auth
    - security
  retries: 2
  env:
    DATABASE_URL: "postgres://localhost/mydb_test"
    JWT_SECRET: "test-secret-not-for-production"
  dev-phase: 2
  phase: build
  milestone: m1-auth
  acceptance:
    - "POST /auth/login returns a signed JWT token for valid credentials"
    - "Requests with invalid tokens receive 401 Unauthorized"
    - "Token expiry is enforced"
  verify:
    - name: "unit tests"
      cmd: "go test ./internal/auth/..."
      expect: exit_code_zero
    - name: "integration tests"
      cmd: "go test -tags integration ./..."
      expect: exit_code_zero
  lane: api-layer
  exclusive: false
```

---

## Part 4: Configuration & Environment

### Versioning

Kiln uses `ldflags` plus git tags for version injection. The `version` variable in `main.go` defaults to `"dev"` and is overridden at build time by the Makefile:

```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

$(KILN): cmd/kiln/main.go
    @go build -ldflags "-X main.version=$(VERSION)" -o $(KILN) ./cmd/kiln
```

**Tagging releases** — use semver tags:

```bash
git tag v1.0.0   # major: breaking changes
git tag v1.1.0   # minor: new features
git tag v1.1.1   # patch: bug fixes
git push --tags
```

`kiln version` prints the injected version string. Building via `make bin/kiln` auto-injects. Bare `go build` without ldflags prints `"dev"`.

---

### Environment Variables

| Variable | Description |
|----------|-------------|
| `KILN_MODEL` | Default Claude model for all subcommands. Fallback: `claude-sonnet-4-6` |

Model resolution precedence (highest to lowest):
1. `--model` flag on the specific command
2. `model:` field in `tasks.yaml` for the specific task (for `kiln exec`)
3. `KILN_MODEL` environment variable
4. Built-in default (`claude-sonnet-4-6`; `claude-opus-4-6` for `kiln plan` and `kiln gen-prompts`)

---

### `.kiln/config.yaml`

Optional project-level configuration file. Supports workflow profiles and project-wide verify gate defaults.

```yaml
# .kiln/config.yaml

# Active workflow profile: speed or reliable
profile: reliable

# Project-wide defaults applied to all tasks
defaults:
  verify:
    - name: "tests pass"
      cmd: "go test ./..."
      expect: exit_code_zero

# Fine-grained overrides on top of the selected profile
overrides:
  require_unify: true
  require_verify_gates: true
  parallelism_limit: 3
  retry_max: 4
  retry_backoff_base: "15s"
```

**Profile defaults:**

| Setting | `speed` | `reliable` |
|---------|---------|------------|
| `require_unify` | `false` | `true` |
| `require_verify_gates` | `false` | `true` |
| `parallelism_limit` | `0` (unlimited) | `2` |
| `retry_max` | `2` | `4` |
| `retry_backoff_base` | `5s` | `10s` |

Note: `require_unify` is forward-compatible — the field is read and respected by `kiln profile` output, but automatic enforcement within `kiln exec` is not yet active (a warning is printed and execution continues).

---

### Makefile Integration Patterns

The standard Makefile pattern for a Kiln project:

```makefile
# ---- Config ----
KILN      := ./bin/kiln
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# ---- Version injection ----
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# ---- Auto-build binary ----
$(KILN): cmd/kiln/main.go
    @go build -ldflags "-X main.version=$(VERSION)" -o $(KILN) ./cmd/kiln

# ---- Include generated targets (optional: no error if missing) ----
-include $(TARGETS_FILE)

.PHONY: plan graph clean

# Generate tasks.yaml from PRD
plan: $(KILN)
    $(KILN) plan

# Generate targets.mk from tasks.yaml
# Use DEV_PHASE=N to limit to a specific dev phase
graph: $(KILN)
    $(KILN) gen-make \
        --tasks $(TASKS_FILE) \
        --out $(TARGETS_FILE) \
        $(if $(DEV_PHASE),--dev-phase $(DEV_PHASE))

clean:
    rm -rf .kiln/done .kiln/logs $(TARGETS_FILE)
```

Run phase-1 tasks only:

```bash
DEV_PHASE=1 make graph
make all
```

---

## Part 5: Features Coming Soon

The following capabilities are planned but not yet implemented in the current codebase.

### Interactive TUI Dashboard

A real-time terminal UI for monitoring and controlling Kiln runs. The dashboard would show a live task graph with color-coded status indicators (done, running, blocked, pending), stream log output for the currently executing task, display progress summaries and error counts, and support keyboard navigation to drill into task details. Manual controls — trigger, retry, skip, and reset individual tasks — would be accessible without leaving the terminal. This becomes essential once task graphs exceed 10–15 tasks and parallel execution is the norm, since `make -jN` output is difficult to read when multiple tasks are running simultaneously.

### Git Automation

Optional git integration that tightens the loop between task completion and version control. Planned capabilities include verifying that a git commit occurred before marking a task complete, auto-committing with a templated message based on the task ID and UNIFY summary, branch-per-task mode for isolated development, and PR creation hooks. All git automation would be opt-in via configuration flags so that projects without this workflow are not affected.

### Engine Abstraction & Multi-Engine Support

Kiln currently invokes only the Claude Code CLI. Engine abstraction would introduce a pluggable engine interface wrapping the invocation, output parsing, and error classification layers. This would enable running tasks with Codex, Cursor, OpenAI CLI, or other agents — including fallback to a secondary engine when the primary is rate-limited or unavailable. Per-task engine selection via the `engine:` field in `tasks.yaml` is already part of the schema; full enforcement depends on this feature.

### Lane & Exclusive Concurrency Enforcement

The `lane` and `exclusive` fields already exist in the task schema and are validated, but `kiln gen-make` does not yet use them to constrain parallelism. Once implemented, tasks in the same `lane` would run serially even when `make -jN` is active, and tasks with `exclusive: true` would block all other concurrent tasks. This is important for tasks that write to shared resources (databases, generated files) and cannot safely run in parallel.

### UNIFY Enforcement in `kiln exec`

The `require_unify` profile setting and the `reliable` profile are fully implemented and visible in `kiln profile` output, but automatic enforcement inside `kiln exec` is not yet active. When `require_unify: true` is set, Kiln currently logs a debug message and proceeds without blocking. Once enforcement is implemented, `kiln exec` would check for a UNIFY closure artifact before writing the `.done` marker, ensuring that every completed task produces a structured reconciliation summary.

### Research Artifact Auto-Injection

Tasks with `kind: research` follow a convention of writing artifacts to `.kiln/artifacts/research/<id>.md`, and prompt chaining (dependency context injection) already checks this path as a priority source. What is not yet implemented is automatic wrapping: a task with `kind: research` would receive a specialized prompt wrapper that instructs it to write its findings to the standard artifact path, and downstream tasks that depend on it would automatically receive those findings as structured context. Currently, research tasks need to be prompted manually to write to the correct location.

---

*This guide reflects Kiln v0.1.0. For issues or contributions, see the project repository.*

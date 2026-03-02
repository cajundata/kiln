# PRD: Kiln (Go CLI + Make Orchestrator)

#effort/active #note/cajundata #note/softwaredevelopment #kiln

Product name: Kiln (kiln)
Owner: Weldon
Last updated: 2026-02-28
Status: MVP PRD (aggressively scoped, workflow-complete)

# 1. App Overview and Objectives

## 1.1 What we’re building

Kiln is a personal developer productivity tool that implements the “Ralph Wiggum” Claude Code workflow pattern while eliminating fragile bash loops. It addresses context rot by ensuring each task is executed via a fresh, single-shot Claude Code invocation, orchestrated by Make (dependency graph + idempotency).

## 1.2 Core problem

Long sequential Claude Code sessions can degrade output quality (context drift, constraint loss, false “done”). Kiln prevents this by:

- Running one task per invocation (fresh context each time)
- Delegating sequencing/parallelism to Make targets
- Enforcing safe execution (timeouts, retries, structured logging)

### 1.3 MVP success criteria

- A user can go from `PRD.md` → `tasks.yaml` → generated Make targets → running tasks with a repeatable Make workflow.
- Kiln fails safely (timeout/retry/clear errors) and never “silently succeeds.”
- The PRD parsing step is not hidden; it is a visible Make target.

## 2. Target Audience

Single developer (personal use) running Claude Code to implement multi-step software work reliably, locally or in CI-like environments.

## 3. High-Level Workflow (MVP)

3.1 Filesystem layout (MVP)

```
.kiln/
  prompts/
    00_extract_tasks.md
    tasks/                # per-task prompts
      <task-id>.md
  tasks.yaml              # graph definition (IDs + deps + prompt file path)
  done/                   # Make idempotency markers
    <task-id>.done
  logs/
    <task-id>.json        # one log per executed task
  targets.mk              # generated Make include file (from tasks.yaml)
Makefile
PRD.md
```

### 3.2 Make targets (MVP standard)

- `make plan` → uses Claude to produce .kiln/tasks.yaml
- `make graph` → runs kiln gen-make to produce .kiln/targets.mk
- `make all` → runs all tasks in the dependency graph (the default “run everything” entry point)

## 4. Core Features and Requirements

### F1. Execute one task prompt safely (`kiln exec`)

#### Description

Runs exactly one AI task per invocation using Claude Code with --output-format stream-json. Handles timeout + retries + structured logs. Enforces a JSON contract in model output to determine status.

#### CLI (MVP)

```Bash
kiln exec --task-id <id> --prompt-file <path> [--timeout 5m] [--max-retries 3] [--backoff exponential]
```

#### Functional Requirements

- Must invoke Claude Code using stream-json output.
- Must enforce --timeout hard kill (transient failure).
- Must implement retry with exponential backoff + jitter for transient failures.
- Must write log file to `.kiln/logs/<task-id>.json` every run attempt.
- Must parse the model’s final response for a required JSON footer

#### JSON completion contract (MVP standard)

Model output (final response text) must end with a JSON object like:

```json
{ "kiln": { "status": "complete", "task_id": "<task-id>" } }
```

#### Valid statuses (MVP):

- `complete`
- `not_complete`
- `blocked`

#### Exit code contract (for Make)

- `0` = successful run (task may be not_complete)
- `2` = successful run + status=complete
- `10` = permanent failure (invalid args, missing engine, auth-like failure patterns)
- `20` = transient failure exhausted retries

#### Acceptance Criteria

- If Claude times out: Kiln retries per policy and logs attempts; eventually exits `20` if retries exhausted.
- If Claude returns invalid output (missing JSON footer): Kiln exits `10` and prints a helpful schema error.
- If JSON footer status is complete: Kiln exits `2`.
- Logs are always written to `.kiln/logs/<task-id>.json` including attempts and final classification.

### F2. Task graph definition file (`tasks.yaml`) + strict validation

#### Description

Defines the dependency graph and maps each task to a prompt file. This file is the only source of truth for graph generation.

#### Schema (MVP, tight by design)

```
version: 1
tasks:
id: "dashboard-api"
prompt: ".kiln/prompts/tasks/dashboard-api.md"
needs: ["auth-module"]
id: "auth-module"
prompt: ".kiln/prompts/tasks/auth-module.md"
needs: []
```

#### Validation Rules (MVP)

- version must exist and equal 1
- tasks must exist and be a non-empty list
- each task requires:
  - id (string, regex: ^[a-z0-9]+(?:-[a-z0-9]+)\*$)
  - prompt (string path; must exist on disk unless --allow-missing-prompts is explicitly used)
  - needs (list; may be empty; all referenced IDs must exist)
- no duplicate task IDs
- no cyclic dependencies (fail fast if detected)

#### Acceptance Criteria

- `kiln gen-make` fails with a clear message if YAML is invalid, missing fields, unknown dependencies, or cycles exist.
- Validation error output includes:
  - the offending task id (if applicable)
  - the field name
  - expected format (example)

### F3. Generate Make dependency targets from tasks.yaml (`kiln gen-make`)

#### Description

Deterministically generates `.kiln/targets.mk` from `.kiln/tasks.yaml`.

#### CLI (MVP)

```bash
kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk
```

#### Generated Make contract (MVP)

- Each task ID produces `.kiln/done/<id>.done`
- `.done` files are created by Make only if kiln exec exits successfully
- Dependencies are represented as Make prerequisites
- Output is deterministic (stable ordering)

#### Acceptance Criteria

- Running `kiln gen-make` produces a valid Make include file.
- `make <task-id>` runs prerequisites first.
- `make -jN` parallelizes independent tasks (based on missing dependencies).
- A failed `kiln exec` run prevents creation of `.done` and stops Make.

### F4. AI-assisted PRD parsing as a first-class workflow (not hidden)

#### Description

Kiln does not implement PRD parsing logic in Go for MVP. Instead, it ships an official prompt template that uses Claude to generate `tasks.yaml` with the strict schema.

#### Required prompt template

- `.kiln/prompts/00_extract_tasks.md

#### Prompt requirements (MVP)

- Must instruct Claude:
  - output only YAML (no commentary)
  - adhere to the exact tasks.yaml schema
  - be conservative with dependencies:
    - only add needs when ordering constraint is clear
    - otherwise leave tasks independent
  - keep task count bounded unless explicitly requested (e.g., ≤ 25)

#### Acceptance Criteria

- `make plan` produces `.kiln/tasks.yaml`
- `make graph` succeeds immediately after plan when the YAML is valid
- if YAML is invalid, graph generation fails fast with helpful errors (no “partial success”)

## 5. Makefile Requirements (MVP)

### 5.1 Required targets

- `plan`: runs Kiln against the PRD parsing prompt and writes `.kiln/tasks.yaml`
- `graph`: runs `kiln gen-make` and writes `.kiln/targets.mk`
- `all`: runs all tasks in the graph

### 5.2 Definition of “all tasks”

- `make all` must expand to “every task defined in `tasks.yaml`”
- The target list is derived from the graph file (i.e., from `.kiln/targets.mk`)

#### Acceptance Criteria

- After `make graph`, running `make all` executes the full graph
- If `.kiln/targets.mk` is missing, make all fails with a clear message to run make graph
- Re-running make all is idempotent due to `.done` markers

## 6. Risk Mitigations (Baked into Requirements)

### Risk 1: Claude produces invalid YAML or schema drift

#### Mitigation requirements

- Tight schema defined in PRD (above)
- Prompt enforces “YAML only”
- Kiln performs strict validation and fails fast with actionable errors

#### MVP acceptance

- Invalid YAML never produces a `.kiln/targets.mk`
- The failure message explains exactly what to fix

### Risk 2: Hallucinated dependencies (graph wrong)

#### Mitigation requirements

- Prompt instructs conservative deps (only when ordering constraint is clear)
- needs is optional/empty by default
- No notes field in MVP schema (kept minimal); dependency reasoning can be handled later via separate artifact if needed

#### MVP acceptance

- Tasks default to independent unless explicitly justified by PRD language
- Make parallelization is safe-by-default (independent tasks can run concurrently)

### Risk 3: PRD parsing becomes a hidden multi-step workflow

#### Mitigation requirements

- Make targets formalize the workflow:
  - `make plan`
  - `make graph`
  - `make all`

#### MVP acceptance

- A new user can run the workflow start-to-finish without “tribal knowledge”
- Each stage is explicit and independently runnable

## 7. Non-Goals (MVP)

- No `.kiln/state.json` resumability beyond Make .done markers
- No engine abstraction beyond Claude Code
- No git automation (commits/PRs/worktrees)
- No automatic prompt generation for per-task prompts (optional future)
- No PRD.md deterministic parsing in Go

## 8. Future Expansion (Post-MVP)

- kiln init scaffolds .kiln/ and Makefile
- Optional kiln make-prompts to generate `.kiln/prompts/tasks/<id>.md` from `tasks.yaml`
- Stateful progress beyond `.done` (status summaries, attempt history)
- Multi-engine support with per-engine parsers
- Validation hooks (tests/lint/build gates) as configurable steps

## 9. Confirmation Checklist (Decisions Locked)

- Completion signal uses JSON footer contract
- `.done` markers created by Make only
- Claude invocation uses

```shell
--output-format stream-json
```

- Logs written to `.kiln/logs/.json`
- `tasks.yaml` stores IDs + dependencies + prompt file path
- Graph generation is in MVP, via `kiln gen-make`
- PRD parsing is supported in MVP via an official prompt + make plan
- `make all` runs all tasks in the graph

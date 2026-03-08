# Task: create-guide — Comprehensive User Guide

## Role
You are an AI coding agent working in a local git repository. Your job is to create documentation that accurately reflects the current codebase.

## TASK ID
create-guide

## SCOPE
Implement ONLY the work required for this task. Do not work on other tasks in the PRD, even if you notice related issues.

## CONTEXT
- This project uses the Kiln workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.

## REQUIREMENTS
1) Implement the task described below.
2) Keep changes minimal and focused on this task.
3) If tests are relevant for this task, add or update tests.
4) Do not introduce large refactors unrelated to the task.
5) Do not change formatting or tooling configs unless required by the task.

## TASK DESCRIPTION

Create a comprehensive User Guide at `docs/user-guide.md` for the Kiln CLI tool. The guide must accurately reflect the current state of the codebase — read `cmd/kiln/main.go`, `CLAUDE.md`, `PRD.md`, `BACKLOG.md`, `Makefile`, `.kiln/tasks.yaml`, and `.kiln/templates/<id>.md` before writing. Run `kiln status --tasks .kiln/tasks.yaml` to confirm the live state if needed.

The guide must have TWO major parts:

### PART 1: Getting Started Tutorial (for new users)

Write a hands-on walkthrough that takes a developer from zero to running their first Kiln workflow. Include:

- **What is Kiln?** — A concise explanation of what Kiln does, the "Ralph Wiggum" pattern (one task per fresh Claude Code invocation), and why context rot matters. Keep it to 2-3 paragraphs.
- **Prerequisites** — Go toolchain, Claude Code CLI installed and authenticated, Make.
- **Installation** — Building from source (`go build -o kiln ./cmd/kiln`), verifying the binary works.
- **Project setup** — The `.kiln/` directory structure and what each subdirectory holds (`prompts/`, `done/`, `logs/`, `locks/`, `state.json`, `targets.mk`, `tasks.yaml`). Include a filesystem tree diagram.
- **The three-step workflow** — Walk through the full cycle with concrete examples:
  1. `make plan` (or `kiln plan`) — How Kiln uses Claude to parse `PRD.md` into `tasks.yaml`. Explain the extraction prompt at `.kiln/prompts/00_extract_tasks.md`.
  2. `make graph` (or `kiln gen-make`) — How `tasks.yaml` becomes `targets.mk`. Explain the `--dev-phase` flag for phased rollouts.
  3. `make all` (or `make -jN` for parallelism) — How Make orchestrates `kiln exec` per task, respecting dependency order. Explain `.done` markers and idempotency.
- **Understanding task results** — How to read logs in `.kiln/logs/<id>.json`, interpret exit codes (0, 2, 10, 20), and check the JSON footer contract (`{"kiln":{"status":"complete","task_id":"<id>"}}`).
- **Prompt generation** — How `kiln gen-prompts` auto-generates per-task prompts from a template, and how to customize the template at `.kiln/templates/<id>.md`.
- **What to do when things fail** — A brief troubleshooting flow: check `kiln status`, read logs, use `kiln report`, then `kiln retry` or `kiln reset`.

### PART 2: Command Reference (for existing users)

Run `kiln version` and include the output as the documented version at the top of the guide (e.g., "This guide covers Kiln version vX.Y.Z").

Document every subcommand with its flags, behavior, and examples. The subcommands to cover are:

- **`kiln version`** — Prints the current Kiln version. The version is injected at build time via `ldflags` from `git describe --tags --always --dirty`. Building via `make` injects the version automatically; building with bare `go build` prints `dev`.

- **`kiln exec`** — Single-task execution engine. Document all flags:
  - `--task-id` (required), `--prompt-file` (optional, resolved from tasks.yaml), `--tasks` (tasks.yaml path for auto-resolution), `--model` (overrides `KILN_MODEL` env var), `--timeout` (default 60m), `--retries`, `--retry-backoff`, `--backoff` (fixed or exponential), `--force-unlock` (for stale locks), `--skip-verify` (skip verification gates), `--no-chain` (disable prompt chaining / dependency context injection), `--max-context-bytes` (default 50000).
  - Explain the execution lifecycle: lock acquisition -> Claude invocation -> footer parsing -> verify gates -> state update -> done marker.
  - Document exit codes: 0 = complete, 2 = not_complete/blocked, 10 = permanent failure, 20 = transient retries exhausted.

- **`kiln plan`** — PRD-to-tasks extraction. Flags: `--prd`, `--prompt`, `--out`, `--model`, `--timeout`.

- **`kiln gen-make`** — Generate Make targets. Flags: `--tasks`, `--out`, `--dev-phase`.

- **`kiln gen-prompts`** — Generate per-task prompt files from a template. Flags: `--tasks`, `--prd`, `--template`, `--model`, `--timeout`, `--overwrite`.

- **`kiln status`** — Task scoreboard. Flags: `--tasks`, `--format` (table or json). Describe the columns: task, status, attempts, kind, phase, last error, needs.

- **`kiln report`** — Execution summary report. Flags: `--format` (table or json), `--log-dir`. Describe error class aggregation, duration stats, and the summary section.

- **`kiln unify`** — Post-completion closure artifact generation. Flags: `--task-id`, `--model`, `--timeout`. Explain UNIFY closure concept: what changed, what's incomplete, decisions made, handoff notes. Output goes to `.kiln/unify/<id>.md`.

- **`kiln retry`** — Re-run tasks. Flags: `--tasks`, `--task-id` (specific task), `--failed` (failed tasks only), `--transient-only` (with --failed, only retryable errors).

- **`kiln reset`** — Clear done markers and state. Flags: `--task-id` (specific task), `--all` (all tasks, requires confirmation), `--tasks`.

- **`kiln resume`** — Generate a resume prompt with prior context. Flags: `--task-id`, `--tasks`. Explain how it injects UNIFY summaries and decision context.

- **`kiln verify-plan`** — Verification coverage checker. Flags: `--tasks`, `--config`, `--strict` (CI mode), `--format` (text or json). Explain acceptance-criteria-to-gate mapping.

- **`kiln validate-schema`** — Validates tasks.yaml schema. Flags: `--tasks`.

- **`kiln validate-cycles`** — Checks for dependency cycles. Flags: `--tasks`.

### PART 3: Task Schema Reference

Document the full `tasks.yaml` schema with all fields:
- Required: `id` (kebab-case), `prompt` (relative path)
- Optional: `needs`, `timeout`, `model`, `description`, `kind` (feature/fix/research/docs — research tasks produce `.kiln/artifacts/research/<id>.md`), `tags`, `retries`, `validation`, `engine`, `env` (map), `dev-phase`, `phase` (plan/build/verify/docs), `milestone` (kebab-case), `acceptance` (list of criteria), `verify` (list of gate objects with `cmd`, `name`, `expect`), `lane` (concurrency group), `exclusive` (bool)
- Include a complete annotated example task entry showing all fields.

### PART 4: Configuration & Environment

- **Versioning** — Kiln uses `ldflags` + git tags for version injection. Explain:
  - The `version` variable in `main.go` defaults to `"dev"` and is overridden at build time
  - The Makefile uses `git describe --tags --always --dirty` to produce the version string
  - Tag releases with semver: `git tag v0.1.0` (major = breaking changes, minor = new features, patch = bug fixes)
  - `kiln version` prints the injected version
  - Building via `make` auto-injects; bare `go build` produces `"dev"`
- `KILN_MODEL` env var — sets default model (fallback: `claude-sonnet-4-6`)
- `--model` flag precedence over env var
- `.kiln/config.yaml` — used by `verify-plan` for project-level gate defaults
- Makefile integration patterns (`DEV_PHASE` variable, `-include targets.mk`, `VERSION` via ldflags)

### PART 5: Features Coming Soon

Review `BACKLOG.md` and compare against the implemented features in the codebase. The following features are NOT yet implemented and should be described as coming soon (describe each independently, do not use internal backlog numbering):

- **Machine-Readable Output Mode** — `--format json|text` for `kiln exec` and `kiln gen-make` stdout, enabling CI/CD integration and automation tooling.
- **`kiln init` Scaffolding** — A scaffolding command to bootstrap the `.kiln/` directory structure, install prompt templates, create/patch Makefile, with profile support (python/go/node/monorepo).
- **Interactive TUI Dashboard** — A terminal UI for real-time task graph visualization, live log streaming, manual task controls (trigger/retry/skip/reset), progress summaries, and keyboard navigation. Built with Bubble Tea.
- **Git Automation** — Optional git integration: verify commits before completion, auto-commit with templated messages, branch-per-task mode, PR creation hooks.
- **Engine Abstraction** — Multi-engine support beyond Claude Code (Codex, Cursor, OpenAI CLI), with per-engine output parsers, error classifiers, and a consistent result schema.
- **Profile Strategy** — Selectable workflow profiles (`speed` vs `reliable`) controlling UNIFY requirements, verify gate enforcement, parallelism limits, and retry aggressiveness.

For each coming-soon feature, write 2-3 sentences explaining what it will do and why it matters. Do NOT promise timelines.

### FORMATTING & STYLE RULES
- Use clear, scannable markdown with headers, code blocks, and tables.
- Every command example must use realistic flag values (not placeholder `<foo>`).
- Keep the tutorial section warm and instructive; keep the reference section precise and terse.
- Do not duplicate the PRD or CLAUDE.md verbatim — synthesize and present for the user audience.
- The document should be self-contained: a developer should be able to use Kiln end-to-end with only this guide.
- Create the `docs/` directory if it does not exist.

## ACCEPTANCE CRITERIA
- `docs/user-guide.md` exists and is well-structured markdown.
- Part 1 (Tutorial) walks a new user through the full plan -> graph -> all workflow with concrete examples.
- Part 2 (Command Reference) documents all 14 subcommands (including `kiln version`) with every flag, default value, and exit code.
- Part 3 (Task Schema) documents every field in the Task struct including verify gates.
- Part 4 (Configuration) covers versioning (ldflags + git tags), KILN_MODEL, --model precedence, config.yaml, and Makefile patterns.
- Part 5 (Coming Soon) lists 6 upcoming features from the backlog without internal numbering or timelines.
- No commands or flags are missing compared to the actual codebase in `cmd/kiln/main.go`.
- The guide does not describe features that don't exist yet as if they are implemented.

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"create-guide"}}

Allowed values for status:
- "complete"     (all acceptance criteria met)
- "not_complete" (work attempted but acceptance criteria not met)
- "blocked"      (cannot proceed due to missing info, permissions, dependencies, or unclear requirements)

## STRICT RULES FOR THE JSON FOOTER
- The JSON object MUST be the final line of your response.
- Output EXACTLY one JSON object.
- No extra text after it.
- No code fences around it.
- The task_id must exactly match the TASK ID above.
- If you are unsure, choose "not_complete" or "blocked" rather than "complete".

If you finish successfully, the correct final line is:
{"kiln":{"status":"complete","task_id":"create-guide"}}

# Kiln Backlog

Post-MVP feature candidates, roughly ordered by impact.

---

## 1. State & Resumability Beyond .done Markers

Introduce `.kiln/state.json` as a first-class state manifest tracking:

- Per-task status (pending/running/completed/failed)
- Attempt count history
- Timestamps
- Last error classification
- Last successful execution metadata (tokens, duration, commit hash, etc.)
- Resumability rules (`--resume`, `--retry-failed`, `--reset`, etc.)

**Why:** `.done` markers handle Make idempotency but don't answer "what happened last time", "why did it fail", "how many attempts did we burn", or enable safe recovery after partial progress where `.done` wasn't written but the repo changed.

**Recommendation:** Introduce a state file as first-class (even if Make still owns `.done`).

---

## 2. Engine Abstraction & Multi-Engine Parsing

Kiln MVP is Claude Code only. The earlier PRD called for:

- Multi-engine support (Claude / Codex / Cursor / OpenAI CLI / etc.)
- Engine-specific output parsers
- Engine-specific error classifiers
- Consistent structured result schema regardless of engine

**Why:** Prevents lock-in and gives fallbacks when one engine is down/rate-limited. Lets you tune cost/quality per task class later.

**Recommendation:** Add an engine interface + `kiln exec --engine <name>` + parser modules.

---

## 3. Validation Hooks as Configurable Gates

Optional post-run validation hooks integrated with task completion:

- Tests/lint/build/custom checks as post-run gates
- Per-task or per-project validation policies
- "Fail if validation fails" semantics integrated with task completion
- Config policy layer: project default validations + optional per-task overrides

**Why:** This is how you stop "looks done" from becoming "actually correct." Also what makes CI adoption real.

**Recommendation:** Add a config policy layer with project defaults and per-task overrides.

---

## 4. `kiln init` Scaffolding Generator

- Scaffold `.kiln/` structure
- Install prompt templates
- Create Makefile (or patch existing)
- Create example PRD/tasks templates
- Profile support (python/go/node/monorepo)

**Why:** Adoption friction drops to near-zero. Makes the tool portable across repos.

**Recommendation:** `kiln init` with profiles.

---

## 5. Machine-Readable Result Output Mode

Beyond `.kiln/logs/<task-id>.json`:

- `--json` (stdout) structured output for automation tooling
- Stable output schema for CI parsing, dashboards, and other tools
- `--format json|text` for `kiln exec` and `kiln gen-make`

**Why:** Logs are great for humans; CI systems prefer stdout contract for immediate consumption. Enables wrapper tools and metrics exporters.

**Recommendation:** Add `--format json|text` for `kiln exec` and `kiln gen-make`.

---

## 6. Concurrency Safety & Duplicate-Execution Prevention

Kiln depends on Make parallelism (`make -jN`) but MVP does not define:

- Concurrency guarantees around log file writes (same task id, repeated runs)
- Task-level locking (prevent two exec runs for same task simultaneously)
- Safe behavior if Make runs two targets that accidentally point to the same task id
- Protection against double execution in parallel mode

**Why:** As soon as you use `make -j3`, you want deterministic behavior under concurrency.

**Recommendation:** Add task-level locking semantics and/or `.kiln/locks/<id>.lock` conventions.

---

## 7. Richer Task Schema (Beyond id/prompt/needs)

Optional per-task metadata:

- Description/title separate from id
- "Kind" (backend/frontend/docs)
- Estimated cost/time
- Validation overrides
- Environment requirements
- Tags/groups (parallel groups, lanes)
- Retry policy overrides (timeouts/retries differ by task type)

**Why:** As the task graph grows, you'll want policy control without multiplying Makefiles.

**Recommendation:** Add optional fields while keeping strict validation.

---

## 8. Prompt Chaining via Completed Task Summary Injection

Kiln uses fresh invocation per task but does not define a structured method for injecting:

- Summaries of completed tasks
- Key outputs/decisions
- Artifacts produced

**Why:** Fresh context prevents rot, but later tasks still need selective memory of prior work.

**Recommendation:** Store "task outcome summary" in state and inject it into subsequent tasks automatically.

---

## 9. Error Taxonomy & Reporting Beyond Exit Codes

Kiln has exit codes but doesn't define:

- Standardized error classes in logs (transient/permanent/validation/schema)
- A summary report across the run
- A "why it failed" aggregation (top causes)
- `kiln report` command (summarize logs/state)
- Consistent error classification fields in logs

**Why:** Progress monitoring and clear status reporting improve UX and debuggability.

**Recommendation:** Add `kiln report` and consistent error classification fields in logs.

---

## 10. Interactive TUI Dashboard

A terminal UI for monitoring and controlling kiln runs in real time:

- Live task graph visualization showing done/running/blocked/runnable states
- Real-time log streaming for the currently executing task
- Manual task controls: trigger, retry, skip, or reset individual tasks
- Summary panel: total progress, elapsed time, error counts
- Keyboard navigation to drill into task details, logs, and footer output
- Color-coded status indicators matching `kiln status` semantics

**Why:** `kiln status` is a snapshot; `make -jN` output interleaves multiple tasks into unreadable noise. A TUI gives live observability over the full graph without leaving the terminal. This becomes critical once task graphs exceed 10-15 tasks and parallel execution is the norm.

**Recommendation:** Build with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Go-native TUI framework). Start with a read-only dashboard (`kiln tui`) that tails logs and polls state, then add interactive controls. Depends on #1 (state file) for reliable status tracking beyond `.done` markers.

---

## 11. Optional Git Automation

Explicitly a non-goal for MVP, but future candidates include:

- Verify "a commit happened" before allowing completion
- Optional auto-commit with templated messages
- Branch-per-task mode
- PR creation hooks

**Why:** Tighter integration between task completion and version control reduces manual toil.

**Recommendation:** Add as opt-in features behind flags/config.

---

## Recommended Development Order

### Phase 1 — Foundation (no dependencies, unblocks everything else)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P1** | **#1 State & Resumability** | Foundation for #8, #9, and richer `kiln status`. `execRunLog` already captures most data — this adds a state file that aggregates across attempts and persists between runs. Unblocks: #8, #9. |
| **P2** | **#7 Richer Task Schema** | Currently `Task` struct is 5 fields with `KnownFields(true)`. Adding optional fields (description, kind, tags, retry overrides, validation overrides) is low-risk and unblocks #3 and #2. Unblocks: #2, #3. |

### Phase 2 — Safety & Correctness (depends on Phase 1)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P3** | **#6 Concurrency Safety** | Already support `make -jN`. Task-level locking via `.kiln/locks/<id>.lock` + atomic log writes is a small surface area. Benefits from #1 (state file tracks "running" status). |
| **P4** | **#9 Error Taxonomy & Reporting** | Already have `timeoutError`, `claudeExitError`, `footerError` and `isRetryable()`. This standardizes those in the log schema and adds `kiln report`. Depends on: #1. |

### Phase 3 — Correctness Gates & Chaining (depends on Phase 1+2)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P5** | **#3 Validation Hooks** | Post-exec gates (test/lint/build). Depends on: #7 (per-task validation overrides). Benefits from #1 (state records validation results). |
| **P6** | **#8 Prompt Chaining** | Injecting completed-task summaries into downstream prompts. Depends on: #1 (state stores task outcome summaries). High-impact for multi-task graphs but needs state layer first. |

### Phase 4 — UX & Adoption (independent, can be done anytime)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P7** | **#5 Machine-Readable Output** | `--format json|text` for `kiln exec` and `kiln gen-make`. No dependencies. Small scope — already writes JSON logs. Could be done earlier if CI integration is urgent. |
| **P8** | **#4 `kiln init`** | Scaffolding generator. No code dependencies, but benefits from #7 so templates are accurate. Better to wait until schema settles. |
| **P9** | **#10 Interactive TUI** | Read-only dashboard first (`kiln tui`), then interactive controls. Depends on: #1 (state file for reliable live status). Benefits from #9 (error taxonomy feeds the summary panel). Natural fit alongside #5 since both are observability improvements. |

### Phase 5 — Abstraction & Extensibility (last, highest complexity)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P10** | **#11 Git Automation** | Opt-in git integration (verify commits, auto-commit, branch-per-task, PR hooks). Benefits from #3 (validation hooks can include "verify commit exists"). Lower refactor surface than engine abstraction. |
| **P11** | **#2 Engine Abstraction** | Multi-engine support requires an `Engine` interface wrapping `commandBuilder`, `parseFooter`, and error classification. Depends on: #7 (per-task engine field). Highest refactor surface — touches `execOnce`, `commandBuilder`, `parseFooter`, and error types. Do this last, after the core is stable and a second engine is actually needed. |

---

## Dependency Graph

```
#7 Richer Schema ──────┬──→ #3 Validation Hooks ──→ #11 Git Automation
                       ├──→ #2 Engine Abstraction
                       │
#1 State & Resumability┬──→ #8 Prompt Chaining
                       ├──→ #9 Error Taxonomy ──→ #10 TUI (benefits)
                       ├──→ #6 Concurrency Safety (soft)
                       ├──→ #10 Interactive TUI
                       │
#5 Machine-Readable Output  (independent)
#4 kiln init                (independent, benefits from #7)
```

---

## Key Observations

1. **#1 and #7 are the two foundation pieces** — almost everything else benefits from or directly depends on them. They're also the lowest-risk changes (additive, no breaking changes).
2. **#5 (JSON output) is a wildcard** — if CI integration is needed soon, bump it to Phase 1. It's trivially small.
3. **#2 (engine abstraction) is deliberately last** — it's the largest refactor and isn't needed until a second engine is actually required. Premature abstraction here would slow down everything else.
4. **#6 (concurrency) is more urgent than it looks** — if anyone runs `make -j3` today, there are no guards. Consider pairing it with #1.
5. **#11 (git automation) before #2** — git integration has a smaller refactor surface and benefits from validation hooks (#3) already being in place, making it a natural next step before tackling the engine abstraction.
6. **#10 (TUI) in Phase 4** — it's a UX feature with no downstream dependents, so it doesn't block anything. But it hard-depends on #1 (state file) for live status beyond `.done` markers, and benefits significantly from #9 (error taxonomy feeds the summary panel). Building it before Phase 4 would mean reimplementing status tracking that #1 already provides.

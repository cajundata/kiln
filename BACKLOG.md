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

**Why:** As soon as you use `make -j3`, you want deterministic behavior under concurrency. Becomes more critical with lanes, phases, research parallelism, and verification gates.

**Recommendation:** Add task-level locking semantics and/or `.kiln/locks/<id>.lock` conventions. Pair with #1 State for "running" status tracking.

---

## 7. Richer Task Schema (Beyond id/prompt/needs)

Optional per-task metadata:

- Description/title separate from id
- `kind`: research | implement | verify | docs (enables safe parallelism of research tasks, clean artifact separation)
- `phase`: plan | build | verify | docs (human-oriented milestone grouping)
- `milestone`: e.g. `M1-auth` (enables `make milestone-M1-auth` entrypoints)
- `acceptance`: list of acceptance criteria (Given/When/Then or bullet AC)
- `verify`: list of gate commands + optional expectations (per-task verification)
- `lane` / `exclusive`: concurrency grouping (parallel groups, mutual exclusion)
- Estimated cost/time
- Validation overrides
- Environment requirements
- Tags/groups
- Retry policy overrides (timeouts/retries differ by task type)

Generated Make entrypoints from phase/milestone metadata:
- `make phase-verify` — run all tasks in the verify phase
- `make milestone-M1-auth` — run all tasks in milestone M1-auth
- `make verify-M1-auth` — combination target

Output conventions for `kind: research`:
- Research tasks produce `.kiln/artifacts/research/<id>.md`
- Prompt wrapper for downstream tasks automatically includes relevant artifacts

**Why:** As the task graph grows, you'll want policy control without multiplying Makefiles. Phases and milestones let humans think in slices ("run the auth milestone") without changing the execution model. Research/implement separation prevents prompt pollution and enables safe parallelism.

**Recommendation:** Add optional fields while keeping strict validation. Keep phases and milestones optional metadata — don't impose a rigid lifecycle.

---

## 8. Prompt Chaining via Completed Task Summary Injection

Kiln uses fresh invocation per task but does not define a structured method for injecting:

- Summaries of completed tasks
- Key outputs/decisions
- Artifacts produced (including UNIFY closure artifacts and research outputs)

**Why:** Fresh context prevents rot, but later tasks still need selective memory of prior work.

**Recommendation:** Inject UNIFY closure artifacts (#12) and research artifacts (#7 kind: research) into downstream prompts automatically. Depends on #12 for high-signal source material.

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
- Lane/milestone grouping views
- Blocked-reason display from state

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

## 12. UNIFY / Closure Artifacts

A post-completion reconciliation primitive that produces a semantic closure artifact per task:

- What changed (files modified, functions added/removed)
- What's incomplete or deferred
- Decisions made and rationale
- What to do next / handoff notes
- Acceptance criteria coverage (if AC defined in task schema)

Artifacts:
- `.kiln/unify/<id>.md` — human-readable closure summary
- `.kiln/decisions.log` (or JSON) — append-only decision ledger across tasks

`kiln unify --task-id <id>` generates the closure artifact as a single-shot invocation (not a long session).

Optional: closure can require that verification gates (#3) have passed before marking complete.

**Why:** State and logs are telemetry; UNIFY is intent-to-reality reconciliation. "It ran" is not "it's reconciled." Produces high-signal context packs for downstream tasks, reduces rework from silent drift, and prevents re-reading diffs/logs manually.

**Recommendation:** Introduce as a first-class post-completion primitive. Feeds #8 Prompt Chaining (closure artifacts become the primary injection source) and strengthens #3 Validation Hooks (closure can require gates to have passed). Keep it single-shot — do not adopt long-session behavior.

---

## 13. Recovery UX (status/resume/retry/reset)

Ergonomic commands that turn state (#1) into action:

- `kiln status` — scoreboard: runnable / running / blocked / complete / failing transiently
- `kiln resume` — regenerate prompt wrapper using last logs + UNIFY summaries + decisions
- `kiln retry --failed --transient-only` — re-run failed tasks with filter flags
- `kiln reset --task-id <id>` — safely clear done marker + state entry

**Why:** Recovery isn't just data — it's ergonomic commands that minimize human context switching and tighten the "observe -> decide -> rerun" loop. The fewer times you open logs manually, the faster you ship.

**Recommendation:** Build as a UX layer over #1 State. Keep the state machine simple — don't adopt heavy phase-restart logic. Benefits from #12 UNIFY (resume can inject closure artifacts) and #9 Error Taxonomy (status shows classified errors).

---

## 14. Verification Mapping (verify-plan)

A lightweight requirements-to-gates coverage check:

- `kiln verify-plan` command that ensures every task with `acceptance` criteria has corresponding `verify` gates
- Sanity-checks gate executability (commands exist, are runnable)
- Reports coverage gaps: tasks with AC but no gates, gates that reference missing commands
- `.done` only touched if `kiln exec` status=complete AND verify gates pass (strong correctness mode)

**Why:** Validation isn't ad hoc — it's coverage. Each requirement should have a gate. This prevents shipping untestable work and reduces churn from "it worked locally" failures. Faster CI adoption because gates are explicit.

**Recommendation:** Introduce as a pre-run planning check that extends #3 Validation Hooks. Don't impose formal BDD scaffolding — keep it optional or profile-based.

---

## 15. Profile Strategy (speed vs reliable)

Selectable workflow profiles that prevent feature sprawl while supporting different use cases:

- `profile: speed` — fewer gates, more parallel, minimal UNIFY requirements (ship fast)
- `profile: reliable` — require UNIFY + verify gates for completion, conservative parallelism (ship correct)

Profiles control defaults for:
- Whether UNIFY closure is required or optional
- Whether verify gates block `.done` creation
- Parallelism limits
- Retry aggressiveness

**Why:** Different projects and phases need different tradeoffs. A prototype sprint needs speed; a production release needs correctness gates. Profiles let you switch without rewriting workflow config.

**Recommendation:** Implement as default-value presets in `.kiln/config.yaml` or project-level settings. Individual task overrides still take precedence. Keep the profile set small (2-3 profiles max).

---

## Recommended Development Order

### Phase 1 — Foundation (no dependencies, unblocks everything else)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P1** | **#1 State & Resumability** | Foundation for #12, #13, #9, and richer `kiln status`. `execRunLog` already captures most data — this adds a state file that aggregates across attempts and persists between runs. Unblocks: #12, #13, #9. |
| **P2** | **#7 Richer Task Schema** | Currently `Task` struct is 5 fields with `KnownFields(true)`. Adding optional fields (kind, phase, milestone, acceptance, verify, lane, retry overrides) is low-risk and unblocks #3, #2, #14, and phase/milestone Make entrypoints. Unblocks: #2, #3, #14. |
| **P3** | **#6 Concurrency Safety** | Already support `make -jN` — no guards exist today. Task-level locking via `.kiln/locks/<id>.lock` + atomic log writes. Benefits from #1 (state tracks "running" status). Promoted to Phase 1 because parallel execution is core to the Make-first model and becomes more critical with lanes, phases, and research parallelism. |

### Phase 2 — Closure & Recovery (depends on Phase 1)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P4** | **#12 UNIFY / Closure Artifacts** | High-leverage new primitive. Produces intent-to-reality reconciliation that feeds #8 Prompt Chaining with high-signal context packs. Also strengthens #3 (closure can require gates). Single-shot invocation — no architectural change. Depends on: #1 (state tracks completion). |
| **P5** | **#13 Recovery UX** | Ergonomic commands (`kiln status`, `kiln resume`, `kiln retry`, `kiln reset`) that turn state into action. Minimizes manual log inspection. Depends on: #1 (state data), benefits from #12 (resume injects closure artifacts). |
| **P6** | **#9 Error Taxonomy & Reporting** | Standardizes error classes in logs and adds `kiln report`. Already have `timeoutError`, `claudeExitError`, `footerError` and `isRetryable()` — this formalizes them. Depends on: #1. Feeds: #10 TUI, #13 Recovery UX. |

### Phase 3 — Correctness Gates & Chaining (depends on Phase 1+2)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P7** | **#3 Validation Hooks** | Post-exec gates (test/lint/build). Depends on: #7 (per-task verify/validation overrides). Benefits from #1 (state records validation results) and #12 (closure can require gates passed). |
| **P8** | **#14 Verification Mapping** | `kiln verify-plan` ensures every task with acceptance criteria has verify gates. Extends #3 with a pre-run coverage check. Depends on: #7 (acceptance/verify fields), #3 (gate execution). |
| **P9** | **#8 Prompt Chaining** | Injecting UNIFY closure artifacts and research outputs into downstream prompts. Depends on: #12 (closure artifacts are the primary injection source), #7 (kind: research produces artifacts). High-impact for multi-task graphs. |

### Phase 4 — UX & Adoption (independent, can be done anytime after Phase 1)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P10** | **#5 Machine-Readable Output** | `--format json|text` for `kiln exec` and `kiln gen-make`. No dependencies. Small scope — already writes JSON logs. Could be done earlier if CI integration is urgent. |
| **P11** | **#4 `kiln init`** | Scaffolding generator. No code dependencies, but benefits from #7 so templates include the full schema. Better to wait until schema settles. |
| **P12** | **#10 Interactive TUI** | Read-only dashboard first (`kiln tui`), then interactive controls. Depends on: #1 (state file for reliable live status). Benefits from #9 (error taxonomy feeds summary panel), #7 (lanes/milestones for grouping views). |
| **P13** | **#15 Profile Strategy** | Speed vs reliable workflow presets. Benefits from #12 (UNIFY requirement toggle), #3 (gate enforcement toggle), #6 (parallelism limits). Low complexity — just default-value presets. |

### Phase 5 — Abstraction & Extensibility (last, highest complexity)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| **P14** | **#11 Git Automation** | Opt-in git integration (verify commits, auto-commit, branch-per-task, PR hooks). Benefits from #3 (validation hooks can include "verify commit exists"). Lower refactor surface than engine abstraction. |
| **P15** | **#2 Engine Abstraction** | Multi-engine support requires an `Engine` interface wrapping `commandBuilder`, `parseFooter`, and error classification. Depends on: #7 (per-task engine field). Highest refactor surface — touches `execOnce`, `commandBuilder`, `parseFooter`, and error types. Do this last, after the core is stable and a second engine is actually needed. |

---

## Dependency Graph

```
#7 Richer Schema ──────┬──→ #3 Validation Hooks ──┬──→ #11 Git Automation
  (kind, phase,        ├──→ #2 Engine Abstraction  ├──→ #14 Verification Mapping
   milestone,          ├──→ #8 Prompt Chaining     │
   acceptance, verify) │     (also needs #12)      │
                       │                           │
#1 State & Resumability┬──→ #12 UNIFY / Closure ──┬──→ #8 Prompt Chaining
                       ├──→ #13 Recovery UX        │    (primary injection source)
                       ├──→ #9 Error Taxonomy ─────┼──→ #10 TUI (benefits)
                       ├──→ #6 Concurrency Safety  │
                       ├──→ #10 Interactive TUI    │
                       │                           │
#5 Machine-Readable Output  (independent)          │
#4 kiln init                (independent, benefits from #7)
#15 Profile Strategy        (independent, benefits from #3, #6, #12)
```

---

## Key Observations

1. **#1, #7, and #6 are the Phase 1 foundation** — almost everything else benefits from or directly depends on them. They're also the lowest-risk changes (additive, no breaking changes). #6 is promoted early because parallel execution is core to the Make-first model.
2. **#12 UNIFY is the highest-leverage new primitive** — it bridges the gap between "task ran" and "task is reconciled," producing the high-signal artifacts that make #8 Prompt Chaining and #13 Recovery UX dramatically more useful.
3. **#8 Prompt Chaining now depends on #12** — UNIFY closure artifacts become the primary injection source, replacing raw state/log data with intent-to-reality reconciliation summaries.
4. **#5 (JSON output) is a wildcard** — if CI integration is needed soon, bump it to Phase 1. It's trivially small.
5. **#2 (engine abstraction) is deliberately last** — it's the largest refactor and isn't needed until a second engine is actually required. Premature abstraction here would slow down everything else.
6. **#14 Verification Mapping (Nyquist)** extends #3 with a coverage guarantee: every acceptance criterion maps to a gate. This is the difference between "we have hooks" and "we have coverage."
7. **#15 Profile Strategy** prevents feature sprawl — speed vs reliable presets let you adopt UNIFY/gates/parallelism controls selectively without rewriting config per project.
8. **Research separation is folded into #7** — the `kind: research` field + artifact conventions enable safe parallelism without building multi-agent orchestration. Let Make handle the parallelism.
9. **#10 (TUI) in Phase 4** — it's a UX feature with no downstream dependents, so it doesn't block anything. But it hard-depends on #1 (state file) for live status beyond `.done` markers, and benefits significantly from #9 (error taxonomy feeds the summary panel).
10. **#11 (git automation) before #2** — git integration has a smaller refactor surface and benefits from validation hooks (#3) already being in place, making it a natural next step before tackling the engine abstraction.

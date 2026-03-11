# Adopting Kiln in a Pre-Existing Project

> For projects already in progress that want to use Kiln for structured, context-rot-free AI development.

This guide walks you through introducing Kiln into a project that already has code, a PRD, and partial progress. Unlike starting from scratch, you need to assess what's done, identify what remains, and set up Kiln's workflow to carry the project forward — without redoing completed work.

---

## Table of Contents

- [[#When to Adopt Kiln Mid-Project]]
- [[#Prerequisites]]
- [[#Step 1: Initialize Kiln in Your Project]]
- [[#Step 2: Assess Current Completion]]
- [[#Step 3: Write or Update Your PRD]]
- [[#Step 4: Generate the Task Graph]]
- [[#Step 5: Curate tasks.yaml for a Mid-Flight Project]]
- [[#Step 6: Mark Completed Work]]
- [[#Step 7: Write Prompts for Remaining Tasks]]
- [[#Step 8: Generate Make Targets and Execute]]
- [[#Step 9: Monitor and Recover]]
- [[#Recipes for Common Scenarios]]
- [[#Reference: Key Commands for Adoption]]

---

## When to Adopt Kiln Mid-Project

Kiln is designed around the "Ralph Wiggum" pattern: one task per fresh Claude Code invocation, orchestrated by Make. This prevents context rot — the gradual degradation in AI output quality that happens when conversations accumulate too much state.

Adopt Kiln mid-project when:

- **Long Claude sessions are producing lower-quality output.** You're spending more time correcting AI work than writing it yourself.
- **You've lost track of what's done vs. what remains.** The PRD says one thing, the code says another, and nobody is sure which tasks are actually complete.
- **You want parallelism.** Multiple independent tasks could run simultaneously, but you've been doing them sequentially in a single session.
- **You need reproducibility.** Each task should be runnable, retryable, and auditable — not buried in a conversation transcript.

Kiln doesn't require starting over. You scaffold the `.kiln/` directory, assess current progress, mark completed work as done, and let Kiln handle the rest.

---

## Prerequisites

Before you begin:

- **Go toolchain** — Go 1.21 or later (`go version`)
- **Claude Code CLI** — installed and authenticated (`claude --version`)
- **GNU Make** — available as `make` in your PATH
- **Kiln binary** — built and on your PATH:

```bash
# Clone and build Kiln (if you haven't already)
git clone <kiln-repo-url>
cd kiln
make bin/kiln
cp bin/kiln /usr/local/bin/kiln

# Verify
kiln version
```

---

## Step 1: Initialize Kiln in Your Project

Navigate to your existing project root and run `kiln init`. Kiln will scaffold the `.kiln/` directory structure and create starter files. It will **not overwrite** files that already exist (like your `Makefile` or `PRD.md`) unless you pass `--force`.

```bash
cd /path/to/your-project

# Initialize with a language profile (go, python, node, or generic)
kiln init --profile go
```

Kiln creates:

```
.kiln/
├── tasks.yaml              # Starter task graph (you'll replace this)
├── prompts/
│   └── tasks/
│       ├── hello-world.md  # Example prompt (you'll replace these)
│       └── follow-up.md
├── done/                   # Idempotency markers (empty at init)
└── logs/                   # Execution logs (empty at init)
```

It also creates a `Makefile` and `PRD.md` if they don't exist. Since your project already has these, you'll see:

```
created: /path/to/your-project/.kiln/tasks.yaml
created: /path/to/your-project/.kiln/prompts/tasks/hello-world.md
created: /path/to/your-project/.kiln/prompts/tasks/follow-up.md
skipped: /path/to/your-project/Makefile (already exists; use --force to overwrite)
skipped: /path/to/your-project/PRD.md (already exists; use --force to overwrite)
```

### Patch Your Existing Makefile

Since `kiln init` won't overwrite your Makefile, add the Kiln targets manually. Add these lines to your existing Makefile:

```makefile
# ---- Kiln integration ----
KILN := kiln
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# Include generated targets (no error if file doesn't exist yet)
-include $(TARGETS_FILE)

.PHONY: plan graph clean-kiln

plan:
	$(KILN) plan

graph:
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE) \
		$(if $(DEV_PHASE),--dev-phase $(DEV_PHASE))

# Fallback if targets.mk doesn't exist
ifeq ($(wildcard $(TARGETS_FILE)),)
.PHONY: all
all:
	$(error $(TARGETS_FILE) not found — run 'make graph' first)
endif

clean-kiln:
	rm -rf .kiln/done .kiln/logs $(TARGETS_FILE)
```

### Update .gitignore

Add transient Kiln artifacts to your `.gitignore`:

```
# Kiln runtime artifacts
.kiln/done/
.kiln/logs/
.kiln/locks/
.kiln/targets.mk
.kiln/state.json
```

Keep `.kiln/tasks.yaml`, `.kiln/prompts/`, `.kiln/templates/`, and `.kiln/config.yaml` tracked in git — these are your project's task definitions and should be versioned.

---

## Step 2: Assess Current Completion

Before generating tasks, you need to understand what your project has already accomplished. This is the most important step when adopting Kiln mid-project.

### Manual Audit

Walk through your PRD and check each requirement against the actual codebase:

1. **List every feature/requirement** in your PRD.
2. **For each, determine its status:**
   - **Done** — fully implemented, tested, working.
   - **Partial** — started but incomplete. Note what's missing.
   - **Not started** — no code exists for this requirement.
3. **Check for undocumented work** — features that exist in the code but aren't in the PRD. Decide whether to add them to the PRD or treat them as out of scope.

### Use Git History as Evidence

```bash
# See what's been worked on recently
git log --oneline --since="2 weeks ago"

# Check which files have been modified most
git log --pretty=format: --name-only --since="1 month ago" | sort | uniq -c | sort -rn | head -20

# Look for TODO/FIXME markers in the codebase
grep -rn "TODO\|FIXME\|HACK\|XXX" --include="*.go" --include="*.py" --include="*.ts" .
```

### Create a Completion Matrix

Before touching `tasks.yaml`, write down your assessment. A simple table works:

| Requirement | Status | Evidence | Notes |
|---|---|---|---|
| User authentication | Done | `internal/auth/` has JWT + refresh | Tests pass |
| Database schema | Done | Migrations applied | |
| REST API endpoints | Partial | 3/7 endpoints implemented | Missing: search, bulk, export, webhook |
| Admin dashboard | Not started | | Depends on API |
| Error handling | Partial | Happy paths only | No retry logic, no error pages |

This matrix becomes the basis for your `tasks.yaml`.

---

## Step 3: Write or Update Your PRD

Kiln's `make plan` command uses Claude to parse your PRD into a task graph. For this to work well with a mid-flight project, your PRD needs to reflect **what remains**, not just the original vision.

### Option A: Annotate Your Existing PRD

Add a "Current Status" section to your PRD that explicitly marks completed work:

```markdown
## Current Status (as of 2026-03-08)

### Completed
- User authentication (JWT + refresh tokens) — fully implemented and tested
- Database schema and migrations — applied and stable
- API endpoints: GET /users, POST /users, GET /users/:id

### In Progress
- API endpoints: remaining 4 of 7 need implementation
- Error handling: happy-path only, needs retry logic and error pages

### Not Started
- Admin dashboard
- Email notifications
- Export/import functionality
```

### Option B: Write a Focused "Remaining Work" PRD

If your original PRD is long or out of date, create a focused document that describes only remaining work:

```markdown
# Remaining Work PRD

## Context
This project has an existing codebase with user auth, database schema,
and 3/7 API endpoints already implemented. The following tasks describe
the remaining work needed to reach MVP.

## Tasks

### Task: api-remaining-endpoints
Implement the 4 remaining REST API endpoints: ...

### Task: error-handling
Add comprehensive error handling across the API layer: ...

### Task: admin-dashboard
Build the admin dashboard UI: ...
```

This focused PRD produces cleaner task extraction because Claude doesn't have to distinguish between "done" and "not done" requirements.

---

## Step 4: Generate the Task Graph

With your PRD updated, use Kiln to extract tasks:

```bash
# If you have a standard PRD at PRD.md
kiln plan

# If your remaining-work PRD is elsewhere
kiln plan --prd docs/remaining-work-prd.md --out .kiln/tasks.yaml
```

This invokes Claude (using `claude-opus-4-6` by default) with the extraction prompt at `.kiln/prompts/00_extract_tasks.md` to produce `.kiln/tasks.yaml`.

### Validate the Output

After `kiln plan` completes, validate the generated task graph:

```bash
# Check schema validity
kiln validate-schema --tasks .kiln/tasks.yaml

# Check for dependency cycles
kiln validate-cycles --tasks .kiln/tasks.yaml
```

---

## Step 5: Curate tasks.yaml for a Mid-Flight Project

The auto-generated `tasks.yaml` is a starting point. For a pre-existing project, you'll almost certainly need to edit it. Open `.kiln/tasks.yaml` and make these adjustments:

### Remove Tasks for Completed Work

If Claude included tasks for work that's already done, delete them. Don't leave them in the file — they'll generate unnecessary Make targets.

### Add Context to Remaining Tasks

For tasks that build on existing code, add a `description` field that references the current state:

```yaml
- id: api-remaining-endpoints
  prompt: .kiln/prompts/tasks/api-remaining-endpoints.md
  needs: []
  description: "Implement search, bulk, export, and webhook endpoints. Existing endpoints in internal/api/ follow RESTful patterns with gin framework."
  kind: feature
  phase: build
```

### Set Accurate Dependencies

Claude's auto-generated `needs` lists are conservative. For a mid-project adoption, you may need to tighten them based on your knowledge of the codebase:

```yaml
- id: admin-dashboard
  prompt: .kiln/prompts/tasks/admin-dashboard.md
  needs:
    - api-remaining-endpoints   # Dashboard calls all 7 API endpoints
  kind: feature
  phase: build

- id: error-handling
  prompt: .kiln/prompts/tasks/error-handling.md
  needs: []                     # Can run independently — touches all layers
  kind: fix
  phase: build
```

### Use Phases and Milestones for Staged Rollout

If you want to execute tasks in waves:

```yaml
- id: api-remaining-endpoints
  prompt: .kiln/prompts/tasks/api-remaining-endpoints.md
  dev-phase: 1
  phase: build
  milestone: mvp-api

- id: admin-dashboard
  prompt: .kiln/prompts/tasks/admin-dashboard.md
  dev-phase: 2
  phase: build
  milestone: mvp-ui
  needs:
    - api-remaining-endpoints
```

Then generate targets for only phase 1:

```bash
make graph DEV_PHASE=1
```

### Add Verify Gates

For tasks with testable acceptance criteria, add verification gates that run after task completion:

```yaml
- id: api-remaining-endpoints
  prompt: .kiln/prompts/tasks/api-remaining-endpoints.md
  acceptance:
    - "GET /search returns paginated results"
    - "POST /bulk accepts batch payloads"
    - "GET /export returns CSV"
    - "POST /webhook validates signatures"
  verify:
    - name: "API tests pass"
      cmd: "go test ./internal/api/..."
    - name: "Build succeeds"
      cmd: "go build ./..."
```

### Re-Validate After Editing

```bash
kiln validate-schema --tasks .kiln/tasks.yaml
kiln validate-cycles --tasks .kiln/tasks.yaml
```

---

## Step 6: Mark Completed Work

If your `tasks.yaml` includes tasks that are already done (for example, as dependency anchors for other tasks), you need to mark them complete so Make skips them.

### Option A: Create .done Markers Manually

```bash
mkdir -p .kiln/done

# Mark specific tasks as complete
touch .kiln/done/user-auth.done
touch .kiln/done/database-schema.done
touch .kiln/done/api-users-endpoints.done
```

Make uses these `.done` files as targets. If the file exists, the task is considered satisfied and won't re-run.

### Option B: Don't Include Already-Done Tasks

The cleaner approach: only put remaining tasks in `tasks.yaml`. If a completed feature is a dependency, it shouldn't be a task at all — the code already exists. Structure your remaining tasks so they don't depend on task IDs that aren't in the file:

```yaml
# Don't include auth as a task — it's already done.
# Just make sure downstream prompts know auth exists.

- id: admin-dashboard
  prompt: .kiln/prompts/tasks/admin-dashboard.md
  needs: []  # Auth is already in the codebase, not a task
```

Then, in the prompt file for `admin-dashboard`, reference the existing auth code:

```markdown
# Task: admin-dashboard

The project already has JWT authentication implemented in `internal/auth/`.
Use the existing auth middleware when protecting admin routes.
...
```

---

## Step 7: Write Prompts for Remaining Tasks

Each task needs a prompt file that tells Claude exactly what to do. For mid-project adoption, prompts need extra context about the existing codebase.

### Auto-Generate Prompts

If you have a template at `.kiln/templates/<id>.md`, Kiln can generate prompts via Claude:

```bash
kiln gen-prompts \
  --tasks .kiln/tasks.yaml \
  --prd PRD.md
```

This invokes Claude once per task (skipping tasks that already have a prompt file) to produce `.kiln/prompts/tasks/<id>.md`.

To regenerate all prompts, even existing ones:

```bash
kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md --overwrite
```

### Write Prompts Manually

For a pre-existing project, manually written prompts often produce better results because you can include codebase-specific context. Each prompt should follow this structure:

```markdown
# Task: <task-id>

## Role
You are an AI coding agent working in a local git repository.

## TASK ID
<task-id>

## CONTEXT
- This is an existing project with [describe current state].
- The following modules already exist and should be used: [list them].
- Do NOT modify: [list files/modules that are stable].

## REQUIREMENTS
1. [Specific requirement 1]
2. [Specific requirement 2]

## EXISTING CODE REFERENCES
- Authentication: `internal/auth/` (JWT + refresh tokens, fully tested)
- Database: `internal/db/` (PostgreSQL, migrations in `migrations/`)
- API framework: Gin (see existing endpoints in `internal/api/handlers.go`)

## ACCEPTANCE CRITERIA
- [Criterion 1]
- [Criterion 2]

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, output a single line:

{"kiln":{"status":"complete","task_id":"<task-id>"}}
```

The key difference from a greenfield prompt: **reference existing code explicitly.** Tell Claude what exists, what patterns to follow, and what not to touch.

---

## Step 8: Generate Make Targets and Execute

### Generate Targets

```bash
# Generate targets for all tasks
make graph

# Or for a specific phase
make graph DEV_PHASE=1
```

This runs `kiln gen-make`, which reads `.kiln/tasks.yaml` and writes `.kiln/targets.mk` — a Make include file with one target per task, respecting dependencies.

### Execute Tasks

```bash
# Run all tasks sequentially
make all

# Run up to 3 tasks in parallel (respects dependency graph)
make -j3 all

# Run a specific task
make .kiln/done/api-remaining-endpoints.done

# Run all tasks in a specific phase
make phase-build

# Run all tasks in a specific milestone
make milestone-mvp-api
```

### Run a Single Task Directly

```bash
# Using kiln exec directly
kiln exec --task-id api-remaining-endpoints

# With custom timeout and retries
kiln exec --task-id api-remaining-endpoints --timeout 30m --retries 2 --retry-backoff 30s

# With exponential backoff
kiln exec --task-id api-remaining-endpoints --retries 3 --retry-backoff 30s --backoff exponential

# Skip verify gates for debugging
kiln exec --task-id api-remaining-endpoints --skip-verify

# Disable prompt chaining (no dependency context injection)
kiln exec --task-id api-remaining-endpoints --no-chain
```

---

## Step 9: Monitor and Recover

### Check Status

```bash
# Human-readable scoreboard
kiln status --tasks .kiln/tasks.yaml

# Machine-readable JSON
kiln status --tasks .kiln/tasks.yaml --format json
```

The scoreboard shows every task's status, attempt count, kind, phase, last error, and dependencies.

### Read Logs

Every `kiln exec` invocation writes a structured JSON log:

```bash
cat .kiln/logs/api-remaining-endpoints.json | python3 -m json.tool
```

Key fields: `task_id`, `status`, `exit_code`, `error_class`, `error_message`, `retryable`, `duration_ms`.

### Generate Reports

```bash
# Summary across all tasks
kiln report

# JSON format for automation
kiln report --format json
```

### When Tasks Fail

1. **Check the scoreboard:**
   ```bash
   kiln status --tasks .kiln/tasks.yaml
   ```

2. **Read the report for error patterns:**
   ```bash
   kiln report
   ```

3. **Retry failed tasks:**
   ```bash
   # Retry all failed tasks
   kiln retry --tasks .kiln/tasks.yaml --failed

   # Retry only transient failures (timeouts, crashes)
   kiln retry --tasks .kiln/tasks.yaml --failed --transient-only

   # Retry a specific task
   kiln retry --tasks .kiln/tasks.yaml --task-id api-remaining-endpoints
   ```

4. **Generate a resume prompt** (includes prior execution context):
   ```bash
   kiln resume --task-id api-remaining-endpoints --tasks .kiln/tasks.yaml
   ```

5. **Reset a task** to start from scratch:
   ```bash
   kiln reset --task-id api-remaining-endpoints --tasks .kiln/tasks.yaml
   ```

6. **Re-run Make** — it picks up where it left off:
   ```bash
   make all
   ```

### Generate Closure Artifacts

After a task completes, generate a UNIFY closure artifact that summarizes what actually changed:

```bash
kiln unify --task-id api-remaining-endpoints
```

This produces `.kiln/unify/api-remaining-endpoints.md` with:
- What changed (files modified, functions added/removed)
- What's incomplete or deferred
- Decisions made and rationale
- Handoff notes for downstream tasks

Closure artifacts are automatically injected into downstream task prompts via prompt chaining, giving later tasks context about what previous tasks actually did.

---

## Recipes for Common Scenarios

### Scenario: Legacy Codebase with No PRD

1. Write a minimal PRD that describes only the remaining work.
2. Run `kiln init --profile go` (or your language).
3. Run `kiln plan --prd remaining-work.md`.
4. Edit `tasks.yaml` to remove anything already done.
5. Write prompts that heavily reference existing code patterns.
6. Run `make graph && make all`.

### Scenario: Partially Complete Feature Set

1. Run `kiln init`.
2. Write a PRD that marks completed features in a "Current Status" section.
3. Run `kiln plan` — Claude will focus on incomplete work.
4. Manually create `.done` markers for any tasks that correspond to completed work.
5. Run `make graph && make -j3 all`.

### Scenario: Refactoring Existing Code

1. Create tasks that target specific refactoring units (one module per task).
2. Set `needs` so refactoring happens in dependency order (leaf modules first).
3. Add verify gates with your existing test suite:
   ```yaml
   verify:
     - name: "Tests still pass"
       cmd: "go test ./..."
     - name: "No regressions"
       cmd: "go vet ./..."
   ```
4. Run tasks sequentially (`make all` without `-j`) to avoid conflicts.

### Scenario: Adding Tests to Untested Code

1. Create one task per module/package that needs tests.
2. Set all tasks as independent (`needs: []`) — test files don't conflict.
3. Use `kind: verify` in `tasks.yaml`.
4. Run in parallel: `make -j4 all`.

### Scenario: Project with Multiple PRDs or Specs

1. Concatenate relevant specs into a single PRD, or reference them from prompts.
2. Alternatively, run `kiln plan` multiple times with different `--prd` flags and merge the resulting `tasks.yaml` files manually.
3. The key constraint: there is one `tasks.yaml` per project.

---

## Reference: Key Commands for Adoption

| Command | Purpose |
|---|---|
| `kiln init --profile go` | Scaffold `.kiln/` directory (won't overwrite existing files) |
| `kiln init --profile go --force` | Scaffold and overwrite existing Kiln files |
| `kiln plan --prd PRD.md` | Generate `tasks.yaml` from your PRD |
| `kiln validate-schema --tasks .kiln/tasks.yaml` | Validate task schema |
| `kiln validate-cycles --tasks .kiln/tasks.yaml` | Check for dependency cycles |
| `kiln gen-prompts --tasks .kiln/tasks.yaml --prd PRD.md` | Auto-generate per-task prompt files |
| `kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk` | Generate Make targets |
| `kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk --dev-phase 1` | Generate targets for phase 1 only |
| `kiln exec --task-id my-task` | Run a single task |
| `kiln status --tasks .kiln/tasks.yaml` | Show task scoreboard |
| `kiln report` | Show execution summary |
| `kiln verify-plan --tasks .kiln/tasks.yaml` | Check acceptance/gate coverage |
| `kiln retry --tasks .kiln/tasks.yaml --failed` | Retry failed tasks |
| `kiln reset --task-id my-task --tasks .kiln/tasks.yaml` | Reset a task to re-run from scratch |
| `kiln resume --task-id my-task --tasks .kiln/tasks.yaml` | Generate resume prompt with prior context |
| `kiln unify --task-id my-task` | Generate closure artifact for a completed task |
| `kiln profile --config .kiln/config.yaml` | Show active workflow profile settings |

### Exit Codes (kiln exec)

| Code | Meaning |
|---|---|
| `0` | Task reported `complete` and all verify gates passed |
| `2` | Task reported `not_complete` or `blocked`; or a verify gate failed |
| `10` | Permanent failure (invalid footer, schema error, lock conflict) |
| `20` | Transient retries exhausted (timeout or Claude process crash) |

### Environment Variables

| Variable | Description |
|---|---|
| `KILN_MODEL` | Default Claude model. Falls back to `claude-sonnet-4-6` if not set |

Model resolution precedence (highest to lowest):
1. `--model` flag
2. Per-task `model` field in `tasks.yaml`
3. `KILN_MODEL` environment variable
4. Built-in default: `claude-sonnet-4-6`

Note: `kiln plan` and `kiln gen-prompts` default to `claude-opus-4-6` when no model override is set.

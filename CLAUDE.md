# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Kiln?

Kiln is a Go CLI tool that prevents Claude Code context rot by running one task per fresh invocation, orchestrated by Make for dependency resolution and parallel execution. It implements the "Ralph Wiggum" pattern: PRD → tasks.yaml → generated Make targets → executed tasks with timeouts, retries, and structured logging.

## Versioning

Kiln uses `ldflags` + git tags for version injection. The `version` variable in `main.go` defaults to `"dev"` and is overridden at build time by the Makefile using `git describe --tags --always --dirty`.

- Tag releases with semver: `git tag v0.1.0`
- `kiln version` prints the current version string
- Building via `make` automatically injects the version; `go build` without ldflags produces `"dev"`

## Build & Test

```bash
# Build (with version injection)
make bin/kiln

# Build (without version — prints "dev")
go build -o kiln ./cmd/kiln

# Run all tests
go test ./cmd/kiln -v

# Run a single test
go test ./cmd/kiln -v -run TestExecTimeout

# Test coverage
go test ./cmd/kiln -v -coverprofile=cover.out && go tool cover -func=cover.out
```

## Workflow Commands

```bash
make plan      # Parse PRD.md → .kiln/tasks.yaml (via Claude)
make graph     # Generate Make targets → .kiln/targets.mk
make all       # Run all tasks in dependency graph (runs graph first)
```

## Architecture

All source lives in `cmd/kiln/main.go` (single-package CLI). Two subcommands:

- **`kiln exec`** — Runs a single Claude Code invocation for one task. Handles timeout (`context.WithTimeout`), retries with backoff, JSON footer parsing, structured logging, and done-marker creation.
- **`kiln gen-make`** — Reads `.kiln/tasks.yaml` and generates `.kiln/targets.mk` with Make targets respecting the dependency graph.

### Execution flow

```
PRD.md → (make plan) → .kiln/tasks.yaml → (make graph / kiln gen-make) → .kiln/targets.mk → (make all) → kiln exec per task → .kiln/logs/<id>.json + .kiln/done/<id>.done
```

### Key abstractions

- **Error types**: `timeoutError`, `claudeExitError`, `footerError` — `isRetryable()` decides retry eligibility (timeouts and claude exit errors are retryable; validation/parse errors are not).
- **JSON footer contract**: Claude output must end with `{"kiln":{"status":"complete|not_complete|blocked","task_id":"<id>"}}`.
- **Exit codes**: 0 = success (complete), 2 = not_complete/blocked, 10 = permanent failure, 20 = transient retries exhausted.
- **Testability**: `commandBuilder` and `sleepFn` are package-level vars injected in tests via `TestHelperProcess` pattern.

### .kiln/ directory

| Path | Purpose |
|------|---------|
| `tasks.yaml` | Task graph (id, prompt path, needs, optional timeout) |
| `targets.mk` | Generated Make include file |
| `prompts/tasks/<id>.md` | Per-task prompt files |
| `logs/<id>.json` | Per-task execution logs (one entry per attempt) |
| `done/<id>.done` | Idempotency markers for Make |

### Environment & flags

- `KILN_MODEL` env var sets default model (fallback: `claude-sonnet-4-6`)
- `--model` flag overrides `KILN_MODEL`
- `--timeout` defaults to 15 minutes
- Task IDs must be kebab-case: `^[a-z0-9]+(?:-[a-z0-9]+)*$`

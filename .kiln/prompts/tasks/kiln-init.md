# Task: kiln-init — Scaffolding Generator

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## TASK ID
kiln-init

## SCOPE
Implement ONLY the `kiln init` scaffolding subcommand described below. Do not work on other backlog items (state resumability, richer task schema, concurrency safety, UNIFY, etc.), even if you notice related opportunities.

## CONTEXT
- This project uses the AgentRun workflow to reduce context rot.
- You are executing a single task in isolation. Assume other tasks may not be done yet unless you can confirm in the repo.
- All source lives in `cmd/kiln/main.go` (single-package CLI).
- Kiln currently has two subcommands: `kiln exec` and `kiln gen-make`. This task adds a third: `kiln init`.
- The `.kiln/` directory structure includes: `tasks.yaml`, `targets.mk`, `prompts/tasks/`, `logs/`, `done/`.
- A `Makefile` at the project root drives the workflow with `make plan`, `make graph`, `make all`.
- Task IDs must be kebab-case: `^[a-z0-9]+(?:-[a-z0-9]+)*$`.

## REQUIREMENTS

1. **New `kiln init` subcommand** — Add a `kiln init` subcommand that scaffolds a complete `.kiln/` directory structure and supporting files for a new or existing project:
   - Parse `kiln init` as a new subcommand alongside `exec` and `gen-make`.
   - Accept an optional `--profile` flag (string) with allowed values: `go`, `python`, `node`, `generic`. Default: `generic`.
   - Accept an optional `--force` flag (bool, default false) that allows overwriting existing files. Without `--force`, refuse to overwrite any file that already exists and print a warning listing the skipped files.

2. **Directory scaffolding** — Create the following directories (using `os.MkdirAll`):
   - `.kiln/`
   - `.kiln/prompts/tasks/`
   - `.kiln/logs/`
   - `.kiln/done/`

3. **Template: `.kiln/tasks.yaml`** — Generate a starter tasks.yaml with two example tasks:
   - Task `hello-world`: no dependencies, prompt path `.kiln/prompts/tasks/hello-world.md`.
   - Task `follow-up`: depends on `hello-world`, prompt path `.kiln/prompts/tasks/follow-up.md`.
   - Include YAML comments explaining the schema fields (`id`, `prompt`, `needs`, and optionally `timeout`).

4. **Template: example prompt files** — Generate two starter prompt files:
   - `.kiln/prompts/tasks/hello-world.md` — A minimal prompt that asks the agent to create a hello-world file in the project's language (based on `--profile`), with the correct kiln JSON footer contract.
   - `.kiln/prompts/tasks/follow-up.md` — A minimal prompt that asks the agent to verify the hello-world file exists and add a comment to it, with the correct kiln JSON footer contract.

5. **Template: `Makefile`** — Generate a project-root `Makefile` (or skip if one exists and `--force` is not set) containing:
   - `include .kiln/targets.mk` (with a `-include` guard so Make doesn't fail if the file doesn't exist yet)
   - `plan` target: placeholder comment (manual step — run claude to generate tasks from PRD)
   - `graph` target: runs `kiln gen-make`
   - `all` target: depends on `graph`, then runs the generated targets
   - Profile-aware: for `go` profile, include `go build` and `go test` as example validation targets; for `python`, include `pytest`; for `node`, include `npm test`; for `generic`, omit language-specific targets.

6. **Template: `PRD.md`** — Generate a starter `PRD.md` at the project root (or skip if exists and no `--force`) with a brief outline structure:
   - Project name placeholder
   - Goals section
   - Tasks section with guidance on how tasks map to `.kiln/tasks.yaml`

7. **Console output** — After scaffolding, print a summary of what was created and what was skipped:
   - List each file/directory created with a `created:` prefix.
   - List each file skipped (already exists) with a `skipped:` prefix.
   - Print a short "next steps" message: edit `PRD.md`, run `make plan`, then `make all`.

8. **No external dependencies** — Use only the Go standard library. Do not add third-party packages.

## Tests

- `kiln init` creates all expected directories (`.kiln/`, `.kiln/prompts/tasks/`, `.kiln/logs/`, `.kiln/done/`)
- `kiln init` creates `.kiln/tasks.yaml` with valid YAML containing two example tasks
- `kiln init` creates example prompt files in `.kiln/prompts/tasks/`
- `kiln init` creates a `Makefile` with `include`, `plan`, `graph`, and `all` targets
- `kiln init` creates a `PRD.md` with placeholder content
- `--profile go` produces a Makefile with `go build` / `go test` targets
- `--profile python` produces a Makefile with `pytest` target
- `--profile node` produces a Makefile with `npm test` target
- `--profile generic` produces a Makefile without language-specific targets
- Without `--force`, existing files are not overwritten and a skip message is printed
- With `--force`, existing files are overwritten
- Invalid `--profile` value produces an error
- Running `kiln init` twice without `--force` skips all existing files and reports them
- All generated YAML is parseable
- Console output includes created/skipped file listing and next-steps message
- `go test ./...` passes with no regressions

## ACCEPTANCE CRITERIA
- `kiln init` subcommand is registered and runs without error on an empty directory
- All directories are created: `.kiln/`, `.kiln/prompts/tasks/`, `.kiln/logs/`, `.kiln/done/`
- `.kiln/tasks.yaml` is generated with valid example tasks and explanatory comments
- Example prompt files are generated with correct kiln JSON footer contract
- `Makefile` is generated with `include`, `plan`, `graph`, and `all` targets
- `PRD.md` is generated with placeholder structure
- `--profile` flag works for `go`, `python`, `node`, `generic` and rejects invalid values
- `--force` flag controls overwrite behavior; without it, existing files are skipped with warnings
- Console output lists created and skipped files plus next-steps guidance
- No external dependencies added (standard library only)
- `go test ./...` passes
- No large refactors unrelated to the init subcommand

## OUTPUT FORMAT CONTRACT (MANDATORY)
At the end of your response, you MUST output a single line containing ONLY a JSON object with this structure:

{"kiln":{"status":"complete","task_id":"kiln-init"}}

Allowed values for status:
- "complete"     (all acceptance criteria met)
- "not_complete" (work attempted but acceptance criteria not met)
- "blocked"      (cannot proceed due to missing info, permissions, dependencies, or unclear requirements)

STRICT RULES FOR THE JSON FOOTER
- The JSON object MUST be the final line of your response.
- Output EXACTLY one JSON object.
- No extra text after it.
- No code fences around it.
- The task_id must exactly match the TASK ID above.
- If you are unsure, choose "not_complete" or "blocked" rather than "complete".

If you finish successfully, the correct final line is:
{"kiln":{"status":"complete","task_id":"kiln-init"}}

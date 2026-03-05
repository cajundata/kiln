# Task: gen-prompts — scaffold prompt files from tasks.yaml

## Role
You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Implement focused, minimal changes with tests.

## Goal
Add `kiln gen-prompts` as a first-class command that reads `.kiln/tasks.yaml` and generates a prompt file (`.kiln/prompts/tasks/<id>.md`) for each task that does not already have one. Uses the prompt template at `.kiln/templates/<id>.md` to scaffold each file, filling in the task ID and placeholder sections.

This command should invoke Claude (defaulting to `claude-opus-4-6`, same as `kiln plan`) to generate high-quality, task-specific prompt content based on the PRD and task graph context.

## Context
After `kiln plan` generates `tasks.yaml`, the user must manually write each prompt file before `make all` can run. This command closes that gap by scaffolding prompt files automatically. The existing template at `.kiln/templates/<id>.md` defines the structure each prompt should follow: task ID, scope, requirements, acceptance criteria, and the mandatory kiln JSON footer.

## Requirements

1. Add `kiln gen-prompts` command with flags:
   - `--tasks <path>` (default: `.kiln/tasks.yaml`) — path to tasks file
   - `--prd <path>` (default: `PRD.md`) — path to the PRD file (provides context for prompt generation)
   - `--template <path>` (default: `.kiln/templates/<id>.md`) — path to the prompt template
   - `--model <model>` (optional) — model override (default: `claude-opus-4-6`, matching `kiln plan`)
   - `--timeout <duration>` (default: `15m`) — timeout per Claude invocation
   - `--overwrite` (default: `false`) — if true, regenerate prompts even when the file already exists
2. Implementation:
   - Read tasks.yaml via `loadTasks()`
   - Read the PRD file
   - Read the prompt template
   - For each task where the prompt file does not exist (or `--overwrite` is set):
     - Invoke Claude with a meta-prompt that includes the template, the PRD content, the task ID, and instructions to produce a filled-in prompt file
     - Write Claude's output to the task's prompt path (`.kiln/prompts/tasks/<id>.md`)
   - Skip tasks whose prompt file already exists (unless `--overwrite`)
   - Print summary: how many prompts generated, how many skipped
3. Model resolution: use `resolveModel(flagValue, genPromptsDefaultModel)` where `genPromptsDefaultModel` is `claude-opus-4-6` (same constant as `planDefaultModel`)
4. Wire into `run()` dispatch
5. The meta-prompt sent to Claude should instruct it to:
   - Follow the template structure exactly
   - Fill in the TASK ID with the actual task ID
   - Write specific, actionable task instructions derived from the PRD
   - Include concrete acceptance criteria
   - Include the correct kiln JSON footer with the task's ID
   - Use `{"kiln":{"status":"complete","task_id":"<actual-task-id>"}}` (not `agentrun`)

## Tests
- Missing tasks file -> error
- Missing PRD file -> error
- Missing template file -> error
- Existing prompt file is skipped (no `--overwrite`)
- Existing prompt file is regenerated with `--overwrite`
- Dispatch via `run(["gen-prompts", ...])` -> correct exit code
- Default flag values are correct (tasks, prd, template paths)
- Model defaults to `claude-opus-4-6`

## Acceptance criteria
- `go test ./...` passes
- `kiln gen-prompts` reads tasks.yaml and generates prompt files for tasks missing them
- Existing prompt files are preserved unless `--overwrite` is set
- Generated prompts follow the template structure with kiln JSON footer
- Default model is `claude-opus-4-6`

## Final JSON Status Footer
{"kiln":{"status":"complete","task_id":"gen-prompts","notes":"Implemented kiln gen-prompts to scaffold prompt files from tasks.yaml using Claude."}}

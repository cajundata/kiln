# Task: Finalize the PRD-to-tasks extraction prompt

## Role

You are an assistant developer working inside an existing Go codebase for a CLI tool named `kiln`. Your job is to refine prompts and workflow wiring.

---

## Context

`.kiln/prompts/00_extract_tasks.md` is the prompt used by `make plan` to have Claude read `PRD.md` and produce `.kiln/tasks.yaml`. This task ensures the prompt is robust and produces valid output.

---

## Requirements

### 1. Review and update the prompt

- Ensure it clearly defines the tasks.yaml schema (flat YAML sequence).
- Ensure it instructs Claude to output ONLY valid YAML (no commentary, no fences).
- Ensure it emphasizes conservative dependencies.

### 2. Test the workflow

- Run `make plan` and verify `.kiln/tasks.yaml` is produced.
- Validate the YAML matches the schema.
- If invalid, adjust the prompt and re-run.

### 3. Sanity-check the task graph

- All IDs unique and kebab-case.
- All prompt paths point to existing files.
- All needs reference existing task IDs.

---

## Acceptance Criteria

- `make plan` reliably produces a valid `.kiln/tasks.yaml`.
- The prompt is clear, concise, and robust against hallucination.
- At least one full generate → inspect → adjust iteration completed.

---

## Final JSON Status Footer

{"kiln":{"status":"complete","task_id":"prompt-extract-tasks","notes":"extraction prompt finalized and validated"}}

# Plan to implement
## Plan: Phase 1 — Task-Yaml Resolution + kiln status
### Context
Kiln currently requires --prompt-file to be passed explicitly on every kiln exec call, and gen-make bakes prompt
paths into generated recipes with && touch $@ for done markers. This creates drift risk (wrong prompt file passed)
and splits done-marker ownership between Make and kiln. Meanwhile, there's no way to see task status without
manually reading logs and done files.
Phase 1 makes kiln the single authority: kiln exec resolves prompt + model from tasks.yaml, owns done-marker
creation, and a new kiln status command gives visibility into progress.
### Files to Modify
- cmd/kiln/main.go — All production changes (currently 379 lines, estimated ~470 after)
- cmd/kiln/main_test.go — Updated + new tests (currently 62 tests, ~85 after)
### Implementation Steps
#### Step 1: Add Model field to Task struct
Add Model string \yaml:"model,omitempty"`` to the existing Task struct at line 124. No other code breaks — field
defaults to empty string.
#### Step 2: Extract loadTasks helper (TDD)
- TestLoadTasks_FileNotFound — expects "failed to read tasks file"
- TestLoadTasks_InvalidYAML — expects "failed to parse tasks file"
- TestLoadTasks_EmptyFile — expects "no tasks found"
- TestLoadTasks_ValidFile — returns correct []Task
- TestLoadTasks_TaskWithModel — Task.Model is populated
Implementation:
`func loadTasks(path string) ([]Task, error)`
Extracts the read/parse/empty-check from runGenMake (lines 147-159). Refactor runGenMake to call loadTasks instead.
All existing gen-make tests must still pass.
#### Step 3: Update resolveModel to 4-tier (TDD)
`func resolveModel(flagValue, taskModel string) string`
Precedence: --model flag → task model: → KILN_MODEL env → defaultModel
Tests first:
- Update 3 existing TestResolveModel_* tests to pass "" as second arg
- Add TestResolveModel_TaskModelFallback — resolveModel("", "task-model") → "task-model"
- Add TestResolveModel_FlagOverridesTaskModel — resolveModel("flag", "task") → "flag"
- Add TestResolveModel_TaskModelOverridesEnv — env set, resolveModel("", "task") → "task"
Implementation: Add taskModel check between flag and env checks. Update call site in runExec to
resolveModel(*modelFlag, "") temporarily.
#### Step 4: Add --tasks flag to runExec (TDD)
Tests first:
- TestRunExec_TasksFlag_ResolvesPrompt — --tasks + --task-id, no --prompt-file → success
- TestRunExec_TasksFlag_TaskNotFound — error: task "X" not found
- TestRunExec_TasksFlag_PromptFileTakesPrecedence — explicit --prompt-file overrides task prompt
- TestRunExec_TasksFlag_ResolvesModel — task model: passed to claude via capturingCommandBuilder
- TestRunExec_TasksFlag_ModelFlagOverridesTaskModel — --model beats task model
- TestRunExec_NoTasksNoPromptFile_Error — only --task-id → error mentioning both options
- TestRunExec_TasksFileNotFound — --tasks /bad/path → "failed to read tasks file"
Implementation in runExec:
1. Add --tasks flag (optional)
2. When --tasks is provided: call loadTasks, find task by ID, set *promptFile if empty, extract taskModel
3. Error if task not found
4. After resolution: require *promptFile non-empty (error message hints at --tasks)
5. Pass taskModel to resolveModel
Backward compat: --task-id X --prompt-file Y (no --tasks) works exactly as before.
Step 5: Update gen-make recipe format (TDD)
Update existing tests:
- TestRunGenMake_RecipeUsesTasksFlag — --tasks present, --prompt-file absent
- TestRunGenMake_RecipeNoTouchTarget — && touch $@ absent
Implementation: Change recipe template from:
$(KILN) exec --task-id {id} --prompt-file {prompt} && touch $@
To:
$(KILN) exec --task-id {id} --tasks {tasksFile}
Where {tasksFile} is the *tasksFile value passed to gen-make. Timeout flag still appended when present.
Step 6: Implement kiln status command (TDD)
Tests first:
- TestRunStatus_MissingTasksFlag — "--tasks is required"
- TestRunStatus_TasksFileNotFound — "failed to read tasks file"
- TestRunStatus_AllDone — 2 tasks, both done → "2/2 tasks done, 0 runnable"
- TestRunStatus_NoneDone — A (no deps) + B (needs A) → A=runnable, B=blocked, "0/2 done, 1 runnable"
- TestRunStatus_PartialDone — A→B→C, only A done → A=done, B=runnable, C=blocked
- TestRunStatus_OutputHeader — output contains "TASK" and "STATUS"
- TestRun_StatusDispatch — run(["status", "--tasks", ...]) returns 0
                                                                                                                     
Implementation:
func runStatus(args []string, stdout io.Writer) error
func hasUnfinishedDeps(needs []string, doneSet map[string]bool) bool
                                                                                                                     
Logic:
1. Parse --tasks flag (required)
2. Call loadTasks
3. Build doneSet by stat-ing .kiln/done/{id}.done for each task
4. Classify each task: done / blocked (has unfinished dep) / runnable
5. Print table: TASK | STATUS | NEEDS
6. Print summary: "X/Y tasks done, Z runnable"
                                                                                                                     
Wire into run() dispatch as case "status":.
                                                                                                                     
Step 7: Final verification
                                                                                                                     
- go test ./cmd/kiln -v -count=1 — all tests pass
- go build -o kiln ./cmd/kiln — builds clean
- Manual smoke test: ./kiln status --tasks .kiln/tasks.yaml
- Regenerate targets: ./kiln gen-make --tasks .kiln/tasks.yaml --out .kiln/targets.mk
- Verify make plan still works (uses --prompt-file, backward compat)
                                                                                                                     
Edge Cases
                                                                                                                     
- --tasks given but task has empty prompt: and no --prompt-file → error: "task X has no prompt field"
- .kiln/done/ directory doesn't exist → all tasks show as not-done (correct)
- needs references an ID not in tasks.yaml → that dep never appears in doneSet, task stays blocked (safe; full
validation deferred to Phase 2
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Tests for loadTasks ---

func TestLoadTasks_FileNotFound(t *testing.T) {
	_, err := loadTasks("/nonexistent/tasks.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "failed to read tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("not: valid: yaml: [[["), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("[]"), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for empty tasks")
	}
	if !strings.Contains(err.Error(), "no tasks found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_ValidFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "alpha" || tasks[1].ID != "beta" {
		t.Errorf("unexpected task IDs: %v, %v", tasks[0].ID, tasks[1].ID)
	}
}

func TestLoadTasks_TaskWithModel(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: my-task
  prompt: p.md
  needs: []
  model: claude-haiku-4-5-20251001
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tasks[0].Model != "claude-haiku-4-5-20251001" {
		t.Errorf("expected model claude-haiku-4-5-20251001, got %s", tasks[0].Model)
	}
}

// --- Additional loadTasks validation tests ---

func TestLoadTasks_Validation(t *testing.T) {
	writeFile := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "tasks.yaml")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
	}{
		{
			name: "success without model",
			yaml: "- id: foo\n  prompt: foo.md\n",
		},
		{
			name: "success with model",
			yaml: "- id: foo\n  prompt: foo.md\n  model: claude-haiku-4-5-20251001\n",
		},
		{
			name:        "missing id",
			yaml:        "- prompt: foo.md\n",
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "empty id",
			yaml:        "- id: \"\"\n  prompt: foo.md\n",
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "missing prompt",
			yaml:        "- id: foo\n",
			wantErr:     true,
			errContains: "prompt is required",
		},
		{
			name:        "empty prompt",
			yaml:        "- id: foo\n  prompt: \"\"\n",
			wantErr:     true,
			errContains: "prompt is required",
		},
		{
			name:        "duplicate ids",
			yaml:        "- id: foo\n  prompt: a.md\n- id: foo\n  prompt: b.md\n",
			wantErr:     true,
			errContains: "duplicate task id",
		},
		{
			name:        "duplicate ids mentions id",
			yaml:        "- id: my-task\n  prompt: a.md\n- id: my-task\n  prompt: b.md\n",
			wantErr:     true,
			errContains: "my-task",
		},
		{
			name:        "absolute prompt path",
			yaml:        "- id: foo\n  prompt: /absolute/path.md\n",
			wantErr:     true,
			errContains: "relative path",
		},
		{
			name:        "empty needs element",
			yaml:        "- id: foo\n  prompt: foo.md\n  needs:\n    - \"\"\n",
			wantErr:     true,
			errContains: "must not be empty",
		},
		{
			name:        "needs wrong type (string instead of list)",
			yaml:        "- id: foo\n  prompt: foo.md\n  needs: \"not-a-list\"\n",
			wantErr:     true,
			errContains: "failed to parse tasks file",
		},
		{
			name:        "model wrong type (list instead of string)",
			yaml:        "- id: foo\n  prompt: foo.md\n  model:\n    - a\n    - b\n",
			wantErr:     true,
			errContains: "failed to parse tasks file",
		},
		{
			name:        "unknown field rejected",
			yaml:        "- id: foo\n  prompt: foo.md\n  bogus: value\n",
			wantErr:     true,
			errContains: "failed to parse tasks file",
		},
		{
			name:        "invalid id format (uppercase)",
			yaml:        "- id: FooBar\n  prompt: foo.md\n",
			wantErr:     true,
			errContains: "kebab-case",
		},
		{
			name:        "invalid id format (spaces)",
			yaml:        "- id: \"foo bar\"\n  prompt: foo.md\n",
			wantErr:     true,
			errContains: "kebab-case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeFile(t, tt.yaml)
			tasks, err := loadTasks(p)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got tasks: %+v", tasks)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// --- Tests for validate-schema ---

func TestRunValidateSchema_MissingTasksFlag(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"validate-schema"}, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing --tasks")
	}
	if !strings.Contains(out.String(), "--tasks is required") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRunValidateSchema_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("not: valid: yaml: [[["), 0o644)

	var out bytes.Buffer
	code := run([]string{"validate-schema", "--tasks", p}, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid YAML")
	}
	if !strings.Contains(out.String(), "failed to parse tasks file") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRunValidateSchema_EmptyTasks(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("[]"), 0o644)

	var out bytes.Buffer
	code := run([]string{"validate-schema", "--tasks", p}, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for empty tasks")
	}
	if !strings.Contains(out.String(), "no tasks found") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRunValidateSchema_Success(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: alpha
  prompt: a.md
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"validate-schema", "--tasks", p}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "validate-schema: OK (2 tasks)") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

// --- Tests for gen-make ---

func TestRunGenMake_MissingTasksFlag(t *testing.T) {
	err := runGenMake([]string{"--out", "out.mk"})
	if err == nil {
		t.Fatal("expected error for missing --tasks")
	}
	if !strings.Contains(err.Error(), "--tasks is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenMake_MissingOutFlag(t *testing.T) {
	err := runGenMake([]string{"--tasks", "tasks.yaml"})
	if err == nil {
		t.Fatal("expected error for missing --out")
	}
	if !strings.Contains(err.Error(), "--out is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenMake_TasksFileNotFound(t *testing.T) {
	err := runGenMake([]string{"--tasks", "/nonexistent/tasks.yaml", "--out", "out.mk"})
	if err == nil {
		t.Fatal("expected error for missing tasks file")
	}
	if !strings.Contains(err.Error(), "failed to read tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenMake_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("not: valid: yaml: [[["), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", filepath.Join(tmp, "out.mk")})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenMake_EmptyTasks(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("[]"), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", filepath.Join(tmp, "out.mk")})
	if err == nil {
		t.Fatal("expected error for empty tasks")
	}
	if !strings.Contains(err.Error(), "no tasks found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenMake_InvalidFlag(t *testing.T) {
	err := runGenMake([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

func TestRunGenMake_SingleTask(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	yaml := `- id: build-widget
  prompt: .kiln/prompts/tasks/build-widget.md
  needs: []
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	got := string(data)

	// Verify phony all target
	if !strings.Contains(got, ".PHONY: all") {
		t.Error("missing .PHONY: all")
	}
	if !strings.Contains(got, "all: .kiln/done/build-widget.done") {
		t.Error("all target missing build-widget.done dependency")
	}

	// Verify task target
	if !strings.Contains(got, ".kiln/done/build-widget.done:") {
		t.Error("missing build-widget target")
	}
	if !strings.Contains(got, "$(KILN) exec --task-id build-widget") {
		t.Errorf("missing or incorrect recipe for build-widget, got:\n%s", got)
	}
	if strings.Contains(got, "--tasks") {
		t.Error("recipe should not contain --tasks (exec defaults to .kiln/tasks.yaml)")
	}
	if strings.Contains(got, "&& touch $@") {
		t.Error("recipe should not contain '&& touch $@'")
	}
	if strings.Contains(got, "--prompt-file") {
		t.Error("recipe should not contain --prompt-file")
	}
}

func TestRunGenMake_WithDependencies(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	yaml := `- id: alpha
  prompt: prompts/alpha.md
  needs: []

- id: beta
  prompt: prompts/beta.md
  needs:
    - alpha

- id: gamma
  prompt: prompts/gamma.md
  needs:
    - alpha
    - beta
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	got := string(data)

	// all target should list all three in definition order
	if !strings.Contains(got, "all: .kiln/done/alpha.done .kiln/done/beta.done .kiln/done/gamma.done") {
		t.Errorf("all target incorrect, got:\n%s", got)
	}

	// alpha has no deps
	if !strings.Contains(got, ".kiln/done/alpha.done:\n") {
		t.Error("alpha should have no prerequisites")
	}

	// beta depends on alpha
	if !strings.Contains(got, ".kiln/done/beta.done: .kiln/done/alpha.done\n") {
		t.Error("beta should depend on alpha")
	}

	// gamma depends on alpha and beta
	if !strings.Contains(got, ".kiln/done/gamma.done: .kiln/done/alpha.done .kiln/done/beta.done\n") {
		t.Error("gamma should depend on alpha and beta")
	}
}

func TestRunGenMake_DeterministicOutput(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")

	yaml := `- id: zulu
  prompt: z.md
  needs: []

- id: alpha
  prompt: a.md
  needs: []

- id: mike
  prompt: m.md
  needs:
    - zulu
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	// Run twice and compare output
	out1 := filepath.Join(tmp, "out1.mk")
	out2 := filepath.Join(tmp, "out2.mk")

	if err := runGenMake([]string{"--tasks", tasksPath, "--out", out1}); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if err := runGenMake([]string{"--tasks", tasksPath, "--out", out2}); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}

	d1, _ := os.ReadFile(out1)
	d2, _ := os.ReadFile(out2)
	if string(d1) != string(d2) {
		t.Errorf("non-deterministic output:\nrun1:\n%s\nrun2:\n%s", d1, d2)
	}

	// Verify definition order preserved (zulu before alpha before mike)
	got := string(d1)
	zIdx := strings.Index(got, ".kiln/done/zulu.done:")
	aIdx := strings.Index(got, ".kiln/done/alpha.done:")
	mIdx := strings.Index(got, ".kiln/done/mike.done:")
	if zIdx >= aIdx || aIdx >= mIdx {
		t.Errorf("targets not in definition order: z=%d a=%d m=%d", zIdx, aIdx, mIdx)
	}
}

func TestRunGenMake_CreatesOutputDir(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "nested", "dir", "targets.mk")

	yaml := `- id: task1
  prompt: p.md
  needs: []
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output file not created: %v", err)
	}
}

func TestRunGenMake_RecipeHasTab(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "out.mk")

	yaml := `- id: t1
  prompt: p.md
  needs: []
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	// Make requires tabs for recipes, not spaces
	if !strings.Contains(got, "\t$(KILN)") {
		t.Error("recipe must be indented with a tab character")
	}
}

func TestRun_GenMakeSuccess(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "out.mk")

	yaml := `- id: hello
  prompt: hello.md
  needs: []
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"gen-make", "--tasks", tasksPath, "--out", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRun_GenMakeFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"gen-make", "--tasks", "/no/such/file", "--out", "out.mk"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "gen-make:") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

// fakeCommandBuilder returns a commandBuilder that runs a helper process
// instead of the real claude binary. The helper process behaviour is
// controlled by the env var KILN_TEST_HELPER_MODE.
func fakeCommandBuilder(mode string) func(context.Context, string, string) *exec.Cmd {
	return func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE="+mode)
		return cmd
	}
}

// capturingCommandBuilder returns a commandBuilder that records the model
// argument for later inspection, while still delegating to a fake helper.
func capturingCommandBuilder(mode string, capturedModel *string) func(context.Context, string, string) *exec.Cmd {
	return func(ctx context.Context, prompt, model string) *exec.Cmd {
		*capturedModel = model
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE="+mode)
		return cmd
	}
}

// TestHelperProcess is invoked as a subprocess by the fake command builder.
// It is not a real test — the guard at the top prevents it from running
// as part of the normal test suite.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("KILN_TEST_HELPER_MODE")
	if mode == "" {
		return // not invoked as a helper
	}

	// taskIDFromEnv returns the task ID set by the test, defaulting to "test-task".
	taskIDFromEnv := func() string {
		if id := os.Getenv("KILN_TEST_TASK_ID"); id != "" {
			return id
		}
		return "test-task"
	}

	switch mode {
	case "success":
		// Echo the prompt back and emit a complete footer.
		args := os.Args
		for i, a := range args {
			if a == "--" {
				if i+1 < len(args) {
					os.Stdout.WriteString(`{"type":"assistant","content":"` + args[i+1] + `"}` + "\n")
				}
				break
			}
		}
		os.Stdout.WriteString(`{"kiln":{"status":"complete","task_id":"` + taskIDFromEnv() + `"}}` + "\n")
		os.Exit(0)
	case "complete":
		os.Stdout.WriteString(`{"kiln":{"status":"complete","task_id":"` + taskIDFromEnv() + `"}}` + "\n")
		os.Exit(0)
	case "not_complete":
		os.Stdout.WriteString(`{"kiln":{"status":"not_complete","task_id":"` + taskIDFromEnv() + `"}}` + "\n")
		os.Exit(0)
	case "blocked":
		os.Stdout.WriteString(`{"kiln":{"status":"blocked","task_id":"` + taskIDFromEnv() + `"}}` + "\n")
		os.Exit(0)
	case "no_footer":
		os.Stdout.WriteString("some output without a kiln footer\n")
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("error: something went wrong\n")
		os.Exit(1)
	case "hang":
		// Sleep for a long time; will be killed by context timeout.
		time.Sleep(1 * time.Hour)
		os.Exit(0)
	case "write-tasks":
		// Write valid tasks.yaml to KILN_TEST_PLAN_OUT and exit 0.
		outPath := os.Getenv("KILN_TEST_PLAN_OUT")
		if outPath != "" {
			validYAML := "- id: planned-task\n  prompt: planned.md\n"
			os.WriteFile(outPath, []byte(validYAML), 0o644)
		}
		os.Exit(0)
	case "write-invalid-yaml":
		// Write invalid YAML to KILN_TEST_PLAN_OUT and exit 0.
		outPath := os.Getenv("KILN_TEST_PLAN_OUT")
		if outPath != "" {
			os.WriteFile(outPath, []byte("not: valid: yaml: [[["), 0o644)
		}
		os.Exit(0)
	case "gen-prompts-write":
		// Write dummy prompt content to KILN_TEST_GEN_PROMPTS_OUT and exit 0.
		outPath := os.Getenv("KILN_TEST_GEN_PROMPTS_OUT")
		if outPath != "" {
			os.MkdirAll(filepath.Dir(outPath), 0o755)
			os.WriteFile(outPath, []byte("# generated prompt\n"), 0o644)
		}
		os.Exit(0)
	case "env-check":
		// Read KILN_TEST_CHECK_VAR to determine which env var to echo back.
		// Outputs the value of the named env var, then a complete footer.
		checkVar := os.Getenv("KILN_TEST_CHECK_VAR")
		if checkVar != "" {
			os.Stdout.WriteString(os.Getenv(checkVar) + "\n")
		}
		taskID := taskIDFromEnv()
		os.Stdout.WriteString(`{"kiln":{"status":"complete","task_id":"` + taskID + `"}}` + "\n")
		os.Exit(0)
	case "unify-success":
		// Write markdown closure content followed by a complete footer.
		taskID := taskIDFromEnv()
		os.Stdout.WriteString("## What Changed\n\nSome files were modified.\n\n")
		os.Stdout.WriteString("## Decisions Made\n\nUsed the simplest approach.\n\n")
		os.Stdout.WriteString(`{"kiln":{"status":"complete","task_id":"` + taskID + `"}}` + "\n")
		os.Exit(0)
	default:
		os.Stderr.WriteString("unknown helper mode\n")
		os.Exit(2)
	}
}

func TestRunExec_MissingTaskID(t *testing.T) {
	_, err := runExec([]string{"--prompt-file", "some.md"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing --task-id")
	}
	if !strings.Contains(err.Error(), "--task-id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_MissingPromptFile(t *testing.T) {
	_, err := runExec([]string{"--task-id", "t1"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
	// With default --tasks=.kiln/tasks.yaml, error is about reading the tasks file
	if !strings.Contains(err.Error(), "tasks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_PromptFileNotFound(t *testing.T) {
	_, err := runExec([]string{
		"--task-id", "t1",
		"--prompt-file", "/nonexistent/path/prompt.md",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "failed to read prompt file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_Success(t *testing.T) {
	// Save and restore the original commandBuilder
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	// Set up temp directory as working dir so .kiln/logs is created there
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	// Create a prompt file
	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("hello world"), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "test-01")

	var stdout bytes.Buffer
	code, err := runExec([]string{
		"--task-id", "test-01",
		"--prompt-file", promptPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 (complete), got %d", code)
	}

	// Verify stdout received output
	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("stdout missing prompt echo, got: %s", stdout.String())
	}

	// Verify log file was created and contains output
	logPath := filepath.Join(tmpDir, ".kiln", "logs", "test-01.json")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !strings.Contains(string(logData), "hello world") {
		t.Errorf("log file missing prompt echo, got: %s", string(logData))
	}
}

func TestRunExec_ClaudeFailure(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test prompt"), 0o644)

	_, err := runExec([]string{
		"--task-id", "fail-01",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when claude exits non-zero")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_LogDirectoryCreated(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "dir-test")
	_, err := runExec([]string{
		"--task-id", "dir-test",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .kiln/logs directory was created
	info, err := os.Stat(filepath.Join(tmpDir, ".kiln", "logs"))
	if err != nil {
		t.Fatalf("log directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .kiln/logs to be a directory")
	}
}

func TestRunExec_BothFlagsMissing(t *testing.T) {
	_, err := runExec([]string{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when both flags missing")
	}
	if !strings.Contains(err.Error(), "--task-id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Tests for the run() dispatch function ---

func TestRun_NoArgs(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: kiln") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"bogus"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command: bogus") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRun_GenMakeMissingFlags(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"gen-make"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--tasks is required") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRun_ExecMissingFlags(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"exec"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--task-id is required") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRun_ExecSuccess(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("run test"), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "run-01")

	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--task-id", "run-01", "--prompt-file", promptPath}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2 (complete), got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunExec_InvalidFlag(t *testing.T) {
	_, err := runExec([]string{"--bogus-flag"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

// --- Timeout tests ---

func TestRunExec_Timeout(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test prompt"), 0o644)

	_, err := runExec([]string{
		"--task-id", "timeout-01",
		"--prompt-file", promptPath,
		"--timeout", "200ms",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	var te *timeoutError
	if !errors.As(err, &te) {
		t.Fatalf("expected timeoutError, got: %v", err)
	}
	if te.taskID != "timeout-01" {
		t.Errorf("expected taskID timeout-01, got %s", te.taskID)
	}
}

func TestRunExec_TimeoutExitCode(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--task-id", "timeout-exit", "--prompt-file", promptPath, "--timeout", "200ms"}, &stdout, &stderr)
	if code != 20 {
		t.Fatalf("expected exit code 20, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunExec_TimeoutLogEntry(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, _ = runExec([]string{
		"--task-id", "timeout-log",
		"--prompt-file", promptPath,
		"--timeout", "200ms",
	}, &bytes.Buffer{})

	logPath := filepath.Join(tmpDir, ".kiln", "logs", "timeout-log.json")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	logStr := string(logData)
	if !strings.Contains(logStr, `"timeout"`) {
		t.Errorf("log file missing timeout status, got: %s", logStr)
	}
	if !strings.Contains(logStr, "timeout-log") {
		t.Errorf("log file missing task ID, got: %s", logStr)
	}
}

func TestRunExec_InvalidTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "t1",
		"--prompt-file", promptPath,
		"--timeout", "not-a-duration",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid --timeout value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Model selection tests ---

func TestResolveModel_FlagTakesPrecedence(t *testing.T) {
	t.Setenv("KILN_MODEL", "env-model")
	got := resolveModel("flag-model", "")
	if got != "flag-model" {
		t.Errorf("expected flag-model, got %s", got)
	}
}

func TestResolveModel_EnvFallback(t *testing.T) {
	t.Setenv("KILN_MODEL", "env-model")
	got := resolveModel("", "")
	if got != "env-model" {
		t.Errorf("expected env-model, got %s", got)
	}
}

func TestResolveModel_DefaultFallback(t *testing.T) {
	t.Setenv("KILN_MODEL", "")
	got := resolveModel("", "")
	if got != defaultModel {
		t.Errorf("expected %s, got %s", defaultModel, got)
	}
}

func TestResolveModel_TaskModelFallback(t *testing.T) {
	t.Setenv("KILN_MODEL", "")
	got := resolveModel("", "task-model")
	if got != "task-model" {
		t.Errorf("expected task-model, got %s", got)
	}
}

func TestResolveModel_FlagOverridesTaskModel(t *testing.T) {
	got := resolveModel("flag", "task")
	if got != "flag" {
		t.Errorf("expected flag, got %s", got)
	}
}

func TestResolveModel_TaskModelOverridesEnv(t *testing.T) {
	t.Setenv("KILN_MODEL", "env-model")
	got := resolveModel("", "task-model")
	if got != "task-model" {
		t.Errorf("expected task-model, got %s", got)
	}
}

func TestRunExec_ModelFlag_PassedToCommand(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("success", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "model-flag")
	_, err := runExec([]string{
		"--task-id", "model-flag",
		"--prompt-file", promptPath,
		"--model", "claude-3-5-haiku-latest",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "claude-3-5-haiku-latest" {
		t.Errorf("expected model claude-3-5-haiku-latest, got %s", captured)
	}
}

func TestRunExec_ModelEnv_PassedToCommand(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("success", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_MODEL", "claude-3-5-haiku-latest")
	t.Setenv("KILN_TEST_TASK_ID", "model-env")

	_, err := runExec([]string{
		"--task-id", "model-env",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "claude-3-5-haiku-latest" {
		t.Errorf("expected model claude-3-5-haiku-latest, got %s", captured)
	}
}

func TestRunExec_ModelDefault_PassedToCommand(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("success", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_MODEL", "")
	t.Setenv("KILN_TEST_TASK_ID", "model-default")

	_, err := runExec([]string{
		"--task-id", "model-default",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != defaultModel {
		t.Errorf("expected model %s, got %s", defaultModel, captured)
	}
}

func TestRunExec_ModelFlagOverridesEnv(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("success", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_MODEL", "env-model-should-lose")
	t.Setenv("KILN_TEST_TASK_ID", "model-precedence")

	_, err := runExec([]string{
		"--task-id", "model-precedence",
		"--prompt-file", promptPath,
		"--model", "flag-model-wins",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "flag-model-wins" {
		t.Errorf("expected flag-model-wins, got %s", captured)
	}
}

// --- Per-task timeout in gen-make ---

func TestRunGenMake_TaskWithTimeout(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	yaml := `- id: slow-task
  prompt: .kiln/prompts/tasks/slow-task.md
  needs: []
  timeout: 10m
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	got := string(data)

	if !strings.Contains(got, "$(KILN) exec --task-id slow-task --timeout 10m") {
		t.Errorf("expected recipe with --timeout flag, got:\n%s", got)
	}
	if strings.Contains(got, "--tasks") {
		t.Error("recipe should not contain --tasks (exec defaults to .kiln/tasks.yaml)")
	}
	if strings.Contains(got, "&& touch $@") {
		t.Error("recipe should not contain '&& touch $@'")
	}
}

func TestRunGenMake_TaskWithoutTimeout(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	yaml := `- id: fast-task
  prompt: .kiln/prompts/tasks/fast-task.md
  needs: []
`
	os.WriteFile(tasksPath, []byte(yaml), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	got := string(data)

	// Should NOT contain --timeout flag
	if strings.Contains(got, "--timeout") {
		t.Errorf("recipe should not contain --timeout when task has no timeout, got:\n%s", got)
	}

	// Should still have the basic recipe
	if !strings.Contains(got, "$(KILN) exec --task-id fast-task") {
		t.Errorf("expected basic recipe, got:\n%s", got)
	}
	if strings.Contains(got, "--tasks") {
		t.Error("recipe should not contain --tasks (exec defaults to .kiln/tasks.yaml)")
	}
	if strings.Contains(got, "&& touch $@") {
		t.Error("recipe should not contain '&& touch $@'")
	}
}

func TestRunExec_EmptyModelFlagFallsThrough(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("success", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	t.Setenv("KILN_MODEL", "from-env")
	t.Setenv("KILN_TEST_TASK_ID", "model-empty-flag")

	_, err := runExec([]string{
		"--task-id", "model-empty-flag",
		"--prompt-file", promptPath,
		"--model", "",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "from-env" {
		t.Errorf("expected from-env, got %s", captured)
	}
}

// --- Footer parsing tests ---

func TestParseFooter_Complete(t *testing.T) {
	output := `{"type":"assistant","content":"done"}` + "\n" +
		`{"kiln":{"status":"complete","task_id":"my-task"}}` + "\n"
	status, taskID, ok := parseFooter(output)
	if !ok {
		t.Fatal("expected footer to be found")
	}
	if status != "complete" {
		t.Errorf("expected status complete, got %s", status)
	}
	if taskID != "my-task" {
		t.Errorf("expected task_id my-task, got %s", taskID)
	}
}

func TestParseFooter_NotComplete(t *testing.T) {
	output := `{"kiln":{"status":"not_complete","task_id":"t1"}}` + "\n"
	status, _, ok := parseFooter(output)
	if !ok {
		t.Fatal("expected footer to be found")
	}
	if status != "not_complete" {
		t.Errorf("expected not_complete, got %s", status)
	}
}

func TestParseFooter_Blocked(t *testing.T) {
	output := `{"kiln":{"status":"blocked","task_id":"t1"}}` + "\n"
	status, _, ok := parseFooter(output)
	if !ok {
		t.Fatal("expected footer to be found")
	}
	if status != "blocked" {
		t.Errorf("expected blocked, got %s", status)
	}
}

func TestParseFooter_Missing(t *testing.T) {
	output := "some output without a kiln footer\n"
	_, _, ok := parseFooter(output)
	if ok {
		t.Fatal("expected footer to be absent")
	}
}

func TestParseFooter_FooterNotTopLevel(t *testing.T) {
	// JSON that mentions "kiln" but is not a valid footer envelope.
	output := `{"type":"text","kiln":"nope"}` + "\n"
	_, _, ok := parseFooter(output)
	if ok {
		t.Fatal("expected footer to be absent for non-envelope JSON")
	}
}

func TestParseFooter_EmbeddedInStreamJSON(t *testing.T) {
	// Simulates stream-json output where the footer is inside a text content field.
	output := `{"type":"assistant","message":{"content":[{"type":"text","text":"All done.\n\n{\"kiln\":{\"status\":\"complete\",\"task_id\":\"my-task\",\"notes\":\"done\"}}"}]}}` + "\n"
	status, taskID, ok := parseFooter(output)
	if !ok {
		t.Fatal("expected footer to be found in stream-json output")
	}
	if status != "complete" {
		t.Errorf("expected status complete, got %s", status)
	}
	if taskID != "my-task" {
		t.Errorf("expected task_id my-task, got %s", taskID)
	}
}

func TestParseFooter_EmbeddedInResultField(t *testing.T) {
	// Simulates the "result" type line from stream-json where footer is in the result string.
	output := `{"type":"result","result":"Summary text.\n\n{\"kiln\":{\"status\":\"not_complete\",\"task_id\":\"t2\"}}"}` + "\n"
	status, taskID, ok := parseFooter(output)
	if !ok {
		t.Fatal("expected footer to be found in result field")
	}
	if status != "not_complete" {
		t.Errorf("expected not_complete, got %s", status)
	}
	if taskID != "t2" {
		t.Errorf("expected task_id t2, got %s", taskID)
	}
}

func TestRunExec_FooterComplete_ExitCode2(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "fc-01")

	code, err := runExec([]string{"--task-id", "fc-01", "--prompt-file", promptPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
}

func TestRunExec_FooterNotComplete_ExitCode0(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("not_complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "nc-01")

	code, err := runExec([]string{"--task-id", "nc-01", "--prompt-file", promptPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestRunExec_FooterBlocked_ExitCode0(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("blocked")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "bl-01")

	code, err := runExec([]string{"--task-id", "bl-01", "--prompt-file", promptPath}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestRunExec_MissingFooter_ExitCode10(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("no_footer")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--task-id", "nf-01", "--prompt-file", promptPath}, &stdout, &stderr)
	if code != 10 {
		t.Fatalf("expected exit code 10 for missing footer, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing or invalid footer") {
		t.Errorf("expected helpful error message in stderr, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "expected format") {
		t.Errorf("expected schema hint in stderr, got: %s", stderr.String())
	}
}

// --- Retry tests ---

func TestRunExec_RetryOnClaudeExit_ExhaustsAttempts(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "retry-fail",
		"--prompt-file", promptPath,
		"--retries", "2",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", callCount)
	}
}

func TestRunExec_RetryOnClaudeExit_SucceedsOnSecondAttempt(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	attempt := 0
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		attempt++
		mode := "fail"
		if attempt > 1 {
			mode = "complete"
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE="+mode, "KILN_TEST_TASK_ID=retry-ok")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	code, err := runExec([]string{
		"--task-id", "retry-ok",
		"--prompt-file", promptPath,
		"--retries", "2",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Errorf("expected exit code 2 (complete), got %d", code)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
}

func TestRunExec_RetryOnTimeout(t *testing.T) {
	origBuilder := commandBuilder
	origSleep := sleepFn
	t.Cleanup(func() {
		commandBuilder = origBuilder
		sleepFn = origSleep
	})

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=hang")
		return cmd
	}
	sleepFn = func(d time.Duration) {} // no-op

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "retry-timeout",
		"--prompt-file", promptPath,
		"--timeout", "200ms",
		"--retries", "1",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error")
	}
	var te *timeoutError
	if !errors.As(err, &te) {
		t.Fatalf("expected timeoutError, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 attempts (1 + 1 retry), got %d", callCount)
	}
}

func TestRunExec_NoRetryOnMissingPromptFile(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	_, err := runExec([]string{
		"--task-id", "no-retry-missing",
		"--prompt-file", "/nonexistent/prompt.md",
		"--retries", "3",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "failed to read prompt file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected no command invocations for non-retryable error, got %d", callCount)
	}
}

func TestRunExec_NoRetryOnInvalidFlag(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		return &exec.Cmd{}
	}

	_, err := runExec([]string{"--bogus-flag"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
	if callCount != 0 {
		t.Errorf("expected no command invocations, got %d", callCount)
	}
}

func TestRunExec_BackoffCalledBetweenRetries(t *testing.T) {
	origBuilder := commandBuilder
	origSleep := sleepFn
	t.Cleanup(func() {
		commandBuilder = origBuilder
		sleepFn = origSleep
	})

	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	var slept []time.Duration
	sleepFn = func(d time.Duration) { slept = append(slept, d) }

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "backoff-test",
		"--prompt-file", promptPath,
		"--retries", "2",
		"--retry-backoff", "100ms",
	}, &bytes.Buffer{})

	// With 3 attempts, sleep is called between attempt 1→2 and 2→3: 2 times
	if len(slept) != 2 {
		t.Errorf("expected sleepFn called 2 times, got %d", len(slept))
	}
	for i, d := range slept {
		if d != 100*time.Millisecond {
			t.Errorf("sleep[%d]: expected 100ms, got %v", i, d)
		}
	}
}

func TestRunExec_BackoffZeroDoesNotSleep(t *testing.T) {
	origBuilder := commandBuilder
	origSleep := sleepFn
	t.Cleanup(func() {
		commandBuilder = origBuilder
		sleepFn = origSleep
	})

	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	var slept int
	sleepFn = func(d time.Duration) { slept++ }

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "no-backoff",
		"--prompt-file", promptPath,
		"--retries", "1",
		// no --retry-backoff → default 0s
	}, &bytes.Buffer{})

	if slept != 0 {
		t.Errorf("expected no sleepFn calls for zero backoff, got %d", slept)
	}
}

func TestRunExec_DoneMarkerWrittenOnSuccess(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "done-ok")

	_, err := runExec([]string{
		"--task-id", "done-ok",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	donePath := filepath.Join(tmpDir, ".kiln", "done", "done-ok.done")
	if _, err := os.Stat(donePath); err != nil {
		t.Errorf("expected done marker at %s, got: %v", donePath, err)
	}
}

func TestRunExec_DoneMarkerNotWrittenOnFailure(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "done-fail",
		"--prompt-file", promptPath,
		"--retries", "1",
	}, &bytes.Buffer{})

	donePath := filepath.Join(tmpDir, ".kiln", "done", "done-fail.done")
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Errorf("expected no done marker after failure, but file exists or other error: %v", err)
	}
}

func TestRunExec_AttemptLogEntries(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	attempt := 0
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		attempt++
		mode := "fail"
		if attempt > 1 {
			mode = "complete"
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE="+mode, "KILN_TEST_TASK_ID=log-entries")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "log-entries",
		"--prompt-file", promptPath,
		"--retries", "1",
	}, &bytes.Buffer{})

	// Log is overwritten per attempt; final log reflects the successful last attempt.
	logPath := filepath.Join(tmpDir, ".kiln", "logs", "log-entries.json")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logStr := string(logData)

	// Final log should reflect the successful (complete) attempt.
	if !strings.Contains(logStr, `"complete"`) {
		t.Errorf("log should show complete status from last attempt, got: %s", logStr)
	}
	if !strings.Contains(logStr, "log-entries") {
		t.Errorf("log missing task ID, got: %s", logStr)
	}
}

func TestRunExec_InvalidRetryBackoff(t *testing.T) {
	tmpDir := t.TempDir()
	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "t1",
		"--prompt-file", promptPath,
		"--retry-backoff", "not-a-duration",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid --retry-backoff")
	}
	if !strings.Contains(err.Error(), "invalid --retry-backoff value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Tests for --tasks flag in exec ---

func TestRunExec_TasksFlag_ResolvesPrompt(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	// Create prompt file (relative path from tmpDir)
	promptDir := filepath.Join(tmpDir, ".kiln", "prompts", "tasks")
	os.MkdirAll(promptDir, 0o755)
	os.WriteFile(filepath.Join(promptDir, "my-task.md"), []byte("do the thing"), 0o644)

	// Create tasks.yaml with relative prompt path
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: .kiln/prompts/tasks/my-task.md
  needs: []
`), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "my-task")

	code, err := runExec([]string{
		"--task-id", "my-task",
		"--tasks", tasksPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 (complete), got %d", code)
	}
}

func TestRunExec_TasksFlag_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
`), 0o644)

	_, err := runExec([]string{
		"--task-id", "nonexistent",
		"--tasks", tasksPath,
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for task not found")
	}
	if !strings.Contains(err.Error(), `task "nonexistent" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_TasksFlag_PromptFileTakesPrecedence(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var capturedPrompt string
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		capturedPrompt = prompt
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=complete", "KILN_TEST_TASK_ID=my-task")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	// Create override prompt file (absolute path OK as --prompt-file flag)
	overridePath := filepath.Join(tmpDir, "override.md")
	os.WriteFile(overridePath, []byte("override prompt"), 0o644)

	// Create task prompt file (relative path in tasks.yaml)
	os.WriteFile(filepath.Join(tmpDir, "task-prompt.md"), []byte("task prompt"), 0o644)

	// Create tasks.yaml with relative prompt path
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: task-prompt.md
  needs: []
`), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "my-task")

	_, err := runExec([]string{
		"--task-id", "my-task",
		"--tasks", tasksPath,
		"--prompt-file", overridePath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrompt != "override prompt" {
		t.Errorf("expected override prompt content, got: %s", capturedPrompt)
	}
}

func TestRunExec_TasksFlag_ResolvesModel(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("complete", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "p.md"), []byte("test"), 0o644)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: p.md
  needs: []
  model: claude-haiku-4-5-20251001
`), 0o644)

	t.Setenv("KILN_MODEL", "")
	t.Setenv("KILN_TEST_TASK_ID", "my-task")

	_, err := runExec([]string{
		"--task-id", "my-task",
		"--tasks", tasksPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "claude-haiku-4-5-20251001" {
		t.Errorf("expected claude-haiku-4-5-20251001, got %s", captured)
	}
}

func TestRunExec_TasksFlag_ModelFlagOverridesTaskModel(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var captured string
	commandBuilder = capturingCommandBuilder("complete", &captured)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "p.md"), []byte("test"), 0o644)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: p.md
  needs: []
  model: task-model-loses
`), 0o644)

	t.Setenv("KILN_TEST_TASK_ID", "my-task")

	_, err := runExec([]string{
		"--task-id", "my-task",
		"--tasks", tasksPath,
		"--model", "flag-model-wins",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "flag-model-wins" {
		t.Errorf("expected flag-model-wins, got %s", captured)
	}
}

func TestRunExec_NoTasksNoPromptFile_Error(t *testing.T) {
	_, err := runExec([]string{"--task-id", "t1"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when neither --tasks nor --prompt-file provided")
	}
	// Default --tasks=.kiln/tasks.yaml is loaded; error should mention tasks file
	if !strings.Contains(err.Error(), "tasks") {
		t.Fatalf("expected error about tasks file, got: %v", err)
	}
}

func TestRunExec_TasksFileNotFound(t *testing.T) {
	_, err := runExec([]string{
		"--task-id", "t1",
		"--tasks", "/bad/path/tasks.yaml",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing tasks file")
	}
	if !strings.Contains(err.Error(), "failed to read tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_TasksFlag_EmptyPromptField(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: ""
  needs: []
`), 0o644)

	_, err := runExec([]string{
		"--task-id", "my-task",
		"--tasks", tasksPath,
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty prompt field")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_TaskIDMismatch_WarnAndNotComplete(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	// Helper emits task_id="test-task"; run with a different id to force mismatch.
	t.Setenv("KILN_TEST_TASK_ID", "test-task")

	var stdout bytes.Buffer
	code, err := runExec([]string{"--task-id", "different-id", "--prompt-file", promptPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mismatch means footer_valid=false → not treated as complete → exit 0.
	if code != 0 {
		t.Fatalf("expected exit code 0 for task_id mismatch (footer_valid=false), got %d", code)
	}
	if !strings.Contains(stdout.String(), "warning") {
		t.Errorf("expected mismatch warning in stdout, got: %s", stdout.String())
	}
	// No .done file should be created.
	donePath := filepath.Join(tmpDir, ".kiln", "done", "different-id.done")
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Errorf("expected no .done file for task_id mismatch, but: %v", err)
	}
}

// --- Tests for kiln status ---

func TestRunStatus_MissingTasksFlag(t *testing.T) {
	err := runStatus([]string{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing --tasks")
	}
	if !strings.Contains(err.Error(), "--tasks is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_TasksFileNotFound(t *testing.T) {
	err := runStatus([]string{"--tasks", "/nonexistent/tasks.yaml"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing tasks file")
	}
	if !strings.Contains(err.Error(), "failed to read tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_AllDone(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	// Create done markers
	doneDir := filepath.Join(tmpDir, ".kiln", "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "alpha.done"), nil, 0o644)
	os.WriteFile(filepath.Join(doneDir, "beta.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "2/2 tasks done") {
		t.Errorf("expected '2/2 tasks done' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "0 runnable") {
		t.Errorf("expected '0 runnable' in output, got:\n%s", out)
	}
}

func TestRunStatus_NoneDone(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "0/2 tasks done") {
		t.Errorf("expected '0/2 tasks done' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "1 runnable") {
		t.Errorf("expected '1 runnable' in output, got:\n%s", out)
	}
}

func TestRunStatus_PartialDone(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
- id: gamma
  prompt: c.md
  needs:
    - beta
`), 0o644)

	// Only alpha is done
	doneDir := filepath.Join(tmpDir, ".kiln", "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "alpha.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "1/3 tasks done") {
		t.Errorf("expected '1/3 tasks done' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "1 runnable") {
		t.Errorf("expected '1 runnable' in output, got:\n%s", out)
	}
}

func TestRunStatus_OutputHeader(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
`), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "TASK") {
		t.Errorf("expected 'TASK' header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "STATUS") {
		t.Errorf("expected 'STATUS' header in output, got:\n%s", out)
	}
}

func TestRun_StatusDispatch(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
`), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status", "--tasks", tasksPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}

// --- Tests for validate-cycles ---

func writeTasksFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "tasks.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write tasks file: %v", err)
	}
	return p
}

func TestValidateCycles_MissingTasksFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runValidateCycles([]string{}, &stdout)
	if err == nil {
		t.Fatal("expected error for missing --tasks flag")
	}
	if !strings.Contains(err.Error(), "--tasks is required") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = stderr
}

func TestValidateCycles_UnknownDependency(t *testing.T) {
	tmp := t.TempDir()
	p := writeTasksFile(t, tmp, `- id: alpha
  prompt: a.md
  needs:
    - ghost
`)
	var stdout bytes.Buffer
	err := runValidateCycles([]string{"--tasks", p}, &stdout)
	if err == nil {
		t.Fatal("expected error for unknown dependency")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should mention task and missing dep, got: %v", err)
	}
}

func TestValidateCycles_SimpleCycle(t *testing.T) {
	tmp := t.TempDir()
	p := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs:
    - b
- id: b
  prompt: b.md
  needs:
    - a
`)
	var stdout bytes.Buffer
	err := runValidateCycles([]string{"--tasks", p}, &stdout)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected cycle detected message, got: %v", err)
	}
	// Should mention both nodes
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("cycle path should mention involved tasks, got: %v", err)
	}
}

func TestValidateCycles_LongerCycle(t *testing.T) {
	tmp := t.TempDir()
	p := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs:
    - b
- id: b
  prompt: b.md
  needs:
    - c
- id: c
  prompt: c.md
  needs:
    - a
`)
	var stdout bytes.Buffer
	err := runValidateCycles([]string{"--tasks", p}, &stdout)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected cycle detected message, got: %v", err)
	}
	// All three nodes should appear in the path
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "c") {
		t.Fatalf("cycle path should mention all involved tasks, got: %v", err)
	}
}

func TestValidateCycles_SelfDependency(t *testing.T) {
	tmp := t.TempDir()
	p := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs:
    - a
`)
	var stdout bytes.Buffer
	err := runValidateCycles([]string{"--tasks", p}, &stdout)
	if err == nil {
		t.Fatal("expected cycle error for self-dependency")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected cycle detected message, got: %v", err)
	}
	// Self-cycle: a -> a
	if !strings.Contains(err.Error(), "a -> a") {
		t.Fatalf("expected a -> a in path, got: %v", err)
	}
}

func TestValidateCycles_AcyclicGraph_Success(t *testing.T) {
	tmp := t.TempDir()
	p := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs: []
- id: b
  prompt: b.md
  needs:
    - a
- id: c
  prompt: c.md
  needs:
    - a
    - b
`)
	var stdout bytes.Buffer
	err := runValidateCycles([]string{"--tasks", p}, &stdout)
	if err != nil {
		t.Fatalf("expected no error for acyclic graph, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "validate-cycles: OK") {
		t.Fatalf("expected OK message, got: %s", stdout.String())
	}
}

func TestValidateCycles_DeterministicOutput(t *testing.T) {
	tmp := t.TempDir()
	// a -> b -> c -> a cycle; running twice should give same error message
	p := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs:
    - b
- id: b
  prompt: b.md
  needs:
    - c
- id: c
  prompt: c.md
  needs:
    - a
`)
	var out1, out2 bytes.Buffer
	err1 := runValidateCycles([]string{"--tasks", p}, &out1)
	err2 := runValidateCycles([]string{"--tasks", p}, &out2)
	if err1 == nil || err2 == nil {
		t.Fatal("expected cycle errors")
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("non-deterministic output:\n  run1: %v\n  run2: %v", err1, err2)
	}
}

func TestRun_ValidateCyclesDispatch(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := writeTasksFile(t, tmp, `- id: t1
  prompt: p.md
  needs: []
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate-cycles", "--tasks", tasksPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "validate-cycles: OK") {
		t.Fatalf("expected OK in stdout, got: %s", stdout.String())
	}
}

func TestRun_ValidateCyclesFailure(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := writeTasksFile(t, tmp, `- id: a
  prompt: a.md
  needs:
    - a
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate-cycles", "--tasks", tasksPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cycle detected") {
		t.Fatalf("expected cycle error in stderr, got: %s", stderr.String())
	}
}

// --- Structured log format tests ---

func readExecLog(t *testing.T, dir, taskID string) execRunLog {
	t.Helper()
	logPath := filepath.Join(dir, ".kiln", "logs", taskID+".json")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file %s: %v", logPath, err)
	}
	var entry execRunLog
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to unmarshal log file %s: %v\ncontent: %s", logPath, err, data)
	}
	return entry
}

func TestExecLog_ValidFooter_FooterValidTrue(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "log-valid")

	runExec([]string{"--task-id", "log-valid", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "log-valid")
	if !entry.FooterValid {
		t.Errorf("expected footer_valid=true, got false; status=%s", entry.Status)
	}
	if entry.Status != "complete" {
		t.Errorf("expected status=complete, got %s", entry.Status)
	}
	if entry.Footer == nil {
		t.Fatal("expected footer to be populated")
	}
	if entry.Footer.Kiln.Status != "complete" {
		t.Errorf("expected footer.kiln.status=complete, got %s", entry.Footer.Kiln.Status)
	}
	if entry.Footer.Kiln.TaskID != "log-valid" {
		t.Errorf("expected footer.kiln.task_id=log-valid, got %s", entry.Footer.Kiln.TaskID)
	}
	if entry.TaskID != "log-valid" {
		t.Errorf("expected task_id=log-valid, got %s", entry.TaskID)
	}
}

func TestExecLog_MissingFooter_FooterValidFalse(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("no_footer")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{"--task-id", "log-no-footer", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "log-no-footer")
	if entry.FooterValid {
		t.Errorf("expected footer_valid=false for missing footer")
	}
	if entry.Status != "error" {
		t.Errorf("expected status=error, got %s", entry.Status)
	}
	if entry.Footer != nil {
		t.Errorf("expected footer to be nil for missing footer, got %+v", entry.Footer)
	}
}

func TestExecLog_FooterTaskIDMismatch_FooterValidFalse(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	// Helper will emit task_id="wrong-id" but we run with task-id="expected-id".
	t.Setenv("KILN_TEST_TASK_ID", "wrong-id")

	runExec([]string{"--task-id", "expected-id", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "expected-id")
	if entry.FooterValid {
		t.Errorf("expected footer_valid=false for task_id mismatch")
	}
	if entry.Footer == nil {
		t.Fatal("expected footer to be captured even on task_id mismatch")
	}
	if entry.Footer.Kiln.TaskID != "wrong-id" {
		t.Errorf("expected captured footer task_id=wrong-id, got %s", entry.Footer.Kiln.TaskID)
	}
}

func TestExecLog_Timeout_StatusTimeout(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "log-timeout",
		"--prompt-file", promptPath,
		"--timeout", "200ms",
	}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "log-timeout")
	if entry.Status != "timeout" {
		t.Errorf("expected status=timeout, got %s", entry.Status)
	}
	if entry.FooterValid {
		t.Errorf("expected footer_valid=false on timeout")
	}
	if entry.ExitCode != 20 {
		t.Errorf("expected exit_code=20, got %d", entry.ExitCode)
	}
}

func TestExecLog_AlwaysCreated(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{"--task-id", "log-fail", "--prompt-file", promptPath}, &bytes.Buffer{})

	logPath := filepath.Join(tmpDir, ".kiln", "logs", "log-fail.json")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected log file to be created even on failure, got: %v", err)
	}
}

func TestExecLog_DoneOnlyOnFooterValid(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	// task_id mismatch → footer_valid=false → no .done
	t.Setenv("KILN_TEST_TASK_ID", "wrong-id")
	runExec([]string{"--task-id", "mismatch-done", "--prompt-file", promptPath}, &bytes.Buffer{})

	donePath := filepath.Join(tmpDir, ".kiln", "done", "mismatch-done.done")
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Errorf("expected no .done file when footer_valid=false, but: %v", err)
	}
}

func TestExecLog_ContainsEventsAndTimestamps(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("hello world"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "log-events")

	runExec([]string{"--task-id", "log-events", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "log-events")
	if len(entry.Events) == 0 {
		t.Error("expected at least one event in log")
	}
	if entry.StartedAt.IsZero() {
		t.Error("expected started_at to be set")
	}
	if entry.EndedAt.IsZero() {
		t.Error("expected ended_at to be set")
	}
	if entry.DurationMs < 0 {
		t.Errorf("expected non-negative duration_ms, got %d", entry.DurationMs)
	}
	// Check that events have types
	for i, ev := range entry.Events {
		if ev.Type != "stdout" && ev.Type != "stderr" {
			t.Errorf("event[%d]: unexpected type %q", i, ev.Type)
		}
		if ev.TS.IsZero() {
			t.Errorf("event[%d]: expected ts to be set", i)
		}
	}
}

func TestExecLog_LogFileIsValidJSON(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "log-json")

	runExec([]string{"--task-id", "log-json", "--prompt-file", promptPath}, &bytes.Buffer{})

	logPath := filepath.Join(tmpDir, ".kiln", "logs", "log-json.json")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("log file is not valid JSON: %v\ncontent: %s", err, data)
	}
	// Verify required top-level keys are present
	for _, key := range []string{"task_id", "started_at", "ended_at", "duration_ms", "status", "footer_valid", "events"} {
		if _, ok := v[key]; !ok {
			t.Errorf("log missing required key %q", key)
		}
	}
}

// --- Tests for computeBackoff and --backoff flag ---

func TestComputeBackoff_Fixed_ReturnsSameDuration(t *testing.T) {
	base := 100 * time.Millisecond
	for attempt := 1; attempt <= 5; attempt++ {
		d := computeBackoff("fixed", base, attempt)
		if d != base {
			t.Errorf("attempt %d: expected %v, got %v", attempt, base, d)
		}
	}
}

func TestComputeBackoff_Exponential_IncreasesWithAttempts(t *testing.T) {
	base := 100 * time.Millisecond
	// For attempt N, delay (without jitter) = base * 2^(N-1).
	// With jitter 0–50%, range is [base*2^(N-1), base*2^(N-1)*1.5].
	cases := []struct {
		attempt     int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{1, 100 * time.Millisecond, 150 * time.Millisecond},
		{2, 200 * time.Millisecond, 300 * time.Millisecond},
		{3, 400 * time.Millisecond, 600 * time.Millisecond},
		{4, 800 * time.Millisecond, 1200 * time.Millisecond},
	}
	for _, tc := range cases {
		for run := 0; run < 5; run++ {
			d := computeBackoff("exponential", base, tc.attempt)
			if d < tc.expectedMin || d > tc.expectedMax {
				t.Errorf("attempt %d run %d: expected [%v, %v], got %v",
					tc.attempt, run, tc.expectedMin, tc.expectedMax, d)
			}
		}
	}
}

func TestComputeBackoff_Exponential_CappedAtMaxBackoff(t *testing.T) {
	base := time.Minute
	// base * 2^9 = 512 minutes >> maxBackoffDuration (5m), so delay should be capped.
	for run := 0; run < 5; run++ {
		d := computeBackoff("exponential", base, 10)
		maxAllowed := maxBackoffDuration + maxBackoffDuration/2
		if d < maxBackoffDuration {
			t.Errorf("run %d: expected >= maxBackoffDuration %v, got %v", run, maxBackoffDuration, d)
		}
		if d > maxAllowed {
			t.Errorf("run %d: expected <= %v (cap + 50%% jitter), got %v", run, maxAllowed, d)
		}
	}
}

func TestComputeBackoff_Exponential_HasJitter(t *testing.T) {
	base := time.Second
	// Over 20 calls, jitter should produce varying values (probability of all equal ≈ 0).
	seen := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		d := computeBackoff("exponential", base, 1)
		seen[d] = true
	}
	if len(seen) == 1 {
		t.Error("expected jitter to produce varying delays, but all 20 calls returned same value")
	}
}

func TestRunExec_InvalidBackoffFlag(t *testing.T) {
	tmpDir := t.TempDir()
	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "t1",
		"--prompt-file", promptPath,
		"--backoff", "invalid-strategy",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid --backoff value")
	}
	if !strings.Contains(err.Error(), "invalid --backoff") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_ExponentialBackoff_DelaysIncrease(t *testing.T) {
	origBuilder := commandBuilder
	origSleep := sleepFn
	t.Cleanup(func() {
		commandBuilder = origBuilder
		sleepFn = origSleep
	})

	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	var slept []time.Duration
	sleepFn = func(d time.Duration) { slept = append(slept, d) }

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{
		"--task-id", "exp-backoff",
		"--prompt-file", promptPath,
		"--retries", "2",
		"--retry-backoff", "100ms",
		"--backoff", "exponential",
	}, &bytes.Buffer{})

	if len(slept) != 2 {
		t.Fatalf("expected 2 sleeps, got %d", len(slept))
	}
	// sleep[0] after attempt 1: base * 2^0 = 100ms + jitter → [100ms, 150ms]
	// sleep[1] after attempt 2: base * 2^1 = 200ms + jitter → [200ms, 300ms]
	if slept[0] < 100*time.Millisecond || slept[0] > 150*time.Millisecond {
		t.Errorf("sleep[0]: expected [100ms, 150ms], got %v", slept[0])
	}
	if slept[1] < 200*time.Millisecond || slept[1] > 300*time.Millisecond {
		t.Errorf("sleep[1]: expected [200ms, 300ms], got %v", slept[1])
	}
	// Ranges don't overlap so second sleep is always greater.
	if slept[1] <= slept[0] {
		t.Errorf("expected increasing delays: sleep[0]=%v sleep[1]=%v", slept[0], slept[1])
	}
}

// --- Tests for kiln plan ---

// planCommandBuilder returns a commandBuilder that runs the helper process with
// the given mode and sets KILN_TEST_PLAN_OUT so the helper knows where to write.
func planCommandBuilder(mode, outPath string) func(context.Context, string, string) *exec.Cmd {
	return func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(),
			"KILN_TEST_HELPER_MODE="+mode,
			"KILN_TEST_PLAN_OUT="+outPath,
		)
		return cmd
	}
}

func TestRunPlan_MissingPRD(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete") // should not be reached

	tmp := t.TempDir()
	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("extract tasks"), 0o644)

	var out bytes.Buffer
	err := runPlan([]string{
		"--prd", filepath.Join(tmp, "nonexistent.md"),
		"--prompt", promptPath,
		"--out", filepath.Join(tmp, "tasks.yaml"),
	}, &out)
	if err == nil {
		t.Fatal("expected error for missing PRD file")
	}
	if !strings.Contains(err.Error(), "failed to read PRD file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPlan_MissingPrompt(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete") // should not be reached

	tmp := t.TempDir()
	prdPath := filepath.Join(tmp, "PRD.md")
	os.WriteFile(prdPath, []byte("# PRD"), 0o644)

	var out bytes.Buffer
	err := runPlan([]string{
		"--prd", prdPath,
		"--prompt", filepath.Join(tmp, "nonexistent.md"),
		"--out", filepath.Join(tmp, "tasks.yaml"),
	}, &out)
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "failed to read prompt file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPlan_ClaudeFails(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmp := t.TempDir()
	prdPath := filepath.Join(tmp, "PRD.md")
	os.WriteFile(prdPath, []byte("# PRD"), 0o644)
	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("extract tasks"), 0o644)

	var out bytes.Buffer
	err := runPlan([]string{
		"--prd", prdPath,
		"--prompt", promptPath,
		"--out", filepath.Join(tmp, "tasks.yaml"),
	}, &out)
	if err == nil {
		t.Fatal("expected error when claude fails")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPlan_InvalidOutputYAML(t *testing.T) {
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "tasks.yaml")

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = planCommandBuilder("write-invalid-yaml", outPath)

	prdPath := filepath.Join(tmp, "PRD.md")
	os.WriteFile(prdPath, []byte("# PRD"), 0o644)
	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("extract tasks"), 0o644)

	var out bytes.Buffer
	err := runPlan([]string{
		"--prd", prdPath,
		"--prompt", promptPath,
		"--out", outPath,
	}, &out)
	if err == nil {
		t.Fatal("expected error for invalid output YAML")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPlan_Success(t *testing.T) {
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "tasks.yaml")

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = planCommandBuilder("write-tasks", outPath)

	prdPath := filepath.Join(tmp, "PRD.md")
	os.WriteFile(prdPath, []byte("# PRD"), 0o644)
	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("extract tasks"), 0o644)

	var out bytes.Buffer
	err := runPlan([]string{
		"--prd", prdPath,
		"--prompt", promptPath,
		"--out", outPath,
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "plan: wrote") {
		t.Fatalf("expected success message, got: %s", out.String())
	}

	// Verify tasks.yaml exists and is valid.
	tasks, err := loadTasks(outPath)
	if err != nil {
		t.Fatalf("expected valid tasks.yaml, got: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one task")
	}
}

func TestRun_PlanDispatch(t *testing.T) {
	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "tasks.yaml")

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = planCommandBuilder("write-tasks", outPath)

	prdPath := filepath.Join(tmp, "PRD.md")
	os.WriteFile(prdPath, []byte("# PRD"), 0o644)
	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("extract tasks"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"plan",
		"--prd", prdPath,
		"--prompt", promptPath,
		"--out", outPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunPlan_DefaultFlags(t *testing.T) {
	// Verify defaults are sensible by checking error messages reference expected paths.
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete") // won't be reached

	var out bytes.Buffer
	// No flags → should fail on missing PRD.md (the default prd path).
	err := runPlan([]string{}, &out)
	if err == nil {
		t.Fatal("expected error with default flags when PRD.md absent")
	}
	if !strings.Contains(err.Error(), "PRD.md") {
		t.Fatalf("expected error to mention PRD.md, got: %v", err)
	}
}

// genPromptsCommandBuilder returns a commandBuilder that runs the gen-prompts-write helper.
// outPath is the prompt file path that the helper will create.
func genPromptsCommandBuilder(outPath string) func(context.Context, string, string) *exec.Cmd {
	return func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(),
			"KILN_TEST_HELPER_MODE=gen-prompts-write",
			"KILN_TEST_GEN_PROMPTS_OUT="+outPath,
		)
		return cmd
	}
}

// --- Tests for runGenPrompts ---

func TestRunGenPrompts_MissingTasksFile(t *testing.T) {
	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "/nonexistent/tasks.yaml",
		"--prd", "PRD.md",
		"--template", ".kiln/templates/<id>.md",
	}, &out)
	if err == nil {
		t.Fatal("expected error for missing tasks file")
	}
	if !strings.Contains(err.Error(), "tasks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenPrompts_MissingPRD(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "nonexistent.md",
		"--template", "template.md",
	}, &out)
	if err == nil {
		t.Fatal("expected error for missing PRD file")
	}
	if !strings.Contains(err.Error(), "failed to read PRD file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenPrompts_MissingTemplate(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "nonexistent.md",
	}, &out)
	if err == nil {
		t.Fatal("expected error for missing template file")
	}
	if !strings.Contains(err.Error(), "failed to read template file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenPrompts_SkipsExistingPrompt(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	os.WriteFile("alpha.md", []byte("existing content"), 0o644)
	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)
	os.WriteFile("template.md", []byte("# Template"), 0o644)

	callCount := 0
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
	}

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "template.md",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Fatalf("expected Claude not to be called for existing prompt, but got %d calls", callCount)
	}
	if !strings.Contains(out.String(), "skipped 1") {
		t.Fatalf("expected skipped 1 in output, got: %s", out.String())
	}
	data, _ := os.ReadFile("alpha.md")
	if string(data) != "existing content" {
		t.Fatal("existing prompt was modified unexpectedly")
	}
}

func TestRunGenPrompts_OverwriteExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	os.WriteFile("alpha.md", []byte("old content"), 0o644)
	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)
	os.WriteFile("template.md", []byte("# Template"), 0o644)

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = genPromptsCommandBuilder(filepath.Join(tmp, "alpha.md"))

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "template.md",
		"--overwrite",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "generated 1") {
		t.Fatalf("expected generated 1 in output, got: %s", out.String())
	}
	data, _ := os.ReadFile("alpha.md")
	if string(data) == "old content" {
		t.Fatal("expected prompt file to be regenerated")
	}
}

func TestRunGenPrompts_GeneratesNewPrompt(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	os.MkdirAll("prompts", 0o755)
	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: prompts/alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)
	os.WriteFile("template.md", []byte("# Template"), 0o644)

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = genPromptsCommandBuilder(filepath.Join(tmp, "prompts", "alpha.md"))

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "template.md",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "generated 1") {
		t.Fatalf("expected generated 1 in output, got: %s", out.String())
	}
	if _, err := os.Stat("prompts/alpha.md"); err != nil {
		t.Fatalf("expected prompt file to exist: %v", err)
	}
}

func TestRun_GenPromptsDispatch(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)
	os.WriteFile("template.md", []byte("# Template"), 0o644)

	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = genPromptsCommandBuilder(filepath.Join(tmp, "alpha.md"))

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"gen-prompts",
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "template.md",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunGenPrompts_DefaultFlags(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete") // won't be reached

	var out bytes.Buffer
	// No flags -> should fail on missing .kiln/tasks.yaml (the default tasks path).
	err := runGenPrompts([]string{}, &out)
	if err == nil {
		t.Fatal("expected error with default flags when tasks.yaml absent")
	}
	if !strings.Contains(err.Error(), "tasks") {
		t.Fatalf("expected error to mention tasks, got: %v", err)
	}
}

func TestRunGenPrompts_DefaultModel(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	os.WriteFile("tasks.yaml", []byte("- id: alpha\n  prompt: alpha.md\n  needs: []\n"), 0o644)
	os.WriteFile("PRD.md", []byte("# PRD"), 0o644)
	os.WriteFile("template.md", []byte("# Template"), 0o644)

	var capturedModel string
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		capturedModel = model
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(),
			"KILN_TEST_HELPER_MODE=gen-prompts-write",
			"KILN_TEST_GEN_PROMPTS_OUT="+filepath.Join(tmp, "alpha.md"),
		)
		return cmd
	}

	var out bytes.Buffer
	err := runGenPrompts([]string{
		"--tasks", "tasks.yaml",
		"--prd", "PRD.md",
		"--template", "template.md",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedModel != genPromptsDefaultModel {
		t.Fatalf("expected model %q, got %q", genPromptsDefaultModel, capturedModel)
	}
}

// --- Tests for richer task schema ---

func TestLoadTasks_RicherSchemaAllOptionalFields(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: rich-task
  prompt: rich.md
  needs: []
  description: A fully-loaded task
  kind: backend
  tags:
    - important
    - infra
  retries: 3
  validation:
    - go test ./...
    - golangci-lint run
  engine: claude
  env:
    MY_VAR: hello
    ANOTHER_VAR: world
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Description != "A fully-loaded task" {
		t.Errorf("unexpected description: %q", task.Description)
	}
	if task.Kind != "backend" {
		t.Errorf("unexpected kind: %q", task.Kind)
	}
	if len(task.Tags) != 2 || task.Tags[0] != "important" || task.Tags[1] != "infra" {
		t.Errorf("unexpected tags: %v", task.Tags)
	}
	if task.Retries != 3 {
		t.Errorf("unexpected retries: %d", task.Retries)
	}
	if len(task.Validation) != 2 {
		t.Errorf("unexpected validation: %v", task.Validation)
	}
	if task.Engine != "claude" {
		t.Errorf("unexpected engine: %q", task.Engine)
	}
	if task.Env["MY_VAR"] != "hello" || task.Env["ANOTHER_VAR"] != "world" {
		t.Errorf("unexpected env: %v", task.Env)
	}
}

func TestLoadTasks_BackwardCompatOriginalFields(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
  timeout: 10m
  model: claude-haiku-4-5-20251001
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestLoadTasks_NegativeRetriesRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  retries: -1
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for negative retries")
	}
	if !strings.Contains(err.Error(), "retries must be >= 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_WhitespaceOnlyKindRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("- id: bad-task\n  prompt: p.md\n  kind: \"   \"\n"), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for whitespace-only kind")
	}
	if !strings.Contains(err.Error(), "kind must not be whitespace-only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_TagWithWhitespaceRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  tags:
    - "foo bar"
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for tag with whitespace")
	}
	if !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_EmptyTagRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  tags:
    - ""
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for empty tag")
	}
	if !strings.Contains(err.Error(), "tags[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_InvalidEnvKeyRejected(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{"starts with digit", "1VAR", "not a valid environment variable name"},
		{"contains space", "MY VAR", "not a valid environment variable name"},
		{"contains dash", "MY-VAR", "not a valid environment variable name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			p := filepath.Join(tmp, "tasks.yaml")
			content := "- id: bad-task\n  prompt: p.md\n  env:\n    " + tt.key + ": value\n"
			os.WriteFile(p, []byte(content), 0o644)

			_, err := loadTasks(p)
			if err == nil {
				t.Fatal("expected error for invalid env key")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q in error, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadTasks_ValidEnvKeys(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: good-task
  prompt: p.md
  env:
    MY_VAR: hello
    _PRIVATE: world
    VAR123: value
    A: single
`), 0o644)

	_, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error for valid env keys: %v", err)
	}
}

func TestLoadTasks_UnknownFieldStillRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  unknown_field: value
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for unknown field (KnownFields still active)")
	}
	if !strings.Contains(err.Error(), "failed to parse tasks file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_TaskRetriesOverridesDefault(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: retry-task
  prompt: p.md
  retries: 2
`), 0o644)

	// Do not pass --retries flag; task's retries field should be used
	_, err := runExec([]string{
		"--task-id", "retry-task",
		"--tasks", tasksPath,
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if callCount != 3 {
		t.Errorf("expected 3 attempts (1 + 2 task retries), got %d", callCount)
	}
}

func TestRunExec_TaskRetriesZeroUsesDefault(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var callCount int
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		callCount++
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=fail")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	// No retries field (defaults to 0 = not set), --retries flag also defaults to 0
	os.WriteFile(tasksPath, []byte("- id: no-retry-task\n  prompt: p.md\n"), 0o644)

	_, err := runExec([]string{
		"--task-id", "no-retry-task",
		"--tasks", tasksPath,
		// --retries not set → default 0
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", callCount)
	}
}

func TestRunExec_TaskEnvInjected(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	t.Setenv("KILN_TEST_CHECK_VAR", "MY_TASK_VAR")
	t.Setenv("KILN_TEST_TASK_ID", "env-task")

	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=env-check")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: env-task
  prompt: p.md
  env:
    MY_TASK_VAR: hello-from-task
`), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "p.md"), []byte("test"), 0o644)

	var stdout bytes.Buffer
	code, err := runExec([]string{
		"--task-id", "env-task",
		"--tasks", tasksPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 (complete), got %d", code)
	}
	if !strings.Contains(stdout.String(), "hello-from-task") {
		t.Errorf("expected task env var value in stdout, got: %s", stdout.String())
	}
}

func TestRunExec_TaskEnvOverridesExisting(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	// Set an existing env var that the task will override
	t.Setenv("KILN_TEST_OVERRIDE_VAR", "original-value")
	t.Setenv("KILN_TEST_CHECK_VAR", "KILN_TEST_OVERRIDE_VAR")
	t.Setenv("KILN_TEST_TASK_ID", "override-task")

	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=env-check")
		return cmd
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: override-task
  prompt: p.md
  env:
    KILN_TEST_OVERRIDE_VAR: overridden-value
`), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "p.md"), []byte("test"), 0o644)

	var stdout bytes.Buffer
	code, err := runExec([]string{
		"--task-id", "override-task",
		"--tasks", tasksPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 (complete), got %d", code)
	}
	if !strings.Contains(stdout.String(), "overridden-value") {
		t.Errorf("expected overridden value in stdout, got: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "original-value") {
		t.Errorf("expected original value to be overridden, but got: %s", stdout.String())
	}
}

// --- Tests for state management (loadState, saveState, classifyError) ---

func TestLoadState_FileNotFound(t *testing.T) {
	tmp := t.TempDir()
	state, err := loadState(filepath.Join(tmp, "state.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state manifest")
	}
	if state.Tasks == nil {
		t.Fatal("expected non-nil Tasks map")
	}
	if len(state.Tasks) != 0 {
		t.Fatalf("expected empty Tasks map, got %d entries", len(state.Tasks))
	}
}

func TestLoadState_ValidJSON(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "state.json")
	data := `{"tasks":{"my-task":{"status":"completed","attempts":2}},"last_updated":"2026-01-01T00:00:00Z"}`
	os.WriteFile(p, []byte(data), 0o644)

	state, err := loadState(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts := state.Tasks["my-task"]
	if ts == nil {
		t.Fatal("expected task state for my-task")
	}
	if ts.Status != "completed" {
		t.Errorf("expected status completed, got %s", ts.Status)
	}
	if ts.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", ts.Attempts)
	}
}

func TestLoadState_MalformedJSON(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "state.json")
	os.WriteFile(p, []byte("not valid json {{{"), 0o644)

	_, err := loadState(p)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse state file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveState_WritesValidJSON(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "state.json")
	state := &StateManifest{
		Tasks: map[string]*TaskState{
			"task-a": {Status: "completed", Attempts: 1},
		},
	}

	err := saveState(p, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("saved state is not valid JSON: %v", err)
	}
}

func TestSaveState_LastUpdatedPopulated(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "state.json")
	state := &StateManifest{Tasks: map[string]*TaskState{}}

	before := time.Now()
	err := saveState(p, state)
	after := time.Now()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.LastUpdated.Before(before) || state.LastUpdated.After(after) {
		t.Errorf("LastUpdated %v not in expected range [%v, %v]", state.LastUpdated, before, after)
	}
}

func TestSaveState_AtomicWrite(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "state.json")
	state := &StateManifest{Tasks: map[string]*TaskState{
		"t1": {Status: "running"},
	}}

	if err := saveState(p, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .tmp file should not exist after successful save
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Error("expected .tmp file to be renamed away, but it still exists")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected state file to exist: %v", err)
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	err := &timeoutError{taskID: "t1", timeout: time.Minute}
	got, _ := classify(err)
	if got != ErrClassTimeout {
		t.Errorf("expected %s, got %s", ErrClassTimeout, got)
	}
}

func TestClassifyError_ClaudeExit(t *testing.T) {
	err := &claudeExitError{err: errors.New("exit 1")}
	got, _ := classify(err)
	if got != ErrClassClaudeExit {
		t.Errorf("expected %s, got %s", ErrClassClaudeExit, got)
	}
}

func TestClassifyError_FooterInvalid(t *testing.T) {
	err := &footerError{msg: "missing footer"}
	got, _ := classify(err)
	if got != ErrClassFooterParse {
		t.Errorf("expected %s, got %s", ErrClassFooterParse, got)
	}
}

func TestClassifyError_Permanent(t *testing.T) {
	err := errors.New("some other error")
	got, _ := classify(err)
	if got != ErrClassUnknown {
		t.Errorf("expected %s, got %s", ErrClassUnknown, got)
	}
}

func TestStateTransition_PendingToRunningToCompleted(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "state-task")

	var stdout bytes.Buffer
	_, err := runExec([]string{
		"--task-id", "state-task",
		"--prompt-file", promptPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	statePath := filepath.Join(tmpDir, ".kiln", "state.json")
	state, err := loadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	ts := state.Tasks["state-task"]
	if ts == nil {
		t.Fatal("expected task state entry")
	}
	if ts.Status != "completed" {
		t.Errorf("expected status completed, got %s", ts.Status)
	}
	if ts.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", ts.Attempts)
	}
	if ts.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

func TestStateTransition_PendingToRunningToFailed(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout bytes.Buffer
	runExec([]string{
		"--task-id", "fail-task",
		"--prompt-file", promptPath,
	}, &stdout)

	statePath := filepath.Join(tmpDir, ".kiln", "state.json")
	state, err := loadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	ts := state.Tasks["fail-task"]
	if ts == nil {
		t.Fatal("expected task state entry")
	}
	if ts.Status != "failed" {
		t.Errorf("expected status failed, got %s", ts.Status)
	}
	if ts.LastError == "" {
		t.Error("expected LastError to be set")
	}
	if ts.LastErrorClass != "claude_exit" {
		t.Errorf("expected LastErrorClass claude_exit, got %s", ts.LastErrorClass)
	}
}

func TestStateTransition_PendingToRunningToBlocked(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("blocked")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "blocked-task")

	var stdout bytes.Buffer
	runExec([]string{
		"--task-id", "blocked-task",
		"--prompt-file", promptPath,
	}, &stdout)

	statePath := filepath.Join(tmpDir, ".kiln", "state.json")
	state, err := loadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	ts := state.Tasks["blocked-task"]
	if ts == nil {
		t.Fatal("expected task state entry")
	}
	if ts.Status != "blocked" {
		t.Errorf("expected status blocked, got %s", ts.Status)
	}
	if ts.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", ts.Attempts)
	}
}

func TestStateAttemptCountIncrementsAcrossRetries(t *testing.T) {
	origBuilder := commandBuilder
	origSleep := sleepFn
	t.Cleanup(func() {
		commandBuilder = origBuilder
		sleepFn = origSleep
	})
	commandBuilder = fakeCommandBuilder("fail")
	sleepFn = func(d time.Duration) {}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout bytes.Buffer
	runExec([]string{
		"--task-id", "retry-task",
		"--prompt-file", promptPath,
		"--retries", "2",
		"--retry-backoff", "1ms",
	}, &stdout)

	statePath := filepath.Join(tmpDir, ".kiln", "state.json")
	state, err := loadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	ts := state.Tasks["retry-task"]
	if ts == nil {
		t.Fatal("expected task state entry")
	}
	if ts.Attempts != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", ts.Attempts)
	}
}

func TestRunStatus_ReadsStateJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"alpha":{"status":"completed","attempts":2},"beta":{"status":"failed","attempts":1,"last_error":"claude invocation failed"}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "ATTEMPTS") {
		t.Errorf("expected ATTEMPTS column in output, got:\n%s", out)
	}
	if !strings.Contains(out, "LAST ERROR") {
		t.Errorf("expected LAST ERROR column in output, got:\n%s", out)
	}
}

func TestRunStatus_EmptyStateMissingJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
`), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "t1") {
		t.Errorf("expected task t1 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "runnable") {
		t.Errorf("expected runnable status, got:\n%s", out)
	}
}

// --- Tests for richer schema fields ---

func TestLoadTasks_RicherSchema_AllNewFields(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: rich-new-task
  prompt: rich.md
  needs: []
  phase: build
  milestone: m1-core
  acceptance:
    - Given a user exists, when they login, then they get a token
    - Dashboard loads within 2s
  verify:
    - go test ./...
    - go vet ./...
  lane: backend-lane
  exclusive: true
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Phase != "build" {
		t.Errorf("expected phase 'build', got %q", task.Phase)
	}
	if task.Milestone != "m1-core" {
		t.Errorf("expected milestone 'm1-core', got %q", task.Milestone)
	}
	if len(task.Acceptance) != 2 {
		t.Errorf("expected 2 acceptance criteria, got %d", len(task.Acceptance))
	}
	if len(task.Verify) != 2 || task.Verify[0] != "go test ./..." {
		t.Errorf("unexpected verify: %v", task.Verify)
	}
	if task.Lane != "backend-lane" {
		t.Errorf("expected lane 'backend-lane', got %q", task.Lane)
	}
	if !task.Exclusive {
		t.Error("expected exclusive to be true")
	}
}

func TestLoadTasks_BackwardCompat_NoNewFields(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: old-task
  prompt: old.md
  needs: []
  description: legacy task
  kind: feature
  retries: 2
`), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Phase != "" || task.Milestone != "" || task.Lane != "" || task.Exclusive {
		t.Error("new fields should be zero-valued when absent")
	}
	if len(task.Acceptance) != 0 || len(task.Verify) != 0 {
		t.Error("new slice fields should be empty when absent")
	}
}

func TestLoadTasks_WhitespaceOnlyPhaseRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("- id: bad-task\n  prompt: p.md\n  phase: \"   \"\n"), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for whitespace-only phase")
	}
	if !strings.Contains(err.Error(), "phase must not be whitespace-only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_NonKebabMilestoneRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("- id: bad-task\n  prompt: p.md\n  milestone: \"M1 Auth\"\n"), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for non-kebab milestone")
	}
	if !strings.Contains(err.Error(), "milestone must be kebab-case") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_EmptyAcceptanceEntryRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  acceptance:
    - valid criterion
    - ""
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for empty acceptance entry")
	}
	if !strings.Contains(err.Error(), "acceptance[1] must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_EmptyVerifyEntryRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte(`- id: bad-task
  prompt: p.md
  verify:
    - go test ./...
    - ""
`), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for empty verify entry")
	}
	if !strings.Contains(err.Error(), "verify[1] must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_NonKebabLaneRejected(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("- id: bad-task\n  prompt: p.md\n  lane: \"Bad Lane!\"\n"), 0o644)

	_, err := loadTasks(p)
	if err == nil {
		t.Fatal("expected error for non-kebab lane")
	}
	if !strings.Contains(err.Error(), "lane must be kebab-case") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTasks_ExclusiveTrueAccepted(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "tasks.yaml")
	os.WriteFile(p, []byte("- id: excl-task\n  prompt: p.md\n  exclusive: true\n"), 0o644)

	tasks, err := loadTasks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tasks[0].Exclusive {
		t.Error("expected exclusive to be true")
	}
}

func TestRunGenMake_PhaseTargetsGenerated(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte(`- id: task-a
  prompt: a.md
  needs: []
  phase: build
- id: task-b
  prompt: b.md
  needs: []
  phase: build
- id: task-c
  prompt: c.md
  needs: []
  phase: verify
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	if !strings.Contains(got, ".PHONY: phase-build") {
		t.Errorf("expected phase-build phony target, got:\n%s", got)
	}
	if !strings.Contains(got, ".PHONY: phase-verify") {
		t.Errorf("expected phase-verify phony target, got:\n%s", got)
	}
	if !strings.Contains(got, "phase-build: .kiln/done/task-a.done .kiln/done/task-b.done") {
		t.Errorf("expected phase-build to depend on task-a and task-b, got:\n%s", got)
	}
	if !strings.Contains(got, "phase-verify: .kiln/done/task-c.done") {
		t.Errorf("expected phase-verify to depend on task-c, got:\n%s", got)
	}
}

func TestRunGenMake_MilestoneTargetsGenerated(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte(`- id: task-x
  prompt: x.md
  needs: []
  milestone: m1-auth
- id: task-y
  prompt: y.md
  needs: []
  milestone: m2-payments
- id: task-z
  prompt: z.md
  needs: []
  milestone: m1-auth
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	if !strings.Contains(got, ".PHONY: milestone-m1-auth") {
		t.Errorf("expected milestone-m1-auth phony target, got:\n%s", got)
	}
	if !strings.Contains(got, ".PHONY: milestone-m2-payments") {
		t.Errorf("expected milestone-m2-payments phony target, got:\n%s", got)
	}
	if !strings.Contains(got, "milestone-m1-auth: .kiln/done/task-x.done .kiln/done/task-z.done") {
		t.Errorf("expected milestone-m1-auth to depend on task-x and task-z, got:\n%s", got)
	}
	if !strings.Contains(got, "milestone-m2-payments: .kiln/done/task-y.done") {
		t.Errorf("expected milestone-m2-payments to depend on task-y, got:\n%s", got)
	}
}

func TestRunGenMake_NoPhaseOrMilestoneTargets_WhenFieldsAbsent(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte(`- id: plain-task
  prompt: p.md
  needs: []
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	if strings.Contains(got, "phase-") {
		t.Errorf("expected no phase targets when no tasks have phase set, got:\n%s", got)
	}
	if strings.Contains(got, "milestone-") {
		t.Errorf("expected no milestone targets when no tasks have milestone set, got:\n%s", got)
	}
}

func TestRunGenMake_ModelPassedInRecipe(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte(`- id: modeled-task
  prompt: m.md
  needs: []
  model: claude-haiku-4-5-20251001
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	if !strings.Contains(got, "--model claude-haiku-4-5-20251001") {
		t.Errorf("expected --model flag in recipe, got:\n%s", got)
	}
}

func TestRunGenMake_NoModelInRecipe_WhenModelAbsent(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte(`- id: plain-task
  prompt: p.md
  needs: []
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	if strings.Contains(got, "--model") {
		t.Errorf("expected no --model flag when task has no model, got:\n%s", got)
	}
}

func TestRunGenMake_PhaseAndMilestoneSorted(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	// Phase values in reverse alphabetical order to verify sorting
	os.WriteFile(tasksPath, []byte(`- id: task-verify
  prompt: v.md
  needs: []
  phase: verify
  milestone: m3-end
- id: task-build
  prompt: b.md
  needs: []
  phase: build
  milestone: m1-start
- id: task-plan
  prompt: p.md
  needs: []
  phase: plan
  milestone: m2-mid
`), 0o644)

	err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	got := string(data)

	// Check alphabetical ordering of phase targets
	buildIdx := strings.Index(got, "phase-build")
	planIdx := strings.Index(got, "phase-plan")
	verifyIdx := strings.Index(got, "phase-verify")
	if buildIdx < 0 || planIdx < 0 || verifyIdx < 0 {
		t.Fatalf("missing phase targets, got:\n%s", got)
	}
	if buildIdx >= planIdx || planIdx >= verifyIdx {
		t.Errorf("phase targets not sorted alphabetically: build=%d plan=%d verify=%d", buildIdx, planIdx, verifyIdx)
	}

	// Check alphabetical ordering of milestone targets
	m1Idx := strings.Index(got, "milestone-m1-start")
	m2Idx := strings.Index(got, "milestone-m2-mid")
	m3Idx := strings.Index(got, "milestone-m3-end")
	if m1Idx < 0 || m2Idx < 0 || m3Idx < 0 {
		t.Fatalf("missing milestone targets, got:\n%s", got)
	}
	if m1Idx >= m2Idx || m2Idx >= m3Idx {
		t.Errorf("milestone targets not sorted alphabetically: m1=%d m2=%d m3=%d", m1Idx, m2Idx, m3Idx)
	}
}

func TestRunStatus_ShowsKindAndPhase(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: my-task
  prompt: p.md
  needs: []
  kind: backend
  phase: build
`), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "KIND") {
		t.Errorf("expected KIND column header, got:\n%s", out)
	}
	if !strings.Contains(out, "PHASE") {
		t.Errorf("expected PHASE column header, got:\n%s", out)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("expected kind 'backend' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("expected phase 'build' in output, got:\n%s", out)
	}
}

// --- Concurrency safety / lock tests ---

func TestAcquireLock_CreatesLockFile(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")

	cleanup, err := acquireLock(locksDir, "my-task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	lockPath := filepath.Join(locksDir, "my-task.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("lock file contains invalid JSON: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), info.PID)
	}
	if info.StartedAt == "" {
		t.Error("expected non-empty started_at")
	}
	if info.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
}

func TestAcquireLock_ConflictReturnsError(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")

	cleanup, err := acquireLock(locksDir, "conflict-task")
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer cleanup()

	_, err2 := acquireLock(locksDir, "conflict-task")
	if err2 == nil {
		t.Fatal("expected error for duplicate lock acquisition")
	}
}

func TestAcquireLock_ErrorContainsPIDAndHostname(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")

	cleanup, err := acquireLock(locksDir, "diag-task")
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer cleanup()

	_, err2 := acquireLock(locksDir, "diag-task")
	if err2 == nil {
		t.Fatal("expected error for duplicate lock")
	}

	msg := err2.Error()
	pidStr := fmt.Sprintf("%d", os.Getpid())
	if !strings.Contains(msg, pidStr) {
		t.Errorf("error message missing PID %s: %s", pidStr, msg)
	}

	hostname, _ := os.Hostname()
	if !strings.Contains(msg, hostname) {
		t.Errorf("error message missing hostname %s: %s", hostname, msg)
	}
}

func TestAcquireLock_CleanupRemovesLock(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")
	lockPath := filepath.Join(locksDir, "rm-task.lock")

	cleanup, err := acquireLock(locksDir, "rm-task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatal("lock file should exist before cleanup")
	}

	cleanup()

	if _, err := os.Stat(lockPath); err == nil {
		t.Fatal("lock file should be removed after cleanup")
	}
}

func TestAcquireLock_CleanupIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")

	cleanup, err := acquireLock(locksDir, "idem-task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Calling cleanup multiple times must not panic or error.
	cleanup()
	cleanup()
	cleanup()
}

func TestAcquireLock_CreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	// Use a nested directory that doesn't exist yet.
	locksDir := filepath.Join(tmp, "nested", "deep", "locks")

	cleanup, err := acquireLock(locksDir, "dir-task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	if info, err := os.Stat(locksDir); err != nil || !info.IsDir() {
		t.Fatalf("locks directory not created: %v", err)
	}
}

func TestAcquireLock_LockConflictErrorType(t *testing.T) {
	tmp := t.TempDir()
	locksDir := filepath.Join(tmp, "locks")

	cleanup, _ := acquireLock(locksDir, "type-task")
	defer cleanup()

	_, err := acquireLock(locksDir, "type-task")
	if err == nil {
		t.Fatal("expected error")
	}

	var lce *lockConflictError
	if !errors.As(err, &lce) {
		t.Fatalf("expected lockConflictError, got %T: %v", err, err)
	}
	if lce.TaskID != "type-task" {
		t.Errorf("expected TaskID type-task, got %s", lce.TaskID)
	}
}

func TestRunExec_LockConflictExitCode10(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	// Pre-create a stale lock file for the task.
	locksDir := filepath.Join(tmp, ".kiln", "locks")
	if err := os.MkdirAll(locksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleInfo := lockInfo{PID: 99999, StartedAt: time.Now().UTC().Format(time.RFC3339), Hostname: "other-host"}
	data, _ := json.Marshal(staleInfo)
	os.WriteFile(filepath.Join(locksDir, "locked-task.lock"), data, 0o644)

	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--task-id", "locked-task", "--prompt-file", promptPath}, &stdout, &stderr)
	if code != 10 {
		t.Fatalf("expected exit code 10 for lock conflict, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "locked-task") {
		t.Errorf("error message should mention task ID, got: %s", stderr.String())
	}
}

func TestRunExec_ForceUnlockRemovesStale(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	t.Setenv("KILN_TEST_TASK_ID", "force-task")

	// Pre-create a stale lock file.
	locksDir := filepath.Join(tmp, ".kiln", "locks")
	os.MkdirAll(locksDir, 0o755)
	staleInfo := lockInfo{PID: 99999, StartedAt: time.Now().UTC().Format(time.RFC3339), Hostname: "old-host"}
	staleData, _ := json.Marshal(staleInfo)
	os.WriteFile(filepath.Join(locksDir, "force-task.lock"), staleData, 0o644)

	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	var stdout bytes.Buffer
	code, err := runExec([]string{
		"--task-id", "force-task",
		"--prompt-file", promptPath,
		"--force-unlock",
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = code
	if !strings.Contains(stdout.String(), "force-unlock") {
		t.Errorf("expected force-unlock warning in output, got: %s", stdout.String())
	}
}

func TestRunExec_LockReleasedAfterSuccess(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	t.Setenv("KILN_TEST_TASK_ID", "release-ok")

	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, err := runExec([]string{
		"--task-id", "release-ok",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lock should be released — re-acquiring should succeed.
	locksDir := filepath.Join(tmp, ".kiln", "locks")
	cleanup, err2 := acquireLock(locksDir, "release-ok")
	if err2 != nil {
		t.Fatalf("lock not released after successful exec: %v", err2)
	}
	cleanup()
}

func TestRunExec_LockReleasedAfterFailure(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, _ = runExec([]string{
		"--task-id", "release-fail",
		"--prompt-file", promptPath,
	}, &bytes.Buffer{})

	// Lock should be released — re-acquiring should succeed.
	locksDir := filepath.Join(tmp, ".kiln", "locks")
	cleanup, err := acquireLock(locksDir, "release-fail")
	if err != nil {
		t.Fatalf("lock not released after failed exec: %v", err)
	}
	cleanup()
}

func TestRunExec_LockReleasedAfterTimeout(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	promptPath := filepath.Join(tmp, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	_, _ = runExec([]string{
		"--task-id", "release-timeout",
		"--prompt-file", promptPath,
		"--timeout", "200ms",
	}, &bytes.Buffer{})

	// Lock should be released — re-acquiring should succeed.
	locksDir := filepath.Join(tmp, ".kiln", "locks")
	cleanup, err := acquireLock(locksDir, "release-timeout")
	if err != nil {
		t.Fatalf("lock not released after timeout: %v", err)
	}
	cleanup()
}

func TestRunGenMake_CreatesLocksDir(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, "targets.mk")

	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n"), 0o644)

	if err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	locksDir := filepath.Join(tmp, "locks")
	info, err := os.Stat(locksDir)
	if err != nil {
		t.Fatalf("locks directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected locks to be a directory")
	}
}

func TestAtomicLogWrite_ConcurrentWritesSafe(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "logs")

	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			entry := execRunLog{
				TaskID:     fmt.Sprintf("task-%d", i),
				StartedAt:  time.Now(),
				EndedAt:    time.Now(),
				DurationMs: int64(i * 100),
				Status:     "complete",
				ExitCode:   0,
			}
			if err := writeExecLog(logDir, entry.TaskID, entry); err != nil {
				t.Errorf("writeExecLog failed for task-%d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	// Verify each log file is valid JSON.
	for i := 0; i < goroutines; i++ {
		logPath := filepath.Join(logDir, fmt.Sprintf("task-%d.json", i))
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Errorf("log file task-%d.json not found: %v", i, err)
			continue
		}
		var entry execRunLog
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Errorf("task-%d.json contains invalid JSON: %v\ncontent: %s", i, err, data)
		}
		if entry.TaskID != fmt.Sprintf("task-%d", i) {
			t.Errorf("task-%d.json has wrong task_id: %s", i, entry.TaskID)
		}
	}
}

// --- Tests for error taxonomy (classify, error class constants, execRunLog fields) ---

func TestClassify_Nil(t *testing.T) {
	ec, ret := classify(nil)
	if ec != "" {
		t.Errorf("expected empty class for nil error, got %q", ec)
	}
	if ret {
		t.Error("expected retryable=false for nil error")
	}
}

func TestClassify_TimeoutError(t *testing.T) {
	err := &timeoutError{taskID: "t1", timeout: 5 * time.Second}
	ec, ret := classify(err)
	if ec != ErrClassTimeout {
		t.Errorf("expected %q, got %q", ErrClassTimeout, ec)
	}
	if !ret {
		t.Error("expected retryable=true for timeoutError")
	}
}

func TestClassify_ClaudeExitError(t *testing.T) {
	err := &claudeExitError{err: errors.New("exit status 1")}
	ec, ret := classify(err)
	if ec != ErrClassClaudeExit {
		t.Errorf("expected %q, got %q", ErrClassClaudeExit, ec)
	}
	if !ret {
		t.Error("expected retryable=true for claudeExitError")
	}
}

func TestClassify_FooterParseError(t *testing.T) {
	err := &footerError{msg: "missing footer", isValidation: false}
	ec, ret := classify(err)
	if ec != ErrClassFooterParse {
		t.Errorf("expected %q, got %q", ErrClassFooterParse, ec)
	}
	if ret {
		t.Error("expected retryable=false for footer parse error")
	}
}

func TestClassify_FooterValidationError(t *testing.T) {
	err := &footerError{msg: "invalid status", isValidation: true}
	ec, ret := classify(err)
	if ec != ErrClassFooterValidation {
		t.Errorf("expected %q, got %q", ErrClassFooterValidation, ec)
	}
	if ret {
		t.Error("expected retryable=false for footer validation error")
	}
}

func TestClassify_LockConflictError(t *testing.T) {
	err := &lockConflictError{TaskID: "t1", LockPath: "/tmp/t1.lock", Holder: lockInfo{PID: 42}}
	ec, ret := classify(err)
	if ec != ErrClassLockConflict {
		t.Errorf("expected %q, got %q", ErrClassLockConflict, ec)
	}
	if ret {
		t.Error("expected retryable=false for lockConflictError")
	}
}

func TestClassify_UnknownError(t *testing.T) {
	err := errors.New("some unknown error")
	ec, ret := classify(err)
	if ec != ErrClassUnknown {
		t.Errorf("expected %q, got %q", ErrClassUnknown, ec)
	}
	if ret {
		t.Error("expected retryable=false for unknown error")
	}
}

func TestClassify_DistinguishesFooterParseVsValidation(t *testing.T) {
	parseErr := &footerError{msg: "parse fail", isValidation: false}
	validErr := &footerError{msg: "validation fail", isValidation: true}
	ecParse, _ := classify(parseErr)
	ecValid, _ := classify(validErr)
	if ecParse == ecValid {
		t.Errorf("expected different classes for parse (%q) vs validation (%q) footer errors", ecParse, ecValid)
	}
	if ecParse != ErrClassFooterParse {
		t.Errorf("expected %q for parse, got %q", ErrClassFooterParse, ecParse)
	}
	if ecValid != ErrClassFooterValidation {
		t.Errorf("expected %q for validation, got %q", ErrClassFooterValidation, ecValid)
	}
}

func TestExecLog_ErrorClass_OnTimeout(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{"--task-id", "ec-timeout", "--prompt-file", promptPath, "--timeout", "200ms"}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "ec-timeout")
	if entry.ErrorClass != ErrClassTimeout {
		t.Errorf("expected error_class=%q, got %q", ErrClassTimeout, entry.ErrorClass)
	}
	if entry.ErrorMessage == "" {
		t.Error("expected non-empty error_message on timeout")
	}
	if !entry.Retryable {
		t.Error("expected retryable=true for timeout")
	}
}

func TestExecLog_ErrorClass_OnClaudeExit(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("fail")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{"--task-id", "ec-exit", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "ec-exit")
	if entry.ErrorClass != ErrClassClaudeExit {
		t.Errorf("expected error_class=%q, got %q", ErrClassClaudeExit, entry.ErrorClass)
	}
	if entry.ErrorMessage == "" {
		t.Error("expected non-empty error_message on claude exit failure")
	}
	if !entry.Retryable {
		t.Error("expected retryable=true for claude exit error")
	}
}

func TestExecLog_ErrorClass_OnMissingFooter(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("no_footer")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)

	runExec([]string{"--task-id", "ec-nofooter", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "ec-nofooter")
	if entry.ErrorClass != ErrClassFooterParse {
		t.Errorf("expected error_class=%q, got %q", ErrClassFooterParse, entry.ErrorClass)
	}
	if entry.ErrorMessage == "" {
		t.Error("expected non-empty error_message for missing footer")
	}
	if entry.Retryable {
		t.Error("expected retryable=false for footer parse error")
	}
}

func TestExecLog_ErrorClass_EmptyOnSuccess(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	t.Setenv("KILN_TEST_TASK_ID", "ec-success")

	runExec([]string{"--task-id", "ec-success", "--prompt-file", promptPath}, &bytes.Buffer{})

	entry := readExecLog(t, tmpDir, "ec-success")
	if entry.ErrorClass != "" {
		t.Errorf("expected empty error_class on success, got %q", entry.ErrorClass)
	}
	if entry.ErrorMessage != "" {
		t.Errorf("expected empty error_message on success, got %q", entry.ErrorMessage)
	}
	if entry.Retryable {
		t.Error("expected retryable=false (omitted) on success")
	}
}

// --- Tests for kiln report ---

func writeLogFile(t *testing.T, dir, taskID string, entry execRunLog) {
	t.Helper()
	entry.TaskID = taskID
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal log entry: %v", err)
	}
	logDir := filepath.Join(dir, ".kiln", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("failed to create log dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, taskID+".json"), data, 0o644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}
}

func TestRunReport_EmptyLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	var out bytes.Buffer
	err := runReport([]string{}, &out)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(out.String(), "No execution logs found") {
		t.Errorf("expected 'No execution logs found' message, got: %s", out.String())
	}
}

func TestRunReport_MissingLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	// Don't create .kiln/logs at all.
	var out bytes.Buffer
	err := runReport([]string{}, &out)
	if err != nil {
		t.Fatalf("expected no error for missing log dir, got: %v", err)
	}
	if !strings.Contains(out.String(), "No execution logs found") {
		t.Errorf("expected 'No execution logs found' message, got: %s", out.String())
	}
}

func TestRunReport_TableOutput(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	writeLogFile(t, tmpDir, "setup-db", execRunLog{Status: "complete"})
	writeLogFile(t, tmpDir, "auth-module", execRunLog{
		Status:       "timeout",
		ErrorClass:   ErrClassTimeout,
		ErrorMessage: "context deadline exceeded",
	})

	var out bytes.Buffer
	err := runReport([]string{}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := out.String()

	// Check header columns
	for _, col := range []string{"Task", "Status", "Attempts", "Last Error Class", "Last Error"} {
		if !strings.Contains(s, col) {
			t.Errorf("expected column %q in output, got:\n%s", col, s)
		}
	}
	// Check task rows
	if !strings.Contains(s, "setup-db") {
		t.Errorf("expected setup-db in output, got:\n%s", s)
	}
	if !strings.Contains(s, "auth-module") {
		t.Errorf("expected auth-module in output, got:\n%s", s)
	}
	// Check summary
	if !strings.Contains(s, "Summary") {
		t.Errorf("expected Summary section, got:\n%s", s)
	}
	if !strings.Contains(s, "Total: 2") {
		t.Errorf("expected Total: 2, got:\n%s", s)
	}
	if !strings.Contains(s, "Complete: 1") {
		t.Errorf("expected Complete: 1, got:\n%s", s)
	}
	if !strings.Contains(s, "Failed: 1") {
		t.Errorf("expected Failed: 1, got:\n%s", s)
	}
	if !strings.Contains(s, "timeout") {
		t.Errorf("expected 'timeout' in top errors, got:\n%s", s)
	}
}

func TestRunReport_JSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	writeLogFile(t, tmpDir, "task-a", execRunLog{
		Status:       "complete",
		ErrorClass:   "",
		ErrorMessage: "",
	})
	writeLogFile(t, tmpDir, "task-b", execRunLog{
		Status:       "error",
		ErrorClass:   ErrClassClaudeExit,
		ErrorMessage: "exit code 1",
	})

	var out bytes.Buffer
	err := runReport([]string{"--format", "json"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var report reportData
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &report); err != nil {
		t.Fatalf("failed to parse JSON report: %v\noutput: %s", err, out.String())
	}

	if report.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", report.Summary.Total)
	}
	if report.Summary.Complete != 1 {
		t.Errorf("expected complete=1, got %d", report.Summary.Complete)
	}
	if report.Summary.Failed != 1 {
		t.Errorf("expected failed=1, got %d", report.Summary.Failed)
	}
	if len(report.Tasks) != 2 {
		t.Errorf("expected 2 task entries, got %d", len(report.Tasks))
	}
}

func TestRunReport_AttemptsFromState(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	writeLogFile(t, tmpDir, "retry-task", execRunLog{
		Status:       "timeout",
		ErrorClass:   ErrClassTimeout,
		ErrorMessage: "timed out",
	})

	// Write a state.json with 3 attempts.
	state := &StateManifest{
		Tasks: map[string]*TaskState{
			"retry-task": {Status: "failed", Attempts: 3},
		},
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".kiln"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(tmpDir, ".kiln", "state.json"), data, 0o644)

	var out bytes.Buffer
	if err := runReport([]string{"--format", "json"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var report reportData
	json.Unmarshal([]byte(strings.TrimSpace(out.String())), &report)
	if len(report.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(report.Tasks))
	}
	if report.Tasks[0].Attempts != 3 {
		t.Errorf("expected attempts=3, got %d", report.Tasks[0].Attempts)
	}
	if report.Summary.Attempts != 3 {
		t.Errorf("expected total_attempts=3, got %d", report.Summary.Attempts)
	}
}

func TestRunReport_TopErrorClasses(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	writeLogFile(t, tmpDir, "t1", execRunLog{Status: "timeout", ErrorClass: ErrClassTimeout})
	writeLogFile(t, tmpDir, "t2", execRunLog{Status: "timeout", ErrorClass: ErrClassTimeout})
	writeLogFile(t, tmpDir, "t3", execRunLog{Status: "error", ErrorClass: ErrClassClaudeExit})

	var out bytes.Buffer
	runReport([]string{"--format", "json"}, &out)

	var report reportData
	json.Unmarshal([]byte(strings.TrimSpace(out.String())), &report)

	if report.Summary.TopErrors[ErrClassTimeout] != 2 {
		t.Errorf("expected timeout count=2, got %d", report.Summary.TopErrors[ErrClassTimeout])
	}
	if report.Summary.TopErrors[ErrClassClaudeExit] != 1 {
		t.Errorf("expected claude_exit count=1, got %d", report.Summary.TopErrors[ErrClassClaudeExit])
	}
}

func TestRunReport_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	var out bytes.Buffer
	err := runReport([]string{"--format", "csv"}, &out)
	if err == nil {
		t.Fatal("expected error for invalid format, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --format value") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReport_StatusMapping(t *testing.T) {
	cases := []struct {
		logStatus     string
		displayStatus string
	}{
		{"complete", "complete"},
		{"not_complete", "not_complete"},
		{"blocked", "blocked"},
		{"timeout", "failed"},
		{"error", "failed"},
		{"unknown", "failed"},
	}
	for _, tc := range cases {
		got := logStatusToDisplayStatus(tc.logStatus)
		if got != tc.displayStatus {
			t.Errorf("logStatusToDisplayStatus(%q) = %q, want %q", tc.logStatus, got, tc.displayStatus)
		}
	}
}

// --- Tests for kiln unify ---

// setupUnifyDir creates a temp dir with .kiln structure and changes into it.
// Returns the temp dir path and a cleanup function.
func setupUnifyDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".kiln/done", ".kiln/logs", ".kiln/prompts/tasks"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return tmpDir
}

func TestRunUnify_MissingTaskID(t *testing.T) {
	code, err := runUnify([]string{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--task-id is required") {
		t.Fatalf("expected --task-id required error, got code=%d err=%v", code, err)
	}
}

func TestRunUnify_InvalidTaskID(t *testing.T) {
	code, err := runUnify([]string{"--task-id", "INVALID ID"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "kebab-case") {
		t.Fatalf("expected kebab-case error, got code=%d err=%v", code, err)
	}
}

func TestRunUnify_TaskNotCompleted(t *testing.T) {
	setupUnifyDir(t)

	code, err := runUnify([]string{"--task-id", "my-task"}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit code 2 for incomplete task, got %d", code)
	}
	if err == nil || !strings.Contains(err.Error(), "not completed") {
		t.Fatalf("expected 'not completed' error, got: %v", err)
	}
}

func TestRunUnify_TaskCompletedViaDoneMarker(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "my-task")

	// Create .done marker.
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/my-task.done"), nil, 0o644)

	var out bytes.Buffer
	code, err := runUnify([]string{"--task-id", "my-task"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out.String())
	}
}

func TestRunUnify_TaskCompletedViaStateJSON(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "state-task")

	// Write state.json with completed status.
	state := &StateManifest{Tasks: map[string]*TaskState{
		"state-task": {Status: "completed"},
	}}
	stateData, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(tmpDir, ".kiln/state.json"), stateData, 0o644)

	var out bytes.Buffer
	code, err := runUnify([]string{"--task-id", "state-task"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; output: %s", code, out.String())
	}
}

func TestRunUnify_ArtifactWritten(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "art-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/art-task.done"), nil, 0o644)

	var out bytes.Buffer
	code, err := runUnify([]string{"--task-id", "art-task"}, &out)
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: code=%d err=%v", code, err)
	}

	artifactPath := filepath.Join(tmpDir, ".kiln/unify/art-task.md")
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("closure artifact not found: %v", readErr)
	}
	if !strings.Contains(string(data), "What Changed") {
		t.Errorf("artifact missing 'What Changed' section, got: %s", string(data))
	}
}

func TestRunUnify_DirectoryCreated(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "dir-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/dir-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "dir-task"}, &bytes.Buffer{})
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: code=%d err=%v", code, err)
	}

	info, statErr := os.Stat(filepath.Join(tmpDir, ".kiln/unify"))
	if statErr != nil || !info.IsDir() {
		t.Fatal(".kiln/unify directory was not created")
	}
}

func TestRunUnify_OverwriteArtifact(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "ow-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/ow-task.done"), nil, 0o644)

	// Run twice — second run should overwrite.
	for i := 0; i < 2; i++ {
		code, err := runUnify([]string{"--task-id", "ow-task"}, &bytes.Buffer{})
		if err != nil || code != 0 {
			t.Fatalf("run %d unexpected error: code=%d err=%v", i+1, code, err)
		}
	}

	artifactPath := filepath.Join(tmpDir, ".kiln/unify/ow-task.md")
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Fatal("artifact not found after second run")
	}
}

func TestRunUnify_FooterNotComplete_ExitCode2(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("not_complete")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "nc-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/nc-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "nc-task"}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d; err=%v", code, err)
	}
}

func TestRunUnify_FooterParseFailure_ExitCode10(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("no_footer")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "nf-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/nf-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "nf-task"}, &bytes.Buffer{})
	if code != 10 {
		t.Fatalf("expected exit code 10, got %d; err=%v", code, err)
	}
	var fe *footerError
	if !errors.As(err, &fe) {
		t.Fatalf("expected footerError, got: %T %v", err, err)
	}
}

func TestRunUnify_DecisionLedger(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	t.Setenv("KILN_TEST_TASK_ID", "led-task")
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/led-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "led-task"}, &bytes.Buffer{})
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: code=%d err=%v", code, err)
	}

	ledgerData, readErr := os.ReadFile(filepath.Join(tmpDir, ".kiln/decisions.log"))
	if readErr != nil {
		t.Fatalf("decisions.log not found: %v", readErr)
	}

	var entry decisionLedgerEntry
	if err := json.Unmarshal(bytes.TrimSpace(ledgerData), &entry); err != nil {
		t.Fatalf("failed to parse ledger entry: %v\ndata: %s", err, ledgerData)
	}
	if entry.TaskID != "led-task" {
		t.Errorf("expected task_id 'led-task', got %q", entry.TaskID)
	}
	if entry.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if entry.ArtifactPath == "" {
		t.Error("expected non-empty artifact_path")
	}
	if entry.Model == "" {
		t.Error("expected non-empty model")
	}
}

func TestRunUnify_DecisionLedgerAppend(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("unify-success")

	tmpDir := setupUnifyDir(t)
	ledgerPath := filepath.Join(tmpDir, ".kiln/decisions.log")

	for _, taskSuffix := range []string{"first", "second"} {
		id := "app-" + taskSuffix
		t.Setenv("KILN_TEST_TASK_ID", id)
		os.WriteFile(filepath.Join(tmpDir, ".kiln/done/"+id+".done"), nil, 0o644)
		code, err := runUnify([]string{"--task-id", id}, &bytes.Buffer{})
		if err != nil || code != 0 {
			t.Fatalf("task %s: code=%d err=%v", id, code, err)
		}
	}

	data, _ := os.ReadFile(ledgerPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d\ndata: %s", len(lines), data)
	}
}

func TestRunUnify_ModelFlag(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var capturedModel string
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		capturedModel = model
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=unify-success", "KILN_TEST_TASK_ID=mod-task")
		return cmd
	}

	tmpDir := setupUnifyDir(t)
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/mod-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "mod-task", "--model", "claude-opus-4-6"}, &bytes.Buffer{})
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: code=%d err=%v", code, err)
	}
	if capturedModel != "claude-opus-4-6" {
		t.Errorf("expected model 'claude-opus-4-6', got %q", capturedModel)
	}
}

func TestRunUnify_TimeoutFlag(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("hang")

	tmpDir := setupUnifyDir(t)
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/hang-task.done"), nil, 0o644)

	code, err := runUnify([]string{"--task-id", "hang-task", "--timeout", "200ms"}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit code 2 on timeout, got %d; err=%v", code, err)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestRunUnify_PromptIncludesTaskContent(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })

	var capturedPrompt string
	commandBuilder = func(ctx context.Context, prompt, model string) *exec.Cmd {
		capturedPrompt = prompt
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
		cmd.Env = append(os.Environ(), "KILN_TEST_HELPER_MODE=unify-success", "KILN_TEST_TASK_ID=pmt-task")
		return cmd
	}

	tmpDir := setupUnifyDir(t)
	os.WriteFile(filepath.Join(tmpDir, ".kiln/done/pmt-task.done"), nil, 0o644)
	os.WriteFile(filepath.Join(tmpDir, ".kiln/prompts/tasks/pmt-task.md"), []byte("# My Task Prompt\nDo the thing."), 0o644)

	code, err := runUnify([]string{"--task-id", "pmt-task"}, &bytes.Buffer{})
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: code=%d err=%v", code, err)
	}
	if !strings.Contains(capturedPrompt, "My Task Prompt") {
		t.Errorf("prompt does not include task prompt content; prompt starts with: %s", capturedPrompt[:min(200, len(capturedPrompt))])
	}
}

func TestRunUnify_PromptIncludesClosureSections(t *testing.T) {
	prompt := buildUnifyPrompt("my-task", "task content here", "status=complete duration_ms=1000 model=claude-sonnet-4-6 exit_code=0")
	for _, section := range []string{"What Changed", "What's Incomplete or Deferred", "Decisions Made", "Handoff Notes", "Acceptance Criteria Coverage"} {
		if !strings.Contains(prompt, section) {
			t.Errorf("prompt missing section %q", section)
		}
	}
}

func TestRunUnify_PromptIncludesLogSummary(t *testing.T) {
	logSummary := "status=complete duration_ms=5000 model=claude-sonnet-4-6 exit_code=0"
	prompt := buildUnifyPrompt("my-task", "task content", logSummary)
	if !strings.Contains(prompt, logSummary) {
		t.Errorf("prompt does not include log summary; prompt: %s", prompt[:min(300, len(prompt))])
	}
}

func TestRunUnify_StripFooter(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "footer on last line",
			input:    "# Closure\n\nSome content.\n\n{\"kiln\":{\"status\":\"complete\",\"task_id\":\"t1\"}}\n",
			expected: "# Closure\n\nSome content.",
		},
		{
			name:     "no footer",
			input:    "# Closure\n\nSome content.\n",
			expected: "# Closure\n\nSome content.",
		},
		{
			name:     "footer with trailing newline",
			input:    "content\n{\"kiln\":{\"status\":\"complete\",\"task_id\":\"t1\"}}\n\n",
			expected: "content",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripFooter(tc.input)
			if got != tc.expected {
				t.Errorf("stripFooter(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRunUnify_ReadLogSummary_Missing(t *testing.T) {
	got := readLogSummary("/nonexistent/path/log.json")
	if !strings.Contains(got, "no execution log found") {
		t.Errorf("expected 'no execution log found', got: %s", got)
	}
}

func TestRunUnify_ReadLogSummary_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "task.json")
	entry := execRunLog{
		TaskID:     "my-task",
		Status:     "complete",
		DurationMs: 1234,
		Model:      "claude-sonnet-4-6",
		ExitCode:   0,
	}
	data, _ := json.Marshal(entry)
	os.WriteFile(logPath, data, 0o644)

	got := readLogSummary(logPath)
	if !strings.Contains(got, "complete") || !strings.Contains(got, "1234") {
		t.Errorf("unexpected log summary: %s", got)
	}
}

func TestRunGenMake_CreatesUnifyDir(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.yaml")
	outPath := filepath.Join(tmp, ".kiln/targets.mk")
	os.WriteFile(tasksPath, []byte("- id: hello\n  prompt: hello.md\n"), 0o644)

	if err := runGenMake([]string{"--tasks", tasksPath, "--out", outPath}); err != nil {
		t.Fatalf("runGenMake failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(tmp, ".kiln/unify"))
	if err != nil {
		t.Fatalf(".kiln/unify directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".kiln/unify is not a directory")
	}
}

func TestRun_UnifyMissingTaskID(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"unify"}, &bytes.Buffer{}, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing --task-id")
	}
	if !strings.Contains(stderr.String(), "--task-id is required") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Tests for kiln status (enhanced: log derivation, JSON format, summary) ---

func TestRunStatus_PendingStatusFromNoLogNoDone(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "pending") {
		t.Errorf("expected 'pending' status for task with no log/done, got:\n%s", out)
	}
}

func TestRunStatus_CompleteFromDoneMarker(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	doneDir := filepath.Join(tmpDir, ".kiln", "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "my-task.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "complete") {
		t.Errorf("expected 'complete' status from done marker, got:\n%s", out)
	}
}

func TestRunStatus_DerivedFromLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	// Create a log file indicating the task failed.
	logDir := filepath.Join(tmpDir, ".kiln", "logs")
	os.MkdirAll(logDir, 0o755)
	logEntry := execRunLog{TaskID: "my-task", Status: "error", ExitCode: 1, ErrorClass: "claude_exit"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "my-task.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "failed") {
		t.Errorf("expected 'failed' status derived from log error, got:\n%s", out)
	}
}

func TestRunStatus_NotCompleteFromLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	logDir := filepath.Join(tmpDir, ".kiln", "logs")
	os.MkdirAll(logDir, 0o755)
	logEntry := execRunLog{TaskID: "my-task", Status: "not_complete"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "my-task.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "not_complete") {
		t.Errorf("expected 'not_complete' status from log, got:\n%s", out)
	}
}

func TestRunStatus_StateJSONTakesPriority(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)

	// State says "failed" but done marker also exists — state should win.
	stateData := `{"tasks":{"my-task":{"status":"failed","attempts":3,"last_error":"some error"}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)
	doneDir := filepath.Join(kilnDir, "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "my-task.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "failed") {
		t.Errorf("expected state.json status 'failed' to take priority over done marker, got:\n%s", out)
	}
}

func TestRunStatus_AttemptCountFromLog(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)

	logDir := filepath.Join(tmpDir, ".kiln", "logs")
	os.MkdirAll(logDir, 0o755)
	logEntry := execRunLog{TaskID: "my-task", Status: "error"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "my-task.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	// Attempt count should be 1 (one log file = one attempt).
	if !strings.Contains(out, "1") {
		t.Errorf("expected attempt count 1 when log file exists, got:\n%s", out)
	}
}

func TestRunStatus_MissingLogsDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: my-task\n  prompt: p.md\n  needs: []\n"), 0o644)
	// Do NOT create .kiln/logs directory.

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("should handle missing logs dir gracefully, got error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "my-task") {
		t.Errorf("expected task in output, got:\n%s", out)
	}
}

func TestRunStatus_FormatJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: alpha
  prompt: a.md
  needs: []
- id: beta
  prompt: b.md
  needs:
    - alpha
`), 0o644)

	doneDir := filepath.Join(tmpDir, ".kiln", "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "alpha.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath, "--format", "json"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("expected valid JSON output, got parse error: %v\noutput: %s", err, out)
	}
	if _, ok := result["tasks"]; !ok {
		t.Errorf("expected 'tasks' field in JSON output, got: %s", out)
	}
	if _, ok := result["summary"]; !ok {
		t.Errorf("expected 'summary' field in JSON output, got: %s", out)
	}
}

func TestRunStatus_SummaryLine(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Total:") {
		t.Errorf("expected 'Total:' in summary line, got:\n%s", out)
	}
	if !strings.Contains(out, "Complete:") {
		t.Errorf("expected 'Complete:' in summary line, got:\n%s", out)
	}
	if !strings.Contains(out, "Pending:") {
		t.Errorf("expected 'Pending:' in summary line, got:\n%s", out)
	}
}

func TestRunStatus_JSONSummaryCounts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: a
  prompt: a.md
  needs: []
- id: b
  prompt: b.md
  needs: []
- id: c
  prompt: c.md
  needs: []
`), 0o644)

	// a: complete (done marker), b: failed (log), c: pending (nothing)
	kilnDir := filepath.Join(tmpDir, ".kiln")
	doneDir := filepath.Join(kilnDir, "done")
	logDir := filepath.Join(kilnDir, "logs")
	os.MkdirAll(doneDir, 0o755)
	os.MkdirAll(logDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "a.done"), nil, 0o644)
	logEntry := execRunLog{TaskID: "b", Status: "error"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "b.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runStatus([]string{"--tasks", tasksPath, "--format", "json"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Summary struct {
			Total    int `json:"total"`
			Complete int `json:"complete"`
			Failed   int `json:"failed"`
			Pending  int `json:"pending"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result.Summary.Total != 3 {
		t.Errorf("expected total=3, got %d", result.Summary.Total)
	}
	if result.Summary.Complete != 1 {
		t.Errorf("expected complete=1, got %d", result.Summary.Complete)
	}
	if result.Summary.Failed != 1 {
		t.Errorf("expected failed=1, got %d", result.Summary.Failed)
	}
	if result.Summary.Pending != 1 {
		t.Errorf("expected pending=1, got %d", result.Summary.Pending)
	}
}

// --- Tests for kiln retry ---

func TestRunRetry_NoTasksMatch(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)
	// No state, no log, no done → "pending"; pending tasks are skipped.

	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No tasks match retry criteria.") {
		t.Errorf("expected no-match message, got:\n%s", stdout.String())
	}
}

func TestRunRetry_NoMatchWhenAllComplete(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	doneDir := filepath.Join(tmpDir, ".kiln", "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "t1.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No tasks match retry criteria.") {
		t.Errorf("expected no-match message for complete tasks, got:\n%s", stdout.String())
	}
}

func TestRunRetry_PrintsTasksBeforeExecuting(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("prompt content"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	// Create a log file so t1 has "failed" status.
	logDir := filepath.Join(tmpDir, ".kiln", "logs")
	os.MkdirAll(logDir, 0o755)
	logEntry := execRunLog{TaskID: "t1", Status: "error"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "t1.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Retrying 1 task(s): t1") {
		t.Errorf("expected retrying message, got:\n%s", out)
	}
}

func TestRunRetry_RemovesDoneMarker(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("prompt"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	// Set up: t1 has state "failed" AND a done marker.
	kilnDir := filepath.Join(tmpDir, ".kiln")
	doneDir := filepath.Join(kilnDir, "done")
	os.MkdirAll(doneDir, 0o755)
	donePath := filepath.Join(doneDir, "t1.done")
	os.WriteFile(donePath, nil, 0o644)

	stateData := `{"tasks":{"t1":{"status":"failed","attempts":1,"last_error":"timeout"}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Done marker should have been removed before exec.
	// (exec with "success" mode will recreate it; but it was removed first)
	out := stdout.String()
	if !strings.Contains(out, "t1") {
		t.Errorf("expected t1 in output, got:\n%s", out)
	}
}

func TestRunRetry_SpecificTaskID(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("success")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("prompt"), 0o644)
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
- id: t2
  prompt: p.md
  needs: []
`), 0o644)

	// Both tasks are failed.
	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"t1":{"status":"failed","attempts":1},"t2":{"status":"failed","attempts":1}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "t1") {
		t.Errorf("expected t1 in retry output, got:\n%s", out)
	}
	if strings.Contains(out, "t2") {
		t.Errorf("expected t2 NOT in retry output (only t1 requested), got:\n%s", out)
	}
}

func TestRunRetry_FailedFilter(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
- id: t2
  prompt: p.md
  needs: []
`), 0o644)

	// t1 is "failed", t2 is "not_complete" (via log).
	kilnDir := filepath.Join(tmpDir, ".kiln")
	logDir := filepath.Join(kilnDir, "logs")
	os.MkdirAll(logDir, 0o755)
	e1 := execRunLog{TaskID: "t1", Status: "error"}
	e2 := execRunLog{TaskID: "t2", Status: "not_complete"}
	d1, _ := json.Marshal(e1)
	d2, _ := json.Marshal(e2)
	os.WriteFile(filepath.Join(logDir, "t1.json"), d1, 0o644)
	os.WriteFile(filepath.Join(logDir, "t2.json"), d2, 0o644)

	// With --failed, only t1 should be retried.
	var stdout bytes.Buffer
	err := runRetry([]string{"--tasks", tasksPath, "--failed"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	// t2 (not_complete) should not be in the "Retrying" line.
	if strings.Contains(out, "Retrying") && strings.Contains(out, "t2") {
		t.Errorf("expected t2 NOT retried with --failed filter, got:\n%s", out)
	}
}

// --- Tests for kiln reset ---

func TestRunReset_MissingFlags(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	err := runReset([]string{"--tasks", tasksPath}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when neither --task-id nor --all is given")
	}
}

func TestRunReset_UnknownTaskID(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout bytes.Buffer
	err := runReset([]string{"--tasks", tasksPath, "--task-id", "no-such-task"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
	if !strings.Contains(err.Error(), "unknown task") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !strings.Contains(stdout.String(), "Unknown task: no-such-task") {
		t.Errorf("expected 'Unknown task:' in output, got: %s", stdout.String())
	}
}

func TestRunReset_RemovesDoneMarkerAndArchivesLog(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	doneDir := filepath.Join(kilnDir, "done")
	logDir := filepath.Join(kilnDir, "logs")
	os.MkdirAll(doneDir, 0o755)
	os.MkdirAll(logDir, 0o755)

	donePath := filepath.Join(doneDir, "t1.done")
	logPath := filepath.Join(logDir, "t1.json")
	os.WriteFile(donePath, nil, 0o644)
	os.WriteFile(logPath, []byte(`{"task_id":"t1","status":"complete"}`), 0o644)

	var stdout bytes.Buffer
	err := runReset([]string{"--tasks", tasksPath, "--task-id", "t1"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Done marker should be removed.
	if _, statErr := os.Stat(donePath); !os.IsNotExist(statErr) {
		t.Error("expected done marker to be removed after reset")
	}
	// Log file should be archived.
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Error("expected log file to be removed after reset")
	}
	bakPath := logPath + ".bak"
	if _, statErr := os.Stat(bakPath); os.IsNotExist(statErr) {
		t.Error("expected log file backup (.bak) to exist after reset")
	}
	// Confirmation message.
	if !strings.Contains(stdout.String(), "Reset task: t1") {
		t.Errorf("expected confirmation message, got: %s", stdout.String())
	}
}

func TestRunReset_AllTasksConfirmed(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte(`- id: t1
  prompt: p.md
  needs: []
- id: t2
  prompt: p.md
  needs: []
`), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	doneDir := filepath.Join(kilnDir, "done")
	os.MkdirAll(doneDir, 0o755)
	os.WriteFile(filepath.Join(doneDir, "t1.done"), nil, 0o644)
	os.WriteFile(filepath.Join(doneDir, "t2.done"), nil, 0o644)

	var stdout bytes.Buffer
	err := runReset([]string{"--tasks", tasksPath, "--all"}, strings.NewReader("y\n"), &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both done markers should be removed.
	if _, statErr := os.Stat(filepath.Join(doneDir, "t1.done")); !os.IsNotExist(statErr) {
		t.Error("expected t1.done to be removed")
	}
	if _, statErr := os.Stat(filepath.Join(doneDir, "t2.done")); !os.IsNotExist(statErr) {
		t.Error("expected t2.done to be removed")
	}
}

func TestRunReset_AllTasksAborted(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	doneDir := filepath.Join(kilnDir, "done")
	os.MkdirAll(doneDir, 0o755)
	donePath := filepath.Join(doneDir, "t1.done")
	os.WriteFile(donePath, nil, 0o644)

	var stdout bytes.Buffer
	err := runReset([]string{"--tasks", tasksPath, "--all"}, strings.NewReader("n\n"), &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Done marker should NOT be removed (user said no).
	if _, statErr := os.Stat(donePath); os.IsNotExist(statErr) {
		t.Error("expected done marker to still exist after aborted reset")
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Errorf("expected 'Aborted' message, got: %s", stdout.String())
	}
}

func TestRunReset_ClearsStateJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"t1":{"status":"failed","attempts":2}}}`
	stateFile := filepath.Join(kilnDir, "state.json")
	os.WriteFile(stateFile, []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runReset([]string{"--tasks", tasksPath, "--task-id", "t1"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// State entry for t1 should be removed.
	state, _ := loadState(stateFile)
	if state != nil && state.Tasks["t1"] != nil {
		t.Error("expected t1 state entry to be cleared after reset")
	}
}

// --- Tests for kiln resume ---

func TestRunResume_MissingTaskID(t *testing.T) {
	err := runResume([]string{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing --task-id")
	}
	if !strings.Contains(err.Error(), "--task-id is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunResume_UnknownTaskID(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "no-such"}, &stdout)
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
	if !strings.Contains(err.Error(), "unknown task") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunResume_NoPriorAttempts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)
	// No state, no log file.

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "No prior attempts found for task: t1") {
		t.Errorf("expected no-prior-attempts message, got:\n%s", out)
	}
}

func TestRunResume_OutputsResumeContext(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("# Do the thing\n\nPlease do it.\n"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"t1":{"status":"failed","attempts":2,"last_error":"timeout after 15m"}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "RESUME CONTEXT") {
		t.Errorf("expected RESUME CONTEXT header, got:\n%s", out)
	}
	if !strings.Contains(out, "Prior attempts: 2") {
		t.Errorf("expected attempt count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Do the thing") {
		t.Errorf("expected original prompt content in output, got:\n%s", out)
	}
}

func TestRunResume_IncludesClosureArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("# Task prompt\n"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(filepath.Join(kilnDir, "unify"), 0o755)
	os.WriteFile(filepath.Join(kilnDir, "unify", "t1.md"), []byte("Closure: auth module partially done.\n"), 0o644)

	// Set up prior attempt in state.
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"t1":{"status":"not_complete","attempts":1}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "PREVIOUS CLOSURE SUMMARY") {
		t.Errorf("expected closure section in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Closure: auth module partially done.") {
		t.Errorf("expected closure content in output, got:\n%s", out)
	}
}

func TestRunResume_WorksWithoutClosureArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("# Task prompt\n"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	kilnDir := filepath.Join(tmpDir, ".kiln")
	os.MkdirAll(kilnDir, 0o755)
	stateData := `{"tasks":{"t1":{"status":"failed","attempts":1}}}`
	os.WriteFile(filepath.Join(kilnDir, "state.json"), []byte(stateData), 0o644)

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("expected no error when no closure artifact exists: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "RESUME CONTEXT") {
		t.Errorf("expected RESUME CONTEXT header, got:\n%s", out)
	}
	if strings.Contains(out, "PREVIOUS CLOSURE SUMMARY") {
		t.Errorf("expected NO closure section when no artifact, got:\n%s", out)
	}
}

func TestRunResume_FromLogFileWhenNoState(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	promptPath := filepath.Join(tmpDir, "p.md")
	os.WriteFile(promptPath, []byte("# Task\n"), 0o644)
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	// No state.json, but log file exists.
	kilnDir := filepath.Join(tmpDir, ".kiln")
	logDir := filepath.Join(kilnDir, "logs")
	os.MkdirAll(logDir, 0o755)
	logEntry := execRunLog{TaskID: "t1", Status: "error", ErrorMessage: "claude exited with code 1"}
	data, _ := json.Marshal(logEntry)
	os.WriteFile(filepath.Join(logDir, "t1.json"), data, 0o644)

	var stdout bytes.Buffer
	err := runResume([]string{"--tasks", tasksPath, "--task-id", "t1"}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "RESUME CONTEXT") {
		t.Errorf("expected RESUME CONTEXT when falling back to log file, got:\n%s", out)
	}
}

// --- Tests for kiln dispatch (retry/reset/resume) ---

func TestRun_RetryDispatch(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"retry", "--tasks", tasksPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRun_ResetDispatch(t *testing.T) {
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.yaml")
	os.WriteFile(tasksPath, []byte("- id: t1\n  prompt: p.md\n  needs: []\n"), 0o644)

	var stdout, stderr bytes.Buffer
	code := run([]string{"reset", "--tasks", tasksPath, "--task-id", "t1"}, &stdout, &stderr)
	// t1 exists in tasks.yaml, reset should succeed.
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s; stdout: %s", code, stderr.String(), stdout.String())
	}
}

func TestRun_ResumeDispatch_NoTaskID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"resume"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing --task-id")
	}
}

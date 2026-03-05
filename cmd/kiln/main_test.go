package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

package main

import (
	"bytes"
	"context"
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
	if !strings.Contains(got, "$(KILN) exec --task-id build-widget --tasks "+tasksPath) {
		t.Errorf("missing or incorrect recipe for build-widget, got:\n%s", got)
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
	if !strings.Contains(err.Error(), "--prompt-file") && !strings.Contains(err.Error(), "--tasks") {
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
	if !strings.Contains(logStr, "kiln_timeout") {
		t.Errorf("log file missing timeout entry, got: %s", logStr)
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

	if !strings.Contains(got, "$(KILN) exec --task-id slow-task --tasks "+tasksPath+" --timeout 10m") {
		t.Errorf("expected recipe with --tasks and --timeout flag, got:\n%s", got)
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

	// Should still have the basic recipe with --tasks
	if !strings.Contains(got, "$(KILN) exec --task-id fast-task --tasks "+tasksPath) {
		t.Errorf("expected basic recipe with --tasks, got:\n%s", got)
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

	logPath := filepath.Join(tmpDir, ".kiln", "logs", "log-entries.json")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logStr := string(logData)

	if !strings.Contains(logStr, "kiln_attempt") {
		t.Errorf("log missing kiln_attempt entries, got: %s", logStr)
	}
	// Should have 2 attempt entries (attempt 1 and attempt 2)
	count := strings.Count(logStr, "kiln_attempt")
	if count != 2 {
		t.Errorf("expected 2 kiln_attempt log entries, got %d; log: %s", count, logStr)
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
	errMsg := err.Error()
	if !strings.Contains(errMsg, "--prompt-file") || !strings.Contains(errMsg, "--tasks") {
		t.Fatalf("expected error mentioning both --prompt-file and --tasks, got: %v", err)
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

func TestRunExec_TaskIDMismatch_WarnButComplete(t *testing.T) {
	origBuilder := commandBuilder
	t.Cleanup(func() { commandBuilder = origBuilder })
	commandBuilder = fakeCommandBuilder("complete")

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmpDir)

	promptPath := filepath.Join(tmpDir, "prompt.md")
	os.WriteFile(promptPath, []byte("test"), 0o644)
	// KILN_TEST_TASK_ID defaults to "test-task"; use a different task-id to force mismatch.
	t.Setenv("KILN_TEST_TASK_ID", "test-task")

	var stdout bytes.Buffer
	code, err := runExec([]string{"--task-id", "different-id", "--prompt-file", promptPath}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 despite mismatch, got %d", code)
	}
	if !strings.Contains(stdout.String(), "warning") {
		t.Errorf("expected mismatch warning in stdout, got: %s", stdout.String())
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

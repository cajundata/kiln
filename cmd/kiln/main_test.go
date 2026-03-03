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
	if !strings.Contains(got, "$(KILN) exec --task-id build-widget --prompt-file .kiln/prompts/tasks/build-widget.md && touch $@") {
		t.Error("missing or incorrect recipe for build-widget")
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

	switch mode {
	case "success":
		// Echo the prompt back as fake stream-json output
		args := os.Args
		for i, a := range args {
			if a == "--" {
				if i+1 < len(args) {
					os.Stdout.WriteString(`{"type":"assistant","content":"` + args[i+1] + `"}` + "\n")
				}
				break
			}
		}
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
	err := runExec([]string{"--prompt-file", "some.md"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing --task-id")
	}
	if !strings.Contains(err.Error(), "--task-id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_MissingPromptFile(t *testing.T) {
	err := runExec([]string{"--task-id", "t1"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing --prompt-file")
	}
	if !strings.Contains(err.Error(), "--prompt-file is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExec_PromptFileNotFound(t *testing.T) {
	err := runExec([]string{
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

	var stdout bytes.Buffer
	err := runExec([]string{
		"--task-id", "test-01",
		"--prompt-file", promptPath,
	}, &stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	err := runExec([]string{
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

	err := runExec([]string{
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
	err := runExec([]string{}, &bytes.Buffer{})
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

	var stdout, stderr bytes.Buffer
	code := run([]string{"exec", "--task-id", "run-01", "--prompt-file", promptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunExec_InvalidFlag(t *testing.T) {
	err := runExec([]string{"--bogus-flag"}, &bytes.Buffer{})
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

	err := runExec([]string{
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

	runExec([]string{
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

	err := runExec([]string{
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
	got := resolveModel("flag-model")
	if got != "flag-model" {
		t.Errorf("expected flag-model, got %s", got)
	}
}

func TestResolveModel_EnvFallback(t *testing.T) {
	t.Setenv("KILN_MODEL", "env-model")
	got := resolveModel("")
	if got != "env-model" {
		t.Errorf("expected env-model, got %s", got)
	}
}

func TestResolveModel_DefaultFallback(t *testing.T) {
	t.Setenv("KILN_MODEL", "")
	got := resolveModel("")
	if got != defaultModel {
		t.Errorf("expected %s, got %s", defaultModel, got)
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

	err := runExec([]string{
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

	err := runExec([]string{
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

	err := runExec([]string{
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

	err := runExec([]string{
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

	err := runExec([]string{
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

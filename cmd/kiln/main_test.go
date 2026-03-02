package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCommandBuilder returns a commandBuilder that runs a helper process
// instead of the real claude binary. The helper process behaviour is
// controlled by the env var KILN_TEST_HELPER_MODE.
func fakeCommandBuilder(mode string) func(string) *exec.Cmd {
	return func(prompt string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", prompt)
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

func TestRun_GenMakeStub(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"gen-make"}, &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "gen-make not implemented yet") {
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

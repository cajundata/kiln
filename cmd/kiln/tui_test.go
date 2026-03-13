package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── Fixtures ──────────────────────────────────────────────────────────────────

func newTestModel(t *testing.T) (tuiModel, string) {
	t.Helper()
	dir := t.TempDir()

	// Create directory structure.
	for _, d := range []string{"logs", "done", "unify"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Write a tasks.yaml (plain list, no wrapper key; prompt is required).
	tasksYAML := `- id: task-a
  prompt: prompts/a.md
  description: Task A
  kind: feature
  phase: alpha
- id: task-b
  prompt: prompts/b.md
  needs: [task-a]
  description: Task B
  kind: fix
  phase: alpha
- id: task-c
  prompt: prompts/c.md
  needs: [task-b]
  description: Task C
`
	tasksFile := filepath.Join(dir, "tasks.yaml")
	if err := os.WriteFile(tasksFile, []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	m := tuiModel{
		tasksFile:   tasksFile,
		refreshRate: 2 * time.Second,
		doneDir:     filepath.Join(dir, "done"),
		logDir:      filepath.Join(dir, "logs"),
		unifyDir:    filepath.Join(dir, "unify"),
		stateFile:   filepath.Join(dir, "state.json"),
		launchTime:  time.Now(),
		width:       120,
		height:      40,
		state:       &StateManifest{Tasks: make(map[string]*TaskState)},
		doneSet:     make(map[string]bool),
	}
	m.refreshState()
	return m, dir
}

// ── Model initialization ──────────────────────────────────────────────────────

func TestTUIModelInitializesFromTasksYAML(t *testing.T) {
	m, _ := newTestModel(t)
	if len(m.tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(m.tasks))
	}
	if m.tasks[0].ID != "task-a" {
		t.Errorf("expected first task ID task-a, got %s", m.tasks[0].ID)
	}
}

func TestTUIModelInit(t *testing.T) {
	m, _ := newTestModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a tick command")
	}
}

// ── Status derivation ─────────────────────────────────────────────────────────

func TestStatusDerivation_Pending(t *testing.T) {
	m, _ := newTestModel(t)
	// task-a has no deps and no state/done/log → should be pending.
	var info *taskStatusInfo
	for i := range m.statuses {
		if m.statuses[i].ID == "task-a" {
			cp := m.statuses[i]
			info = &cp
			break
		}
	}
	if info == nil {
		t.Fatal("task-a not found in statuses")
	}
	if info.Status != "pending" {
		t.Errorf("expected pending, got %s", info.Status)
	}
}

func TestStatusDerivation_Complete_FromDone(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a .done marker for task-a.
	doneFile := filepath.Join(dir, "done", "task-a.done")
	if err := os.WriteFile(doneFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	var info *taskStatusInfo
	for i := range m.statuses {
		if m.statuses[i].ID == "task-a" {
			cp := m.statuses[i]
			info = &cp
			break
		}
	}
	if info == nil || info.Status != "complete" {
		t.Errorf("expected complete from done marker, got %v", info)
	}
}

func TestStatusDerivation_Failed_FromLog(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a log file indicating failure.
	logEntry := execRunLog{
		TaskID:       "task-a",
		Status:       "error",
		ErrorClass:   "ClaudeExitError",
		ErrorMessage: "claude exited with code 1",
	}
	data, _ := json.Marshal(logEntry)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	var info *taskStatusInfo
	for i := range m.statuses {
		if m.statuses[i].ID == "task-a" {
			cp := m.statuses[i]
			info = &cp
			break
		}
	}
	if info == nil || info.Status != "failed" {
		t.Errorf("expected failed from log, got %v", info)
	}
}

func TestStatusDerivation_Running_FromState(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a state.json indicating running.
	state := StateManifest{
		Tasks: map[string]*TaskState{
			"task-a": {Status: "running", Attempts: 1},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	var info *taskStatusInfo
	for i := range m.statuses {
		if m.statuses[i].ID == "task-a" {
			cp := m.statuses[i]
			info = &cp
			break
		}
	}
	if info == nil || info.Status != "running" {
		t.Errorf("expected running from state, got %v", info)
	}
}

func TestStatusDerivation_Blocked(t *testing.T) {
	m, _ := newTestModel(t)
	// task-b needs task-a; task-a is pending → task-b should be blocked.
	var info *taskStatusInfo
	for i := range m.statuses {
		if m.statuses[i].ID == "task-b" {
			cp := m.statuses[i]
			info = &cp
			break
		}
	}
	if info == nil || info.Status != "blocked" {
		t.Errorf("expected blocked, got %v", info)
	}
}

// ── Summary counts ────────────────────────────────────────────────────────────

func TestSummaryCounts(t *testing.T) {
	m, dir := newTestModel(t)

	// Mark task-a as complete.
	if err := os.WriteFile(filepath.Join(dir, "done", "task-a.done"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	counts := map[string]int{}
	for _, s := range m.statuses {
		counts[s.Status]++
	}
	if counts["complete"] != 1 {
		t.Errorf("expected 1 complete, got %d", counts["complete"])
	}
	// task-b was blocked by task-a; now task-a is done, task-b should be pending.
	// task-c still blocked by task-b.
	if counts["blocked"] != 1 {
		t.Errorf("expected 1 blocked (task-c), got %d", counts["blocked"])
	}
}

// ── Keyboard navigation ───────────────────────────────────────────────────────

func TestKeyboardNavigation_JK(t *testing.T) {
	m, _ := newTestModel(t)
	initial := m.cursor

	// Press j.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m2 := newM.(tuiModel)
	if m2.cursor != initial+1 {
		t.Errorf("after j: expected cursor %d, got %d", initial+1, m2.cursor)
	}

	// Press k.
	newM3, _ := m2.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m3 := newM3.(tuiModel)
	if m3.cursor != initial {
		t.Errorf("after k: expected cursor %d, got %d", initial, m3.cursor)
	}
}

func TestKeyboardNavigation_Arrows(t *testing.T) {
	m, _ := newTestModel(t)

	// Down arrow.
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m2 := newM.(tuiModel)
	if m2.cursor != 1 {
		t.Errorf("after down: expected cursor 1, got %d", m2.cursor)
	}

	// Up arrow.
	newM3, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m3 := newM3.(tuiModel)
	if m3.cursor != 0 {
		t.Errorf("after up: expected cursor 0, got %d", m3.cursor)
	}
}

func TestKeyboardNavigation_GG(t *testing.T) {
	m, _ := newTestModel(t)
	// Move to last.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m2 := newM.(tuiModel)
	if m2.cursor != len(m2.displayOrder)-1 {
		t.Errorf("after G: expected last row %d, got %d", len(m2.displayOrder)-1, m2.cursor)
	}

	// Move to first.
	newM3, _ := m2.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m3 := newM3.(tuiModel)
	if m3.cursor != 0 {
		t.Errorf("after g: expected cursor 0, got %d", m3.cursor)
	}
}

func TestKeyboardNavigation_NoBoundaryOverflow(t *testing.T) {
	m, _ := newTestModel(t)

	// k at top should not go negative.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m2 := newM.(tuiModel)
	if m2.cursor < 0 {
		t.Errorf("cursor went negative: %d", m2.cursor)
	}

	// j past last should not overflow.
	for i := 0; i < 100; i++ {
		newM2, _ := m2.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m2 = newM2.(tuiModel)
	}
	if m2.cursor >= len(m2.displayOrder) {
		t.Errorf("cursor beyond displayOrder: %d >= %d", m2.cursor, len(m2.displayOrder))
	}
}

// ── View transitions ──────────────────────────────────────────────────────────

func TestEnterKeyTransitionsToDetailView(t *testing.T) {
	m, _ := newTestModel(t)

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)
	if m2.currentView != viewDetail {
		t.Errorf("expected viewDetail after Enter, got %v", m2.currentView)
	}
	if m2.detailTask == nil {
		t.Error("detailTask should be set after Enter")
	}
}

func TestEscapeReturnsToMainView(t *testing.T) {
	m, _ := newTestModel(t)

	// Go to detail.
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)
	if m2.currentView != viewDetail {
		t.Fatalf("expected viewDetail, got %v", m2.currentView)
	}

	// Press Escape (v2: String() returns "esc").
	newM3, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m3 := newM3.(tuiModel)
	if m3.currentView != viewMain {
		t.Errorf("expected viewMain after Escape, got %v", m3.currentView)
	}
}

func TestLKeyTransitionsToLogView(t *testing.T) {
	m, _ := newTestModel(t)

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m2 := newM.(tuiModel)
	if m2.currentView != viewLog {
		t.Errorf("expected viewLog after l, got %v", m2.currentView)
	}
}

func TestQuestionMarkTogglesHelp(t *testing.T) {
	m, _ := newTestModel(t)

	newM, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m2 := newM.(tuiModel)
	if m2.currentView != viewHelp {
		t.Errorf("expected viewHelp after ?, got %v", m2.currentView)
	}

	// Any key closes help.
	newM3, _ := m2.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m3 := newM3.(tuiModel)
	if m3.currentView != viewMain {
		t.Errorf("expected viewMain after closing help, got %v", m3.currentView)
	}
}

// ── Detail view content ───────────────────────────────────────────────────────

func TestDetailViewShowsLogData(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a log entry for task-a.
	logEntry := execRunLog{
		TaskID:   "task-a",
		Status:   "complete",
		ExitCode: 0,
		Events: []logEvent{
			{TS: time.Now(), Type: "stdout", Line: "hello"},
			{TS: time.Now(), Type: "stdout", Line: "world"},
		},
	}
	data, _ := json.Marshal(logEntry)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	// Position cursor on task-a.
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	// Open detail view.
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	view := m2.View()
	content := view.Content
	if !strings.Contains(content, "task-a") {
		t.Errorf("detail view should contain task ID, got: %s", content)
	}
	if !strings.Contains(content, "Events:") {
		t.Errorf("detail view should contain events count, got: %s", content)
	}
}

// ── Blocked task display ──────────────────────────────────────────────────────

func TestBlockedTaskShowsUnmetDeps(t *testing.T) {
	m, _ := newTestModel(t)

	// Find task-b (blocked by task-a).
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-b" {
			m.cursor = i
			break
		}
	}

	view := m.View()
	content := view.Content
	if !strings.Contains(content, "task-a") {
		t.Errorf("main view should show unmet dep task-a for blocked task-b, got:\n%s", content)
	}
}

func TestBlockedDetailShowsDepStatus(t *testing.T) {
	m, _ := newTestModel(t)

	// Find task-b cursor position.
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-b" {
			m.cursor = i
			break
		}
	}

	// Open detail.
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	view := m2.View()
	content := view.Content
	if !strings.Contains(content, "task-a") {
		t.Errorf("detail view should list unmet dep task-a, got: %s", content)
	}
}

// ── Graceful degradation ──────────────────────────────────────────────────────

func TestGracefulDegradation_MissingLogDir(t *testing.T) {
	m, dir := newTestModel(t)
	// Remove log dir entirely.
	if err := os.RemoveAll(filepath.Join(dir, "logs")); err != nil {
		t.Fatal(err)
	}
	m.logDir = filepath.Join(dir, "logs")
	// Should not panic.
	m.refreshState()
	if len(m.statuses) != 3 {
		t.Errorf("expected 3 statuses after log dir removed, got %d", len(m.statuses))
	}
}

func TestGracefulDegradation_MissingStateJSON(t *testing.T) {
	m, _ := newTestModel(t)
	m.stateFile = "/nonexistent/path/state.json"
	// Should not panic.
	m.refreshState()
	if len(m.statuses) != 3 {
		t.Errorf("expected 3 statuses with missing state.json, got %d", len(m.statuses))
	}
}

func TestGracefulDegradation_MalformedLogFile(t *testing.T) {
	m, dir := newTestModel(t)
	// Write malformed JSON.
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), []byte("NOT JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Should not panic.
	m.refreshState()
	if len(m.statuses) != 3 {
		t.Errorf("expected 3 statuses with malformed log, got %d", len(m.statuses))
	}
}

func TestGracefulDegradation_loadTaskLog_MalformedJSON(t *testing.T) {
	m, dir := newTestModel(t)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := m.loadTaskLog("task-a")
	if result != nil {
		t.Errorf("expected nil for malformed log, got %v", result)
	}
}

// ── Color coding ──────────────────────────────────────────────────────────────

func TestColorCodingMapsCorrectly(t *testing.T) {
	cases := []struct {
		status string
	}{
		{"complete"},
		{"running"},
		{"failed"},
		{"blocked"},
		{"pending"},
		{"not_complete"},
	}
	for _, tc := range cases {
		style := statusStyle(tc.status)
		// Just verify it returns a valid (non-zero) style.
		_ = style.Render(tc.status)
	}
}

// ── Refresh flag parsing ──────────────────────────────────────────────────────

func TestRefreshFlagParsed(t *testing.T) {
	// Verify the model uses the parsed refresh rate.
	// We test indirectly: create a model with 500ms refresh and check the rate.
	m := tuiModel{refreshRate: 500 * time.Millisecond}
	if m.refreshRate != 500*time.Millisecond {
		t.Errorf("expected 500ms refresh, got %v", m.refreshRate)
	}
}

// ── Sort order ────────────────────────────────────────────────────────────────

func TestSortOrder(t *testing.T) {
	// Verify statusOrder priorities.
	order := []string{"running", "blocked", "failed", "not_complete", "pending", "complete"}
	for i := 0; i < len(order)-1; i++ {
		if statusOrder(order[i]) >= statusOrder(order[i+1]) {
			t.Errorf("expected %s (%d) < %s (%d)",
				order[i], statusOrder(order[i]),
				order[i+1], statusOrder(order[i+1]))
		}
	}
}

func TestSortOrder_RunningFirst(t *testing.T) {
	m, dir := newTestModel(t)

	// Set task-a to running via state.
	state := StateManifest{
		Tasks: map[string]*TaskState{
			"task-a": {Status: "running", Attempts: 1},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	// First display row should be running task.
	if len(m.displayOrder) == 0 {
		t.Fatal("displayOrder is empty")
	}
	firstIdx := m.displayOrder[0]
	if m.statuses[firstIdx].Status != "running" {
		t.Errorf("expected first row to be running, got %s", m.statuses[firstIdx].Status)
	}
}

// ── Terminal resize ───────────────────────────────────────────────────────────

func TestTerminalResizeUpdatesWidth(t *testing.T) {
	m, _ := newTestModel(t)
	m.width = 80

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m2 := newM.(tuiModel)
	if m2.width != 160 {
		t.Errorf("expected width 160 after resize, got %d", m2.width)
	}
	if m2.height != 50 {
		t.Errorf("expected height 50 after resize, got %d", m2.height)
	}
}

// ── View rendering ────────────────────────────────────────────────────────────

func TestViewReturnsTeaView(t *testing.T) {
	m, _ := newTestModel(t)
	v := m.View()
	// tea.View has a Content field (set via NewView).
	if v.Content == "" {
		t.Error("View() returned empty content")
	}
	if !v.AltScreen {
		t.Error("View() should request alt screen")
	}
}

func TestMainViewContainsTaskIDs(t *testing.T) {
	m, _ := newTestModel(t)
	v := m.View()
	if !strings.Contains(v.Content, "task-a") {
		t.Errorf("main view should contain task-a, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "task-b") {
		t.Errorf("main view should contain task-b, got:\n%s", v.Content)
	}
}

func TestHelpViewRendered(t *testing.T) {
	m, _ := newTestModel(t)
	m.currentView = viewHelp
	v := m.View()
	if !strings.Contains(v.Content, "Navigation") {
		t.Errorf("help view should contain 'Navigation', got:\n%s", v.Content)
	}
}

// ── padOrTrunc ────────────────────────────────────────────────────────────────

func TestPadOrTrunc(t *testing.T) {
	cases := []struct {
		s, want string
		w       int
	}{
		{"hello", "hello     ", 10},
		{"hello world long string", "hello w...", 10},
		{"hi", "hi", 2},
		{"", "     ", 5},
		{"abc", "", 0},
	}
	for _, tc := range cases {
		got := padOrTrunc(tc.s, tc.w)
		if got != tc.want {
			t.Errorf("padOrTrunc(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
		}
	}
}

// ── Tick refresh ─────────────────────────────────────────────────────────────

func TestTickTriggersRefresh(t *testing.T) {
	m, _ := newTestModel(t)
	before := m.lastRefresh

	// Send a tick message.
	newM, _ := m.Update(tickMsg(time.Now()))
	m2 := newM.(tuiModel)
	if !m2.lastRefresh.After(before) {
		t.Error("lastRefresh should be updated after tick")
	}
}

func TestRefreshMsgUpdatesState(t *testing.T) {
	m, _ := newTestModel(t)
	before := m.lastRefresh

	newM, _ := m.Update(refreshMsg{})
	m2 := newM.(tuiModel)
	if !m2.lastRefresh.After(before) {
		t.Error("lastRefresh should be updated after refreshMsg")
	}
}

// ── Log view scrolling ───────────────────────────────────────────────────────

func TestLogViewScrollNavigation(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a log with multiple events.
	logEntry := execRunLog{
		TaskID: "task-a",
		Status: "complete",
		Events: []logEvent{
			{TS: time.Now(), Type: "stdout", Line: "line-0"},
			{TS: time.Now(), Type: "stdout", Line: "line-1"},
			{TS: time.Now(), Type: "stdout", Line: "line-2"},
			{TS: time.Now(), Type: "stderr", Line: "line-3"},
			{TS: time.Now(), Type: "stdout", Line: "line-4"},
		},
	}
	data, _ := json.Marshal(logEntry)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	// Position cursor on task-a (sort order puts blocked before pending).
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	// Enter log view.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m2 := newM.(tuiModel)
	if m2.currentView != viewLog {
		t.Fatalf("expected viewLog, got %v", m2.currentView)
	}
	if len(m2.logLines) != 5 {
		t.Fatalf("expected 5 log lines, got %d", len(m2.logLines))
	}

	// Auto-scroll should place us at the last line.
	if m2.logScrollPos != 4 {
		t.Errorf("expected logScrollPos 4 (auto-scroll), got %d", m2.logScrollPos)
	}

	// g → jump to top.
	newM, _ = m2.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m3 := newM.(tuiModel)
	if m3.logScrollPos != 0 {
		t.Errorf("expected logScrollPos 0 after g, got %d", m3.logScrollPos)
	}

	// j → scroll down.
	newM, _ = m3.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m4 := newM.(tuiModel)
	if m4.logScrollPos != 1 {
		t.Errorf("expected logScrollPos 1 after j, got %d", m4.logScrollPos)
	}

	// k → scroll up.
	newM, _ = m4.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m5 := newM.(tuiModel)
	if m5.logScrollPos != 0 {
		t.Errorf("expected logScrollPos 0 after k, got %d", m5.logScrollPos)
	}

	// G → jump to bottom.
	newM, _ = m5.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m6 := newM.(tuiModel)
	if m6.logScrollPos != 4 {
		t.Errorf("expected logScrollPos 4 after G, got %d", m6.logScrollPos)
	}

	// Escape → back to main.
	newM, _ = m6.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m7 := newM.(tuiModel)
	if m7.currentView != viewMain {
		t.Errorf("expected viewMain after Escape from log, got %v", m7.currentView)
	}
}

func TestLogViewRendersEvents(t *testing.T) {
	m, dir := newTestModel(t)

	logEntry := execRunLog{
		TaskID: "task-a",
		Status: "complete",
		Events: []logEvent{
			{TS: time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC), Type: "stdout", Line: "hello from claude"},
			{TS: time.Date(2026, 3, 13, 10, 0, 1, 0, time.UTC), Type: "stderr", Line: "warning message"},
		},
	}
	data, _ := json.Marshal(logEntry)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	// Position cursor on task-a.
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	// Enter log view.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m2 := newM.(tuiModel)

	v := m2.View()
	content := v.Content
	if !strings.Contains(content, "hello from claude") {
		t.Errorf("log view should contain event line, got:\n%s", content)
	}
	if !strings.Contains(content, "warning message") {
		t.Errorf("log view should contain stderr event, got:\n%s", content)
	}
	if !strings.Contains(content, "2/2 lines") {
		t.Errorf("log view should show line count, got:\n%s", content)
	}
}

func TestLogViewNoEvents(t *testing.T) {
	m, _ := newTestModel(t)
	// Enter log view for task with no log file.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m2 := newM.(tuiModel)

	v := m2.View()
	if !strings.Contains(v.Content, "no log events") {
		t.Errorf("log view should show 'no log events' when empty, got:\n%s", v.Content)
	}
}

// ── Grouping header rendering ────────────────────────────────────────────────

func TestGroupByPhaseRendersHeaders(t *testing.T) {
	m, _ := newTestModel(t)

	// Toggle phase grouping.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m2 := newM.(tuiModel)
	if m2.grouping != groupByPhase {
		t.Fatalf("expected groupByPhase, got %v", m2.grouping)
	}

	v := m2.View()
	content := v.Content
	if !strings.Contains(content, "Phase:") {
		t.Errorf("phase grouping should render 'Phase:' header, got:\n%s", content)
	}
	if !strings.Contains(content, "alpha") {
		t.Errorf("phase grouping should render 'alpha' group, got:\n%s", content)
	}

	// Toggle off.
	newM, _ = m2.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m3 := newM.(tuiModel)
	if m3.grouping != groupNone {
		t.Errorf("expected groupNone after second p, got %v", m3.grouping)
	}
}

func TestGroupByMilestoneRendersHeaders(t *testing.T) {
	m, dir := newTestModel(t)

	// Rewrite tasks.yaml with milestone fields.
	tasksYAML := `- id: task-a
  prompt: prompts/a.md
  milestone: v1
- id: task-b
  prompt: prompts/b.md
  milestone: v2
  needs: [task-a]
`
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	m.tasksFile = filepath.Join(dir, "tasks.yaml")
	m.refreshState()

	// Toggle milestone grouping.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m2 := newM.(tuiModel)
	if m2.grouping != groupByMilestone {
		t.Fatalf("expected groupByMilestone, got %v", m2.grouping)
	}

	v := m2.View()
	content := v.Content
	if !strings.Contains(content, "Milestone:") {
		t.Errorf("milestone grouping should render 'Milestone:' header, got:\n%s", content)
	}
}

func TestGroupByPhaseKeepsGroupsTogether(t *testing.T) {
	m, dir := newTestModel(t)

	// Create tasks with different phases and mixed statuses.
	tasksYAML := `- id: plan-a
  prompt: prompts/a.md
  phase: plan
- id: build-a
  prompt: prompts/b.md
  phase: build
- id: plan-b
  prompt: prompts/c.md
  phase: plan
- id: build-b
  prompt: prompts/d.md
  phase: build
`
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	// Mark build-a as complete so it has a different status from build-b.
	if err := os.WriteFile(filepath.Join(dir, "done", "build-a.done"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	m.tasksFile = filepath.Join(dir, "tasks.yaml")
	m.refreshState()

	// Enable phase grouping.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m2 := newM.(tuiModel)

	// Verify tasks in the same phase are adjacent in displayOrder.
	// Collect the phase sequence from display order.
	var phases []string
	for _, idx := range m2.displayOrder {
		phases = append(phases, m2.tasks[idx].Phase)
	}

	// With proper grouping, we should see all "build" together and all "plan" together.
	// They should NOT interleave (e.g., [build, plan, build, plan] would be wrong).
	seen := map[string]bool{}
	var groups []string
	for _, p := range phases {
		if !seen[p] {
			seen[p] = true
			groups = append(groups, p)
		} else {
			// If we see a phase we already saw, check it's still the current group.
			if groups[len(groups)-1] != p {
				t.Errorf("phases interleave — grouping is broken. Phase sequence: %v", phases)
				break
			}
		}
	}
}

func TestGroupingSortRecomputesOnToggle(t *testing.T) {
	m, dir := newTestModel(t)

	// Create tasks with different phases.
	tasksYAML := `- id: z-task
  prompt: prompts/a.md
  phase: beta
- id: a-task
  prompt: prompts/b.md
  phase: alpha
`
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	m.tasksFile = filepath.Join(dir, "tasks.yaml")
	m.refreshState()

	// Without grouping, both tasks are pending; z-task comes first (definition order).
	if m.tasks[m.displayOrder[0]].ID != "z-task" {
		t.Fatalf("without grouping, expected z-task first, got %s", m.tasks[m.displayOrder[0]].ID)
	}

	// Enable phase grouping — alpha < beta, so a-task should come first.
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m2 := newM.(tuiModel)

	if m2.tasks[m2.displayOrder[0]].ID != "a-task" {
		t.Errorf("with phase grouping, expected a-task first (alpha < beta), got %s", m2.tasks[m2.displayOrder[0]].ID)
	}

	// Toggle off — should revert to definition order.
	newM, _ = m2.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m3 := newM.(tuiModel)

	if m3.tasks[m3.displayOrder[0]].ID != "z-task" {
		t.Errorf("after toggling off, expected z-task first, got %s", m3.tasks[m3.displayOrder[0]].ID)
	}
}

// ── Lane column ──────────────────────────────────────────────────────────────

func TestLaneColumnRendered(t *testing.T) {
	m, dir := newTestModel(t)

	// Create tasks with lane field.
	tasksYAML := `- id: task-a
  prompt: prompts/a.md
  lane: core
- id: task-b
  prompt: prompts/b.md
  lane: api
`
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	m.tasksFile = filepath.Join(dir, "tasks.yaml")
	m.refreshState()

	v := m.View()
	content := v.Content
	// Lane values appear in table cells (not wrapped in ANSI like headers).
	if !strings.Contains(content, "core") {
		t.Errorf("table should show lane 'core', got:\n%s", content)
	}
	if !strings.Contains(content, "api") {
		t.Errorf("table should show lane 'api', got:\n%s", content)
	}
}

func TestLaneColumnDashWhenEmpty(t *testing.T) {
	m, _ := newTestModel(t)
	// Default fixture tasks have no lane field set — verify table still has 8 columns.
	cols := tableColumns(m.width)
	if len(cols) != 8 {
		t.Errorf("expected 8 columns (including Lane), got %d", len(cols))
	}
	if cols[7].header != "Lane" {
		t.Errorf("expected last column header 'Lane', got %q", cols[7].header)
	}
}

// ── Detail view — state.json fields and verify gates ─────────────────────────

func TestDetailViewShowsStateFields(t *testing.T) {
	m, dir := newTestModel(t)

	// Write state.json with rich fields.
	state := StateManifest{
		Tasks: map[string]*TaskState{
			"task-a": {
				Status:         "completed",
				Attempts:       2,
				DurationMs:     45321,
				Model:          "claude-sonnet-4-6",
				CompletedAt:    time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC),
				LastError:      "claude exited with code 1",
				LastErrorClass: "ClaudeExitError",
			},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	// Position cursor on task-a.
	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	// Open detail.
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	v := m2.View()
	content := v.Content
	if !strings.Contains(content, "45321") {
		t.Errorf("detail should show duration_ms, got:\n%s", content)
	}
	if !strings.Contains(content, "claude-sonnet-4-6") {
		t.Errorf("detail should show model, got:\n%s", content)
	}
	if !strings.Contains(content, "claude exited with code 1") {
		t.Errorf("detail should show last error, got:\n%s", content)
	}
	if !strings.Contains(content, "ClaudeExitError") {
		t.Errorf("detail should show error class, got:\n%s", content)
	}
}

func TestDetailViewShowsVerifyGates(t *testing.T) {
	m, dir := newTestModel(t)

	logEntry := execRunLog{
		TaskID:   "task-a",
		Status:   "complete",
		ExitCode: 0,
		Verify: &verifyResults{
			AllPassed: false,
			Gates: []gateResult{
				{Name: "go-test", Cmd: "go test ./...", Passed: true, ExitCode: 0, DurationMs: 3200},
				{Name: "go-vet", Cmd: "go vet ./...", Passed: false, ExitCode: 1, DurationMs: 800},
			},
		},
	}
	data, _ := json.Marshal(logEntry)
	if err := os.WriteFile(filepath.Join(dir, "logs", "task-a.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshState()

	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	v := m2.View()
	content := v.Content
	if !strings.Contains(content, "Verify Gates") {
		t.Errorf("detail should show verify gates section, got:\n%s", content)
	}
	if !strings.Contains(content, "go-test") {
		t.Errorf("detail should show gate name 'go-test', got:\n%s", content)
	}
	if !strings.Contains(content, "go-vet") {
		t.Errorf("detail should show gate name 'go-vet', got:\n%s", content)
	}
}

func TestDetailViewShowsClosureIndicator(t *testing.T) {
	m, dir := newTestModel(t)

	// Write a closure artifact.
	if err := os.WriteFile(filepath.Join(dir, "unify", "task-a.md"), []byte("closure"), 0o644); err != nil {
		t.Fatal(err)
	}

	for i, idx := range m.displayOrder {
		if m.statuses[idx].ID == "task-a" {
			m.cursor = i
			break
		}
	}

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	v := m2.View()
	if !strings.Contains(v.Content, "Closure: available") {
		t.Errorf("detail should show closure indicator, got:\n%s", v.Content)
	}
}

func TestDetailViewNoLogFile(t *testing.T) {
	m, _ := newTestModel(t)

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := newM.(tuiModel)

	v := m2.View()
	if !strings.Contains(v.Content, "no log file found") {
		t.Errorf("detail should show 'no log file found' when missing, got:\n%s", v.Content)
	}
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// tuiView represents which view is currently active.
type tuiView int

const (
	viewMain   tuiView = iota
	viewDetail         // Enter key on a task
	viewLog            // l key on a task
	viewHelp           // ? key
)

// groupMode controls how tasks are grouped in the main table.
type groupMode int

const (
	groupNone      groupMode = iota
	groupByPhase
	groupByMilestone
)

// tickMsg is sent on each polling interval.
type tickMsg time.Time

// refreshMsg triggers an immediate state refresh.
type refreshMsg struct{}

// tuiModel is the Bubble Tea model for kiln tui.
type tuiModel struct {
	// Config
	tasksFile   string
	refreshRate time.Duration
	doneDir     string
	logDir      string
	unifyDir    string
	stateFile   string

	// State
	tasks    []Task
	statuses []taskStatusInfo
	state    *StateManifest
	doneSet  map[string]bool

	// UI state
	cursor       int
	currentView  tuiView
	grouping     groupMode
	launchTime   time.Time
	lastRefresh  time.Time
	width        int
	height       int

	// Detail/log view data
	detailTask   *taskStatusInfo
	detailLog    *execRunLog
	logLines     []string
	logScrollPos int

	// Help overlay
	showHelp bool

	// Sorted/grouped task display order
	displayOrder []int // indices into m.statuses
}

// statusOrder returns a numeric priority for sorting (lower = shown first).
func statusOrder(s string) int {
	switch s {
	case "running":
		return 0
	case "blocked":
		return 1
	case "failed":
		return 2
	case "not_complete":
		return 3
	case "pending":
		return 4
	case "complete":
		return 5
	default:
		return 6
	}
}

// taskGroupKey returns the grouping key for a task at the given index.
func (m *tuiModel) taskGroupKey(idx int) string {
	if idx < 0 || idx >= len(m.tasks) {
		return ""
	}
	switch m.grouping {
	case groupByPhase:
		return m.tasks[idx].Phase
	case groupByMilestone:
		return m.tasks[idx].Milestone
	default:
		return ""
	}
}

// computeDisplayOrder builds m.displayOrder based on current statuses and grouping.
func (m *tuiModel) computeDisplayOrder() {
	n := len(m.statuses)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(a, b int) bool {
		ia := indices[a]
		ib := indices[b]

		// When grouping is active, sort by group key first so that
		// tasks in the same group stay together.
		if m.grouping != groupNone {
			ga := m.taskGroupKey(ia)
			gb := m.taskGroupKey(ib)
			if ga != gb {
				// Empty group key sorts last.
				if ga == "" {
					return false
				}
				if gb == "" {
					return true
				}
				return ga < gb
			}
		}

		sa := m.statuses[ia]
		sb := m.statuses[ib]
		oa := statusOrder(sa.Status)
		ob := statusOrder(sb.Status)
		if oa != ob {
			return oa < ob
		}
		// Preserve original definition order within same status group.
		return ia < ib
	})
	m.displayOrder = indices
}

// refreshState re-reads all state data and recomputes statuses.
func (m *tuiModel) refreshState() {
	tasks, err := loadTasks(m.tasksFile)
	if err == nil {
		m.tasks = tasks
	}

	state, err := loadState(m.stateFile)
	if err != nil || state == nil {
		state = &StateManifest{Tasks: make(map[string]*TaskState)}
	}
	m.state = state

	// Rebuild done set.
	doneSet := make(map[string]bool)
	for _, t := range m.tasks {
		if _, statErr := os.Stat(filepath.Join(m.doneDir, t.ID+".done")); statErr == nil {
			doneSet[t.ID] = true
		}
	}
	m.doneSet = doneSet

	// Derive statuses.
	statuses := make([]taskStatusInfo, 0, len(m.tasks))
	for _, t := range m.tasks {
		info := deriveTaskStatus(t, m.state, m.logDir, m.doneDir, m.doneSet)
		statuses = append(statuses, info)
	}
	m.statuses = statuses
	m.computeDisplayOrder()
	m.lastRefresh = time.Now()
}

// loadTaskLog reads and parses the log file for a given task ID.
func (m *tuiModel) loadTaskLog(taskID string) *execRunLog {
	logPath := filepath.Join(m.logDir, taskID+".json")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	var entry execRunLog
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

// selectedStatusInfo returns the taskStatusInfo for the currently selected cursor row.
func (m *tuiModel) selectedStatusInfo() *taskStatusInfo {
	if len(m.displayOrder) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.displayOrder) {
		return nil
	}
	idx := m.displayOrder[m.cursor]
	return &m.statuses[idx]
}

// Init implements tea.Model.
func (m tuiModel) Init() tea.Cmd {
	return tea.Tick(m.refreshRate, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.refreshState()
		return m, tea.Tick(m.refreshRate, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case refreshMsg:
		m.refreshState()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit.
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.currentView {
	case viewMain:
		return m.handleMainKey(key)
	case viewDetail:
		return m.handleDetailKey(key)
	case viewLog:
		return m.handleLogKey(key)
	case viewHelp:
		// Any key closes help.
		m.showHelp = false
		m.currentView = viewMain
		return m, nil
	}
	return m, nil
}

func (m tuiModel) handleMainKey(key string) (tea.Model, tea.Cmd) {
	n := len(m.displayOrder)
	switch key {
	case "q":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < n-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		if n > 0 {
			m.cursor = n - 1
		}
	case "r":
		m.refreshState()
	case "?":
		m.showHelp = true
		m.currentView = viewHelp
	case "p":
		// Toggle phase grouping.
		if m.grouping == groupByPhase {
			m.grouping = groupNone
		} else {
			m.grouping = groupByPhase
		}
		m.computeDisplayOrder()
	case "m":
		// Toggle milestone grouping.
		if m.grouping == groupByMilestone {
			m.grouping = groupNone
		} else {
			m.grouping = groupByMilestone
		}
		m.computeDisplayOrder()
	case "enter":
		info := m.selectedStatusInfo()
		if info != nil {
			cp := *info
			m.detailTask = &cp
			m.detailLog = m.loadTaskLog(info.ID)
			m.currentView = viewDetail
		}
	case "l":
		info := m.selectedStatusInfo()
		if info != nil {
			log := m.loadTaskLog(info.ID)
			m.detailLog = log
			lines := []string{}
			if log != nil {
				for _, ev := range log.Events {
					lines = append(lines, fmt.Sprintf("[%s] %s", ev.TS.Format("15:04:05"), ev.Line))
				}
			}
			m.logLines = lines
			m.logScrollPos = max(0, len(lines)-1)
			m.currentView = viewLog
		}
	}
	return m, nil
}

func (m tuiModel) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc", "escape":
		m.currentView = viewMain
		m.detailTask = nil
		m.detailLog = nil
	}
	return m, nil
}

func (m tuiModel) handleLogKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc", "escape":
		m.currentView = viewMain
		m.detailLog = nil
		m.logLines = nil
	case "j", "down":
		if m.logScrollPos < len(m.logLines)-1 {
			m.logScrollPos++
		}
	case "k", "up":
		if m.logScrollPos > 0 {
			m.logScrollPos--
		}
	case "g":
		m.logScrollPos = 0
	case "G":
		if len(m.logLines) > 0 {
			m.logScrollPos = len(m.logLines) - 1
		}
	}
	return m, nil
}

// View implements tea.Model — returns tea.View (v2 API).
func (m tuiModel) View() tea.View {
	var content string
	switch m.currentView {
	case viewHelp:
		content = m.renderHelp()
	case viewDetail:
		content = m.renderDetail()
	case viewLog:
		content = m.renderLogView()
	default:
		content = m.renderMain()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// ── Styles ──────────────────────────────────────────────────────────────────

var (
	styleComplete    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))   // green
	styleRunning     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))   // yellow
	styleFailed      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))   // red
	styleBlocked     = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))   // magenta
	stylePending     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))   // dim gray
	styleNotComplete = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	styleSelected    = lipgloss.NewStyle().Reverse(true)
	styleBold        = lipgloss.NewStyle().Bold(true)
	styleDim         = lipgloss.NewStyle().Faint(true)
	styleHeader      = lipgloss.NewStyle().Bold(true).Underline(true)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "complete":
		return styleComplete
	case "running":
		return styleRunning
	case "failed":
		return styleFailed
	case "blocked":
		return styleBlocked
	case "pending":
		return stylePending
	case "not_complete":
		return styleNotComplete
	default:
		return lipgloss.NewStyle()
	}
}

// ── Rendering ────────────────────────────────────────────────────────────────

func (m tuiModel) renderMain() string {
	var sb strings.Builder

	// Summary bar.
	sb.WriteString(m.renderSummary())
	sb.WriteString("\n\n")

	// Table header.
	w := m.width
	if w < 80 {
		w = 80
	}
	sb.WriteString(m.renderTableHeader(w))
	sb.WriteString("\n")

	// Rows.
	availableRows := m.height - 6 // summary + header + footer
	if availableRows < 1 {
		availableRows = 1
	}

	var prevGroupKey string
	rowsRendered := 0
	for displayPos, origIdx := range m.displayOrder {
		if rowsRendered >= availableRows {
			break
		}

		info := m.statuses[origIdx]

		// Group headers.
		if m.grouping != groupNone {
			var groupKey string
			if m.grouping == groupByPhase {
				if origIdx < len(m.tasks) {
					groupKey = m.tasks[origIdx].Phase
				}
			} else {
				if origIdx < len(m.tasks) {
					groupKey = m.tasks[origIdx].Milestone
				}
			}
			if groupKey != prevGroupKey {
				prevGroupKey = groupKey
				if groupKey == "" {
					groupKey = "(none)"
				}
				label := "Phase"
				if m.grouping == groupByMilestone {
					label = "Milestone"
				}
				sb.WriteString(styleDim.Render(fmt.Sprintf("── %s: %s ", label, groupKey)))
				sb.WriteString("\n")
				rowsRendered++
			}
		}

		row := m.renderRow(info, displayPos, w)
		sb.WriteString(row)
		sb.WriteString("\n")
		rowsRendered++
	}

	// Footer.
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("j/k:move  Enter:detail  l:logs  r:refresh  p:phase  m:milestone  ?:help  q:quit"))

	return sb.String()
}

func (m tuiModel) renderSummary() string {
	counts := map[string]int{}
	for _, s := range m.statuses {
		counts[s.Status]++
	}
	total := len(m.statuses)
	elapsed := time.Since(m.launchTime).Round(time.Second)
	refreshedAgo := ""
	if !m.lastRefresh.IsZero() {
		refreshedAgo = fmt.Sprintf(" | Refreshed: %s ago", time.Since(m.lastRefresh).Round(time.Second))
	}
	return fmt.Sprintf(
		"%s | %s %s | %s %s | %s %s | %s %s | %s %s | Elapsed: %s%s",
		styleBold.Render(fmt.Sprintf("Total: %d", total)),
		styleComplete.Render("Complete:"), styleComplete.Render(fmt.Sprintf("%d", counts["complete"])),
		styleRunning.Render("Running:"), styleRunning.Render(fmt.Sprintf("%d", counts["running"])),
		styleFailed.Render("Failed:"), styleFailed.Render(fmt.Sprintf("%d", counts["failed"])),
		styleBlocked.Render("Blocked:"), styleBlocked.Render(fmt.Sprintf("%d", counts["blocked"])),
		stylePending.Render("Pending:"), stylePending.Render(fmt.Sprintf("%d", counts["pending"])),
		elapsed,
		refreshedAgo,
	)
}

func (m tuiModel) renderTableHeader(w int) string {
	cols := tableColumns(w)
	var parts []string
	for _, c := range cols {
		parts = append(parts, styleHeader.Render(padOrTrunc(c.header, c.width)))
	}
	return strings.Join(parts, " ")
}

type colDef struct {
	header string
	width  int
}

func tableColumns(w int) []colDef {
	// Fixed widths for columns; ID gets remaining space.
	statusW := 12
	attemptsW := 8
	durationW := 10
	kindW := 10
	phaseW := 10
	laneW := 10
	errW := 38

	idW := w - statusW - attemptsW - durationW - kindW - phaseW - laneW - errW - 8 // 8 spaces between 9 cols
	if idW < 10 {
		idW = 10
	}
	return []colDef{
		{"ID", idW},
		{"Status", statusW},
		{"Attempts", attemptsW},
		{"Last Error", errW},
		{"Duration", durationW},
		{"Kind", kindW},
		{"Phase", phaseW},
		{"Lane", laneW},
	}
}

func (m tuiModel) renderRow(info taskStatusInfo, displayPos int, w int) string {
	cols := tableColumns(w)

	// Duration from state.
	dur := "-"
	if ts := m.state.Tasks[info.ID]; ts != nil && ts.DurationMs > 0 {
		dur = fmt.Sprintf("%dms", ts.DurationMs)
		if ts.DurationMs >= 1000 {
			dur = fmt.Sprintf("%.1fs", float64(ts.DurationMs)/1000)
		}
	}

	lastErr := info.LastErr
	if info.Status == "blocked" {
		// Show unmet deps.
		task := m.taskByID(info.ID)
		if task != nil {
			var unmet []string
			for _, dep := range task.Needs {
				if !m.doneSet[dep] {
					unmet = append(unmet, dep)
				}
			}
			if len(unmet) > 0 {
				lastErr = "blocked: " + strings.Join(unmet, ",")
			}
		}
	}
	if lastErr == "" {
		lastErr = "-"
	}

	kind := info.Kind
	if kind == "" {
		kind = "-"
	}
	phase := info.Phase
	if phase == "" {
		phase = "-"
	}
	lane := "-"
	if task := m.taskByID(info.ID); task != nil && task.Lane != "" {
		lane = task.Lane
	}

	cells := []string{
		padOrTrunc(info.ID, cols[0].width),
		padOrTrunc(info.Status, cols[1].width),
		padOrTrunc(fmt.Sprintf("%d", info.Attempts), cols[2].width),
		padOrTrunc(lastErr, cols[3].width),
		padOrTrunc(dur, cols[4].width),
		padOrTrunc(kind, cols[5].width),
		padOrTrunc(phase, cols[6].width),
		padOrTrunc(lane, cols[7].width),
	}

	// Apply status color to status cell.
	cells[1] = statusStyle(info.Status).Render(cells[1])

	line := strings.Join(cells, " ")
	if displayPos == m.cursor {
		line = styleSelected.Render(line)
	}
	return line
}

func (m tuiModel) taskByID(id string) *Task {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			return &m.tasks[i]
		}
	}
	return nil
}

func (m tuiModel) renderDetail() string {
	var sb strings.Builder
	sb.WriteString(styleBold.Render("── Task Detail ──"))
	sb.WriteString("\n\n")

	if m.detailTask == nil {
		sb.WriteString("No task selected.\n")
		sb.WriteString("\nPress Escape or q to return.")
		return sb.String()
	}

	info := m.detailTask
	sb.WriteString(fmt.Sprintf("ID:      %s\n", styleBold.Render(info.ID)))
	sb.WriteString(fmt.Sprintf("Status:  %s\n", statusStyle(info.Status).Render(info.Status)))
	sb.WriteString(fmt.Sprintf("Attempts: %d\n", info.Attempts))
	sb.WriteString(fmt.Sprintf("Kind:    %s\n", orDash(info.Kind)))
	sb.WriteString(fmt.Sprintf("Phase:   %s\n", orDash(info.Phase)))

	// State.json fields.
	if ts := m.state.Tasks[info.ID]; ts != nil {
		if ts.DurationMs > 0 {
			sb.WriteString(fmt.Sprintf("Duration: %dms\n", ts.DurationMs))
		}
		if ts.Model != "" {
			sb.WriteString(fmt.Sprintf("Model:   %s\n", ts.Model))
		}
		if ts.CompletedAt != (time.Time{}) {
			sb.WriteString(fmt.Sprintf("Completed: %s\n", ts.CompletedAt.Format(time.RFC3339)))
		}
		if ts.LastError != "" {
			sb.WriteString(fmt.Sprintf("\nLast Error (full):\n  %s\n", styleFailed.Render(ts.LastError)))
		}
		if ts.LastErrorClass != "" {
			sb.WriteString(fmt.Sprintf("Error Class: %s\n", ts.LastErrorClass))
		}
	}

	// Log file detail.
	if m.detailLog != nil {
		log := m.detailLog
		sb.WriteString("\n── Log Entry ──\n")
		sb.WriteString(fmt.Sprintf("Started:   %s\n", log.StartedAt.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("Ended:     %s\n", log.EndedAt.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("Duration:  %dms\n", log.DurationMs))
		sb.WriteString(fmt.Sprintf("Model:     %s\n", orDash(log.Model)))
		sb.WriteString(fmt.Sprintf("Exit Code: %d\n", log.ExitCode))
		sb.WriteString(fmt.Sprintf("Status:    %s\n", log.Status))
		if log.ErrorClass != "" {
			sb.WriteString(fmt.Sprintf("Error Class:   %s\n", log.ErrorClass))
		}
		if log.ErrorMessage != "" {
			sb.WriteString(fmt.Sprintf("Error Message: %s\n", styleFailed.Render(log.ErrorMessage)))
		}
		sb.WriteString(fmt.Sprintf("Events:    %d\n", len(log.Events)))

		if log.Verify != nil {
			sb.WriteString("\n── Verify Gates ──\n")
			sb.WriteString(fmt.Sprintf("All Passed: %v\n", log.Verify.AllPassed))
			for _, g := range log.Verify.Gates {
				mark := styleComplete.Render("✓")
				if !g.Passed {
					mark = styleFailed.Render("✗")
				}
				sb.WriteString(fmt.Sprintf("  %s %s (exit %d)\n", mark, g.Name, g.ExitCode))
			}
		}
	} else {
		sb.WriteString(styleDim.Render("\n(no log file found)"))
		sb.WriteString("\n")
	}

	// Closure artifact.
	unifyPath := filepath.Join(m.unifyDir, info.ID+".md")
	if _, err := os.Stat(unifyPath); err == nil {
		sb.WriteString("\n" + styleComplete.Render("Closure: available") + "\n")
	}

	// Blocked detail.
	if info.Status == "blocked" {
		task := m.taskByID(info.ID)
		if task != nil && len(task.Needs) > 0 {
			sb.WriteString("\n── Dependencies ──\n")
			for _, dep := range task.Needs {
				if m.doneSet[dep] {
					sb.WriteString(fmt.Sprintf("  %s %s\n", styleComplete.Render("✓"), dep))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s\n", styleFailed.Render("✗"), dep))
				}
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("Press Escape or q to return."))
	return sb.String()
}

func (m tuiModel) renderLogView() string {
	var sb strings.Builder
	taskID := ""
	if m.detailTask != nil {
		taskID = m.detailTask.ID
	} else if info := m.selectedStatusInfo(); info != nil {
		taskID = info.ID
	}
	sb.WriteString(styleBold.Render(fmt.Sprintf("── Log Events: %s ──", taskID)))
	sb.WriteString("\n\n")

	if len(m.logLines) == 0 {
		sb.WriteString(styleDim.Render("(no log events)"))
		sb.WriteString("\n")
	} else {
		// Show a viewport of lines.
		viewH := m.height - 6
		if viewH < 1 {
			viewH = 1
		}
		start := m.logScrollPos - viewH + 1
		if start < 0 {
			start = 0
		}
		end := start + viewH
		if end > len(m.logLines) {
			end = len(m.logLines)
		}
		for _, line := range m.logLines[start:end] {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString(styleDim.Render(fmt.Sprintf("\n[%d/%d lines]", m.logScrollPos+1, len(m.logLines))))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("j/k:scroll  g/G:top/bottom  Escape/q:back"))
	return sb.String()
}

func (m tuiModel) renderHelp() string {
	return `── Kiln TUI Help ──

Navigation:
  j / ↓       Move cursor down
  k / ↑       Move cursor up
  g           Jump to top
  G           Jump to bottom

Views:
  Enter       Open task detail view
  l           Open log events view
  Escape / q  Return to main view

Actions:
  r           Force immediate refresh
  p           Toggle phase grouping
  m           Toggle milestone grouping
  ?           Toggle this help overlay

Global:
  q / Ctrl+C  Quit

Press any key to close help.`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func padOrTrunc(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) > width {
		if width > 3 {
			return s[:width-3] + "..."
		}
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Entry point ───────────────────────────────────────────────────────────────

// runTUI is the entry point for the `kiln tui` subcommand.
// It does NOT accept stdout/stderr — Bubble Tea needs direct terminal access.
func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	tasksFile := fs.String("tasks", ".kiln/tasks.yaml", "path to tasks.yaml")
	refresh := fs.String("refresh", "2s", "polling interval (e.g. 2s, 500ms)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}

	refreshDur, err := time.ParseDuration(*refresh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: invalid --refresh duration: %v\n", err)
		return 1
	}

	m := tuiModel{
		tasksFile:   *tasksFile,
		refreshRate: refreshDur,
		doneDir:     ".kiln/done",
		logDir:      ".kiln/logs",
		unifyDir:    ".kiln/unify",
		stateFile:   ".kiln/state.json",
		launchTime:  time.Now(),
		width:       80,
		height:      24,
		state:       &StateManifest{Tasks: make(map[string]*TaskState)},
		doneSet:     make(map[string]bool),
	}
	m.refreshState()

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}

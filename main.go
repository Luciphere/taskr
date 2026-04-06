package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Colours ───────────────────────────────────────────────────────────────────

var (
	colPurple   = lipgloss.Color("#9D7CD8")
	colCyan     = lipgloss.Color("#7DCFFF")
	colGreen    = lipgloss.Color("#9ECE6A")
	colYellow   = lipgloss.Color("#E0AF68")
	colRed      = lipgloss.Color("#F7768E")
	colOrange   = lipgloss.Color("#FF9E64")
	colMuted    = lipgloss.Color("#565F89")
	colFg       = lipgloss.Color("#C0CAF5")
	colBgSelect = lipgloss.Color("#292E42")
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleSelectedRow = lipgloss.NewStyle().
				Background(colBgSelect).
				Foreground(colFg).
				Bold(true)

	styleColHeader = lipgloss.NewStyle().
			Foreground(colCyan).
			Bold(true)

	stylePriH = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	stylePriM = lipgloss.NewStyle().Foreground(colOrange)
	stylePriL = lipgloss.NewStyle().Foreground(colGreen)
	stylePri0 = lipgloss.NewStyle().Foreground(colMuted)

	styleOverdue  = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	styleDueSoon  = lipgloss.NewStyle().Foreground(colYellow)
	styleDueOk    = lipgloss.NewStyle().Foreground(colGreen)
	styleMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styleFg       = lipgloss.NewStyle().Foreground(colFg)
	styleActive   = lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	styleHelpKey  = lipgloss.NewStyle().Foreground(colCyan)
	styleHelpDesc = lipgloss.NewStyle().Foreground(colMuted)

	styleDetailLabel = lipgloss.NewStyle().Foreground(colCyan).Bold(true).Width(12)
	styleDetailValue = lipgloss.NewStyle().Foreground(colFg)

	styleInputPanel = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colCyan).
			Padding(1, 3)

	styleError = lipgloss.NewStyle().Foreground(colRed)
)

// ── Data types ────────────────────────────────────────────────────────────────

type Annotation struct {
	Entry       string `json:"entry"`
	Description string `json:"description"`
}

type Task struct {
	ID          int          `json:"id"`
	UUID        string       `json:"uuid"`
	Description string       `json:"description"`
	Project     string       `json:"project"`
	Priority    string       `json:"priority"`
	Status      string       `json:"status"`
	Tags        []string     `json:"tags"`
	Due         string       `json:"due"`
	Start       string       `json:"start"`
	Urgency     float64      `json:"urgency"`
	Depends     []string     `json:"depends"`
	Entry       string       `json:"entry"`
	Modified    string       `json:"modified"`
	End         string       `json:"end"`
	Scheduled   string       `json:"scheduled"`
	Wait        string       `json:"wait"`
	Recur       string       `json:"recur"`
	Annotations []Annotation `json:"annotations"`
}

func (t Task) IsActive() bool { return t.Start != "" }

func parseTaskDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse("20060102T150405Z", s)
	return parsed, err == nil
}

func (t Task) ParseDue() (time.Time, bool) { return parseTaskDate(t.Due) }

// ── App state ─────────────────────────────────────────────────────────────────

type viewState int

const (
	viewList viewState = iota
	viewProjects
	viewHistory
	viewAdd
)

// detailPaneH is the total rendered height of the detail pane (content + border).
const detailPaneH = 18

type editField struct {
	label string
	input textinput.Model
}

type projectSummary struct {
	name    string
	total   int
	active  int
	overdue int
}

type model struct {
	tasks               []Task
	completedTasks      []Task
	cursor              int
	projectCursor       int
	historyCursor       int
	state               viewState
	input               textinput.Model
	editFields          []editField
	editFocus           int
	editTaskID          int
	detailFocused       bool
	searching           bool
	searchInput         textinput.Model
	historySearching    bool
	historySearchInput  textinput.Model
	confirming          bool
	confirmMsg          string
	confirmAction       tea.Cmd
	width               int
	height              int
	err                 error
}

// ── Messages ──────────────────────────────────────────────────────────────────

type allTasksLoadedMsg struct {
	pending   []Task
	completed []Task
}
type errMsg struct{ err error }

// ── Commands ──────────────────────────────────────────────────────────────────

// fetchAll fetches both pending and completed tasks in one shot,
// producing a single message so the UI only redraws once.
func fetchAll() tea.Cmd {
	return func() tea.Msg {
		var pending []Task
		out, err := exec.Command("task", "rc.confirmation=no", "status:pending", "export").Output()
		if err != nil {
			return errMsg{err}
		}
		if err := json.Unmarshal(out, &pending); err != nil {
			return errMsg{err}
		}

		var completed []Task
		for _, status := range []string{"completed", "deleted"} {
			out, err := exec.Command("task", "rc.confirmation=no", "status:"+status, "export").Output()
			if err != nil {
				continue
			}
			var tasks []Task
			if err := json.Unmarshal(out, &tasks); err != nil {
				continue
			}
			completed = append(completed, tasks...)
		}
		sort.Slice(completed, func(i, j int) bool {
			return completed[i].End > completed[j].End
		})
		return allTasksLoadedMsg{pending: pending, completed: completed}
	}
}

func runTask(args ...string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("task", append([]string{"rc.confirmation=no"}, args...)...)
		if err := cmd.Run(); err != nil {
			return errMsg{err}
		}
		return fetchAll()()
	}
}

// purgeHistoryTask permanently removes a task. Completed tasks are deleted first
// since taskwarrior's purge only works on status:deleted tasks.
func purgeHistoryTask(t Task) tea.Cmd {
	return func() tea.Msg {
		if t.Status == "completed" {
			exec.Command("task", "rc.confirmation=no", t.UUID, "delete").Run()
		}
		if err := exec.Command("task", "rc.confirmation=no", t.UUID, "purge").Run(); err != nil {
			return errMsg{err}
		}
		return fetchAll()()
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "Describe the task…"
	ti.CharLimit = 200
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colCyan)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colFg)

	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 100
	si.PromptStyle = lipgloss.NewStyle().Foreground(colYellow)
	si.TextStyle = lipgloss.NewStyle().Foreground(colFg)

	hi := textinput.New()
	hi.Placeholder = "search history…"
	hi.CharLimit = 100
	hi.PromptStyle = lipgloss.NewStyle().Foreground(colYellow)
	hi.TextStyle = lipgloss.NewStyle().Foreground(colFg)

	return model{input: ti, searchInput: si, historySearchInput: hi}
}

func (m model) Init() tea.Cmd {
	return fetchAll()
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case allTasksLoadedMsg:
		m.tasks = msg.pending
		m.completedTasks = msg.completed
		m.err = nil
		if m.cursor >= len(m.tasks) {
			m.cursor = max(0, len(m.tasks)-1)
		}
		if m.historyCursor >= len(m.completedTasks) {
			m.historyCursor = max(0, len(m.completedTasks)-1)
		}
		if !m.detailFocused {
			m = m.syncEditFields()
		}

	case errMsg:
		m.err = msg.err

	case tea.KeyMsg:
		if m.confirming {
			if msg.String() == "y" {
				m.confirming = false
				return m, m.confirmAction
			}
			m.confirming = false
			return m, nil
		}
		switch m.state {
		case viewList:
			if m.detailFocused {
				return m.updateDetailPane(msg)
			}
			if m.searching {
				return m.updateSearch(msg)
			}
			if msg.String() == "tab" {
				m.state = viewProjects
				return m, nil
			}
			return m.updateList(msg)
		case viewProjects:
			if msg.String() == "tab" {
				m.state = viewHistory
				return m, nil
			}
			return m.updateProjects(msg)
		case viewHistory:
			if m.historySearching {
				return m.updateHistorySearch(msg)
			}
			if msg.String() == "tab" {
				m.state = viewList
				return m, nil
			}
			return m.updateHistory(msg)
		case viewAdd:
			return m.updateAdd(msg)
		}
	}
	return m, nil
}

func (m model) updateSearch(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.searching = false
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.cursor = 0
		m = m.syncEditFields()
		return m, nil
	case "enter":
		m.searching = false
		m.searchInput.Blur()
		m.cursor = 0
		m = m.syncEditFields()
		return m, nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(k)
		m.cursor = 0
		m = m.syncEditFields()
		return m, cmd
	}
}

// filteredTasks returns tasks matching the current search query.
func (m model) filteredTasks() []Task {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" {
		return m.tasks
	}
	var out []Task
	for _, t := range m.tasks {
		if strings.Contains(strings.ToLower(t.Description), q) ||
			strings.Contains(strings.ToLower(t.Project), q) ||
			strings.Contains(strings.ToLower(strings.Join(t.Tags, " ")), q) {
			out = append(out, t)
		}
	}
	return out
}

func (m model) updateList(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.searchInput.SetValue("")
		m.searchInput.Focus()
		return m, textinput.Blink
	case "j", "down":
		if m.cursor < len(m.filteredTasks())-1 {
			m.cursor++
			m = m.syncEditFields()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m = m.syncEditFields()
		}
	case "enter":
		if ft := m.filteredTasks(); len(ft) > 0 {
			m2, cmd := m.openEdit(ft[m.cursor])
			m2.detailFocused = true
			return m2, cmd
		}
	case "a":
		m.state = viewAdd
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "d":
		if ft := m.filteredTasks(); len(ft) > 0 {
			return m, runTask(fmt.Sprintf("%d", ft[m.cursor].ID), "done")
		}
	case "x":
		if ft := m.filteredTasks(); len(ft) > 0 {
			t := ft[m.cursor]
			m.confirming = true
			m.confirmMsg = fmt.Sprintf("Delete \"%s\"? [y/N]", truncate(t.Description, 40))
			m.confirmAction = runTask(fmt.Sprintf("%d", t.ID), "delete")
			return m, nil
		}
	case "s":
		if ft := m.filteredTasks(); len(ft) > 0 {
			t := ft[m.cursor]
			if t.IsActive() {
				return m, runTask(fmt.Sprintf("%d", t.ID), "stop")
			}
			return m, runTask(fmt.Sprintf("%d", t.ID), "start")
		}
	}
	return m, nil
}

func (m model) updateDetailPane(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.detailFocused = false
		if len(m.editFields) > 0 {
			m.editFields[m.editFocus].input.Blur()
		}
		m = m.syncEditFields()
		return m, nil
	case "down":
		m.editFields[m.editFocus].input.Blur()
		m.editFocus = (m.editFocus + 1) % len(m.editFields)
		m.editFields[m.editFocus].input.Focus()
		return m, textinput.Blink
	case "up":
		m.editFields[m.editFocus].input.Blur()
		m.editFocus = (m.editFocus - 1 + len(m.editFields)) % len(m.editFields)
		m.editFields[m.editFocus].input.Focus()
		return m, textinput.Blink
	case "enter":
		m.detailFocused = false
		return m, m.buildModifyCmd()
	default:
		var cmd tea.Cmd
		m.editFields[m.editFocus].input, cmd = m.editFields[m.editFocus].input.Update(k)
		return m, cmd
	}
}

func (m model) updateHistorySearch(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.historySearching = false
		m.historySearchInput.SetValue("")
		m.historySearchInput.Blur()
		m.historyCursor = 0
		return m, nil
	case "enter":
		m.historySearching = false
		m.historySearchInput.Blur()
		m.historyCursor = 0
		return m, nil
	default:
		var cmd tea.Cmd
		m.historySearchInput, cmd = m.historySearchInput.Update(k)
		m.historyCursor = 0
		return m, cmd
	}
}

// historyFiltered returns the 10 most recent tasks by default, or all
// matching tasks when a search query is active (preserving recency order).
func (m model) historyFiltered() []Task {
	q := strings.ToLower(strings.TrimSpace(m.historySearchInput.Value()))
	if q == "" {
		if len(m.completedTasks) > 10 {
			return m.completedTasks[:10]
		}
		return m.completedTasks
	}
	var out []Task
	for _, t := range m.completedTasks {
		if strings.Contains(strings.ToLower(t.Description), q) ||
			strings.Contains(strings.ToLower(t.Project), q) ||
			strings.Contains(strings.ToLower(strings.Join(t.Tags, " ")), q) {
			out = append(out, t)
		}
	}
	return out
}

func (m model) updateHistory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		m.historySearching = true
		m.historySearchInput.SetValue("")
		m.historySearchInput.Focus()
		return m, textinput.Blink
	case "j", "down":
		if m.historyCursor < len(m.historyFiltered())-1 {
			m.historyCursor++
		}
	case "k", "up":
		if m.historyCursor > 0 {
			m.historyCursor--
		}
	case "u":
		if ft := m.historyFiltered(); len(ft) > 0 {
			t := ft[m.historyCursor]
			return m, runTask(t.UUID, "modify", "status:pending", "end:")
		}
	case "x":
		if ft := m.historyFiltered(); len(ft) > 0 {
			t := ft[m.historyCursor]
			m.confirming = true
			m.confirmMsg = fmt.Sprintf("Permanently purge \"%s\"? This cannot be undone. [y/N]", truncate(t.Description, 30))
			m.confirmAction = purgeHistoryTask(t)
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateProjects(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	projects := m.projectSummaries()
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.projectCursor < len(projects)-1 {
			m.projectCursor++
		}
	case "k", "up":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	}
	return m, nil
}

func (m model) projectSummaries() []projectSummary {
	pm := map[string]*projectSummary{}
	for _, t := range m.tasks {
		name := t.Project
		if name == "" {
			name = "(no project)"
		}
		if pm[name] == nil {
			pm[name] = &projectSummary{name: name}
		}
		pm[name].total++
		if t.IsActive() {
			pm[name].active++
		}
		if due, ok := t.ParseDue(); ok && time.Until(due) < 0 {
			pm[name].overdue++
		}
	}
	var names []string
	for name := range pm {
		names = append(names, name)
	}
	sort.Strings(names)
	var result []projectSummary
	for _, name := range names {
		result = append(result, *pm[name])
	}
	return result
}

func (m model) updateAdd(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.state = viewList
		m.input.Blur()
		return m, nil
	case "enter":
		desc := strings.TrimSpace(m.input.Value())
		if desc != "" {
			m.state = viewList
			m.input.Blur()
			return m, runTask("add", desc)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// ── Edit fields ───────────────────────────────────────────────────────────────

func (m model) inputWidth() int {
	// inner panel width = m.width - 4 (border 2 + margin 2)
	// minus padding (0,2) = 4 chars → usable = m.width - 8
	// minus label width = 12
	return max(10, m.width-20)
}

func newEditInput(placeholder, value string, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.CharLimit = 200
	ti.Width = width
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colCyan)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colFg)
	return ti
}

// dependsToIDs converts a slice of UUIDs to a comma-separated string of task IDs.
func (m model) dependsToIDs(uuids []string) string {
	var ids []string
	for _, uuid := range uuids {
		for _, t := range m.tasks {
			if t.UUID == uuid {
				ids = append(ids, fmt.Sprintf("%d", t.ID))
				break
			}
		}
	}
	return strings.Join(ids, ", ")
}

func (m model) buildEditFields(t Task) []editField {
	fmtDate := func(s string) string {
		if d, ok := parseTaskDate(s); ok {
			return d.Format("2006-01-02")
		}
		return ""
	}
	w := m.inputWidth()
	return []editField{
		{"Description", newEditInput("Task description…", t.Description, w)},
		{"Project", newEditInput("project name", t.Project, w)},
		{"Tags", newEditInput("tag1 tag2…", strings.Join(t.Tags, " "), w)},
		{"Priority", newEditInput("H / M / L", t.Priority, w)},
		{"Due", newEditInput("YYYY-MM-DD", fmtDate(t.Due), w)},
		{"Scheduled", newEditInput("YYYY-MM-DD", fmtDate(t.Scheduled), w)},
		{"Wait", newEditInput("YYYY-MM-DD", fmtDate(t.Wait), w)},
		{"Recur", newEditInput("e.g. weekly, 2d…", t.Recur, w)},
		{"Depends", newEditInput("task IDs, e.g. 1 2 3", m.dependsToIDs(t.Depends), w)},
		{"Annotate", newEditInput("Add a note…", "", w)},
	}
}

func (m model) openEdit(t Task) (model, tea.Cmd) {
	fields := m.buildEditFields(t)
	fields[0].input.Focus()
	m.editFields = fields
	m.editFocus = 0
	m.editTaskID = t.ID
	return m, textinput.Blink
}

// syncEditFields refreshes the edit fields from the currently selected task
// without changing focus or detailFocused state.
func (m model) syncEditFields() model {
	ft := m.filteredTasks()
	if len(ft) == 0 {
		m.editFields = nil
		return m
	}
	if m.cursor >= len(ft) {
		m.cursor = len(ft) - 1
	}
	t := ft[m.cursor]
	m.editFields = m.buildEditFields(t)
	m.editFocus = 0
	m.editTaskID = t.ID
	return m
}

func (m model) buildModifyCmd() tea.Cmd {
	get := func(i int) string { return strings.TrimSpace(m.editFields[i].input.Value()) }
	taskID := fmt.Sprintf("%d", m.editTaskID)
	annotateText := get(9)

	return func() tea.Msg {
		// Build modify args
		args := []string{"rc.confirmation=no", taskID, "modify"}
		if desc := get(0); desc != "" {
			args = append(args, "description:"+desc)
		}
		args = append(args, "project:"+get(1))
		if tags := get(2); tags != "" {
			for _, tag := range strings.Fields(tags) {
				args = append(args, "+"+tag)
			}
		}
		pri := strings.ToUpper(get(3))
		if pri == "H" || pri == "M" || pri == "L" {
			args = append(args, "priority:"+pri)
		} else {
			args = append(args, "priority:")
		}
		args = append(args, "due:"+get(4))
		args = append(args, "scheduled:"+get(5))
		args = append(args, "wait:"+get(6))
		args = append(args, "recur:"+get(7))
		depends := strings.Join(strings.Fields(get(8)), ",")
		args = append(args, "depends:"+depends)

		if err := exec.Command("task", args...).Run(); err != nil {
			return errMsg{err}
		}

		// Annotate if provided
		if annotateText != "" {
			annotArgs := []string{"rc.confirmation=no", taskID, "annotate", annotateText}
			if err := exec.Command("task", annotArgs...).Run(); err != nil {
				return errMsg{err}
			}
		}

		return fetchAll()()
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.confirming {
		return m.renderConfirm()
	}
	switch m.state {
	case viewAdd:
		return m.renderAdd()
	case viewProjects:
		return m.renderProjectsView()
	case viewHistory:
		return m.renderHistoryView()
	default:
		return m.renderMain()
	}
}

func (m model) renderMain() string {
	// header=3, searchBar=1 (when active), detailPane=detailPaneH, footer=2
	searchBarH := 0
	if m.searching {
		searchBarH = 1
	}
	tableAvailH := m.height - 3 - searchBarH - detailPaneH - 2
	parts := []string{m.renderHeader()}
	if m.searching {
		parts = append(parts, m.renderSearchBar())
	}
	parts = append(parts, m.renderTable(tableAvailH), m.renderDetailPane(), m.renderFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderProjectsView() string {
	bodyH := m.height - 5 // header(3) + footer(2)
	tableMaxH := (bodyH * 2) / 5
	ganttH := bodyH - tableMaxH

	projects := m.projectSummaries()
	projectName := ""
	if m.projectCursor < len(projects) {
		projectName = projects[m.projectCursor].name
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		m.renderProjects(tableMaxH),
		m.renderGantt(projectName, ganttH),
		m.renderFooter(),
	)
}

const historyDetailH = 9

func (m model) renderHistoryView() string {
	parts := []string{m.renderHeader()}
	if m.historySearching {
		parts = append(parts, m.renderHistorySearchBar())
	}
	parts = append(parts, m.renderHistory(), m.renderHistoryDetail(), m.renderFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderHistorySearchBar() string {
	prompt := lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("/")
	return lipgloss.NewStyle().Padding(0, 1).Render(prompt + " " + m.historySearchInput.View())
}

// ── Header ────────────────────────────────────────────────────────────────────

func (m model) renderHeader() string {
	left := lipgloss.NewStyle().Foreground(colPurple).Bold(true).Render("taskwarrior")
	countStr := fmt.Sprintf("  %d pending", len(m.tasks))
	if m.searching && m.searchInput.Value() != "" {
		countStr = fmt.Sprintf("  %d / %d", len(m.filteredTasks()), len(m.tasks))
	}
	count := styleMuted.Render(countStr)

	right := ""
	if m.err != nil {
		right = styleError.Render("⚠ " + m.err.Error())
	}

	leftFull := left + count
	gap := strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftFull)-lipgloss.Width(right)-2))

	bar := lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Render(leftFull + gap + right)

	tabActive   := lipgloss.NewStyle().Foreground(colCyan).Bold(true).Underline(true)
	tabInactive := lipgloss.NewStyle().Foreground(colMuted)
	tab := func(label string, state viewState) string {
		if m.state == state {
			return tabActive.Render(label)
		}
		return tabInactive.Render(label)
	}
	sep := styleMuted.Render("  ·  ")
	tabs := lipgloss.NewStyle().Padding(0, 1).Render(
		tab("Tasks", viewList) + sep + tab("Projects", viewProjects) + sep + tab("History", viewHistory),
	)

	divider := lipgloss.NewStyle().Foreground(colPurple).Render(strings.Repeat("─", m.width))
	return bar + "\n" + tabs + "\n" + divider
}

func (m model) renderSearchBar() string {
	prompt := lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("/")
	return lipgloss.NewStyle().Padding(0, 1).Render(prompt + " " + m.searchInput.View())
}

// ── Table ─────────────────────────────────────────────────────────────────────

func (m model) renderTable(availH int) string {
	idW   := 5
	priW  := 3
	actW  := 3
	projW := 16
	tagW  := 16
	dueW  := 11
	descW := max(10, m.width-idW-priW-actW-projW-tagW-dueW-4)

	headerRow := "  " + lipgloss.JoinHorizontal(lipgloss.Left,
		styleColHeader.Width(idW).Render("ID"),
		styleColHeader.Width(priW).Render("!"),
		styleColHeader.Width(actW).Render(""),
		styleColHeader.Width(descW).Render("Description"),
		styleColHeader.Width(projW).Render("Project"),
		styleColHeader.Width(tagW).Render("Tags"),
		styleColHeader.Width(dueW).Render("Due"),
	)
	sep := styleMuted.Render(strings.Repeat("─", m.width))

	tasks := m.filteredTasks()
	if len(tasks) == 0 {
		msg := "No pending tasks · press a to add one"
		if m.searching {
			msg = "No tasks match your search"
		}
		empty := lipgloss.NewStyle().
			Foreground(colMuted).
			Width(m.width).
			Align(lipgloss.Center).
			Padding(2, 0).
			Render(msg)
		return lipgloss.JoinVertical(lipgloss.Left, headerRow, sep, empty)
	}

	tableH := max(1, availH-2) // subtract col header + sep
	start := 0
	if m.cursor >= tableH {
		start = m.cursor - tableH + 1
	}
	end := min(start+tableH, len(tasks))

	var rows []string
	rows = append(rows, headerRow, sep)
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(tasks[i], i == m.cursor, idW, priW, actW, descW, projW, tagW, dueW))
	}
	return strings.Join(rows, "\n")
}

func (m model) renderRow(t Task, selected bool, idW, priW, actW, descW, projW, tagW, dueW int) string {
	id  := styleMuted.Width(idW).Render(fmt.Sprintf("%d", t.ID))
	pri := m.renderPri(t.Priority, priW)
	act := styleMuted.Width(actW).Render(" ")
	if t.IsActive() {
		act = styleActive.Width(actW).Render("▶")
	}

	desc := truncate(t.Description, descW-1)
	proj := truncate(t.Project, projW-1)
	tags := truncate(strings.Join(t.Tags, ","), tagW-1)
	due  := m.renderDue(t, dueW)

	descS := styleFg.Width(descW).Render(desc)
	projS := styleMuted.Width(projW).Render(proj)
	tagS  := styleMuted.Width(tagW).Render(tags)

	row := lipgloss.JoinHorizontal(lipgloss.Left, id, pri, act, descS, projS, tagS, due)

	if selected {
		return styleSelectedRow.Width(m.width).Padding(0, 1).Render(row)
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(row)
}

func (m model) renderPri(p string, w int) string {
	switch p {
	case "H":
		return stylePriH.Width(w).Render("H")
	case "M":
		return stylePriM.Width(w).Render("M")
	case "L":
		return stylePriL.Width(w).Render("L")
	default:
		return stylePri0.Width(w).Render("·")
	}
}

func (m model) renderDue(t Task, w int) string {
	due, ok := t.ParseDue()
	if !ok {
		return styleMuted.Width(w).Render("")
	}
	diff := time.Until(due)
	switch {
	case diff < 0:
		return styleOverdue.Width(w).Render("overdue")
	case diff < 24*time.Hour:
		return styleDueSoon.Width(w).Render("today")
	case diff < 48*time.Hour:
		return styleDueSoon.Width(w).Render("tomorrow")
	default:
		return styleDueOk.Width(w).Render(due.Format("Jan 02"))
	}
}

// ── Detail pane ───────────────────────────────────────────────────────────────

func (m model) renderDetailPane() string {
	borderCol := colMuted
	if m.detailFocused {
		borderCol = colCyan
	}

	innerW := m.width - 4 // border (2) + 1px margin each side
	panelStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Padding(0, 2).
		Width(innerW).
		Height(detailPaneH - 2)

	if len(m.tasks) == 0 || len(m.editFields) == 0 {
		return panelStyle.Render(styleMuted.Render("No task selected"))
	}

	t := m.tasks[m.cursor]

	var lines []string

	if m.detailFocused {
		// Editable: all fields with active text inputs
		for i, f := range m.editFields {
			label := styleDetailLabel.Render(f.label)
			var val string
			if i == m.editFocus {
				val = f.input.View()
			} else {
				v := f.input.Value()
				if v == "" {
					val = styleMuted.Render(f.input.Placeholder)
				} else {
					val = styleDetailValue.Render(v)
				}
			}
			lines = append(lines, label+val)
		}
	} else {
		// Read-only: title + all metadata including new fields
		due := "—"
		if d, ok := t.ParseDue(); ok {
			due = d.Format("2006-01-02")
		}
		created := "—"
		if e, ok := parseTaskDate(t.Entry); ok {
			created = e.Format("2006-01-02")
		}
		depends := dash(m.dependsToIDs(t.Depends))

		modified := "—"
		if mod, ok := parseTaskDate(t.Modified); ok {
			modified = mod.Format("2006-01-02 15:04")
		}
		status := t.Status
		if t.IsActive() {
			status = "active"
		}
		row := func(label, value string) string {
			return styleDetailLabel.Render(label) + styleDetailValue.Render(value)
		}
		scheduled := "—"
		if s, ok := parseTaskDate(t.Scheduled); ok {
			scheduled = s.Format("2006-01-02")
		}
		wait := "—"
		if w, ok := parseTaskDate(t.Wait); ok {
			wait = w.Format("2006-01-02")
		}
		lines = append(lines,
			row("ID", fmt.Sprintf("%d", t.ID)),
			lipgloss.NewStyle().Foreground(colPurple).Bold(true).Render(truncate(t.Description, innerW-6)),
			row("Status", status),
			row("Urgency", fmt.Sprintf("%.2f", t.Urgency)),
			row("Project", dash(t.Project)),
			row("Priority", dash(t.Priority)),
			row("Tags", dash(strings.Join(t.Tags, ", "))),
			row("Due", due),
			row("Scheduled", scheduled),
			row("Wait", wait),
			row("Recur", dash(t.Recur)),
			row("Depends", depends),
			row("Created", created),
			row("Modified", modified),
			row("UUID", t.UUID),
		)
		if len(t.Annotations) > 0 {
			lines = append(lines, styleDetailLabel.Render("Notes"))
			for _, a := range t.Annotations {
				lines = append(lines, "  "+styleMuted.Render("·")+" "+styleDetailValue.Render(truncate(a.Description, innerW-6)))
			}
		}
	}

	return panelStyle.Render(strings.Join(lines, "\n"))
}

// ── Projects view ─────────────────────────────────────────────────────────────

func (m model) renderProjects(maxH int) string {
	projects := m.projectSummaries()

	nameW  := max(20, m.width-42)
	totalW := 8
	activeW := 8
	overdueW := 10

	headerRow := "  " + lipgloss.JoinHorizontal(lipgloss.Left,
		styleColHeader.Width(nameW).Render("Project"),
		styleColHeader.Width(totalW).Render("Tasks"),
		styleColHeader.Width(activeW).Render("Active"),
		styleColHeader.Width(overdueW).Render("Overdue"),
	)
	sep := styleMuted.Render(strings.Repeat("─", m.width))

	if len(projects) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(colMuted).
			Width(m.width).
			Align(lipgloss.Center).
			Padding(4, 0).
			Render("No projects yet · add tasks with a project field")
		return lipgloss.JoinVertical(lipgloss.Left, headerRow, sep, empty)
	}

	tableH := max(1, maxH-2)
	start := 0
	if m.projectCursor >= tableH {
		start = m.projectCursor - tableH + 1
	}
	end := min(start+tableH, len(projects))

	var rows []string
	rows = append(rows, headerRow, sep)
	for i := start; i < end; i++ {
		p := projects[i]
		name    := styleFg.Width(nameW).Render(truncate(p.name, nameW-1))
		total   := styleMuted.Width(totalW).Render(fmt.Sprintf("%d", p.total))
		active  := styleMuted.Width(activeW).Render(fmt.Sprintf("%d", p.active))
		var overdue string
		if p.overdue > 0 {
			overdue = styleOverdue.Width(overdueW).Render(fmt.Sprintf("%d", p.overdue))
		} else {
			overdue = styleMuted.Width(overdueW).Render("0")
		}
		row := lipgloss.JoinHorizontal(lipgloss.Left, name, total, active, overdue)
		if i == m.projectCursor {
			rows = append(rows, styleSelectedRow.Width(m.width).Padding(0, 1).Render(row))
		} else {
			rows = append(rows, lipgloss.NewStyle().Padding(0, 1).Render(row))
		}
	}
	return strings.Join(rows, "\n")
}

// ── Gantt chart ───────────────────────────────────────────────────────────────

func (m model) renderGantt(projectName string, availH int) string {
	if availH < 4 {
		return ""
	}

	// Collect pending + completed/deleted tasks for this project
	var tasks []Task
	for _, t := range append(m.tasks, m.completedTasks...) {
		name := t.Project
		if name == "" {
			name = "(no project)"
		}
		if name == projectName {
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		ei, _ := parseTaskDate(tasks[i].Entry)
		ej, _ := parseTaskDate(tasks[j].Entry)
		return ei.Before(ej)
	})

	// Time window
	today := time.Now().Truncate(24 * time.Hour)
	statusColW := 2
	nameColW := 20
	leftW := statusColW + nameColW
	timelineW := max(10, m.width-leftW-2)
	lookback := timelineW / 4
	winStart := today.AddDate(0, 0, -lookback)
	winEnd := today.AddDate(0, 0, timelineW-lookback-1)
	todayCol := lookback

	// Section header
	title := styleColHeader.Render("Gantt  ") + styleMuted.Render(projectName)
	divider := styleMuted.Render(strings.Repeat("─", m.width))

	// Single axis row: weekly labels + ▼ today marker
	type axisSeg struct {
		col   int
		text  string
		style lipgloss.Style
	}
	var segs []axisSeg
	cur := winStart
	for cur.Weekday() != time.Monday {
		cur = cur.AddDate(0, 0, 1)
	}
	for !cur.After(winEnd) {
		col := int(cur.Sub(winStart).Hours() / 24)
		if col >= 0 && col+6 <= timelineW {
			segs = append(segs, axisSeg{col, cur.Format("Jan 02"), styleMuted})
		}
		cur = cur.AddDate(0, 0, 7)
	}
	segs = append(segs, axisSeg{todayCol, "▼", styleActive})
	sort.Slice(segs, func(i, j int) bool { return segs[i].col < segs[j].col })
	axisStr := strings.Repeat(" ", leftW)
	pos := 0
	for _, seg := range segs {
		if seg.col < pos {
			continue // overlapping label — skip
		}
		if seg.col > pos {
			axisStr += strings.Repeat(" ", seg.col-pos)
		}
		axisStr += seg.style.Render(seg.text)
		pos = seg.col + len([]rune(seg.text))
	}

	lines := []string{title, divider, axisStr}
	taskRows := availH - 3
	overflow := 0

	for i, t := range tasks {
		if i >= taskRows {
			overflow = len(tasks) - taskRows
			break
		}

		entryDate, _ := parseTaskDate(t.Entry)
		var barStart, barEnd time.Time

		if t.Scheduled != "" {
			barStart, _ = parseTaskDate(t.Scheduled)
		} else {
			barStart = entryDate
		}

		switch t.Status {
		case "completed", "deleted":
			if t.End != "" {
				barEnd, _ = parseTaskDate(t.End)
			} else {
				barEnd = barStart
			}
		default:
			if t.Due != "" {
				barEnd, _ = parseTaskDate(t.Due)
			} else if t.IsActive() {
				barEnd = today
			} else {
				barEnd = barStart
			}
		}

		startCol := int(barStart.Sub(winStart).Hours() / 24)
		endCol := int(barEnd.Sub(winStart).Hours() / 24)
		startCol = max(0, min(startCol, timelineW-1))
		endCol = max(0, min(endCol, timelineW-1))
		if startCol > endCol {
			startCol = endCol
		}

		// Status icon
		var statusIcon string
		switch {
		case t.Status == "completed":
			statusIcon = styleDueOk.Render("✓ ")
		case t.Status == "deleted":
			statusIcon = styleOverdue.Render("✗ ")
		case t.IsActive():
			statusIcon = styleActive.Render("▶ ")
		default:
			statusIcon = styleMuted.Render("· ")
		}

		// Bar char and style
		var barChar string
		var barStyle lipgloss.Style
		switch {
		case t.Status == "completed":
			barChar = "─"
			barStyle = styleDueOk
		case t.Status == "deleted":
			barChar = "─"
			barStyle = styleMuted
		case t.IsActive():
			barChar = "█"
			barStyle = styleActive
		case t.Due != "":
			dueTime, _ := parseTaskDate(t.Due)
			barChar = "─"
			if dueTime.Before(today) {
				barStyle = styleOverdue
			} else if dueTime.Before(today.Add(3 * 24 * time.Hour)) {
				barStyle = styleDueSoon
			} else {
				barStyle = styleDueOk
			}
		default:
			barChar = "◆"
			barStyle = styleMuted
		}

		length := max(1, endCol-startCol+1)
		barStr := barStyle.Render(strings.Repeat(barChar, length))
		nameStr := styleFg.Width(nameColW).Render(truncate(t.Description, nameColW-1))
		row := statusIcon + nameStr + strings.Repeat(" ", startCol) + barStr
		lines = append(lines, row)
	}

	if overflow > 0 {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  … %d more tasks", overflow)))
	}
	for len(lines) < availH {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// ── History view ──────────────────────────────────────────────────────────────

func (m model) renderHistory() string {
	statusW := 12
	projW   := 14
	endW    := 12
	// Width() in lipgloss includes padding, so -4 leaves m.width-4 content
	// which fits inside Width(m.width).Padding(0,1)'s content area of m.width-2
	descW := max(20, m.width-statusW-projW-endW-4)

	headerRow := "  " + lipgloss.JoinHorizontal(lipgloss.Left,
		styleColHeader.Width(descW).Render("Description"),
		styleColHeader.Width(statusW).Render("Status"),
		styleColHeader.Width(projW).Render("Project"),
		styleColHeader.Width(endW).Render("Ended"),
	)
	sep := styleMuted.Render(strings.Repeat("─", m.width))

	tasks := m.historyFiltered()
	if len(tasks) == 0 {
		msg := "No completed or deleted tasks yet"
		if m.historySearching || m.historySearchInput.Value() != "" {
			msg = "No history matches your search"
		}
		empty := lipgloss.NewStyle().
			Foreground(colMuted).
			Width(m.width).
			Align(lipgloss.Center).
			Padding(4, 0).
			Render(msg)
		return lipgloss.JoinVertical(lipgloss.Left, headerRow, sep, empty)
	}

	searchBarH := 0
	if m.historySearching {
		searchBarH = 1
	}
	availH := m.height - 3 - searchBarH - historyDetailH - 2
	tableH := max(1, availH-2)
	start := 0
	if m.historyCursor >= tableH {
		start = m.historyCursor - tableH + 1
	}
	end := min(start+tableH, len(tasks))

	var rows []string
	rows = append(rows, headerRow, sep)
	for i := start; i < end; i++ {
		t := tasks[i]

		desc := styleFg.Width(descW).Render(truncate(t.Description, descW-1))

		var statusS string
		if t.Status == "deleted" {
			statusS = styleError.Width(statusW).Render("deleted")
		} else {
			statusS = styleDueOk.Width(statusW).Render("completed")
		}

		proj   := styleMuted.Width(projW).Render(truncate(t.Project, projW-1))
		ended  := "—"
		if e, ok := parseTaskDate(t.End); ok {
			ended = e.Format("Jan 02 2006")
		}
		endS := styleMuted.Width(endW).Render(ended)

		row := lipgloss.JoinHorizontal(lipgloss.Left, desc, statusS, proj, endS)
		if i == m.historyCursor {
			rows = append(rows, styleSelectedRow.Width(m.width).Padding(0, 1).Render(row))
		} else {
			rows = append(rows, lipgloss.NewStyle().Padding(0, 1).Render(row))
		}
	}
	return strings.Join(rows, "\n")
}

func (m model) renderHistoryDetail() string {
	innerW := m.width - 4
	panelStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colMuted).
		Padding(0, 2).
		Width(innerW).
		Height(historyDetailH - 2)

	ft := m.historyFiltered()
	if len(ft) == 0 {
		return panelStyle.Render(styleMuted.Render("No task selected"))
	}
	if m.historyCursor >= len(ft) {
		return panelStyle.Render(styleMuted.Render("No task selected"))
	}

	t := ft[m.historyCursor]

	ended := "—"
	if e, ok := parseTaskDate(t.End); ok {
		ended = e.Format("2006-01-02")
	}
	created := "—"
	if e, ok := parseTaskDate(t.Entry); ok {
		created = e.Format("2006-01-02")
	}
	var statusStyle lipgloss.Style
	if t.Status == "deleted" {
		statusStyle = styleError
	} else {
		statusStyle = styleDueOk
	}

	row := func(label, value string) string {
		return styleDetailLabel.Render(label) + styleDetailValue.Render(value)
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(colPurple).Bold(true).Render(truncate(t.Description, innerW-6)),
		styleDetailLabel.Render("Status") + statusStyle.Render(t.Status),
		row("Project", dash(t.Project)),
		row("Priority", dash(t.Priority)),
		row("Tags", dash(strings.Join(t.Tags, ", "))),
		row("Ended", ended),
		row("Created", created),
	}

	return panelStyle.Render(strings.Join(lines, "\n"))
}

// ── Footer ────────────────────────────────────────────────────────────────────

func (m model) renderFooter() string {
	var bindings [][2]string
	switch {
	case m.detailFocused:
		bindings = [][2]string{
			{"↑/↓", "next field"},
			{"enter", "save"},
			{"esc", "cancel"},
		}
	case m.state == viewProjects:
		bindings = [][2]string{
			{"j/k", "navigate"},
			{"tab", "switch tab"},
			{"q", "quit"},
		}
	case m.historySearching:
		bindings = [][2]string{
			{"type", "filter"},
			{"enter", "confirm"},
			{"esc", "clear"},
		}
	case m.state == viewHistory:
		bindings = [][2]string{
			{"j/k", "navigate"},
			{"u", "restore"},
			{"x", "purge"},
			{"/", "search"},
			{"tab", "switch tab"},
			{"q", "quit"},
		}
	case m.searching:
		bindings = [][2]string{
			{"type", "filter"},
			{"enter", "confirm"},
			{"esc", "clear"},
		}
	default:
		bindings = [][2]string{
			{"j/k", "navigate"},
			{"enter", "edit"},
			{"a", "add"},
			{"s", "start/stop"},
			{"d", "done"},
			{"x", "delete"},
			{"/", "search"},
			{"tab", "switch tab"},
			{"q", "quit"},
		}
	}
	var parts []string
	for _, b := range bindings {
		parts = append(parts, styleHelpKey.Render(b[0])+styleHelpDesc.Render(" "+b[1]))
	}
	return lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colMuted).
		Width(m.width).
		Padding(0, 1).
		Render(strings.Join(parts, styleMuted.Render("  ·  ")))
}

// ── Confirm overlay ───────────────────────────────────────────────────────────

func (m model) renderConfirm() string {
	msg   := styleError.Bold(true).Render(m.confirmMsg)
	hint  := styleMuted.Render("y") + styleHelpDesc.Render(" confirm  ") + styleMuted.Render("any other key") + styleHelpDesc.Render(" cancel")
	inner := lipgloss.JoinVertical(lipgloss.Center, msg, "", hint)
	panel := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colRed).
		Padding(1, 4).
		Render(inner)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(panel)
}

// ── Add view ──────────────────────────────────────────────────────────────────

func (m model) renderAdd() string {
	title := lipgloss.NewStyle().Foreground(colCyan).Bold(true).Render("New task")
	hint  := styleMuted.Render("enter to confirm · esc to cancel")
	inner := lipgloss.JoinVertical(lipgloss.Left, title+"\n", m.input.View(), "\n"+hint)
	panel := styleInputPanel.Width(min(60, m.width-8)).Render(inner)

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(panel)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen(), tea.WithFPS(60))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

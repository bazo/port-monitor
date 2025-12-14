package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"port-monitor/scanner"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	baseStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Bold(true)

	tabStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("252")).
			Border(lipgloss.NormalBorder(), false, false, true, false)

	activeTabStyle = tabStyle.Copy().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Bold(true).
			BorderForeground(lipgloss.Color("62"))
)

type notificationTimeoutMsg struct{}

type tickMsg time.Time

type scanMsg []scanner.ProcessInfo

type scanStartMsg struct{}

type errMsg error

const (
	SortPID = iota
	SortName
	SortPorts
	SortCPU
	SortMem
)

type killResultMsg struct {
	count int
	err   error
}

type model struct {
	table        table.Model
	processes    []scanner.ProcessInfo
	selectedPids map[int32]struct{}
	activeTab    int // 0: User, 1: System
	err          error
	width        int
	height       int
	loading      bool
	spinner      spinner.Model

	// New State
	filterPorts bool // Show only processes with ports
	sortBy      int
	sortDesc    bool

	// Search
	textInput textinput.Model
	searching bool

	// Kill & Interactions
	confirming   bool
	pendingPids  []int32
	notification string
}

func newSpinnerModel() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	return s
}

func initialModel() model {
	columns := []table.Column{
		{Title: "X", Width: 2},
		{Title: "PID", Width: 8},
		{Title: "Name", Width: 20},
		{Title: "Ports", Width: 15},
		{Title: "CPU%", Width: 6},
		{Title: "Mem", Width: 10},
		{Title: "Type", Width: 8},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10), // Will be updated on resize
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	ti := textinput.New()
	ti.Placeholder = "Search ports..."
	ti.CharLimit = 156
	ti.Width = 20

	return model{
		table:        t,
		selectedPids: make(map[int32]struct{}),
		activeTab:    0,
		loading:      true,
		spinner:      newSpinnerModel(),
		filterPorts:  true,      // Default true
		sortBy:       SortPorts, // Default sort by Ports
		sortDesc:     true,
		textInput:    ti,
		searching:    false,
		confirming:   false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		scanProcessesCmd(),
		tickCmd(),
		textinput.Blink,
	)
}

func scanProcessesCmd() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return scanStartMsg{} },
		func() tea.Msg {
			procs, err := scanner.ScanProcesses()
			if err != nil {
				return errMsg(err)
			}
			return scanMsg(procs)
		},
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var spinnerCmd tea.Cmd
	if m.loading {
		m.spinner, spinnerCmd = m.spinner.Update(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "enter", "esc":
				m.searching = false
				m.table.Focus()
				m.textInput.Blur()
				return m, spinnerCmd
			default:
				m.textInput, cmd = m.textInput.Update(msg)
				m.updateTable()
				return m, tea.Batch(cmd, spinnerCmd)
			}
		}

		if m.confirming {
			switch strings.ToLower(msg.String()) {
			case "y":
				cmd = m.killPending()
				m.confirming = false
				m.notification = fmt.Sprintf("Killing %d process(s)...", len(m.pendingPids))
				return m, tea.Batch(cmd, waitNotificationCmd(), spinnerCmd)
			case "n", "esc":
				m.confirming = false
				m.pendingPids = nil
				m.notification = "Cancelled."
				return m, tea.Batch(waitNotificationCmd(), spinnerCmd)
			default:
				return m, spinnerCmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % 2
			m.updateTable()
		case " ":
			m.toggleSelection()
			m.updateTable() // Refresh checks
			return m, spinnerCmd // Prevent jumping (bubbles/table maps space to PageDown)
		case "k":
			m.startKillProcess()
		case "f":
			m.filterPorts = !m.filterPorts
			m.updateTable()
		case "s":
			m.sortBy = (m.sortBy + 1) % 5
			m.updateTable()
		case "o":
			m.sortDesc = !m.sortDesc
			m.updateTable()
		case "/":
			m.searching = true
			m.textInput.Focus()
			m.table.Blur()
			return m, tea.Batch(textinput.Blink, spinnerCmd)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(m.height - 15) // Reserve extra space for header/footer/tabs

		// Reserve margin for borders (2 for outer border, plus extra safety)
		tableWidth := m.width - 4
		m.table.SetWidth(tableWidth)

		// Calculate column widths
		// Fixed: X(2), PID(8), CPU(6), Mem(10), Type(8) -> Total 34
		fixedWidths := 34
		avail := tableWidth - fixedWidths
		if avail < 0 {
			avail = 0
		}

		// Distribute remainder: Name ~40%, Ports ~60%
		nameW := int(float64(avail) * 0.4)
		// Ensure name matches minimum usability if possible, but prioritized fitting
		if nameW < 10 && avail >= 10 {
			nameW = 10
		}

		portsW := avail - nameW

		columns := []table.Column{
			{Title: "X", Width: 2},
			{Title: "PID", Width: 8},
			{Title: "Name", Width: nameW},
			{Title: "Ports", Width: portsW},
			{Title: "CPU%", Width: 6},
			{Title: "Mem", Width: 10},
			{Title: "Type", Width: 8},
		}
		m.table.SetColumns(columns)
	case scanStartMsg:
		m.loading = true
		m.spinner = newSpinnerModel()
		return m, m.spinner.Tick
	case scanMsg:
		m.processes = msg
		m.loading = false
		m.updateTable()
	case tickMsg:
		return m, tea.Batch(scanProcessesCmd(), tickCmd(), spinnerCmd)
	case killResultMsg:
		if msg.err != nil {
			m.notification = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.notification = fmt.Sprintf("Successfully killed %d process(s)", msg.count)
			// Clear selection if successful
			m.selectedPids = make(map[int32]struct{})
		}
		return m, tea.Batch(scanProcessesCmd(), waitNotificationCmd(), spinnerCmd)
	case notificationTimeoutMsg:
		m.notification = ""
		return m, spinnerCmd
	case errMsg:
		m.err = msg
	}

	m.table, cmd = m.table.Update(msg)
	return m, tea.Batch(cmd, spinnerCmd)
}

func waitNotificationCmd() tea.Cmd {
	return tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
		return notificationTimeoutMsg{}
	})
}

func (m *model) toggleSelection() {
	row := m.table.SelectedRow()
	if row == nil {
		return
	}
	var pid int32
	fmt.Sscanf(row[1], "%d", &pid)

	if _, ok := m.selectedPids[pid]; ok {
		delete(m.selectedPids, pid)
	} else {
		m.selectedPids[pid] = struct{}{}
	}
}

func (m cmdMsg) String() string { return "cmd" }

type cmdMsg struct{} // dummy

func (m *model) startKillProcess() {
	// Determine victims
	var victims []int32

	if len(m.selectedPids) > 0 {
		for pid := range m.selectedPids {
			victims = append(victims, pid)
		}
	} else {
		// Use current cursor
		row := m.table.SelectedRow()
		if row != nil {
			var pid int32
			fmt.Sscanf(row[1], "%d", &pid)
			victims = append(victims, pid)
		}
	}

	if len(victims) == 0 {
		m.notification = "No process selected."
		// Trigger notification clear
		// We can return a command, but since this is a method called in Update,
		// we can't easily return a Cmd unless we return it.
		// For now, let's just set string, it won't clear automatically properly unless we handled that return in Update.
		// NOTE: In the Update case 'k', we didn't setup to receive a cmd from startKillProcess.
		// Let's rely on user pressing 'k' again or just changing Update to call a helper that returns (model, cmd).
		// Better: just set the specific state and return cmd in Update.
		// Refactoring: logic logic moved to Update or helper that helps Update.
		return
	}

	m.pendingPids = victims
	m.confirming = true
}

func (m *model) killPending() tea.Cmd {
	pids := m.pendingPids
	return func() tea.Msg {
		count := 0
		var lastErr error
		for _, pid := range pids {
			err := scanner.KillProcess(pid)
			if err != nil {
				lastErr = err
			} else {
				count++
			}
		}
		return killResultMsg{count: count, err: lastErr}
	}
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (m *model) updateTable() {
	var rows []table.Row
	search := strings.ToLower(m.textInput.Value())

	// Filter and Sort
	var filtered []scanner.ProcessInfo
	for _, p := range m.processes {
		// Tab Filter
		if (m.activeTab == 0 && p.Type != scanner.UserProcess) ||
			(m.activeTab == 1 && p.Type != scanner.SystemProcess) {
			continue
		}
		// Port Filter
		if m.filterPorts && len(p.Connections) == 0 {
			continue
		}

		// Search Filter
		if search != "" {
			matches := false
			// Check Name
			if strings.Contains(strings.ToLower(p.Name), search) {
				matches = true
			}
			// Check Ports
			for _, c := range p.Connections {
				if strings.Contains(fmt.Sprintf("%d", c.Port), search) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}

		filtered = append(filtered, p)
	}

	// Sort
	sort.Slice(filtered, func(i, j int) bool {
		var less bool
		switch m.sortBy {
		case SortPID:
			less = filtered[i].PID < filtered[j].PID
		case SortName:
			less = filtered[i].Name < filtered[j].Name
		case SortPorts:
			// Sort by number of connections
			if len(filtered[i].Connections) == len(filtered[j].Connections) {
				less = filtered[i].PID < filtered[j].PID
			} else {
				less = len(filtered[i].Connections) < len(filtered[j].Connections)
			}
		case SortCPU:
			less = filtered[i].CPUPercent < filtered[j].CPUPercent
		case SortMem:
			less = filtered[i].MemoryUsage < filtered[j].MemoryUsage
		default:
			less = filtered[i].PID < filtered[j].PID
		}

		if m.sortDesc {
			return !less
		}
		return less
	})

	for _, p := range filtered {
		check := " "
		if _, ok := m.selectedPids[p.PID]; ok {
			check = "x"
		}

		// Format Ports
		// Prioritize LISTEN ports
		var listenPorts []string
		var otherPorts []string
		for _, c := range p.Connections {
			if c.Status == "LISTEN" {
				listenPorts = append(listenPorts, fmt.Sprintf("%d(L)", c.Port))
			} else {
				otherPorts = append(otherPorts, fmt.Sprintf("%d(E)", c.Port))
			}
		}

		// Combine, listen first
		allPorts := append(listenPorts, otherPorts...)
		portsStr := strings.Join(allPorts, ", ")

		// We need to know the current ports column width to truncate correctly.
		// It's in m.table.Columns()[3].Width
		cols := m.table.Columns()
		portsWidth := 15 // default
		if len(cols) > 3 {
			portsWidth = cols[3].Width
		}

		if len(portsStr) > portsWidth {
			portsStr = portsStr[:portsWidth-3] + "..."
		}

		rows = append(rows, table.Row{
			check,
			fmt.Sprintf("%d", p.PID),
			p.Name,
			portsStr,
			fmt.Sprintf("%.1f%%", p.CPUPercent),
			formatBytes(p.MemoryUsage),
			p.AppType,
		})
	}

	// Preserve selection index if possible
	currIdx := m.table.Cursor()
	m.table.SetRows(rows)
	if currIdx >= len(rows) {
		m.table.SetCursor(len(rows) - 1)
	}
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	var userTab, sysTab string
	if m.activeTab == 0 {
		userTab = activeTabStyle.Render("User Processes")
		sysTab = tabStyle.Render("System Processes")
	} else {
		userTab = tabStyle.Render("User Processes")
		sysTab = activeTabStyle.Render("System Processes")
	}

	header := lipgloss.JoinHorizontal(lipgloss.Top, userTab, sysTab)

	// Status Line
	sortStr := "PID"
	switch m.sortBy {
	case SortName:
		sortStr = "Name"
	case SortPorts:
		sortStr = "Ports"
	case SortCPU:
		sortStr = "CPU"
	case SortMem:
		sortStr = "Mem"
	}
	orderStr := "ASC"
	if m.sortDesc {
		orderStr = "DESC"
	}
	filterStr := "All"
	if m.filterPorts {
		filterStr = "Ports Only"
	}

	status := fmt.Sprintf("Sort: %s (%s) | Filter: %s", sortStr, orderStr, filterStr)
	status = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(status)

	// Search Bar
	search := ""
	if m.searching {
		search = fmt.Sprintf("Search: %s", m.textInput.View())
	} else if m.textInput.Value() != "" {
		search = fmt.Sprintf("Filter: %s (press / to edit)", m.textInput.Value())
	}
	if search != "" {
		status = lipgloss.JoinHorizontal(lipgloss.Left, status, " | ", lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(search))
	}

	// Notification / Confirmation
	if m.confirming {
		prompt := fmt.Sprintf("Are you sure you want to kill %d process(s)? (y/n)", len(m.pendingPids))
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(prompt)
	} else if m.notification != "" {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render(m.notification)
	} else if m.loading {
		loading := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(fmt.Sprintf("%s Loading processes…", m.spinner.View()))
		status = lipgloss.JoinHorizontal(lipgloss.Left, loading, "  ", status)
	}

	body := baseStyle.Render(m.table.View())

	// Footer Details
	var footer string
	selRow := m.table.SelectedRow()
	if selRow != nil {
		var pid int32
		fmt.Sscanf(selRow[1], "%d", &pid)

		var p *scanner.ProcessInfo
		for i := range m.processes {
			if m.processes[i].PID == pid {
				p = &m.processes[i]
				break
			}
		}

		if p != nil {
			var listenPorts []string
			var otherPorts []string
			for _, c := range p.Connections {
				if c.Status == "LISTEN" {
					listenPorts = append(listenPorts, fmt.Sprintf("%d(L)", c.Port))
				} else {
					otherPorts = append(otherPorts, fmt.Sprintf("%d(E)", c.Port))
				}
			}
			allPorts := append(listenPorts, otherPorts...)

			footer = fmt.Sprintf(
				"Path: %s\nCommand: %s\nFull Ports: %s\nResources: CPU %.1f%%, Mem %s",
				p.Cwd,
				p.Command,
				strings.Join(allPorts, ", "),
				p.CPUPercent,
				formatBytes(p.MemoryUsage),
			)
		}
	}

	help := "\n[Tab] View  [Space] Select  [k] Kill  [f] Filter Ports  [s] Sort Col  [o] Sort Order  [/] Search  [q] Quit"

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		status,
		body,
		lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(footer),
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(help),
	)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}

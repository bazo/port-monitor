package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"port-monitor/scanner"

	"github.com/charmbracelet/bubbles/table"
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
			BorderForeground(lipgloss.Color("62")) // Keep the bottom border color or remove it?
		// Let's make it look like a real tab, maybe remove bottom border for active?
		// For now, just better colors.
)

type tickMsg time.Time

type scanMsg []scanner.ProcessInfo

type errMsg error

const (
	SortPID = iota
	SortName
	SortPorts
	SortCPU
	SortMem
)

type model struct {
	table        table.Model
	processes    []scanner.ProcessInfo
	selectedPids map[int32]struct{}
	activeTab    int // 0: User, 1: System
	err          error
	width        int
	height       int
	loading      bool

	// New State
	filterPorts bool // Show only processes with ports
	sortBy      int
	sortDesc    bool
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

	return model{
		table:        t,
		selectedPids: make(map[int32]struct{}),
		activeTab:    0,
		loading:      true,
		filterPorts:  true,      // Default true
		sortBy:       SortPorts, // Default sort by Ports? Or PID. Request says "by default show only those that have". Maybe sort by ports count or just existence?
		sortDesc:     true,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		scanProcessesCmd,
		tickCmd(),
	)
}

func scanProcessesCmd() tea.Msg {
	procs, err := scanner.ScanProcesses()
	if err != nil {
		return errMsg(err)
	}
	return scanMsg(procs)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % 2
			m.updateTable()
		case " ":
			m.toggleSelection()
			m.updateTable() // Refresh checks
		case "k":
			if len(m.selectedPids) > 0 {
				cmd := m.killSelected()
				m.selectedPids = make(map[int32]struct{}) // Clear selection immediately
				return m, cmd
			}
		case "f":
			m.filterPorts = !m.filterPorts
			m.updateTable()
		case "s":
			m.sortBy = (m.sortBy + 1) % 5
			m.updateTable()
		case "o":
			m.sortDesc = !m.sortDesc
			m.updateTable()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(m.height - 15) // Reserve extra space for header/footer/tabs
		m.table.SetWidth(m.width - 4)    // Reserve minimal margin

		// Calculate column widths
		// Fixed: X(2), PID(8), CPU(6), Mem(10), Type(8) -> Total 34
		// Table container has 2px border (from baseStyle)
		// So available space for columns is (width - 4)
		effectiveWidth := m.width - 4

		fixedWidths := 34
		avail := effectiveWidth - fixedWidths
		if avail < 35 {
			avail = 35 // Minimum for Name+Ports
		}

		// Distribute remainder
		// Name gets ~40%, Ports gets ~60%
		nameW := int(float64(avail) * 0.4)
		if nameW < 20 {
			nameW = 20
		}

		portsW := avail - nameW
		if portsW < 15 {
			portsW = 15
		}

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
	case scanMsg:
		m.processes = msg
		m.loading = false
		m.updateTable()
	case tickMsg:
		return m, tea.Batch(scanProcessesCmd, tickCmd())
	case errMsg:
		m.err = msg
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
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

func (m *model) killSelected() tea.Cmd {
	// Copy PIDs to slice to avoid data race in the command closure
	var pids []int32
	for pid := range m.selectedPids {
		pids = append(pids, pid)
	}

	return func() tea.Msg {
		for _, pid := range pids {
			scanner.KillProcess(pid)
		}
		return scanProcessesCmd()
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

	// Filter and Sort
	var filtered []scanner.ProcessInfo
	for _, p := range m.processes {
		// Tab Filter
		if (m.activeTab == 0 && p.Type != scanner.UserProcess) ||
			(m.activeTab == 1 && p.Type != scanner.SystemProcess) {
			continue
		}
		// Port Filter
		// Filter if no ports connected? Or no LISTENING ports?
		// "show processes that have a port open".
		// Let's filter if len(Connections) == 0.
		if m.filterPorts && len(p.Connections) == 0 {
			continue
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

	help := "\n[Tab] View  [Space] Select  [k] Kill  [f] Filter Ports  [s] Sort Col  [o] Sort Order  [q] Quit"

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

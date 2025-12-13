package scanner

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type ProcessType string

const (
	SystemProcess ProcessType = "System"
	UserProcess   ProcessType = "User"
)

type ProcessInfo struct {
	PID         int32
	Name        string
	User        string
	Type        ProcessType
	Connections []Connection
	Cwd         string
	Command     string
	AppType     string // GUI, CLI, Daemon (heuristic)
	IsSelected  bool   // For UI selection
	CPUPercent  float64
	MemoryUsage uint64 // RSS in bytes
}

type Connection struct {
	Port   uint32
	Status string
}

func ScanProcesses() ([]ProcessInfo, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	procs, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	var results []ProcessInfo

	// Get all network connections once to map them to PIDs
	connections, err := net.Connections("inet")
	connMap := make(map[int32][]Connection)
	if err == nil {
		for _, conn := range connections {
			// We capture all, but maybe we want to group or filter by interesting ones?
			// The user just said "distinguish".
			c := Connection{
				Port:   conn.Laddr.Port,
				Status: conn.Status,
			}
			connMap[conn.Pid] = append(connMap[conn.Pid], c)
		}
	}

	for _, p := range procs {
		// Basic info
		name, err := p.Name()
		if err != nil {
			continue // Process might have terminated
		}

		// User
		username, err := p.Username()
		if err != nil {
			username = "unknown"
		}

		// Type
		pType := SystemProcess
		if username == currentUser.Username {
			pType = UserProcess
		}

		// Cwd
		cwd, err := p.Cwd()
		if err != nil {
			cwd = ""
		}

		// Command line
		cmdline, err := p.Cmdline()
		if err != nil {
			cmdline = ""
		}

		// Connections
		conns := connMap[p.Pid]

		// CPU & Mem
		cpuPct, err := p.Percent(0)
		if err != nil {
			cpuPct = 0
		}

		memInfo, err := p.MemoryInfo()
		var memUsage uint64
		if err == nil {
			memUsage = memInfo.RSS
		}

		// App Type Heuristic (Very basic)
		appType := "Unknown"
		if strings.HasPrefix(cwd, "/Applications") || strings.HasSuffix(name, ".app") {
			appType = "GUI App"
		} else if strings.Contains(cmdline, " go run ") || strings.HasPrefix(filepath.Base(cwd), "apps") {
			appType = "Dev Tool"
		} else {
			appType = "Binary"
		}

		results = append(results, ProcessInfo{
			PID:         p.Pid,
			Name:        name,
			User:        username,
			Type:        pType,
			Connections: conns,
			Cwd:         cwd,
			Command:     cmdline,
			AppType:     appType,
			CPUPercent:  cpuPct,
			MemoryUsage: memUsage,
		})
	}

	return results, nil
}

func KillProcess(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

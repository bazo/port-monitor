# Port Monitor

A TUI application to monitor processes and their ports.

## Features

- **Process List**: View running processes separated by User and System.
- **Port Monitoring**: See which ports are being used by each process.
- **Details**: View working directory and command details.
- **Filtering**: By default, only processes with open ports are shown. Toggle to see all processes.
- **Sorting**: Sort by PID, Name, Ports, CPU, or Memory.
- **Resource Usage**: Monitor CPU and Memory consumption.

## Installation

You can install `ports` easily using the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/bazo/port-monitor/main/install.sh | bash
```

Or you can download the binary for your system from the [Releases](https://github.com/bazo/port-monitor/releases) page.

## Usage

Run the application:

```bash
go run main.go
```

**Note**: To see system process details (like Working Directory, Ports, or Resource Usage) or to kill system processes, you might need to run with `sudo`:
```bash
sudo go run main.go
```

## Controls

- `Tab`: Switch between **User** and **System** processes.
- `Space`: Select/Deselect a process.
- `k`: Kill selected processes.
- `f`: Toggle **Ports Only** filter.
- `s`: Cycle sort column (PID -> Name -> Ports -> CPU -> Mem).
- `o`: Toggle sort order (ASC/DESC).
- `q`: Quit.

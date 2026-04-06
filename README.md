# taskr

A terminal UI for [Taskwarrior](https://taskwarrior.org/), built with [Bubbletea](https://github.com/charmbracelet/bubbletea). Tokyo Night colour scheme.

## Install

```bash
sudo curl -L https://github.com/Luciphere/taskr/releases/latest/download/taskr \
  -o /usr/local/bin/taskr && sudo chmod +x /usr/local/bin/taskr
```

Requires Taskwarrior (`task`) to be installed.

## Features

- Task list with inline search (`/`)
- Split-pane detail view with inline editing (↑/↓ to move between fields)
- Start/stop, mark done, delete tasks
- Projects tab with task stats
- History tab — browse completed & deleted tasks, restore or purge them
- Tokyo Night colours throughout

## Keybindings

### Tasks tab
| Key | Action |
|-----|--------|
| `j/k` | Navigate |
| `enter` | Edit selected task |
| `a` | Add new task |
| `s` | Start / stop task |
| `d` | Mark done |
| `x` | Delete task |
| `/` | Search |
| `tab` | Switch tab |
| `q` | Quit |

### Detail pane (edit mode)
| Key | Action |
|-----|--------|
| `↑/↓` | Move between fields |
| `enter` | Save |
| `esc` | Cancel |

### History tab
| Key | Action |
|-----|--------|
| `j/k` | Navigate |
| `u` | Restore task to pending |
| `x` | Permanently purge |
| `/` | Search |
| `tab` | Switch tab |

## Building from source

```bash
git clone https://github.com/Luciphere/taskr.git
cd taskr
go build -o taskr .
```

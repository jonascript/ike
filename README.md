# ike

An Eisenhower matrix task manager for the terminal, named for the president who
popularized the method. One binary, three ways in:

- **TUI** — run `ike` for an interactive 4-quadrant matrix
- **CLI** — `ike add`, `ike done`, … for quick capture and scripting
- **MCP** — `ike mcp` serves the Model Context Protocol so AI agents can manage your matrix

All three share a single JSON data file, safe against concurrent writes.

## The matrix

| | Urgent | Not urgent |
|---|---|---|
| **Important** | 1 · Do | 2 · Schedule |
| **Not important** | 3 · Delegate | 4 · Eliminate |

## Install

Requires Go 1.24+.

```sh
go install github.com/joncrockett/ike@latest
```

Or from a checkout, with a stamped version:

```sh
go build -ldflags "-X github.com/joncrockett/ike/internal/cli.version=$(git describe --tags --always)" -o ike .
```

## TUI

Run `ike` with no arguments.

| Key | Action |
|---|---|
| `1`–`4`, `tab` / `shift+tab` | focus quadrant |
| `j`/`k` or `↑`/`↓` | select task |
| `a` | add task to focused quadrant |
| `e` | edit task title |
| `x` / `enter` | complete task (moves to archive) |
| `m` then `1`–`4` | move task to quadrant |
| `d` twice | delete task permanently |
| `v` | archive view |
| `?` | toggle help |
| `q` | quit |

Changes made by the CLI or MCP server while the TUI is open appear within ~2 seconds.

## CLI

```sh
ike add "Fix prod bug" -q 1     # quadrant 1-4; defaults to 2 (Schedule)
ike list                        # active tasks grouped by quadrant
ike list -q 1 --json            # filter + machine-readable output
ike done 3                      # complete task 3 (archives it)
ike mv 3 2                      # move task 3 to quadrant 2
ike rm 3                        # delete permanently (no archive)
ike archive                     # completed tasks, newest first
```

## MCP

`ike mcp` runs an MCP server on stdio with tools `list_tasks`, `add_task`,
`complete_task`, `move_task`, `update_task`, `delete_task`, and `list_archive`.

Register with Claude Code:

```sh
claude mcp add ike -- ike mcp
```

Or in any MCP client config:

```json
{ "mcpServers": { "ike": { "command": "ike", "args": ["mcp"] } } }
```

## Data

Tasks live in `$XDG_DATA_HOME/ike/tasks.json` (default
`~/.local/share/ike/tasks.json`). Override with `IKE_DATA_FILE`. Writes are
atomic and serialized through a lock file, so the TUI, CLI, and MCP server can
run at the same time without losing updates.

Linux and macOS are supported; Windows is untested (path resolution assumes
XDG conventions).

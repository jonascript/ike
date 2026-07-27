<p align="center">
  <img src="assets/logo.svg" width="190" alt="Portrait of Dwight D. Eisenhower in a circular badge with five stars">
</p>

<h1 align="center">ike</h1>

<p align="center"><em>The Eisenhower matrix, in your terminal.</em></p>

---

An Eisenhower matrix task manager, named for the president who popularized the
method ("Ike" was his campaign-era nickname). One binary, three ways in:

- **TUI** — run `ike` for an interactive 4-quadrant matrix
- **CLI** — `ike add`, `ike done`, … for quick capture and scripting
- **MCP** — `ike mcp` serves the Model Context Protocol so AI agents can manage your matrix

All three share a single JSON data file, safe against concurrent writes.

## The matrix

| | Urgent | Not urgent |
|---|---|---|
| **Important** | 1 · Do It First | 2 · Schedule It |
| **Not important** | 3 · Delegate It | 4 · Consider Eliminating It |

Those headings are defaults, not fixed: rename any quadrant with `t` in the TUI
or `ike label`. The quadrant *numbers* never change, so scripts and the MCP
tools keep working whatever you call them.

## Install

Requires Go 1.25+.

```sh
go install github.com/jonascript/ike@latest
```

Or from a checkout, with a stamped version:

```sh
go build -ldflags "-X github.com/jonascript/ike/internal/cli.version=$(git describe --tags --always)" -o ike .
```

## TUI

Run `ike` with no arguments.

| Key | Action |
|---|---|
| `1`–`4`, `tab` / `shift+tab` | focus quadrant |
| `j`/`k` or `↑`/`↓` | select task |
| `J`/`K` or `shift+↑`/`shift+↓` | move task down/up within its quadrant |
| `a` | add task to focused quadrant |
| `e` | edit task title |
| `t` | rename the focused quadrant |
| `x` / `enter` | complete task (moves to archive) |
| `m` then `1`–`4` | move task to quadrant |
| `d` twice | delete task permanently |
| `u` | undo the last change |
| `U` / `ctrl+r` | redo |
| `v` | archive view (`r` there restores a task) |
| `?` | toggle help |
| `q` | quit |

Changes made by the CLI or MCP server while the TUI is open appear within ~2 seconds.

A dim `◆ mcp` marker sits in the footer while AI agent access is enabled; see
[MCP](#mcp). No marker means nothing but you can reach the matrix.

## CLI

```sh
ike add "Fix prod bug" -q 1     # quadrant 1-4; defaults to 2 (Schedule)
ike list                        # active tasks grouped by quadrant
ike list -q 1 --json            # filter + machine-readable output
ike done 3                      # complete task 3 (archives it)
ike mv 3 2                      # move task 3 to quadrant 2
ike reorder 3 up                # up | down | top | bottom, within its quadrant
ike rm 3                        # delete permanently (no archive)
ike archive                     # completed tasks, newest first
ike restore 3                   # un-archive task 3, back to its quadrant
ike undo                        # revert the last change, from any frontend
ike redo                        # re-apply the last undone change
ike label                       # show the four quadrant headings
ike label 1 "Firefighting"      # rename a quadrant
ike label 1 --reset             # restore its default name
```

## Renaming the quadrants

The default headings are *Do It First*, *Schedule It*, *Delegate It*, and
*Consider Eliminating It*. Rename any of them with `t` in the TUI or `ike label`
— up to 40 characters. Custom names are stored per data file and shared by all
three frontends; a rename is undoable like any other change.

Only the display name changes. Quadrant numbers 1–4 and their urgent/important
meanings are fixed, so `ike add -q 1`, the matrix layout, and the MCP tools are
unaffected by whatever you call them. Clearing a name (empty input, or
`--reset`) drops the override and restores the default, which also means a
future release's defaults reach you rather than being frozen at rename time.

## Undo and redo

Every change records a snapshot, so `ike undo` (or `u` in the TUI) reverts the
last one — including a delete, and including changes made from a different
frontend. Run it repeatedly to walk further back; the last 20 changes are kept
in the data file, so history survives restarts. Undo does not recycle task IDs.

`ike redo` (or `U` / `ctrl+r`) re-applies what you just undid, and is itself
undoable. **Any new change discards the redo history** — that includes a change
made from another frontend, so an MCP agent writing while your TUI is open will
clear a redo you were holding. The alternative is replaying a snapshot from a
branch you've diverged from, which would silently clobber the newer change. The
TUI only shows the redo hint while a redo is actually available.

## MCP

**Agent access is off by default.** ike will not serve your tasks over MCP
until you say so, and registering ike with an MCP client is not enough on its
own — `ike mcp` refuses to start while access is off:

```sh
ike mcp enable      # allow AI agents to read and manage this matrix
ike mcp disable     # revoke it, including for a session already connected
ike mcp status      # show the current setting and which data file it applies to
```

The setting is remembered in the data file, so it survives restarts and
upgrades, and it is scoped to that matrix: enabling access for your personal
tasks does not enable it for a second matrix under `IKE_DATA_FILE`. It is not
part of undo history — no sequence of `ike undo` can re-open access you closed.
While access is on, the TUI shows a `◆ mcp` marker in its footer, and `?`
explains the setting either way.

`ike mcp disable` takes effect immediately, including on a client that is
already connected: the setting is re-checked on every read and every write, so
the next thing an agent tries fails. It does not wait for the session to end.

> **What this is, and isn't.** The gate is a **consent mechanism, not a security
> boundary.** It controls whether ike's MCP server will serve your matrix. It
> cannot restrain an agent that already has shell access on your machine — such
> an agent can read `~/.local/share/ike/tasks.json` directly, run `ike list`, or
> simply run `ike mcp enable` itself. Treat it as "I have decided to let my
> agent manage my tasks", not as a sandbox.

Once enabled, `ike mcp` runs an MCP server on stdio with tools `list_tasks`,
`add_task`, `complete_task`, `move_task`, `reorder_task`, `update_task`,
`delete_task`, `list_archive`, `restore_task`, `undo`, `redo`, `list_quadrants`,
and `set_quadrant_label`.

Register with Claude Code:

```sh
ike mcp enable
claude mcp add ike -- ike mcp
```

Or in any MCP client config:

```json
{ "mcpServers": { "ike": { "command": "ike", "args": ["mcp"] } } }
```

## Data

Tasks live in `$XDG_DATA_HOME/ike/tasks.json` (default
`~/.local/share/ike/tasks.json`), created mode `0600` in a `0700` directory —
your matrix is not readable by other users on the machine.

Writes are serialized through a sidecar lock file and land via an atomic
rename, so the TUI, CLI, and MCP server can run at the same time without losing
updates. Each write is flushed to disk before the rename, and the previous
contents are kept as `tasks.json.bak`, so an interrupted write costs at most
one change rather than the whole matrix. If the file is ever unreadable, ike
refuses to overwrite it rather than starting fresh over the top.

Override the location with `IKE_DATA_FILE`, which must be an **absolute** path
whose parent directory already exists (a relative path would resolve against
whatever directory the process happened to start in — not something you can
predict when an MCP client launches ike for you).

Linux and macOS are supported; Windows is untested (path resolution assumes
XDG conventions). One caveat on the concurrency guarantee: it relies on
advisory `flock`, which is unreliable on NFS and some FUSE mounts — so pointing
`IKE_DATA_FILE` at a Dropbox, iCloud, or network-mounted folder and writing
from two machines at once is not covered.

## Contributing

Bug reports and focused pull requests are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the build commands, how the three
frontends sit over one store, and the store invariants worth knowing before
changing it. Security issues go through [SECURITY.md](SECURITY.md) rather than
the issue tracker.

## License

[MIT](LICENSE). Third-party dependencies and their licenses — all permissive,
none copyleft — are listed in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

---

*Logo based on Eisenhower's [official White House portrait of May 29,
1959](https://commons.wikimedia.org/wiki/File:Dwight_D._Eisenhower,_official_photo_portrait,_May_29,_1959.jpg)
(White House / Eisenhower Presidential Library). It is a work of an employee of
the Executive Office of the President made as part of their official duties,
and so is in the public domain as a work of the U.S. federal government. No
attribution is required; it is recorded here so anyone redistributing ike can
verify the claim for themselves.*

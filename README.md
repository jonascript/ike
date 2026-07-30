<p align="center">
  <img src="assets/logo.svg" width="190" alt="Portrait of Dwight D. Eisenhower in a circular badge with five stars">
</p>

<h1 align="center">ike</h1>

<p align="center"><em>The Eisenhower matrix, in your terminal.</em></p>

<p align="center">
  <a href="https://github.com/jonascript/ike/actions/workflows/ci.yml"><img src="https://github.com/jonascript/ike/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="https://github.com/jonascript/ike/releases/latest"><img src="https://img.shields.io/github/v/release/jonascript/ike" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/jonascript/ike" alt="MIT licensed"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/jonascript/ike" alt="Minimum Go version">
</p>

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

## Spaces

One data file holds several independent matrices, called **spaces** — work and
personal, say. Each has its own tasks, archive, quadrant headings, ID numbering,
and undo history, so `ike undo` in one can never reach into another. A fresh
install has a single space named `default`, and nothing changes until you make
a second.

```sh
ike space                       # list spaces, marking the current one
ike space new work              # create it (does not switch)
ike space use work              # switch; every later command follows
ike space rename work job
ike space rm work               # refuses a non-empty space without --force
```

Every command takes `-s/--space NAME` to act on one space just once, without
switching:

```sh
ike add "Fix prod bug" -s work -q 1
ike list -s work
```

Two concepts worth keeping apart: a **file** is a document and contains spaces;
a **space** is one matrix inside it. `--file` picks the document, `--space` picks
the matrix. In the TUI, `s` opens a space picker and `]`/`[` move between spaces.

**Deleting a space cannot be undone.** History lives inside the space, so there
is no stack left to revert from — which is why `ike space rm` names the counts it
is about to destroy and needs `--force` if the space still holds anything. The
previous file contents remain in `tasks.json.bak` until the next change.

## Moving a matrix between machines

The data file is self-contained and fully portable: nothing in it refers to a
path or a machine, and timestamps are stored in UTC. Copy it to another computer
and every space comes with it. The sidecar `.lock` and `.bak` files do not need
copying.

To move one space rather than the whole file:

```sh
ike space export work ~/work-matrix.json    # a standalone ike data file
# copy that one file to the other machine, then:
ike space import ~/work-matrix.json
ike space import ~/old.json --as archive-2025
ike --file ~/work-matrix.json list          # or just open it in place
```

An export is an ordinary data file, so `--file` opens it directly. **MCP access
is always off in an exported file**, whatever it was in the original: agent
access is a decision about a file on a machine, and an export is made to travel.
Importing a name that is already in use is an error rather than a merge — use
`--as` to bring it in under a different name.

## Install

```sh
brew install jonascript/tap/ike
```

The formula builds from source, so Homebrew fetches Go for the build. Linux and
macOS, Intel and Apple silicon.

Or with Go 1.25+:

```sh
go install github.com/jonascript/ike@latest
```

Or from a checkout, with a stamped version:

```sh
go build -ldflags "-X github.com/jonascript/ike/internal/cli.version=$(git describe --tags --always)" -o ike .
```

### Prebuilt archives

Every [release](https://github.com/jonascript/ike/releases) attaches archives
for macOS and Linux on both architectures. They are **not** code-signed, so
verify what you download — against `checksums.txt`:

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

and, if you have the GitHub CLI, against the build provenance ike publishes,
which proves the archive was built by this repository's release workflow from
the tag it claims:

```sh
gh attestation verify ike_0.1.0_darwin_arm64.tar.gz --repo jonascript/ike
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
| `s` | space picker (`enter` switch, `n` new, `r` rename, `d` twice delete) |
| `]` / `[` | next / previous space |
| `f` | data file picker (`o` there types a path) |
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

ike space                       # spaces: see "Spaces" below
ike list -s work                # act on one space just this once
ike --file /path/to.json list   # act on a different data file
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
History belongs to the space it was made in, so `ike undo` never reverts a change
made in a different one.

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
upgrades, and it is scoped to that **file** — which means every space in it.
Enabling access for one space enables it for all of them; to keep a matrix out
of reach, put it in a separate file (see [Spaces](#spaces)) or pin the server to
one space with `ike -s work mcp`. Access never travels: a space you export
always lands with it switched off. It is not
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
`set_quadrant_label`, and `list_spaces`.

Every tool takes an optional `space` argument and defaults to whichever space
you are on, so an agent can work in one matrix while you work in another. It is
read-only about spaces: no tool can create, rename, delete, or switch one, since
switching would change what your own TUI and a bare `ike list` show. Launching
the server as `ike -s work mcp` pins it to that space — a request naming another
is refused, and `list_spaces` then reports only the one it was launched for.

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

Override the location with `--file` or `IKE_DATA_FILE` — highest precedence
first: `--file`, then `IKE_DATA_FILE`, then the default above. Either must be an
**absolute** path whose parent directory already exists (a relative path would resolve against
whatever directory the process happened to start in — not something you can
predict when an MCP client launches ike for you).

One caveat on the concurrency guarantee: it relies on advisory `flock`, which is
unreliable on NFS and some FUSE mounts — so pointing the data file at a Dropbox,
iCloud, or network-mounted folder and writing from two machines at once is not
covered. A single machine writing to a synced folder is fine.

## Platform support

**Linux and macOS.** Both run the full test suite on every change, and release
archives are built for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, and
`linux/arm64`.

**Windows is not supported**, and is not on the roadmap. It is not built, not
released, and not tested. `go install` will probably give you a working binary —
nothing in ike is deliberately Unix-only — but the data file would land at
`%USERPROFILE%\.local\share\ike\tasks.json`, following XDG conventions rather
than anywhere a Windows user would think to look, and no Windows path is
exercised by CI. So: unsupported rather than known-broken. A pull request adding
proper path resolution *together with* Windows CI coverage would be welcome; one
without the CI would just move the untested surface around.

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

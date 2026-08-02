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

<p align="center">
  <img src="assets/demo.gif" width="900" alt="Capturing a task from the shell with ike add, then the four-quadrant TUI: moving between tasks, opening an attached plan, adding a task, reclassifying it into another quadrant, and undoing that with an explanation of what was undone">
</p>

---

A task manager that sorts work by urgency against importance, into four
quadrants — the [method popularized
by](https://en.wikipedia.org/wiki/Time_management#The_Eisenhower_Method) the
president whose campaign-era nickname it borrows. What makes it unusual is that
an agent can work with it in both directions:

- **An agent can manage your matrix.** `ike mcp` serves the Model Context
  Protocol, so Claude Code or any MCP client can capture, complete, and
  reprioritise tasks while you work. Off until you allow it.
- **You can hand a task to an agent.** `ike plan 3` drafts a plan you read and
  argue with; `ike delegate 3` carries it out in the task's own directory and
  streams what it does. Also off until you allow it — separately.

One Go binary, three ways in (TUI, CLI, MCP), over a single JSON file that is
yours: mode `0600`, on your disk, no account and no server. All three can run at
once without losing writes.

## Install

```sh
brew install jonascript/tap/ike
```

Or with Go 1.25+:

```sh
go install github.com/jonascript/ike@latest
```

Linux and macOS, Intel and Apple silicon. Prebuilt archives are attached to
every [release](https://github.com/jonascript/ike/releases) — they are not
code-signed, so verify what you download:

```sh
shasum -a 256 -c checksums.txt --ignore-missing
gh attestation verify ike_0.2.0_darwin_arm64.tar.gz --repo jonascript/ike
```

That second command checks ike's published build provenance, which proves the
archive was built by this repository's release workflow from the tag it claims.

## Quickstart

```sh
ike add "Fix prod bug" -q 1        # quadrant 1: urgent and important
ike add "Write the release notes"  # defaults to quadrant 2
ike                                # open the matrix
```

| | Urgent | Not urgent |
|---|---|---|
| **Important** | 1 · Do It First | 2 · Schedule It |
| **Not important** | 3 · Delegate It | 4 · Consider Eliminating It |

Those headings are defaults, not fixed — rename any of them with `t` in the TUI
or `ike label`. The quadrant *numbers* never change, so scripts and the MCP
tools keep working whatever you call them.

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
| `p` | show the plan attached to the selected task |
| `P` | ask an agent to draft a plan |
| `c` | open a Claude Code session and talk the plan through (resumes the task's) |
| `C` | same, but to work on it together |
| `D` | delegate the task to an agent, or reattach to a run already going |
| `?` | toggle help |
| `q` | quit |

Changes made by the CLI or MCP server while the TUI is open appear within ~2 seconds.

A dim `◆ mcp` marker sits in the footer while [agent access](#agents) is
enabled. No marker means nothing but you can reach the matrix. After a title,
`✎` means the task has a plan attached, `⌁` that it has a conversation you can
pick back up, and `⣾` that an agent is working on it right now.

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
ike label 1 "Firefighting"      # rename a quadrant (--reset restores the default)

ike plan 3                      # draft a plan for task 3
ike delegate 3                  # hand task 3 to an agent
ike agent status                # whether ike may run an agent

ike space                       # list spaces
ike list -s work                # act on one space just this once
ike --file /path/to.json list   # act on a different data file
```

## Agents

Both directions are off by default, and they are separate decisions: letting an
agent edit your task list and letting ike start a process that edits your
*files* have very different blast radii.

```sh
ike mcp enable      # let an MCP client read and manage this matrix
ike agent enable    # let ike run an agent on a task's working directory
```

**Serving your matrix over MCP.** `ike mcp` runs a stdio server with tools for
listing, adding, completing, moving, reordering, archiving, and undoing —
scoped to one space if you launch it that way. `ike mcp disable` takes effect
immediately, even on a client already connected.

```sh
ike mcp enable
claude mcp add ike -- ike mcp
```

**Handing a task over.** `ike plan 3` asks an agent to explore the task's
directory and draft a plan, which is attached to the task for you to read and
edit — that run is read-only and needs no permission. `ike delegate 3` then
carries the plan out and streams what it does. `-i` on either hands you the
terminal for a real Claude Code session instead, resuming the same conversation
each time you come back to the task.

A delegated run never completes the task. You read what it did and decide.

→ **[docs/agents.md](docs/agents.md)** covers the consent gates, what a
delegated run is actually allowed to do (with measurements — the permission
modes are not a safety ladder), how ike picks an effort level per run, and where
plans are stored.

## Spaces and data

One file holds several independent matrices, called **spaces** — work and
personal, say. Each has its own tasks, archive, headings, ID numbering, and undo
history, so `ike undo` in one can never reach into another.

```sh
ike space new work
ike space use work              # every later command follows
ike add "Fix prod bug" -s work  # or act on one space just once
```

Everything lives in `~/.local/share/ike/tasks.json` (mode `0600`, in a `0700`
directory). Writes go through a lock file and an atomic rename with the previous
contents kept as `.bak`, so three frontends can run at once and an interrupted
write costs one change rather than the matrix. The file is self-contained and
portable — copy it to another machine and every space comes with it.

→ **[docs/data.md](docs/data.md)** covers spaces in full, exporting and
importing a single matrix, the file's durability guarantees and their one
caveat (`flock` on network mounts), undo/redo semantics, and renaming quadrants.

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

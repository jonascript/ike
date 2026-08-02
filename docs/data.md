# Spaces, files, and your data

Everything ike knows lives in one JSON file you own. This page covers how that
file is organised, where it lives, how it survives being written to by three
frontends at once, and how to move it around.

- [Spaces](#spaces)
- [Moving a matrix between machines](#moving-a-matrix-between-machines)
- [Where your data lives](#where-your-data-lives)
- [Undo and redo](#undo-and-redo)
- [Renaming the quadrants](#renaming-the-quadrants)

---

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

Note that attached plans do **not** yet travel with `ike space export`.

## Where your data lives

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
**absolute** path whose parent directory already exists (a relative path would
resolve against whatever directory the process happened to start in — not
something you can predict when an MCP client launches ike for you).

One caveat on the concurrency guarantee: it relies on advisory `flock`, which is
unreliable on NFS and some FUSE mounts — so pointing the data file at a Dropbox,
iCloud, or network-mounted folder and writing from two machines at once is not
covered. A single machine writing to a synced folder is fine.

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

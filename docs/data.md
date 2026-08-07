# Spaces, files, and your data

Everything ike knows lives in a handful of JSON files you own: a small
manifest, and one file per space beside it. This page covers how that data is
organised, where it lives, how it survives being written to by three frontends
at once, and how to move it around.

- [Spaces](#spaces)
- [Moving a matrix between machines](#moving-a-matrix-between-machines)
- [Where your data lives](#where-your-data-lives)
- [When a space file is damaged](#when-a-space-file-is-damaged)
- [Undo and redo](#undo-and-redo)
- [Renaming the quadrants](#renaming-the-quadrants)

---

## Spaces

Your data holds several independent matrices, called **spaces** — work and
personal, say. Each has its own tasks, archive, quadrant headings, ID numbering,
and undo history, so `ike undo` in one can never reach into another. A fresh
install has a single space named `default`, and nothing changes until you make
a second.

**Each space is one file on disk.** `tasks.json` is a small manifest recording
which space is current and the two agent-consent settings; the spaces
themselves live beside it in `tasks.json.spaces/`, one JSON file each, named
after the space. Corruption in one space's file cannot touch the others, and
copying a space to another machine is copying one file.

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
is about to destroy and needs `--force` if the space still holds anything.
Removing a space renames its file to `<name>.json.bak` inside the spaces
directory rather than deleting it, so the contents survive until a new space
claims the name.

## Moving a matrix between machines

A space's file is self-contained and fully portable: nothing in it refers to a
path or a machine, and timestamps are stored in UTC. `ike space export` writes
exactly the same bytes as the space's own file in `tasks.json.spaces/`, so
exporting and copying the file by hand are the same operation.

```sh
ike space export work ~/work-matrix.json    # a standalone space file
# copy that one file to the other machine, then:
ike space import ~/work-matrix.json
ike space import ~/old.json --as archive-2025
ike --file ~/work-matrix.json list          # or just open it in place
```

A single space file opens with `--file` for reading and editing — task changes,
undo, everything except growing more spaces, which a one-space file has no room
for. **Neither consent setting travels in an exported file**: the space-file
format simply has no field for them, because agent access is a decision about a
file on a machine, and an export is made to travel. Importing a name that is
already in use is an error rather than a merge — use `--as` to bring it in
under a different name.

To move *everything*, copy `tasks.json` together with the whole
`tasks.json.spaces/` directory (and `tasks.json.plans/` if you use plans), then
`ike space import <copied tasks.json> --all` — or just point `IKE_DATA_FILE` at
the copy. The sidecar `.lock` and `.bak` files never need copying.

`ike space import` still reads data files from every earlier version of ike,
including the old single-file format.

Note that attached plans do **not** yet travel with `ike space export`. To move
them by hand, copy `tasks.json.plans/<space>/` alongside the exported file.

## Where your data lives

Tasks live under `$XDG_DATA_HOME/ike/` (default `~/.local/share/ike/`):

```
tasks.json              # manifest: current space + consent settings
tasks.json.spaces/      # one file per space
  work.json
  work.json.bak         # that space's previous contents
tasks.json.plans/       # plan bodies, one file per task
tasks.json.bak          # the manifest's previous contents
tasks.json.lock         # write lock; never needs touching
```

Everything is created mode `0600` in `0700` directories — your matrix is not
readable by other users on the machine.

Writes are serialized through the sidecar lock file and land via atomic
renames, so the TUI, CLI, and MCP server can run at the same time without
losing updates. A mutation rewrites only the files it changed. Each write is
flushed to disk before the rename, and every file's previous contents are kept
as its `.bak`, so an interrupted write costs at most one change rather than the
whole matrix. If a file is ever unreadable, ike refuses to overwrite it rather
than starting fresh over the top.

Data from older ike versions (a single `tasks.json` holding everything) is read
as-is and split into the new layout on the first change you make. The original
file is kept as `tasks.json.pre-v5.bak`, permanently — it is your escape hatch
back to the pre-split state, and nothing ever overwrites it.

Override the location with `--file` or `IKE_DATA_FILE` — highest precedence
first: `--file`, then `IKE_DATA_FILE`, then the default above. Either must be an
**absolute** path whose parent directory already exists (a relative path would
resolve against whatever directory the process happened to start in — not
something you can predict when an MCP client launches ike for you).

One caveat on the concurrency guarantee: it relies on advisory `flock`, which is
unreliable on NFS and some FUSE mounts — so pointing the data file at a Dropbox,
iCloud, or network-mounted folder and writing from two machines at once is not
covered. A single machine writing to a synced folder is fine.

## When a space file is damaged

One damaged space costs that one space, never the others. If a space's file
cannot be parsed — a bad sync, a stray hand edit — every other space keeps
working, and the damaged one shows up in `ike space list` and the TUI picker
marked **unreadable** rather than silently disappearing. Commands aimed at it
say what is wrong and which file to look at.

Nothing ike does will touch an unreadable file, so you can try to repair it in
place (it is JSON; the `.bak` beside it may also be intact). Once you give up
on it, `ike space rm <name> --force` retires it — even then the file is renamed
to `.bak`, not deleted, in case it can still be recovered later.

## Undo and redo

Every change records a snapshot, so `ike undo` (or `u` in the TUI) reverts the
last one — including a delete, and including changes made from a different
frontend. Run it repeatedly to walk further back; the last 20 changes are kept
in the space's file, so history survives restarts. Undo does not recycle task
IDs. History belongs to the space it was made in, so `ike undo` never reverts a
change made in a different one.

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

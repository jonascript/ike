# Contributing to ike

Thanks for taking a look. ike is small and deliberately stays that way, so the
most useful contributions are usually bug fixes, a sharper error message, or a
test that pins something currently untested.

## Getting set up

You need Go 1.25 or newer. Everything else is in the module.

```sh
go build ./...
go test ./...
go test -race ./...     # what CI enforces
go vet ./...
gofmt -l .              # must print nothing
```

Linting matches CI:

```sh
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...
```

**Always point `IKE_DATA_FILE` at a scratch file when trying things out**, or
you will be editing your own real matrix:

```sh
IKE_DATA_FILE=$(mktemp -d)/t.json go run . list
```

It must be an absolute path whose parent directory exists.

## How the pieces fit together

Dependency direction is `cli` → `tui`/`mcpserver` → `store` → `task`.

- `internal/task` — the domain. Zero dependencies beyond stdlib: the `Task`
  struct, quadrants 1–4, and `Less`/`SortOrder`, which is the single definition
  of display order.
- `internal/store` — **all business logic lives in `ops.go`.** Add, Complete,
  Restore, Move, Reorder, Rename, Delete, SetQuadrantLabel, Undo, Redo, List,
  ListArchive.
- `internal/tui`, `internal/cli`, `internal/mcpserver` — three frontends that
  must stay thin wrappers over the store.

If you find yourself putting logic in a frontend, it probably belongs in
`ops.go` so all three get it. `CLAUDE.md` documents the architecture in more
detail, including the reasoning behind the parts that look like they could be
simplified.

## Invariants worth knowing before you change the store

These are load-bearing, and each exists because the alternative broke
something. Tests pin all of them, so you will find out — but it saves time to
know up front.

- **Every mutation goes through `Store.Mutate`.** It locks a *sidecar*
  `tasks.json.lock` (locking the data file itself cannot work — the atomic
  rename replaces the inode), re-reads the file *inside* the lock, applies the
  change, then writes via a temp file and rename. Never write the data file any
  other way.
- **The re-read inside the lock is what prevents lost updates**, and returning
  early on a read error is what stops a corrupt file being overwritten with
  stale in-memory state. Keep that ordering.
- **Writes are `0600`, the directory `0700`**, the temp file comes from
  `os.CreateTemp` (never a predictable name — a symlink at a guessable path can
  redirect the write), and the data is fsynced before the rename.
- **`Labels.Of(q)` is the only correct way to render a quadrant name.** Never
  print `q.Label()` in a frontend; that is the *default* name and ignores a
  user's rename.
- **Render user text through `Task.DisplayTitle()` and `Labels.Of()`.** They
  strip control characters. A title containing an escape sequence can otherwise
  repaint the terminal, so `ike list` would show something other than what is
  stored. `--json` deliberately keeps raw values, since `encoding/json` escapes
  them.
- **`pushUndo` is called after validation**, so failed and no-op mutations do
  not record history. `Redo` must use `recordUndo`, not `pushUndo`, or it clears
  the stack it is walking.
- **`MCPEnabled` is never in `Snapshot`.** Undo must not be able to re-open
  access someone revoked.
- **`mutateFile` is the only write path.** `Mutate` is a thin wrapper that
  resolves the space inside it; the space operations use `mutateFile` directly.
  Giving either its own write path would duplicate all five properties above
  where no test covers them.
- **A space is everything a mutation can touch.** Tasks, archive, labels,
  `NextID`, and both history stacks live inside `Data`; only `Version` and
  `MCPEnabled` sit on the enclosing `File`. Undo is therefore per space, and
  space lifecycle changes are deliberately not undoable.
- **Space resolution never creates.** A name that is not in the file fails
  before `fn` and before any write, which is what stops a typo conjuring an
  empty matrix — and what keeps "no MCP tool can create a space" true.
- **`Data`'s `Space`, `AllSpaces`, and `MCPAllowed` are derived, never
  persisted.** They describe the document, and exist so a frontend can render an
  operation's outcome without a second read.

## Platforms

Linux and macOS only, and Windows is not planned — see the README's platform
support section. If you do want to add Windows, the path resolution and the
Windows CI coverage need to arrive together, or it is just untested surface in a
different place.

## Pull requests

- Keep the change focused; one concern per PR.
- Add a test. For a bug fix, a test that fails before your change is ideal.
- Run the commands above before pushing. CI runs them on Linux and macOS.
- Explain *why* in the commit message, not just what. If a change looks
  redundant but is not, say so in a comment — several already exist for exactly
  that reason.

## Reporting bugs

Open an issue with what you ran, what happened, and what you expected. Include
`ike --version`. If it involves the data file, please redact your task titles —
they are personal, and nobody needs them to fix the bug.

For anything security-related, see [SECURITY.md](SECURITY.md) instead.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`ike` is an Eisenhower matrix task manager in Go with three frontends over one JSON store: a Bubble Tea TUI (bare `ike`), cobra CLI subcommands, and an MCP server (`ike mcp`, stdio).

## Commands

```sh
go build ./...                         # build
go test ./...                          # all tests
go test ./internal/store -run TestConcurrentWriters -v   # single test
go vet ./...                           # vet (keep clean)
IKE_DATA_FILE=/tmp/t.json go run . list   # run against a scratch data file
```

`IKE_DATA_FILE` overrides the data path (default `$XDG_DATA_HOME/ike/tasks.json`); always set it when smoke-testing so you don't touch the real matrix.

## Architecture

Dependency direction: `cli` → `tui`/`mcpserver` → `store` → `task`. **All business logic lives in `internal/store/ops.go`** (Add/Complete/Restore/Move/Reorder/Rename/Delete/SetQuadrantLabel/Undo/Redo/List/ListArchive); the three frontends must stay thin wrappers over it. `internal/task` is the zero-dependency domain (Task struct, Quadrant 1–4 with default labels, and `Less`/`SortOrder`, the single definition of display order).

Quadrant headings are user-customizable. `task.Quadrant.Label()` gives the **default** name; `store.Data.Labels` (a sparse `map[Quadrant]string`, only renamed quadrants present) holds overrides, and **`Labels.Of(q)` is the only correct way to render a quadrant name** — it is nil-safe, so `d.Labels.Of(q)` works on a store that has never been renamed. Never print `q.Label()` in a frontend. Clearing a label deletes the map entry rather than storing `""`, so later changes to the defaults still reach users who reset. The quadrant *number* is the stable identifier for classification and scripting; only the display name varies.

Concurrency contract (`internal/store/store.go`): every mutation goes through `Store.Mutate`, which flocks a **sidecar** `tasks.json.lock` (locking the data file itself would break — atomic rename replaces the inode), re-reads fresh inside the lock, applies the change, and writes via temp-file + `os.Rename`. Never write the data file any other way. Completing a task moves it from `tasks` to `archive` with `DoneAt` stamped (`Restore` reverses that); IDs are monotonic via `next_id` and never reused. Deleting skips the archive.

Ordering: each task carries a `Rank` float; display order is quadrant, then rank, then ID. `Reorder(id, delta)` rewrites a whole quadrant's ranks as multiples of `rankGap`, so repeated moves never lose precision — do not switch it to midpoint insertion without adding a renormalization path. Rank 0 means "unranked" and sorts by ID; `normalizeRanks` (called from every read) backfills it, which is what makes pre-rank files work.

Undo/redo is snapshot-based, with two stacks in the data file (each capped at `undoDepth`), so history works across frontends and restarts. A `Snapshot` covers tasks, archive, **and quadrant labels** — anything a mutation can touch has to be in there, or undoing a rename would silently do nothing. Ops call `pushUndo(d, label)` inside their `Mutate` callback, *after* validation and immediately before mutating, so failed and no-op mutations don't record. `pushUndo` also clears the redo stack — a new change diverges from the redone branch, and replaying it would clobber the change. **`Redo` must use `recordUndo`, not `pushUndo`**, or it clears the stack it is walking and only ever redoes one step (`TestRedoMultipleSteps` covers exactly this). `Undo` deliberately does not roll back `next_id`: IDs stay monotonic and are never reused.

Data file version is **2** (ranks + history stacks). `readFile` accepts version 1 and upgrades in memory; the next write persists version 2. A newer version than `currentVersion` is still a hard error. The `redo` field was added within version 2 as `omitempty` rather than bumping again — losing redo history to an older binary is harmless, whereas a bump makes older binaries refuse the file outright.

The TUI (`internal/tui`) is a single `Model` with a mode enum (normal/input/move/archive); `model.go` holds state + key handling, `view.go` all rendering. It re-renders from `Mutate` results and polls file mtime every 2s to pick up CLI/MCP writes. `archCursor` indexes `archiveList()` (newest first), *not* `data.Archive` — use the helper when acting on the selected archive row.

## Gotchas

- Charm libraries are **v2** with vanity import paths `charm.land/{bubbletea,lipgloss,bubbles}/v2` — all three must stay on v2 together. v2 API: keys arrive as `tea.KeyPressMsg`, `View()` returns `tea.View` (AltScreen is set there), colors adapt via `lipgloss.LightDark(isDark)` fed by `tea.BackgroundColorMsg` (`AdaptiveColor` no longer exists).
- **MCP access is gated.** `Data.MCPEnabled` (default false, so fresh installs and pre-existing files start closed) must be true or `ike mcp` exits before `mcpserver.Run` — the check lives in `internal/cli/mcp.go`, not in `mcpserver`, so the server package stays a pure transport. Toggle it only via `Store.SetMCPEnabled` (`ike mcp enable|disable|status`). It is deliberately **not** in `Snapshot`: undo must never be able to re-open revoked access, which `TestUndoCannotReopenRevokedAccess` pins. Never expose a tool that flips it — a disabled server cannot enable itself, and an enabled one must not be able to make that permanent.
- MCP SDK is the official `github.com/modelcontextprotocol/go-sdk` pinned to v1.x stable — don't bump to `-pre` releases. Tools are registered with typed In/Out structs via `mcp.AddTool`; domain errors become tool errors (`IsError`), not protocol errors.
- In `ike mcp`, stdout carries JSON-RPC — anything human-facing must go to stderr. Client disconnect surfaces as an "is closing"/EOF error that `internal/cli/mcp.go` deliberately treats as a clean exit.
- MCP server tests connect an in-process client via `mcp.NewInMemoryTransports()` (server must connect before client).
- TUI tests drive `Update` with synthetic `tea.KeyPressMsg` — no PTY. If a test adds to the store after `New()`, call `refreshFromStore` before pressing keys, since the model won't see external writes until a tick.

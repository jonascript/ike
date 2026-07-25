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

Dependency direction: `cli` → `tui`/`mcpserver` → `store` → `task`. **All business logic lives in `internal/store/ops.go`** (Add/Complete/Move/Rename/Delete/List/ListArchive); the three frontends must stay thin wrappers over it. `internal/task` is the zero-dependency domain (Task struct, Quadrant 1–4 with labels).

Concurrency contract (`internal/store/store.go`): every mutation goes through `Store.Mutate`, which flocks a **sidecar** `tasks.json.lock` (locking the data file itself would break — atomic rename replaces the inode), re-reads fresh inside the lock, applies the change, and writes via temp-file + `os.Rename`. Never write the data file any other way. Completing a task moves it from `tasks` to `archive` with `DoneAt` stamped; IDs are monotonic via `next_id` and never reused. Deleting skips the archive.

The TUI (`internal/tui`) is a single `Model` with a mode enum (normal/input/move/archive); `model.go` holds state + key handling, `view.go` all rendering. It re-renders from `Mutate` results and polls file mtime every 2s to pick up CLI/MCP writes.

## Gotchas

- Charm libraries are **v2** with vanity import paths `charm.land/{bubbletea,lipgloss,bubbles}/v2` — all three must stay on v2 together. v2 API: keys arrive as `tea.KeyPressMsg`, `View()` returns `tea.View` (AltScreen is set there), colors adapt via `lipgloss.LightDark(isDark)` fed by `tea.BackgroundColorMsg` (`AdaptiveColor` no longer exists).
- MCP SDK is the official `github.com/modelcontextprotocol/go-sdk` pinned to v1.x stable — don't bump to `-pre` releases. Tools are registered with typed In/Out structs via `mcp.AddTool`; domain errors become tool errors (`IsError`), not protocol errors.
- In `ike mcp`, stdout carries JSON-RPC — anything human-facing must go to stderr. Client disconnect surfaces as an "is closing"/EOF error that `internal/cli/mcp.go` deliberately treats as a clean exit.
- MCP server tests connect an in-process client via `mcp.NewInMemoryTransports()` (server must connect before client).
- TUI tests drive `Update` with synthetic `tea.KeyPressMsg` — no PTY. If a test adds to the store after `New()`, call `refreshFromStore` before pressing keys, since the model won't see external writes until a tick.

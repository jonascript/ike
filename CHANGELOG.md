# Changelog

All notable changes to ike are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing released yet. The entries below describe what will be in the first
tagged version.

### Added

- Bubble Tea TUI (`ike`) with a four-quadrant matrix, axis labels, an archive
  view, and inline help.
- CLI subcommands: `add`, `list`, `done`, `mv`, `reorder`, `rm`, `archive`,
  `restore`, `undo`, `redo`, `label`.
- MCP server (`ike mcp`) over stdio, with typed tools for every store
  operation. **Access is off by default** and must be enabled per data file
  with `ike mcp enable`.
- Renameable quadrant headings, stored per data file and shared by all three
  frontends. Quadrant *numbers* stay fixed, so scripts keep working.
- Manual ordering within a quadrant, with rank-based reordering that does not
  lose precision over repeated moves.
- Snapshot-based undo and redo that work across frontends and survive restarts.
- `THIRD_PARTY_LICENSES.md` covering every dependency linked into the binary.

### Security

- The data file is created mode `0600` in a `0700` directory. It was previously
  `0644` in a `0755` directory, i.e. readable by any other local user.
- Task titles and quadrant labels reject control characters, including `ESC`,
  and are length-bounded. Rendering also replaces control characters on the way
  out, so a title from an older file or a hand edit cannot repaint the terminal
  and make `ike list` display something other than what is stored.
- `ike mcp disable` now revokes a session that is already connected. The gate is
  re-checked on every read and write; previously it was checked only at startup,
  so a connected client kept full access indefinitely while `ike mcp status`
  reported "off".
- Errors returned to an MCP client no longer include the data file's absolute
  path, which disclosed the OS username and home directory layout.

### Fixed

- Undo snapshots no longer copy the whole archive. With 1500 archived tasks the
  data file was ~21x larger than the data in it — 276K of tasks became an 8MB
  file, re-written on every change (79ms per `ike add`) and re-parsed by the TUI
  on every poll. Now 1.4x and 17ms. Data file version 3; a file from an older
  version is upgraded on read, and its undo history is dropped rather than
  reinterpreted.
- A task whose `quadrant` was outside 1–4 (only reachable by hand-editing the
  file, or another writer) persisted through every save while appearing in no
  human-facing view, though it still showed in `--json`. Such tasks are now
  moved into quadrant 4 on read, so they are visible and can be dealt with.
- Writes are fsynced before the atomic rename, so a crash cannot leave a
  truncated data file.
- The previous file contents are kept as `tasks.json.bak`.
- The temp file used during a write is created with a random name rather than a
  predictable one, which a symlink could otherwise redirect.
- Waiting for the lock now times out with a clear message instead of hanging
  forever behind a stuck process.
- `IKE_DATA_FILE` is validated: absolute, no unexpanded `~`, parent must exist.
- The TUI now reports a failed background reload instead of silently continuing
  to render stale data.
- The TUI showed default quadrant names instead of renamed ones in two status
  messages.

[Unreleased]: https://github.com/jonascript/ike/commits/main

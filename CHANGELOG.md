# Changelog

All notable changes to ike are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing released yet. The entries below describe what will be in the first
tagged version.

### Added

- **Spaces: several independent matrices in one data file.** Each has its own
  tasks, archive, quadrant headings, ID numbering, and undo history, so `ike undo`
  in one can never reach into another. `ike space list|new|use|rename|rm` manages
  them, a root-level `--space/-s` acts on one just once without switching, and
  `s` in the TUI opens a picker (`]`/`[` cycle). Existing files upgrade into a
  single space named `default`.
- **`--file/-f` to select a data file per command**, validated the same way
  `IKE_DATA_FILE` is. Precedence: `--file`, then `IKE_DATA_FILE`, then the
  default location. `f` in the TUI opens a picker of files opened before.
- **`ike space export` and `ike space import`** to move a matrix between files or
  machines. An export is an ordinary data file, so `--file` opens it directly.
  MCP access is always off in an exported file: agent access is a decision about
  a file on a machine, and an export is made to travel.
- **`list_spaces` MCP tool, and an optional `space` argument on every tool.**
  Read-only by design: no tool can create, rename, delete, or switch a space,
  since switching would change what your own TUI shows. `ike -s work mcp` pins a
  server to one space, and a request naming another is refused.
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

### Changed

- The data file format is **version 4**, which wraps the matrix in a document
  that can hold several. Older files upgrade in memory on read and are written
  back at version 4 by the next change; an older ike binary refuses a version 4
  file outright rather than misreading it.
- `mcp_enabled` is scoped to the **file**, so it covers every space in it. The
  README previously described the scope as the matrix, which is no longer the
  same thing.
- The TUI needs one more row than before (12 rather than 10), for the line that
  names the current space.

[Unreleased]: https://github.com/jonascript/ike/commits/main

<!--
Thanks for the PR. Keeping it focused on one concern makes it much easier to
review and much more likely to land.
-->

## What this changes

## Why

<!-- The reasoning, not just the diff. If something looks redundant but isn't,
say so here and in a code comment. -->

## Checklist

- [ ] `go build ./...`, `go vet ./...`, and `gofmt -l .` are clean
- [ ] `go test -race ./...` passes
- [ ] Added or updated a test (for a fix, one that failed before the change)
- [ ] Business logic went in `internal/store/ops.go`, not into a frontend
- [ ] Any user-facing text is rendered via `Task.DisplayTitle()` / `Labels.Of()`
- [ ] `CHANGELOG.md` updated under `Unreleased`, if user-visible

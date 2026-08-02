# Seeds a throwaway matrix for the demo recording. Meant to be sourced, not run:
# it exports PATH and IKE_DATA_FILE into the calling shell, which is what lets
# demo.tape drive a real `ike` rather than a mock.
#
# The data file is a fresh mktemp directory every time, so recording a demo can
# never touch the matrix you actually use. Everything else in here is chosen to
# look like a real week of work on this repo.

set -e

demo_dir="$(mktemp -d)"
demo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Built from the working tree, so the GIF always shows the code it ships with.
go build -o "$demo_dir/ike" "$demo_root"

export PATH="$demo_dir:$PATH"
export IKE_DATA_FILE="$demo_dir/demo.json"

ike add "Fix the flaky reorder test" -q 1 >/dev/null
ike add "Reply to the security report" -q 1 >/dev/null
ike add "Write the v0.3 release notes" -q 2 >/dev/null
ike add "Add plan export to space export" -q 2 >/dev/null
ike add "Bump the Homebrew formula" -q 3 >/dev/null
ike add "Triage this week's new issues" -q 3 >/dev/null
ike add "Hand-tune the logo SVG again" -q 4 >/dev/null

# One task carries a plan, so the ✎ mark and the plan view have something real
# to show without the demo having to run (and pay for) an agent.
cat > "$demo_dir/plan.md" <<'PLAN'
## Goal

Make reorder deterministic under -race.

## Steps

1. Reproduce with `go test -run TestReorder -race -count=50`
2. Renormalise ranks inside the flock, not after it
3. Add a regression test that interleaves two writers

## Checking it worked

The loop above passes 50/50, and `ike list` order is unchanged.
PLAN
ike plan 1 --from-file "$demo_dir/plan.md" >/dev/null

set +e

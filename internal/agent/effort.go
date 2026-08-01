package agent

import (
	"fmt"
	"slices"
	"strings"
)

// Effort is how hard the agent works: how deeply it thinks, how many tools it
// reaches for, how much it says on the way. It is a finer dial than the model,
// and on a delegated run it is usually the one that matters — a run's wall
// clock is dominated by how many turns it takes, and effort is what moves that.
//
// ike picks a level rather than leaving the CLI's own default (high) in place
// for every run, because the two things ike delegates are not the same shape of
// work. Drafting a plan *is* the reasoning; carrying out a plan that has
// already been reviewed is mostly follow-through, and paying to re-derive
// decisions the plan already records buys nothing.
//
// The choice is a judgement about how much thinking a run has left to do, not a
// measurement — so it is always visible in the run header, and --effort always
// wins. Anything ike guesses about a task should be arguable by the person
// whose task it is.

// EffortLevels are the values the CLI accepts, in its own order — least effort
// first, so the slice doubles as the scale.
//
// Validated here rather than passed straight through, for the reason
// PermissionModes gives: left to the CLI, `--effort mid` becomes an exec that
// dies on a bad option, and a mistyped flag surfaces as a failed run.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// ValidateEffort checks a user-supplied level. Empty means "no preference",
// which is what lets ResolveEffort choose.
func ValidateEffort(level string) error {
	if level == "" || slices.Contains(EffortLevels, level) {
		return nil
	}
	return fmt.Errorf("unknown effort %q; expected one of %s",
		level, strings.Join(EffortLevels, ", "))
}

// The reasons, as constants so the frontends and the tests name the same
// strings rather than three copies of the same sentence drifting apart.
const (
	effortDrafting  = "drafting a plan"
	effortNoPlan    = "no plan to follow"
	effortFollowing = "following an attached plan"
)

// ResolveEffort reports the effort a run will use and why, given what the user
// asked for and what the run has to work with.
//
// An explicit level comes back with no reason: there is nothing to explain
// about a flag someone typed, and an empty reason is what tells a caller to
// print the level on its own.
//
// It is a pure function of its arguments precisely so the frontends can call it
// to render what is about to happen while args builds the same answer for the
// command line. Two calls, one definition — the alternative is a spec that runs
// at one level and a header that claims another.
func ResolveEffort(mode Mode, requested string, hasPlan bool) (level, reason string) {
	if requested != "" {
		return requested, ""
	}
	switch {
	case mode == ModePlan:
		// The plan is the product. Thinking is the work, not overhead on it.
		return "high", effortDrafting
	case hasPlan:
		// The approach was decided and reviewed already; this run is carrying
		// it out. Stepping down is the whole point of having attached a plan.
		return "medium", effortFollowing
	default:
		// No plan, so the agent has to work out the approach as well as do it —
		// which is the planning run's job folded into this one.
		return "high", effortNoPlan
	}
}

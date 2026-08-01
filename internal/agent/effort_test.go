package agent

import (
	"strings"
	"testing"
)

func TestValidateEffort(t *testing.T) {
	// Empty is not an error: it is how a caller says "no preference", which is
	// what lets ResolveEffort choose.
	for _, level := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		if err := ValidateEffort(level); err != nil {
			t.Errorf("ValidateEffort(%q) = %v, want it accepted", level, err)
		}
	}

	// A typo must fail before a process is started, naming the alternatives —
	// left to the CLI it dies on an unknown option and looks like a failed run
	// rather than the mistyped flag it is.
	err := ValidateEffort("mid")
	if err == nil {
		t.Fatal("a bad effort level should be refused")
	}
	for _, want := range []string{"medium", "xhigh", "max"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, it should list %q as an alternative", err, want)
		}
	}

	// Levels the CLI would reject, and the two shapes of near-miss most likely
	// to be typed: a level from another scale, and one with the wrong casing.
	for _, level := range []string{"none", "highest", "High", "XHIGH", "1"} {
		if err := ValidateEffort(level); err == nil {
			t.Errorf("ValidateEffort(%q) = nil, want it refused", level)
		}
	}
}

// The recommendation is the feature: a plan run and a run following a reviewed
// plan are different shapes of work, and this is where ike says so.
func TestResolveEffort(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       Mode
		requested  string
		hasPlan    bool
		wantLevel  string
		wantReason string
	}{
		{"planning is the reasoning", ModePlan, "", false, "high", effortDrafting},
		{"revising a plan is still planning", ModePlan, "", true, "high", effortDrafting},
		{"no plan means working it out too", ModeExecute, "", false, "high", effortNoPlan},
		{"an attached plan is follow-through", ModeExecute, "", true, "medium", effortFollowing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, reason := ResolveEffort(tc.mode, tc.requested, tc.hasPlan)
			if level != tc.wantLevel {
				t.Errorf("level = %q, want %q", level, tc.wantLevel)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}

	// Whatever ike would have picked, an explicit level wins — the point of
	// showing the recommendation is that it can be argued with.
	for _, mode := range []Mode{ModePlan, ModeExecute} {
		for _, hasPlan := range []bool{false, true} {
			level, reason := ResolveEffort(mode, "low", hasPlan)
			if level != "low" {
				t.Errorf("level = %q, want the requested low", level)
			}
			// No reason, because there is nothing to explain about a flag
			// somebody typed — and an empty reason is what tells a frontend to
			// print the level on its own.
			if reason != "" {
				t.Errorf("reason = %q, want none for an explicit level", reason)
			}
		}
	}
}

// Both command lines must actually carry the level, or the recommendation is
// something ike prints and then does not do.
func TestEffortReachesTheCommandLine(t *testing.T) {
	joined := func(parts []string) string { return strings.Join(parts, " ") }

	run := joined(args(Spec{Mode: ModeExecute}))
	if !strings.Contains(run, "--effort high") {
		t.Errorf("execute args = %q, want the recommendation for a run with no plan", run)
	}
	withPlan := joined(args(Spec{Mode: ModeExecute, Plan: "step one"}))
	if !strings.Contains(withPlan, "--effort medium") {
		t.Errorf("execute args = %q, want the step down for an attached plan", withPlan)
	}
	if plan := joined(args(Spec{Mode: ModePlan})); !strings.Contains(plan, "--effort high") {
		t.Errorf("plan args = %q, want high", plan)
	}
	custom := joined(args(Spec{Mode: ModeExecute, Plan: "step one", Effort: "max"}))
	if !strings.Contains(custom, "--effort max") {
		t.Errorf("execute args = %q, want the override", custom)
	}

	// A session resolves the same way, so talking a task through and delegating
	// it do not silently run at different depths.
	s := Session{Mode: ModeExecute, SessionID: "id", Plan: "step one"}
	if got := joined(sessionArgs(s)); !strings.Contains(got, "--effort medium") {
		t.Errorf("session args = %q, want the same recommendation a run gets", got)
	}
	s.Effort = "low"
	if got := joined(sessionArgs(s)); !strings.Contains(got, "--effort low") {
		t.Errorf("session args = %q, want the override", got)
	}
}

// A bad level must be caught where a session is built, the way a bad permission
// mode is: InteractiveCommand is the point past which the terminal is gone.
func TestInteractiveCommandRefusesABadEffort(t *testing.T) {
	_, err := InteractiveCommand(t.Context(), Session{
		Mode:      ModeExecute,
		Dir:       t.TempDir(),
		SessionID: "id",
		Effort:    "mid",
	})
	if err == nil {
		t.Fatal("a bad effort level should be refused")
	}
	if !strings.Contains(err.Error(), "medium") {
		t.Errorf("error = %q, it should list the valid levels", err)
	}
}

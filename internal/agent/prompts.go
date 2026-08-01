package agent

import (
	"fmt"
	"strings"
)

// The prompts live together in one file because they are the product, in the
// way the jsonschema tags on the MCP tools are: they are the whole of what the
// agent knows about the task, and reviewing a change to them means reading them
// side by side.
//
// Both are written to a task that is often one line long. An ike task is a
// title in a quadrant — "fix the flaky login test" — so the prompt's job is to
// supply the context that terseness leaves out: which directory the work is in,
// how the user classified its urgency, and what has already been decided.

// prompt builds the text for a run.
func prompt(s Spec) string {
	if s.Mode == ModePlan {
		return planPrompt(s)
	}
	return executePrompt(s)
}

// planPrompt asks for a plan and nothing else.
//
// The output is stored verbatim as the task's plan, so the instruction to skip
// the preamble is not politeness: an "I'll help you with that!" opening would
// be saved as the first line of the plan and shown every time it is opened.
func planPrompt(s Spec) string {
	var b strings.Builder
	b.WriteString("Draft an implementation plan for one task from my Eisenhower matrix.\n\n")
	b.WriteString(describe(s))

	if s.Plan != "" {
		b.WriteString("\nThere is already a plan attached. Revise it — keep what still " +
			"holds, change what does not:\n\n")
		b.WriteString(fence(s.Plan))
	}

	b.WriteString("\nExplore the working directory as much as you need to. Then reply with " +
		"ONLY the plan, as markdown, with no preamble and no closing remarks — your " +
		"entire reply is stored as the plan and shown to me verbatim.\n\n")
	b.WriteString("Keep it under about 40 lines and cover: the goal in a sentence, the " +
		"concrete steps in order, the files to be touched, and how to check it worked. " +
		"Where a real decision has to be made, say what you chose and why, so I can " +
		"disagree with it before any code is written.\n")
	return b.String()
}

// executePrompt asks for the work to be done.
func executePrompt(s Spec) string {
	var b strings.Builder
	b.WriteString("Carry out one task from my Eisenhower matrix.\n\n")
	b.WriteString(describe(s))

	if s.Plan != "" {
		b.WriteString("\nI have already reviewed and attached this plan. Follow it. If you " +
			"find it is wrong, stop and say so rather than quietly doing something " +
			"else — I approved this, not a substitute:\n\n")
		b.WriteString(fence(s.Plan))
	} else {
		b.WriteString("\nNo plan is attached, so work out the approach yourself.\n")
	}

	b.WriteString("\nWhen you are done, finish with two or three sentences saying what you " +
		"actually changed. If you could not finish, say what is left and why — I am " +
		"reading this to decide whether the task is done, so an optimistic summary is " +
		"worse than no summary.\n")
	return b.String()
}

// describe states the task itself, shared by both prompts so they cannot
// disagree about how a task is presented.
func describe(s Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", s.Title)
	if s.Quadrant != "" {
		// The quadrant is how the user classified the work, and the label may
		// be one they wrote themselves, so it carries their own words for it.
		fmt.Fprintf(&b, "Quadrant: %s\n", s.Quadrant)
	}
	fmt.Fprintf(&b, "Working directory: %s\n", s.Dir)
	return b.String()
}

// fence wraps a plan so its markdown cannot be read as part of the
// instructions around it. A plan is full of headings and list items, and
// dropping it in raw would leave the model guessing where it ended.
func fence(plan string) string {
	// A fence long enough not to be closed by anything inside the plan, which
	// may itself contain fenced code blocks.
	const f = "``````"
	return f + "markdown\n" + strings.TrimRight(plan, "\n") + "\n" + f + "\n"
}

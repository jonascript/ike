package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/agent"
	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// agentDisabledMsg explains how to switch delegation on. It is worded like
// mcpDisabledMsg and for the same reason: the whole value of the message is
// that it names the command to run.
//
// It says what is being consented to rather than just that a flag is off.
// "Access is disabled" would undersell it — the thing being allowed is ike
// starting a process that edits files.
const agentDisabledMsg = "Delegation is off for this matrix.\n" +
	"`ike delegate` runs the Claude Code CLI in a task's working directory, where it\n" +
	"can read and change files. ike does not do that until you allow it:\n\n" +
	"  ike agent enable\n\n" +
	"Run `ike agent status` to see the current setting and which data file it applies to.\n" +
	"Drafting a plan with `ike plan` is read-only and needs no permission."

func newAgentCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Control whether ike may run an agent on your tasks",
		Long: "Delegation runs the Claude Code CLI as a subprocess, in the working\n" +
			"directory a task carries, where it can read and change files.\n\n" +
			"It is off until you allow it:\n\n" +
			"  ike agent enable\n\n" +
			"The setting is remembered per data file, and `ike agent disable` revokes it.\n" +
			"It is separate from `ike mcp enable`: letting an agent edit your task list is\n" +
			"not the same decision as letting ike start one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newAgentSetCmd(open, true), newAgentSetCmd(open, false), newAgentStatusCmd(open))
	return cmd
}

// newAgentSetCmd builds `ike agent enable` or `ike agent disable`, as one
// function for the reason newMCPSetCmd gives: two near-identical command
// bodies is how the two drift apart.
func newAgentSetCmd(open opener, on bool) *cobra.Command {
	use, short := "disable", "Revoke ike's permission to run an agent"
	if on {
		use, short = "enable", "Allow ike to run an agent on a task's working directory"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			changed, err := s.SetAgentEnabled(on)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !changed {
				fmt.Fprintf(out, "delegation was already %s for %s\n", onOff(on), s.Path())
				return nil
			}
			fmt.Fprintf(out, "delegation is now %s for %s\n", onOff(on), s.Path())
			if on {
				// Stated plainly rather than reassuringly. The mode names invite
				// the guess that the default holds something back, and it does
				// not: acceptEdits and auto both let a delegated agent run `rm`.
				fmt.Fprintf(out, "Runs use --permission-mode %s, which lets the agent change files "+
					"and run\ncommands in that directory. Use --permission-mode manual for a run that\n"+
					"stops at anything it would otherwise have to ask about.\n",
					agent.DefaultPermissionMode)
			}
			return nil
		}),
	}
}

// newAgentStatusCmd reports the setting, and like `ike mcp status` answers even
// after delegation has been revoked.
func newAgentStatusCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether delegation is enabled, and for which data file",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			enabled, err := s.AgentEnabled()
			if err != nil {
				return err
			}
			hint := "ike agent enable"
			if enabled {
				hint = "ike agent disable"
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "delegation: %s\n", onOff(enabled))
			fmt.Fprintf(out, "data file:  %s\n", s.Path())
			// Whether the binary is actually there is the other half of "will
			// this work", and finding out now beats finding out mid-run.
			fmt.Fprintf(out, "claude:     %s\n", claudeStatus())
			fmt.Fprintf(out, "change it:  %s\n", hint)
			return nil
		}),
	}
}

func claudeStatus() string {
	if override := os.Getenv("IKE_AGENT_CMD"); override != "" {
		return override + " (from IKE_AGENT_CMD)"
	}
	p, err := exec.LookPath("claude")
	if err != nil {
		return "not found on PATH — see https://claude.com/claude-code"
	}
	return p
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func newPlanCmd(open opener) *cobra.Command {
	var (
		show, edit, clear, prune bool
		interactive, fresh       bool
		fromFile, dir, model     string
	)
	cmd := &cobra.Command{
		Use:   "plan <id>",
		Short: "Draft, show, or edit the plan attached to a task",
		Long: "With no flags, asks the agent to draft a plan and attaches it to the task.\n" +
			"That run is read-only: it explores the working directory but cannot change it,\n" +
			"so it needs no permission.\n\n" +
			"With -i it opens a real Claude Code session instead, so you can talk the plan\n" +
			"through. The conversation is remembered: run it again and you pick up where you\n" +
			"left off. Whatever you agree on is attached to the task when you exit.\n\n" +
			"A plan is yours to keep or hand on. `ike delegate` follows it if there is one.",
		Args: cobra.MaximumNArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if prune {
				if len(args) != 0 {
					return errors.New("--prune sweeps every orphaned plan; it takes no task id")
				}
				n, err := s.PrunePlans()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", plural(n, "orphaned plan"))
				return nil
			}
			if len(args) != 1 {
				return errors.New("which task? pass a task id, or --prune to sweep orphaned plans")
			}
			id, err := parseID(args[0])
			if err != nil {
				return err
			}

			switch {
			case clear:
				t, d, err := s.ClearPlan(id)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "cleared the plan for %d  %s%s\n",
					t.ID, t.DisplayTitle(), inSpace(cmd, d))
				return nil

			case show:
				body, err := s.Plan(id)
				if err != nil {
					return err
				}
				if body == "" {
					return fmt.Errorf("task %d has no plan; run `ike plan %d` to draft one", id, id)
				}
				fmt.Fprint(cmd.OutOrStdout(), ensureNewline(body))
				return nil

			case fromFile != "":
				b, err := os.ReadFile(fromFile)
				if err != nil {
					return err
				}
				return savePlan(cmd, s, id, string(b))

			case edit:
				return editPlan(cmd, s, id)

			case interactive:
				return session(cmd, s, id, agent.ModePlan,
					sessionOpts{dir: dir, model: model, fresh: fresh})
			}

			return draftPlan(cmd, s, id, dir, model)
		}),
	}
	cmd.Flags().BoolVar(&show, "show", false, "print the attached plan")
	cmd.Flags().BoolVar(&edit, "edit", false, "open the plan in $EDITOR")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the attached plan")
	cmd.Flags().BoolVar(&prune, "prune", false, "delete plan files left behind by deleted tasks")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "attach a plan written elsewhere")
	cmd.Flags().StringVar(&dir, "dir", "", "working directory to explore (remembered on the task)")
	cmd.Flags().StringVar(&model, "model", "", "model to draft with, e.g. opus or sonnet")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false,
		"open a Claude Code session and talk the plan through, resuming last time's")
	cmd.Flags().BoolVar(&fresh, "new-session", false,
		"start a new conversation instead of resuming the task's")
	return cmd
}

func newDelegateCmd(open opener) *cobra.Command {
	var (
		dir, permission, model   string
		planFirst                bool
		interactive, freshSesson bool
	)
	cmd := &cobra.Command{
		Use:   "delegate <id>",
		Short: "Hand a task to an agent and watch it work",
		Long: "Runs the Claude Code CLI in the task's working directory, following the\n" +
			"attached plan if there is one, and streams what it does.\n\n" +
			"With -i it hands you the terminal instead, so you can supervise and answer\n" +
			"questions. The conversation is remembered per task: run it again and you pick\n" +
			"up where you left off rather than briefing a new agent.\n\n" +
			"This needs permission, because the agent can change files and run commands:\n\n" +
			"  ike agent enable\n\n" +
			"Runs are unattended, so nothing can prompt you mid-run. --permission-mode\n" +
			"manual makes the agent stop at anything it would have asked about; the\n" +
			"default lets it work without stopping.\n\n" +
			"The task is not completed automatically — read what it did and decide.",
		Args: cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			// Checked before the gate and before any process, so a typo costs
			// nothing and says what the alternatives are.
			if err := agent.ValidatePermissionMode(permission); err != nil {
				return err
			}
			// The gate, checked before anything else happens. A delegated run
			// has no session to revoke mid-flight the way an MCP client does,
			// so unlike the MCP gate this one check is the whole of it.
			enabled, err := s.AgentEnabled()
			if err != nil {
				return err
			}
			if !enabled {
				//nolint:staticcheck // ST1005: formatted for a human, not a caller.
				return errors.New(agentDisabledMsg)
			}

			if interactive {
				// Supervised: you are at the terminal, so the agent can ask
				// rather than deciding on your behalf.
				return session(cmd, s, id, agent.ModeExecute, sessionOpts{
					dir: dir, permission: permission, model: model, fresh: freshSesson,
				})
			}

			if planFirst {
				if err := draftPlan(cmd, s, id, dir, model); err != nil {
					return err
				}
				dir = "" // already resolved and stored by the planning run
				fmt.Fprintln(cmd.OutOrStdout())
			}

			t, plan, err := prepare(cmd, s, id, dir)
			if err != nil {
				return err
			}
			d, err := s.Load()
			if err != nil {
				return err
			}

			return stream(cmd, agent.Spec{
				Mode:           agent.ModeExecute,
				Title:          t.Title,
				Quadrant:       d.Labels.Of(t.Quadrant),
				Plan:           plan,
				Dir:            t.Dir,
				PermissionMode: permission,
				Model:          model,
			})
		}),
	}
	cmd.Flags().StringVar(&dir, "dir", "", "working directory to run in (remembered on the task)")
	cmd.Flags().StringVar(&permission, "permission-mode", "",
		"one of acceptEdits, auto, bypassPermissions, manual, dontAsk (default "+
			agent.DefaultPermissionMode+"; manual is the one that restrains a run)")
	cmd.Flags().StringVar(&model, "model", "", "model to run with, e.g. opus or sonnet")
	cmd.Flags().BoolVar(&planFirst, "plan-first", false, "draft a plan, then carry it out")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false,
		"open a Claude Code session and supervise the work, resuming last time's")
	cmd.Flags().BoolVar(&freshSesson, "new-session", false,
		"start a new conversation instead of resuming the task's")
	return cmd
}

// session hands the terminal to an interactive agent, then picks up whatever it
// left behind.
//
// The conversation is pinned to the task: the ID is generated and stored before
// the agent starts, so even a session that ends badly is resumable, and every
// later visit continues it rather than briefing a new agent from scratch.
func session(cmd *cobra.Command, s *store.Store, id int, mode agent.Mode, opts sessionOpts) error {
	t, plan, err := prepare(cmd, s, id, opts.dir)
	if err != nil {
		return err
	}
	d, err := s.Load()
	if err != nil {
		return err
	}

	resume := t.HasSession() && !opts.fresh
	if !resume {
		sid, err := agent.NewSessionID()
		if err != nil {
			return err
		}
		// Stored first. A session ike started but did not record would be
		// unreachable afterwards — the conversation would exist with no way
		// back to it.
		updated, _, err := s.SetSession(id, sid)
		if err != nil {
			return err
		}
		t = updated
	}

	draft, err := s.PlanDraftPath(id)
	if err != nil {
		return err
	}

	c, err := agent.InteractiveCommand(cmd.Context(), agent.Session{
		Mode:           mode,
		Title:          t.Title,
		Quadrant:       d.Labels.Of(t.Quadrant),
		Plan:           plan,
		Dir:            t.Dir,
		SessionID:      t.SessionID,
		Resume:         resume,
		DraftPath:      draft,
		PermissionMode: opts.permission,
		Model:          opts.model,
	})
	if err != nil {
		return err
	}
	// The real terminal, because this is a conversation.
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr

	out := cmd.OutOrStdout()
	verb := "resuming the conversation about"
	if !resume {
		verb = "starting a conversation about"
	}
	fmt.Fprintf(out, "%s %d  %s\n  in %s\n  come back to it any time with the same command\n\n",
		verb, t.ID, t.DisplayTitle(), t.Dir)

	if err := c.Run(); err != nil {
		// An agent you quit out of is the ordinary ending, and exit status is
		// not a reliable way to tell that from a real failure — so the draft is
		// still adopted below rather than the error stopping everything.
		fmt.Fprintf(cmd.ErrOrStderr(), "the session ended with: %v\n", err)
	}
	return adoptDraft(cmd, s, id)
}

// sessionOpts are the flags a session shares with a streamed run.
type sessionOpts struct {
	dir        string
	permission string
	model      string
	fresh      bool
}

// adoptDraft attaches a plan the agent left behind, if it left one.
func adoptDraft(cmd *cobra.Command, s *store.Store, id int) error {
	t, d, got, err := s.AdoptPlanDraft(id)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nattached the plan you agreed on to %d  %s%s\n  see it with: ike plan %d --show\n",
		t.ID, t.DisplayTitle(), inSpace(cmd, d), t.ID)
	return nil
}

// draftPlan runs a planning agent and attaches what it produces.
//
// It needs no gate: the run is --permission-mode plan, so it can read the
// working directory but not change it.
func draftPlan(cmd *cobra.Command, s *store.Store, id int, dir, model string) error {
	t, existing, err := prepare(cmd, s, id, dir)
	if err != nil {
		return err
	}
	d, err := s.Load()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "planning %d  %s\n  in %s\n\n", t.ID, t.DisplayTitle(), t.Dir)

	// The plan is the run's final result rather than everything it said, so it
	// is captured here instead of being reassembled from the transcript.
	var plan string
	err = streamInto(cmd, agent.Spec{
		Mode:     agent.ModePlan,
		Title:    t.Title,
		Quadrant: d.Labels.Of(t.Quadrant),
		Plan:     existing,
		Dir:      t.Dir,
		Model:    model,
	}, func(e agent.Event) {
		if e.Kind == agent.KindResult && !e.IsError {
			plan = e.Text
		}
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(plan) == "" {
		return errors.New("the agent finished without producing a plan")
	}
	fmt.Fprintln(out)
	return savePlan(cmd, s, id, plan)
}

// prepare resolves the task and its working directory, storing the directory
// if this is the first run or --dir was given.
//
// Falling back to the process's own directory is what makes `cd ~/dev/thing &&
// ike delegate 3` do the obvious thing. It is stored rather than used once, so
// the same task run later from anywhere goes back to the same project — which
// is the whole reason the field exists.
func prepare(cmd *cobra.Command, s *store.Store, id int, dir string) (task.Task, string, error) {
	d, err := s.Load()
	if err != nil {
		return task.Task{}, "", err
	}
	var t task.Task
	found := false
	for _, candidate := range d.Tasks {
		if candidate.ID == id {
			t, found = candidate, true
			break
		}
	}
	if !found {
		return task.Task{}, "", fmt.Errorf("no active task with id %d", id)
	}

	if dir == "" && t.Dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return task.Task{}, "", err
		}
		dir = cwd
		fmt.Fprintf(cmd.ErrOrStderr(), "using the current directory, and remembering it: %s\n", dir)
	}
	if dir != "" {
		source := "--dir"
		if !cmd.Flags().Changed("dir") {
			source = "the current directory"
		}
		updated, _, err := s.SetDir(id, source, dir)
		if err != nil {
			return task.Task{}, "", err
		}
		t = updated
	}

	plan, err := s.Plan(id)
	if err != nil {
		return task.Task{}, "", err
	}
	return t, plan, nil
}

func savePlan(cmd *cobra.Command, s *store.Store, id int, body string) error {
	t, d, err := s.SetPlan(id, strings.TrimSpace(body))
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "attached a plan to %d  %s%s\n  see it with: ike plan %d --show\n",
		t.ID, t.DisplayTitle(), inSpace(cmd, d), t.ID)
	return nil
}

// editPlan opens the plan in $EDITOR and stores whatever comes back.
//
// It goes through a temp file rather than handing the editor the sidecar path,
// so an editor that is killed, or one that writes a partial file, cannot leave
// the stored plan truncated — and so the result still passes validation before
// it is saved.
func editPlan(cmd *cobra.Command, s *store.Store, id int) error {
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editor == "" {
		return errors.New("set $EDITOR (or $VISUAL) to edit a plan, " +
			"or use `ike plan <id> --from-file`")
	}
	body, err := s.Plan(id)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "ike-plan-*.md")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// The editor is interactive, so it gets the real terminal.
	ed := exec.Command(editor, tmp) //nolint:gosec // the user's own $EDITOR, by definition
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := ed.Run(); err != nil {
		return fmt.Errorf("running %s: %w", editor, err)
	}

	edited, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(edited)) == strings.TrimSpace(body) {
		fmt.Fprintln(cmd.OutOrStdout(), "no change")
		return nil
	}
	return savePlan(cmd, s, id, string(edited))
}

// stream runs an agent and prints the transcript.
func stream(cmd *cobra.Command, spec agent.Spec) error {
	return streamInto(cmd, spec, nil)
}

// streamInto is stream with a hook, so the planning command can keep the final
// result without printing the transcript twice.
func streamInto(cmd *cobra.Command, spec agent.Spec, watch func(agent.Event)) error {
	run, err := agent.Start(cmd.Context(), spec)
	if err != nil {
		return err
	}
	// A Ctrl-C reaches the whole process group anyway, but cobra's context is
	// what carries a cancellation the caller arranged, and honoring it here is
	// what stops a delegated run outliving `ike`.
	defer run.Cancel()

	out := cmd.OutOrStdout()
	// A failure is returned rather than printed, so cobra reports it once and
	// the exit status is honest for anything scripting around ike. render
	// deliberately prints nothing for these two, or the reason would appear
	// twice — once in the transcript and again after "Error:".
	var failure string
	for e := range run.Events() {
		if watch != nil {
			watch(e)
		}
		render(out, e)
		switch {
		case e.Kind == agent.KindResult && e.IsError:
			failure = "the agent did not finish the task: " + oneLine(e.Text)
		case e.Kind == agent.KindError:
			failure = e.Text
		}
	}
	if failure != "" {
		//nolint:staticcheck // ST1005: the agent's own words, shown to a human.
		return errors.New(failure)
	}
	return nil
}

// render writes one event as a line of transcript.
//
// Text is already sanitized — internal/agent does it at the single point
// everything leaves that package — so nothing here has to remember to.
func render(w io.Writer, e agent.Event) {
	switch e.Kind {
	case agent.KindStarted:
		if e.Model != "" {
			fmt.Fprintf(w, "  · %s\n", e.Model)
		}
	case agent.KindThinking:
		fmt.Fprintf(w, "  · %s\n", firstLine(e.Text))
	case agent.KindTool:
		fmt.Fprintf(w, "  → %s\n", e.Tool)
	case agent.KindText:
		fmt.Fprintf(w, "%s\n", e.Text)
	case agent.KindResult:
		// A failed result is not printed here: streamInto returns it as the
		// command's error, and cobra prints that. Printing both would show the
		// reason twice.
		if !e.IsError && e.CostUSD > 0 {
			fmt.Fprintf(w, "\n  · done ($%.2f)\n", e.CostUSD)
		}
	case agent.KindError:
		// Likewise returned rather than printed.
	}
}

// oneLine flattens a multi-line message for use in an error, which is printed
// as a single line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// firstLine keeps a long block to one line of transcript. Thinking is shown as
// a hint that work is happening rather than as something to read.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 100
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

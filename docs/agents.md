# Agents

ike works with agents in both directions: an agent can manage your matrix, and
you can hand a task *to* an agent. They are separate features with separate
permissions, and both are off until you say otherwise.

- [MCP: letting an agent manage your matrix](#mcp)
- [Delegating a task to an agent](#delegating-a-task)

---

## MCP

**Agent access is off by default.** ike will not serve your tasks over MCP
until you say so, and registering ike with an MCP client is not enough on its
own — `ike mcp` refuses to start while access is off:

```sh
ike mcp enable      # allow AI agents to read and manage this matrix
ike mcp disable     # revoke it, including for a session already connected
ike mcp status      # show the current setting and which data file it applies to
```

The setting is remembered in the data file, so it survives restarts and
upgrades, and it is scoped to that **file** — which means every space in it.
Enabling access for one space enables it for all of them; to keep a matrix out
of reach, put it in a separate file (see [Spaces](data.md#spaces)) or pin the
server to one space with `ike -s work mcp`. Access never travels: a space you
export always lands with it switched off. It is not part of undo history — no
sequence of `ike undo` can re-open access you closed. While access is on, the
TUI shows a `◆ mcp` marker in its footer, and `?` explains the setting either
way.

`ike mcp disable` takes effect immediately, including on a client that is
already connected: the setting is re-checked on every read and every write, so
the next thing an agent tries fails. It does not wait for the session to end.

> **What this is, and isn't.** The gate is a **consent mechanism, not a security
> boundary.** It controls whether ike's MCP server will serve your matrix. It
> cannot restrain an agent that already has shell access on your machine — such
> an agent can read `~/.local/share/ike/tasks.json` directly, run `ike list`, or
> simply run `ike mcp enable` itself. Treat it as "I have decided to let my
> agent manage my tasks", not as a sandbox.

Once enabled, `ike mcp` runs an MCP server on stdio with tools `list_tasks`,
`add_task`, `complete_task`, `move_task`, `reorder_task`, `update_task`,
`delete_task`, `list_archive`, `restore_task`, `undo`, `redo`, `list_quadrants`,
`set_quadrant_label`, and `list_spaces`.

Every tool takes an optional `space` argument and defaults to whichever space
you are on, so an agent can work in one matrix while you work in another. It is
read-only about spaces: no tool can create, rename, delete, or switch one, since
switching would change what your own TUI and a bare `ike list` show. Launching
the server as `ike -s work mcp` pins it to that space — a request naming another
is refused, and `list_spaces` then reports only the one it was launched for.

Register with Claude Code:

```sh
ike mcp enable
claude mcp add ike -- ike mcp
```

Or in any MCP client config:

```json
{ "mcpServers": { "ike": { "command": "ike", "args": ["mcp"] } } }
```

---

## Delegating a task

MCP lets an agent manage your matrix. This is the other direction: handing a
task *to* an agent. It runs the [Claude Code](https://claude.com/claude-code)
CLI — your own installation, so your authentication, settings, and per-project
`CLAUDE.md` all apply.

There are two steps, and you can stop after the first.

### Draft a plan

`ike plan 3` (or `P` in the TUI) asks an agent to explore the task's working
directory and write a plan, which is then attached to the task. This is
read-only — it runs in Claude Code's plan mode and cannot change anything — so
it needs no permission.

```sh
ike plan 3                       # draft one, streaming as it works
ike plan 3 --show                # print it
ike plan 3 --edit                # open it in $EDITOR
ike plan 3 --from-file notes.md  # attach one you wrote yourself
ike plan 3 --clear               # remove it
```

The plan is yours. Read it, argue with it, edit it — then either do the work
yourself, or hand it on.

### Or talk it through

`ike plan 3 -i` (or `c` in the TUI) hands the terminal to a real Claude Code
session, opened in the task's directory and already briefed on it. Exit the
session and you're back in ike exactly where you were.

The conversation belongs to the task. Run it again next week and you resume the
same session — full history, nothing re-explained:

```sh
ike plan 3 -i                # first time: a new conversation, briefed on the task
ike plan 3 -i                # later: picks up where you left off
ike plan 3 -i --new-session  # start over deliberately
```

When you agree on a plan, the agent writes it to a path ike gave it, and ike
attaches it to the task as you come back — so `ike plan 3 --show` reflects what
you actually decided together. A conversation that ends without a plan attaches
nothing, which is most of them.

`⌁` marks a task with a conversation waiting to be picked up.

### Hand it on

`ike delegate 3` (or `D`) runs an agent that carries the task out, following the
attached plan if there is one.

```sh
ike agent enable                 # required before any delegated run
ike delegate 3                   # follow the attached plan, streaming
ike delegate 3 -i                # supervise it in a real session instead
ike delegate 3 --plan-first      # draft a plan, then carry it out
ike delegate 3 --dir ~/dev/thing # set the working directory
ike delegate 3 --model opus      # choose a model
ike delegate 3 --effort max      # choose how hard it works
```

`-i` works here too, and resumes the same per-task conversation: the agent does
the work while you watch and answer its questions, rather than reporting back
afterwards. Being present doesn't remove the gate — `ike agent enable` is still
required, because it's still ike starting an agent that edits your files.

**Delegation is off by default**, separately from MCP access. Letting an agent
edit your task list and letting ike start a process that edits your *files* are
different decisions, so agreeing to one is not agreeing to the other. Like the
MCP gate, the setting is per data file, survives restarts, is never carried into
an export, and is not part of undo history — no sequence of `ike undo` can
re-open it.

**A delegated run never completes the task** — read what it did and decide.

### How hard the agent works

Effort controls how deeply the agent thinks, how many tools it reaches for, and
how much it says on the way. It usually matters more than the model: a run's wall
clock is dominated by how many turns it takes, and effort is what moves that.

ike picks a level per run rather than using one setting for everything, because
the two things it delegates are not the same shape of work — and it prints what
it chose, and why, in the run header:

| run | effort | why |
|---|---|---|
| `ike plan 3` | `high` | drafting a plan — the thinking *is* the product |
| `ike delegate 3` with a plan attached | `medium` | following an attached plan; the approach is already decided and reviewed |
| `ike delegate 3` with no plan | `high` | no plan to follow, so it has to work the approach out as well as do it |

```
$ ike delegate 3
delegating 3  Fix the flaky reorder test
  in /Users/you/dev/thing
  · effort medium — following an attached plan
```

`--effort low|medium|high|xhigh|max` overrides it, on both `ike plan` and
`ike delegate`, and the header then just states the level. A mistyped level fails
before any process starts. Attaching a plan is therefore also the cheapest way to
make a delegated run cheaper: the run stops paying to re-derive decisions the
plan already records.

The TUI has no flag for this and always uses the recommendation, shown in the
same line at the top of the run view.

### What a delegated run is allowed to do

Runs are unattended, so nothing can prompt you part-way through. The default is
`--permission-mode auto`, which lets the agent change files *and run commands* in
the working directory.

Do not read the mode names as a safety ladder — they are not. Measured against a
real run, asking an agent to `rm` a file:

| `--permission-mode` | result |
|---|---|
| `manual` | denied — the file survived |
| `acceptEdits` | allowed — the file was deleted |
| `auto` (default) | allowed — the file was deleted |
| `bypassPermissions` | allowed, by definition |

So `acceptEdits` is **not** a middle setting that withholds the shell.
`manual` is the one mode that meaningfully restrains a delegated run: with nobody
to ask, it denies anything needing approval, and the denials come back in the
transcript. Use it when you want the agent to work and then stop at the first
thing you would have wanted to be asked about.

```sh
ike delegate 3 --permission-mode manual
```

One trap worth knowing if you test this yourself: commands the harness
classifies as safe — `echo`, `ls` — run under *every* mode including `manual`,
so a harmless command shows no difference between any of them.

The practical control is therefore the gate — whether ike starts an agent at all
— rather than the mode it starts it in.

Each task remembers a **working directory**. The first run stores the one you
give it, or the directory you ran from, so `cd ~/dev/thing && ike delegate 3`
does the obvious thing and later runs go back to the same project wherever you
start them.

In the TUI a run takes over the screen and streams. `esc` detaches and the run
keeps going — the task shows `⣾` in the matrix and `D` on it reattaches. `ctrl+c`
in the run view stops the run. Quitting ike stops it too: the agent is a child
process, and ike will not leave one running with nothing reading it.

> **The same caveat as MCP.** The gate is a consent mechanism, not a security
> boundary. It decides whether *ike* starts an agent; it does nothing about what
> that agent then does, which is governed by Claude Code's own permissions. And
> anyone who can run commands as you can run `claude` directly.

Plans are stored beside the data file, one file per task, in
`tasks.json.plans/<space>/<id>.md` — not inside `tasks.json`, so a few KB of
markdown per task is not copied into every undo snapshot. Deleting a task leaves
its plan, so undoing the delete brings both back; `ike plan --prune` sweeps the
ones left behind. Note that plans do **not** travel with `ike space export` yet.

Delegation needs `claude` on your `PATH`; `ike agent status` says whether it
found it. Set `IKE_AGENT_CMD` to point at a different binary or a wrapper.

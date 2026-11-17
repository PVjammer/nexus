# Work items and tasks

*Workstream task-mgmt, 2026-11-13 → 11-17. Built into desk (`prototypes/desk`) on branch `fm/task-mgmt`. The clickable mocks that led here are in `prototypes/tasks-mock/` (v1: three home screens; v2: a desktop of widgets).*

## The problem

On a Monday the question is "what needs me, across everything, and what do I do first?". Today the answer is a pile of sessions to read. Foreman's ledger on 2026-11-13 had 5 open workstreams; each was blocked on one short action (four of them the user's), every one showed the same "stale: close it out or split" nudge, and the links between them appeared nowhere. Threads linger because their blocker is invisible or too vague to act on.

What's wanted: **focus** (what needs me now), **decomposition** (the concrete next actions, not 700 words), **clarity** (whose move it is, and what done means), with **no extra overhead** and the ability to dig in.

## Decisions

1. **Work items live in their source; Nexus never owns them.** A work item is a Jira ticket, a GitHub issue, a foreman workstream, a backlog line, a doc. Nexus reads it, links to it, and never writes to it. Mirroring anything back is an explicit write the user approves.
2. **Tasks live only in Nexus.** A task is one concrete next action toward a work item ("Review MR !41", "Grant API access (IT-882)"). A task with no work item is a plain todo.
3. **The board is the plain kanban:** To do, In progress, Blocked, In review, Done. Each task is assigned to **you**, **agent**, or **a named person** (which records that you're waiting on them). It runs in the background: cards move as work happens, and you never have to look at it.
4. **Nexus keeps three things about a work item:** whether it's in progress (or parked, and on what), its done-when list, and its tasks. Parking hides its tasks until you bring it back.
5. **Whose move it is is derived, never typed:** yours (a task of yours that isn't blocked, or a review waiting on you), the agent's, someone else's, blocked, or no next action yet.
6. **Clicking a work item opens its workspace:** the project with that work item open (what's left, grouped by whose move; done-when), the project agent beside it, and the task you came from docked at the top.
7. **Everything on screen is a widget, and a widget is data:** what it shows, for what (everything, one project, one work item), in which shape, at which size. Home, project screens and workspaces are layouts of widgets. Task management is one **app** among others (wiki, docs, sheets, terminal, browser, plugins); its default views (Needs you, Board, Work items, Todos) are just widgets you never have to open.
8. **Home starts as one prompt box.** It answers anything, and it can also arrange the screen. **Customize manually** does the same by hand. The "need you" count in the top bar can't be hidden by any layout.
9. **The agent builds with the same parts you do.** Its changes go through the same validated functions, are marked as the agent's with its reason, and can be undone. "What is this?" on any widget shows its spec.
10. **Every capability agents use to change Nexus ships with a skill,** written so that non-frontier models can use it, kept from drifting by tests, and checked against a real model.

## The model

```
Work item (in its source; read only)       Tracked (Nexus)                 WorkTask (Nexus only)
  ref: ws:<project>/<name>                   state: in-progress | parked     title, status (board)
       bl:<project>/<hash>                          | not-started            assignee: you | agent | name
       item:<id>    (later jira:, gh:)        park note, done-when list       reviewer, blocked_by, due, note
  title, source, source state, report                                       item ref ("" = todo)
```

- **Sources today:** open foreman workstreams (in progress by default), foreman backlog lines and Nexus list items (not started until you start them). Jira, GitHub and doc-based sources slot into the same shape.
- **Settle:** a blocked task whose blockers are all done moves to To do by itself. A blocked task with no blockers is waiting on something outside Nexus and stays blocked until someone moves it.
- **Needs you:** your To do or In progress tasks, plus reviews waiting on you, in work items that are in progress (and todos). They join the existing needs-you list, so the top-bar count and every Needs you widget include them.
- **Relation to desk's older pieces:** desk's `Item` (a project's Nexus list) becomes one work-item source. Desk's `Task` (a dispatched background agent run) stays what it is: an agent task can be turned into one ("Draft for a task agent"). `PlanStep` remains the task agent's own checklist.

## What you see

- **Home:** the prompt box. Ask "what needs me?" and get the answer in place; "keep this on screen" turns it into a widget. "Set up my usual dashboard" adds Needs you, Running, Work items, Inbox and Projects.
- **Customize manually:** columns 1–4, tiling Flow (page scrolls) or Tile (widgets fill the screen), and per widget: move, width, height, shape, scope, remove; Add a widget from the catalog; Undo; presets.
- **Chats:** every conversation with an agent, saved and listed whether or not a chat widget shows it. The chat widget has a Minimal and a Conversation style.
- **Apps → Tasks:** Needs you, Board, Work items (in progress with whose move, progress and next task; then parked and not started, with Start and Bring back), Todos.
- **The task panel:** status, who, reviewer, waits on, due, note; Done, Take it / Hand to the agent, Draft for a task agent, and **Open in workspace ↗**.
- **The workspace:** the work item's pane in its project: what's left, grouped by whose move, done-when, a docked task, and the project agent beside it (it sees the work item and its tasks).

## Agents

- **Tools:** `home_*` (show, add, change, remove, grid, reset, undo) for the home agent; `work_*` (items, tasks, task_add, task_update, item_set) for the home and project agents. Project agents can change only their own project's tasks. `pane_open kind=workitem` opens a workspace pane.
- **Skills:** `skills/home-layout` and `skills/tasks`. They use small fixed choices, a worked example per common request, errors that say how to fix the call, and ids or names (a kind, an exact task title) accepted. The catalog and statuses are generated into them at agent start. `TestSkillExamples` runs every example against the real tools.
- **Eval:** `make eval-home MODEL=…` gives a real model 13 plain requests. These cover arranging the screen, breaking work down, recording who you're waiting on, parking, and a plain question that must change nothing. The homelab Qwen3.6-35B-A3B (low) passed 10/10 of the layout cases, and Claude Code (sonnet) passed a 3-case subset (see the report for the full run).
- **Agent loops:** pi, or Claude Code headless (`claude -p`, no SDK, the user's own login) per agent kind. With Claude Code, its own tools are off. It gets Nexus's tools from `nexus-core mcp`, runs in an empty folder, and loads no user settings, hooks, MCP servers or CLAUDE.md. It still adds the account email and environment details to its context.

## Cowork, not autopilot

- The agent proposes and arranges; outward actions (posting, merging, closing in the source) stay confirmed presses. In this build, Nexus never writes to a source at all.
- Every agent change is visible (badge, reason, toast with Undo) and reversible.
- Take it / Hand to the agent on every agent task. Waiting on a person is a private note; they never see it.
- A question changes nothing: the agent answers and offers, it doesn't add tasks or widgets unasked.

## Open questions

1. Should agent tasks start on their own when they stay inside the agent's environment, or always wait for a press? Today an agent task is a record, and "Draft for a task agent" is the press.
2. Should starting a fifth in-progress work item nudge you to park one? The mock did this at four; the build doesn't yet.
3. Which source comes first after foreman: Jira (work) or GitHub (personal)?

## Follow-ups

- **Triggers and suggestions** (on the backlog, 2026-11-17): scheduled checks (deadline in N days, idle for Y days, source watchers like the SEC-431 watcher), callbacks (source changed, task finished), and suggested next actions. They're built from data the user and agent compose, they read and propose only, and each one is visible, pausable and logged.
- **Project dashboards** with the same widget model, and widgets in workspaces.
- **Sources:** Jira and GitHub adapters (list, read, watch; writes only as approved, brokered actions).
- **For foreman's landing workstream:** keep the work item separate from the workstream, show whose move it is with a day count during in-review, and map `followups.jsonl` to start / queue / drop.

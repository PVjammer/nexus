# Work items and tasks

*Workstream task-mgmt, 2026-11-13 → 11-17 (onboarding, shaping and the customizer added 11-17). Built into desk (`prototypes/desk`) on branch `fm/task-mgmt`. The clickable mocks that led here are in `prototypes/tasks-mock/` (v1: three home screens; v2: a desktop of widgets).*

## The problem

On a Monday the question is "what needs me, across everything, and what do I do first?". Today the answer is a pile of sessions to read. Foreman's ledger on 2026-11-13 had 5 open workstreams; each was blocked on one short action (four of them the user's), every one showed the same "stale: close it out or split" nudge, and the links between them appeared nowhere. Threads linger because their blocker is invisible or too vague to act on.

What's wanted: **focus** (what needs me now), **decomposition** (the concrete next actions, not 700 words), **clarity** (whose move it is, and what done means), with **no extra overhead** and the ability to dig in.

## Decisions

1. **A work item is what a project is working toward.** By default it lives on the project's list in Nexus (Nexus's data directory, never the repo). When a project has a tracker (Jira, GitHub, Linear), its tickets are work items read from that source, and Nexus never writes to the source; mirroring anything back is an explicit write the user approves.
2. **Tasks live only in Nexus.** A task is one concrete next action toward a work item ("Review MR !41", "Grant API access (IT-882)"). A task with no work item is a plain todo.
3. **The board is the plain kanban:** To do, In progress, Blocked, In review, Done. Each task is assigned to **you**, **agent**, or **a named person** (which records that you're waiting on them). It runs in the background: cards move as work happens, and you never have to look at it.
4. **Nexus keeps three things about a work item:** whether it's in progress (or parked, and on what), its done-when list, and its tasks. Parking hides its tasks until you bring it back.
5. **Whose move it is is derived, never typed:** yours (a task of yours that isn't blocked, or a review waiting on you), the agent's, someone else's, blocked, or no next action yet.
6. **Clicking a work item opens its workspace:** the project with that work item open (what's left, grouped by whose move; done-when), the project agent beside it, and the task you came from docked at the top.
7. **Everything on screen is a widget, and a widget is data:** what it shows, for what (everything, one project, one work item), in which shape, at which size. Home, project screens and workspaces are layouts of widgets. Task management is one **app** among others (wiki, docs, sheets, terminal, browser, plugins); its default views (Needs you, Board, Work items, Todos) are just widgets you never have to open.
8. **Home starts as one prompt box.** It answers anything, and it can also arrange the screen. **Customize manually** does the same by hand. The "need you" count in the top bar can't be hidden by any layout.
9. **The agent builds with the same parts you do.** Its changes go through the same validated functions, are marked as the agent's with its reason, and can be undone. "What is this?" on any widget shows its spec.
10. **Every capability agents use to change Nexus ships with a skill,** written so that non-frontier models can use it, kept from drifting by tests, and checked against a real model.
11. **Nexus assumes no particular tool.** A new user has a git repo and nothing else. Whatever a project keeps (a backlog file, design notes, reports, another tool's folders) is found by looking, the same way for every project. Integrations such as the foreman ledger are per-project add-ons, off unless switched on (`-foreman` makes them default on).
12. **Onboarding is a conversation with the project agent, in four steps you can skip or redo:**
    - **scan:** plain code, no model, lists the signs of work with where each one is (unmerged branches, uncommitted changes, unchecked checklists, plan sections, TODO comments, design docs);
    - **propose:** the agent groups the findings into 3 to 8 work items, each with a draft brief and done-when from what it read, which you tick as a checklist; what you dismiss isn't suggested again. Proposals from one turn are one checklist;
    - **ask:** what else do you want to do here, and which are you working on now (the in-progress limit applies);
    - **shape:** for each item in progress, up to 3 questions with answers to click, then a brief, a done-when list and 2 to 5 proposed tasks.
13. **Shaped means:** a brief, a done-when list and at least one task. An in-progress item without them shows **Needs shaping** and a **Shape** button, which starts the same questions-then-tasks conversation. Proposed tasks can name what they come **after**; accepted, such a task starts blocked on it and moves to To do when it's done, so Needs you holds only what you can do now. Agents ask **one question per turn** (the core refuses a second), and an unanswered question is in Needs you with its answers.
14. **The agent proposes; you tick.** Work it found and tasks it suggests arrive as a checklist (in the chat, in Needs you and in Tasks), never straight onto your list. What you name yourself goes straight in.
15. **The customizer (Eitri) changes how Nexus looks, on a draft.** Ctrl+. or the hammer button opens a small floating window; you ask, point at a widget if you like, and go back to work. While it works, what it's changing is under construction on your screen, and new widgets appear as a door that workers carry things through, one trip per real step. When it's done, small changes (resize, move, reshape, remove) go live with Undo; anything new is shown in place to try, with **Keep** or **Roll back**. The name is one constant.

## The model

```
Work item (in its source; read only)       Tracked (Nexus)                 WorkTask (Nexus only)
  ref: ws:<project>/<name>                   state: in-progress | parked     title, status (board)
       bl:<project>/<hash>                          | not-started            assignee: you | agent | name
       item:<id>    (later jira:, gh:)        park note, done-when list       reviewer, blocked_by, due, note
  title, source, source state, report                                       item ref ("" = todo)
```

- **Sources today:** the project's list in Nexus (`item:<id>`), plus foreman workstreams and backlog lines when a project switches the foreman add-on on. Jira, GitHub and doc-based sources slot into the same shape. Tools accept a ref or a work item's exact title.
- **Tracked** also keeps a **brief** (what it is, in the user's words). **Proposals** (`WorkProposal`) are kept per batch: a work item for a project, or a task toward a work item, with why and evidence; open, accepted or dismissed.
- **Settle:** a blocked task whose blockers are all done moves to To do by itself. A blocked task with no blockers is waiting on something outside Nexus and stays blocked until someone moves it.
- **Needs you:** your To do or In progress tasks, plus reviews waiting on you, in work items that are in progress (and todos). They join the existing needs-you list, so the top-bar count and every Needs you widget include them.
- **Relation to desk's older pieces:** desk's `Item` (a project's Nexus list) becomes one work-item source. Desk's `Task` (a dispatched background agent run) stays what it is, shown as **Agent runs** in the project rail so "task" means one thing on screen: an agent task can be turned into one ("Draft for a task agent"). `PlanStep` remains the task agent's own checklist.

## What you see

- **Home:** the prompt box. Ask "what needs me?" and get the answer in place; "keep this on screen" turns it into a widget. "Set up my usual dashboard" adds Needs you, Running, Work items, Inbox and Projects.
- **Customize manually:** columns 1–4, tiling Flow (page scrolls) or Tile (widgets fill the screen), and per widget: move, width, height, shape, scope, remove; Add a widget from the catalog; Undo; presets.
- **Chats:** every conversation with an agent, saved and listed whether or not a chat widget shows it. The chat widget has a Minimal and a Conversation style.
- **Apps → Tasks:** Needs you, Board, Work items (suggestions to tick; in progress with whose move, progress and next task, or Needs shaping with Shape; then parked and not started, with Start and Bring back; projects with no work items yet, with Set up), Todos.
- **A new project:** its chat opens with **Set up this project**. The agent's questions show as answers to click, and its suggestions as a checklist in the chat.
- **Eitri:** the floating window (Ctrl+.), "Point at something", the construction on the home screen while it works, and a bar to try the result with Keep or Roll back.
- **The task panel:** status, who, reviewer, waits on, due, note; Done, Take it / Hand to the agent, Draft for a task agent, and **Open in workspace ↗**.
- **The workspace:** the work item's pane in its project: what's left, grouped by whose move, done-when, a docked task, and the project agent beside it (it sees the work item and its tasks).

## Agents

- **Tools:** `home_*` (show, add, change, remove, grid, reset, undo) for the home agent and the customizer (whose edits go to a draft); `work_*` (items, tasks, task_add, task_update, item_set, item_add, propose) plus `project_scan` and `ask_user` for the home and project agents. Project agents can change only their own project's tasks. `pane_open kind=workitem` opens a workspace pane.
- **Skills:** `skills/home-layout`, `skills/tasks` and `skills/onboarding`. They use small fixed choices, a worked example per common request, errors that say how to fix the call, and ids or names (a kind, an exact task title) accepted. The catalog and statuses are generated into them at agent start. `TestSkillExamples` runs every example against the real tools.
- **Eval:** `make eval-home MODEL=…` gives a real model 18 plain requests. These cover arranging the screen, breaking work down, recording who you're waiting on, parking, a plain question that must change nothing, setting up a project, shaping a work item, and three requests to the customizer (a small change, a new widget, "remove this" while pointing). See the report for the runs.
- **Agent loops:** pi, or Claude Code headless (`claude -p`, no SDK, the user's own login) per agent kind. With Claude Code, its own tools are off. It gets Nexus's tools from `nexus-core mcp`, runs in an empty folder, and loads no user settings, hooks, MCP servers or CLAUDE.md. It still adds the account email and environment details to its context.

## Cowork, not autopilot

- The agent proposes and arranges; outward actions (posting, merging, closing in the source) stay confirmed presses. In this build, Nexus never writes to a source at all.
- Every agent change is visible (badge, reason, toast with Undo) and reversible.
- Take it / Hand to the agent on every agent task. Waiting on a person is a private note; they never see it.
- A question changes nothing: the agent answers and offers, it doesn't add tasks or widgets unasked.

## Open questions

1. Should agent tasks start on their own when they stay inside the agent's environment, or always wait for a press? Today an agent task is a record, and "Draft for a task agent" is the press.
2. Which tracker comes first as a source: Jira (work) or GitHub issues (personal)?
3. Should a project's list ever be kept in the repo (an opt-in export the user commits), for people who want it versioned with the code?

## Follow-ups

- **Triggers and suggestions** (on the backlog, 2026-11-17): scheduled checks (deadline in N days, idle for Y days, source watchers like the SEC-431 watcher), callbacks (source changed, task finished), and suggested next actions. They're built from data the user and agent compose, they read and propose only, and each one is visible, pausable and logged.
- **Project dashboards** with the same widget model, and widgets in workspaces.
- **Sources:** Jira and GitHub adapters (list, read, watch; writes only as approved, brokered actions).
- **The customizer on other screens:** once project dashboards and workspaces are layouts, Eitri works there the same way.
- **For foreman's landing workstream:** keep the work item separate from the workstream, show whose move it is with a day count during in-review, and map `followups.jsonl` to start / queue / drop.

# Nexus desk prototype

The second UX prototype. It replaces the frame of [`../ux`](../ux) (a tiling window manager with a chat column) with the direction from the 2026-11-12 review: **a desk for doing project work with agents, organized around your real projects**. The mechanisms from `ux` carry over: secure tool and entry, broker, review threads, sheets and plugins. **Every backend choice is a placeholder** (see [where-we-are](../../docs/design/where-we-are.md)).

## Run it

```bash
cd projects/nexus/prototypes/desk
make deps        # once: web dependencies
make dev         # first run is onboarding: the home agent finds and adds your projects
# or
make seeded      # skip onboarding: Homelab, AI Projects, foreman, agent-lockdown and Nexus up front
```

Open the printed URL (it ends in `#token=…`). It uses port 8741 and stores data in `~/.local/share/nexus-desk`, so it can run alongside the `ux` prototype. `make reset` starts over. **Nexus only reads your project folders. It never writes to them**, and foreman's ledgers aren't touched either. Foreman ledgers are only read when a project switches the add-on on, or with `CORE_FLAGS=-foreman`.

## What's in it

| | What you see | What to try |
|---|---|---|
| **Home** | Starts as **one prompt box** (the home agent). Ask it anything; it can also arrange this screen ("put what needs me under this box", "two columns", "undo that"). **Customize manually** (bottom right) does the same by hand: columns, Flow or Tile, add/move/resize/remove widgets, Undo, presets. Every widget is data (what it shows, for what, in which shape, at which size); **⋯ → What is this?** shows it, and anything the agent added is marked. The **need you** count in the top bar is always there. | "Set up my home screen", then Customize manually |
| **Chats** | Every conversation with an agent (home, each project, each task), saved and listed whether or not it's on a screen. The chat widget has two styles: **Minimal** (prompt + last exchange) and **Conversation**. | Open Chats, pick the home agent, keep talking |
| **Apps** | What you and the agents share: **Tasks**, Wiki, Docs, Sheets, Terminal, Browser, Action log, and plugins. The launcher is how you open them by hand; agents use the same apps through tools and can open them for you. | Apps → Tasks |
| **Set up a project** | A new project's chat offers **Set up this project**. The project agent scans the folder (plain code, no model: branches, uncommitted changes, checklists, plan sections, TODOs, design docs; each with where it is), proposes a few work items as a **checklist you tick**, asks what else you want to do and which you're on now, then **shapes** each: up to 3 questions with answers to click, a brief, done-when, and proposed tasks. **Shape** on any item marked Needs shaping does the last part. | Open a project, press Set up this project |
| **Eitri** | Ctrl+. (or the hammer button) opens a small floating window that changes how Nexus looks. Ask, optionally **Point at something**, and go back to work. It builds on a draft: what it's changing is under construction, new widgets appear as a door with workers (one trip per real step). Small changes go live with Undo; anything new is shown to try, with **Keep** or **Roll back**. | Ctrl+., "add my task board under the prompt" |
| **Tasks** | **Work items** are on the project's list in Nexus (or come from a source such as a foreman ledger, when that add-on is on) and are never changed in a source. **Tasks** are next actions toward them, kept only in Nexus, on the board To do / In progress / Blocked / In review / Done, assigned to you, an agent or a named person. Tasks waiting on you join Needs you; parking a work item hides its tasks until you bring it back. Click a task for its panel (status, who, waits on, due, Take it / Hand to the agent); **Open in workspace ↗** opens the work item in its project with the task docked, next to the project agent. Widgets: Work items, Task board, Todos. | "Break demo-ws down", "I'm waiting on IT for access", then Apps → Tasks → Board |
| **First run** | A blank canvas and the home agent. It scans `~/Workspace` for git repos with a README, proposes a list, adds the projects with briefs in its own words, points you at Set up in each, and offers a widget. | `make reset && make dev`, then click **Find my projects** |
| **Capture** | **+ Capture**, Ctrl+Shift+Space, or the box on the dashboard. One line per thought. The home agent proposes a project, a kind and a clean title, and you **Accept**, change or dismiss each one. | Paste a messy dump of notes |
| **Projects** | A rail showing the brief, Needs you, Tasks, Items, foreman **Workstreams** (click one to open its report), the **Backlog** (→ dispatches an item), Docs and a **Journal**. Panes sit in the middle and the **project agent** on the right. | Open a foreman report and comment on it with *ask agent* |
| **Dispatch** | The project agent drafts a task (title plus brief), or you click → on an item or backlog line. You edit the brief and click **Start**. A **task agent** works in the background (read-only on the project), writes a report and finishes. The report lands in Needs you and opens as a commentable document. | "Draft a background task: does GitLab plus a runner fit on the HP box?" |
| **Machines** | Panes say where they live. The terminal is **your machine**, and agents can only propose commands there; they **can't read it**. Task panes are the **agent's workspace**. Secure panes are **private to you**. | Ask the project agent to propose a command |
| **Secure** | Secrets go to your screen and never to an agent: a secure pane in a project, or a secure card at home. Secret-looking files (`.env`, keys, credentials) are refused to every agent. | "What's the Wi-Fi password?" |
| **Wiki** | Each project has a wiki (stored by Nexus, not in your repo). **Build the wiki** in the rail has the project agent draft `index.md` and a few pages from the README, docs and reports. Every agent in the project starts from the index. The project agent edits pages directly, and every version is kept. Task agents only *propose* changes, which appear in Needs you as a diff to accept or reject. Click **Edit** on a page to change it yourself. `[[page]]` links work everywhere. Pages whose sources changed are marked **stale**. | "Build the wiki", then dispatch a task and accept its wiki proposal |
| **Add-ons** | Per-project switches at the bottom of the rail. **Code graph** (on for git projects) gives project and task agents `code_explore`, `code_symbols`, `code_symbol`, `code_callers` and `code_impact`. It indexes a Nexus-owned mirror of your git files with secrets skipped and telemetry off, and refreshes every 2 min. **Wiki check** and the **foreman ledger** view can be switched off too. | "How does X work?" in a code project |
| **Panes** | Each group header can split right or down, float, zoom and close; middle-click a tab to close it. tmux keys: **Ctrl+B**, then `%` split right, `"` split down, `x` close, `z` zoom (Esc restores), `f` float, `o`/arrows to move, `c` new terminal. Floating windows can be dragged and resized. | Float a terminal next to a report |
| **Widgets** | The home agent builds dashboard widgets as sandboxed plugins. Data comes only through `projects_summary`, for permissions you grant. | "Make me a widget with 14-day activity per project" |

## Agents

| Agent | Sees | Can do | Can't |
|---|---|---|---|
| **Home** (one) | every project (read), the overview | discover and add projects, propose filings, search across projects, widgets, secure tools | arrange panes or dispatch. It isn't how you work *in* a project. |
| **Project** (one per project) | its project (read), its panes, your selection | panes, notes and sheets under `desk/<project>/`, review threads and suggestions, items, **draft** tasks, propose commands | write to the repo, start tasks, read the terminal |
| **Task** (one per dispatch, ephemeral) | its project (read) | write a report, finish | change anything; it stops when done |

Each agent is its own process with its own token, which decides its tools. The core defines and polices all tools (`internal/app/tools.go`). Instructions are in `pi/home.md`, `pi/project.md` and `pi/task.md`, plus the **skills** for that agent kind.

**Agent loops.** pi by default. **Claude Code** works too, headless (`claude -p`, no SDK, your Claude Code login): `CORE_FLAGS='-agent claude'`, or per kind (`-agent-task claude -claude-model-task opus`). Its own tools are off; it gets Nexus's tools from `nexus-core mcp` (an MCP server that forwards to the core with the agent's token), runs in an empty folder in the data dir, and loads none of your Claude Code settings, hooks, MCP servers or CLAUDE.md. Claude Code still adds your account email and environment details to its context (not removable without `--bare`, which refuses logins). One conversation per agent, resumed after a restart; Stop interrupts the turn.

**Skills.** Each capability agents use to change Nexus ships with a skill (`skills/<name>/SKILL.md`, agentskills.io format): `home-layout` (home agent) and `tasks` (home and project agents). They're written for small local models: one tool per action, fixed choices, a worked example per common request, errors that say how to fix the call, ids or names accepted. Parts that must match the code (the widget catalog, the task statuses) are generated into the text when an agent starts, and `TestSkillExamples` runs every example against the real tools, so a skill can't drift. `make eval-home MODEL=…` checks a real model against 13 plain requests.

## Tests

- **`make test`** runs Go tests: reading a foreman ledger (inbox minus acks, lenient JSONL), project scopes and read-only folders, secret-file refusal, the secure tool, focus and undo, capture → propose → file, the task lifecycle and report versioning, the wiki (writes, history, proposals, scopes, bootstrap), plans, add-on tool gating, and widget validation.
- **`make eval-home MODEL=spark/Qwen3.6-35B-A3B:low`** (or `MODEL=claude:sonnet`) runs a real model through 13 requests: arranging the home screen, breaking work down, recording who you're waiting on, parking, and one plain question that must change nothing.
- **`make e2e`** runs 52 checks against a live core and a scratch project with a canary `.env`, playing both the UI and each agent (no model needed, via `-test-hooks`). It ends with a leak check over Nexus's data directory.
- **Tried live** on 2026-11-12 with pi and the homelab model:
  - onboarding of the five real projects
  - "what needs me in the homelab"
  - filing three captures
  - a dispatched task that read the GitLab design and hardware inventory and reported "doesn't fit comfortably on 9.4 GB" with ranked options

## Known gaps

- **The wiki lives in Nexus only.** A per-project "commit to the repo" mode (edits as a reviewed change) isn't built yet.
- **Ctrl+B is taken by Nexus,** including inside terminals, so it won't reach a tmux running there.

- **Foreman is read-only here.** Clearing a need hides it in Nexus only. Dispatch uses Nexus's own task agents, not foreman workstreams.
- **Work item sources** are the project's list in Nexus, plus foreman workstreams and backlog lines when that add-on is on. Jira, GitHub and doc-based sources are next; the work-item shape is ready for them.
- **Tasks don't run agents by themselves.** An agent task is a record of who's on it; "Draft for a task agent" turns it into a dispatched task you start. Triggers (cron, callbacks, suggestions) are on the backlog.
- **Layouts are for Home only.** Project screens keep their pane layout; project dashboards would use the same widget model.
- **No sandbox.** Task agents are read-only through the tools. A real environment (worktree plus agentbox or OpenShell) is the parked exploration in `the-split.md`.
- **Filing quality depends on the model.** In the live run, "openshell under podman" went to homelab, although agent-lockdown has an `openshell-compare` workstream. The home instructions now say to search first.
- **No global hotkey yet.** Ctrl+Shift+Space works only inside the window.

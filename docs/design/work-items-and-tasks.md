# Work items and tasks

*2026-11-13, workstream task-mgmt. **Draft 1: the model and the views (§1–2).** §3–7 are sketches until the check-in answers come back.*

## The question

How should Nexus model work so that three things are always obvious: **where you're needed**, **what's blocked and on what**, and **what's left before a work item is done**? And how can the model itself keep the number of open threads low?

Answers that would change a decision:
- whether desk's `Item`, `Task` and `PlanStep` merge into one Nexus `Task` (§5)
- whether Nexus needs any store of its own for scope and done criteria, or reads them from the source only (Q1)
- whether foreman needs new states (waiting on whom, parked) or only a reader on the Nexus side (§6)

## The pain, as seen in foreman today (2026-11-13)

`projects/foreman` has 5 open workstreams:

| Workstream | Report | Really waiting on | What foreman shows |
|---|---|---|---|
| landing | needs-input since Nov 9 | **you**: approach + 8 questions | "stale: close it out or split" |
| ws-control-isolation | needs-input since Nov 4 | **you**: approve the test plan; also built on harness-wrapper's branch | "stale: close it out or split" |
| remote-agents | done | **you**: close-out (its answer is an input to landing) | "stale: close it out or split" |
| gui-spike | done | **you**: close-out | "stale: close it out or split" |
| harness-wrapper | done | agent-lockdown (agentbox `--new-session` fix), another project | last turn summary |

Each one is blocked on a single short action, four of them yours. The nudge is identical for all of them, it doesn't say *what* the action is, and the links between them (landing ← remote-agents, ws-control-isolation ← harness-wrapper, harness-wrapper → agent-lockdown) appear nowhere. That's the pain: **threads linger because their blocker is invisible or too vague to act on, and nothing stops a sixth from starting.**

## 1. The model

```
 SOURCE (yours)                      NEXUS (owns tasks and attention)
 ┌─────────────────────────┐         ┌────────────────────────────────────────┐
 │ foreman ledger          │  read   │ WorkItemRef ──▶ cached snapshot        │
 │ doc in a project        │ ──────▶ │   + attention: active | parked | none  │
 │ Jira/Linear (fixture)   │         │   + done criteria (from source, or      │
 │ ...                     │ ◀ ─ ─ ─ │     drafted here, see Q1)               │
 └─────────────────────────┘ propose │                                        │
        ▲   brokered write,          │ Task  (kind, state, owner, due, deps)  │
        │   you approve each one     │   └── Session (attempts: agent run,    │
        │   (comment / checklist)    │        foreman ws, your workspace)     │
        └────────────────────────────┤                                        │
                                     │ Task with no work item = a todo        │
                                     └────────────────────────────────────────┘
```

### Sources: Nexus reads, never owns

A **source** is an adapter over a system you already use. Interface:

| Call | Returns / does |
|---|---|
| `list(scope)` | refs to the work items in a project (open ones by default) |
| `read(ref)` | snapshot: title, url, status in the source, **scope**, **done criteria** (if the source has them), updated, links |
| `watch(ref)` / poll | change events: status changed, new comment, report arrived, closed |
| `capabilities` | which writes it supports: `comment`, `checklist`, `transition`. Never `create`. |
| `propose(write)` | a **brokered write**: Nexus shows you the exact text and target; it runs only when you approve that one write |

Sources for this machine, and how each yields scope and done criteria:

| Source | A work item is | Scope | Done criteria | Signals |
|---|---|---|---|---|
| **foreman** | a workstream (or a backlog line not yet started) | the brief's *Goal* | the brief's *Done looks like* | report status (working / needs-input / done), closeout ready, stale |
| **doc** | a Markdown file or a heading in one (`docs/design/x.md`, a homelab plan) | its intro or a `Scope` section | a `Done when` checklist, if present | file changed |
| **jira** (fixture) | a ticket | description | acceptance criteria field | status, comments, assignee |
| *(later)* todo app, calendar, email | | | | |

The snapshot is a **read cache**: never edited in Nexus, refreshed on read, labelled with when it was fetched. That's what keeps it from being a parallel copy.

**Foreman is both a source and a session runner.** A workstream with a ticket is a *session* for the ticket's task. A workstream without one (tracker = ledger, as in this project) *is* the work item. Rule: **the work item is the most upstream thing you track in.**

### Tasks: owned by Nexus

| Field | Meaning |
|---|---|
| `title` | a verb phrase: "sign off on the design" |
| `workItem` | a ref, or **none** (a plain todo) |
| `project` | from the work item; for a todo, picked (default **Personal**) |
| `kind` | `agent` · `you` · `gate` · `wait` (below) |
| `status` | `proposed` → `open` → `running` → `done` / `dropped`. Agent tasks also have `needs_you` (asked a question, or a report to review) |
| `owner` | follows from kind: you, an agent profile, or a named other for `wait` ("Alex", "QA") |
| `deps` | tasks (any work item, any project) or a whole work item that must be done first |
| `due` | optional date |
| `covers` | which done criteria this task proves (so progress is measured against criteria) |
| `sessions` | the runs that worked on it |

**The four kinds:**

| Kind | Who acts | Done when | Example (NEX-12 "Jira support for Nexus") |
|---|---|---|---|
| **agent** | an agent, in a session | you accept its report | build the Jira plugin; run the regression suite |
| **you** | you | you mark it done | get a Jira sandbox token |
| **gate** | you, judging evidence | pass. **Fail reopens the task it judges**, with your note as the next brief | sign off on the design; review the plugin diff |
| **wait** | someone or something else | the source signals it, or you say so | live Jira access from IT. Has `on` (who), `check by` (date) and `how we'll know` (source watch / ask you) |

`gate` is separate from `you` because it has a verdict and an object: it points at an artifact and a fail sends work back. That's the "review gate" backlog item: an optional agent pre-check can run *before* a gate reaches you (§5).

### State is stored small; the column is derived

Stored: `status`, `parked` (on the work item), `deps`, `due`. Everything else is computed by one function, so cards can't be dragged into a lie:

```
column(task):
  done | dropped                         → Done
  work item parked                       → (hidden; in the Parked tray)
  any dep not done                       → Blocked   "blocked by <root blocker>"
  kind = wait                            → Waiting   "on Alex, check Thu"
  kind = agent, running                  → Agent running
  kind = agent, needs_you                → Needs you "answer 2 questions" / "review report"
  kind = you|gate, and (it's its item's next action, or due ≤ today)
                                         → Needs you
  otherwise                              → Ready     (can start; nobody is stuck on it yet)
```

**Root blocker.** A blocked card names the first *unblocked* task at the end of its dependency chain, not its immediate dep: "blocked by → *sign off on design* (you)". If the root blocker is yours, it rises in Needs you with "unblocks 3".

### Work item progress

Progress is **done criteria met**, not tasks closed. Tasks are means; criteria are the end. Each criterion shows which tasks cover it; a criterion with no task is flagged ("nothing proves this yet"). The **next action** of a work item is the first not-done, unblocked task in dependency order, preferring one of yours that unblocks agent work.

### Plain todos: tasks with no work item

"Schedule the dentist" is a task with `workItem = none` in a project (default **Personal**). Why this and not a "personal source":
- The constraint exists so you don't keep two trackers in sync. A todo has no other home, so there's nothing to duplicate.
- A personal source would be a Nexus-owned store of work items under another name: the parallel tracker the constraint rules out.
- A todo doesn't need scope or done criteria: its title is its done criterion.

**Promotion:** when a todo grows (you add a second step, a dependency, or ask for decomposition), Nexus says "this looks like a work item; where should it live?" You pick a doc or create it in your tracker yourself, then link it. Nexus doesn't create work items (see Q6).

### Sessions

`WorkItem 1──N Task 1──N Session`. A session is one attempt at one task: a desk task agent, a foreman workstream, or a workspace you open yourself. It's seeded from the task and its work item (§4) and reports back into the task. Several attempts are normal (a gate fails, the agent runs again with your note).

## 2. Views: one task set, many filters

Every view is a filter and a grouping over the same tasks. None stores anything.

### Today (all projects): "what needs me?"

```
 TODAY  Fri Nov 13                                   active 3/3 · agents 2/3
 ─────────────────────────────────────────────────────────────────────────
 NEEDS YOU (4)                                     ordered: unblocks most → due → oldest
  ▸ Answer landing's 8 questions        foreman · landing      unblocks 3 · 4d
  ▸ Sign off on Jira plugin design      NEX-12   · gate        unblocks 2
  ▸ Review: GitLab fit report           Homelab · agent done   new
  ▸ Book Mom's birthday dinner          Personal              due today
 CHECK ON (1)
  ▸ Live Jira access from IT            NEX-12 · waiting since Nov 10, check today
                                        [still waiting · got it · nudge (draft) · drop]
 RUNNING (2)
  ◐ Regression suite (NEX-12)  12 min   ◐ openshell compare (agent-lockdown)
```

### Per project

Work items as rows, each with its progress, its next action and its blockers; the project's todos at the bottom.

### In progress (all projects)

Active work items against the limit, running agents, your `running` tasks. This is where WIP shows.

### Finish line: "what's left for X?"

```
 NEX-12  Jira support for Nexus                     jira · In Progress · active
 Scope: read-only Jira source; tickets → work items; comments as brokered writes.
 DONE WHEN                                   2/4 met
  ✓ plugin lists and reads tickets           ← build plugin
  ✓ comments go through the broker           ← build plugin
  ○ no regressions in desk e2e               ← regression suite (running)
  ○ works against live Jira                  ← test on live Jira (blocked)
 TASKS (as stages: parallel within, sequential across)
  1  ✓ sign off design (gate)
  2  ✓ build plugin (agent)   ✓ get sandbox token (you)
  3  ◐ regression suite (agent)   ⧗ live Jira access (wait: IT, check Fri)
  4  ○ test on live Jira (agent)  blocked by → live Jira access
  5  ○ review & close (gate)
 NEXT ACTION  check on IT's access (waiting 3 days)
 SESSIONS  t7 build plugin (done, report) · t9 regression (running)
```

For a foreman workstream the same view lists its brief's *Done looks like* as the criteria.

### Kanban, shared across projects: columns move themselves

```
 NEEDS YOU      AGENT RUNNING    BLOCKED / WAITING     READY          DONE
 ┌──────────┐   ┌──────────┐     ┌────────────────┐    ┌─────────┐    ┌──────┐
 │landing Qs│   │regression│     │test live Jira  │    │plan the │    │build │
 │ unblocks3│   │ NEX-12   │     │ ← IT access    │    │ rest    │    │plugin│
 └──────────┘   └──────────┘     └────────────────┘    └─────────┘    └──────┘
```

- Cards move only because their state changed: an agent finishes → its card slides from *Agent running* to *Needs you* ("review report"), and when you accept, its dependents slide from *Blocked* to *Ready*. Each move is an event in the log, with a short toast.
- **No dragging.** Every card has its verbs instead (start, review, pass/fail, park, done, drop). Test 2 (direct action) holds: you can change anything, and the column follows.
- Swimlanes by work item are optional; the default is flat, sorted by the same order as Today.

## 3. Keeping threads few *(sketch)*

| Mechanism | Pain it answers |
|---|---|
| **WIP limit on active work items** (default 3, all projects). Starting a 4th asks you to park, close or swap one first. | too many in parallel |
| **Root-blocker surfacing**: blocked cards name the action that unblocks them; yours rise in Needs you with "unblocks N" | linger because of blockers |
| **Every wait has a check-by date**; on that date it shows in *Check on* with "still waiting / got it / draft a nudge / drop" | linger because of blockers (external) |
| **Specific stale nudges**: "landing has waited 4 days on *your* 8 answers" instead of "close it out or split" | linger; unclear where input is needed |
| **One next action per work item**, always shown | unclear what's left |
| **Close-out nudge** when all done criteria are met but the item is still open | done work lingers (4 of foreman's 5) |
| **Decompose the next 3 only**; the rest is one "plan the rest" task (B4) | backlog blow-up |

## 4–7 *(after the check-in)*

4. Start a session from a task: the seed and the report-back. 5. How this relates to desk and the backlog. 6. Foreman implications. 7. Open questions.

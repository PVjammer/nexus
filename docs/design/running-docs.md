# Running docs and nightly jobs: what's missing, and the blocks that enable them

*Workstream running-docs, 2026-11-20. Decisions D1–D4 recorded by the user the same day. Code refs are in `prototypes/desk` as of `f546432`.*

**Question answered:** What does Nexus need so that users can build running docs and scheduled prep jobs themselves, out of general building blocks, and how does that fit the settled design?

## Verdict

- **The "run" half already exists.** Desk has ephemeral, read-only task agents that work from a brief and write versioned reports. It also has a wiki with source-based staleness and proposals, Needs you, the journal and action log, "since your last turn" diffs, and project-owned instruction files that override a default skill (`closeout.md`).
- **The "when" and "where to" halves are missing:**
  - **No clock:** nothing in the core acts on time. The two loops are hard-coded pollers.
  - **No saved spec you can re-run:** a Task is one brief, run once.
  - **No stable output:** reports are named after the task, so nothing is "the weekly status doc".
  - **No delivery policy.**
  - **No unattended credentials:** vault handles live in memory, and task agents can't call the broker.
- **The smallest set:** a **workflow spec** (the backlog item, in the SKILL.md format) with three optional fields: **`on:`** (trigger), **`output:`** (a versioned doc slot that knows when it's stale) and **`delivery:`**. Each run is an existing task agent with an unattended, propose-only profile.
  - A running doc is a workflow whose output is a doc slot.
  - Nightly prep is a workflow whose output is the morning view.
  - No new noun is needed.

## 1. The building blocks

| Block | What desk has now | What's missing |
|---|---|---|
| **Trigger** (schedule, source changed, event, on demand) | <ul><li>**On demand:** the Start press (`internal/app/chat.go:399`).</li><li>**Hard-coded loops:** project poll every 8 s (`internal/app/app.go:227`) and code-graph sync every 2 min (`internal/app/addons.go:240`).</li><li>**Change detection:** project details hashed (`app.go:233-256`); repo snapshot and diff (`internal/app/changes.go:76,206`); wiki source mtimes (`internal/app/tools_wiki.go:205-224`).</li><li>Add-ons already have a `Kind: "automation"` with a per-project switch (`addons.go:27,42`).</li></ul> | <ul><li>A scheduler: jobs that persist, the next run time, catch-up after sleep, pause, a global pause.</li><li>A general "source changed" trigger with debounce.</li><li>An internal event bus: `notifyTask` only broadcasts a toast to the UI (`chat.go:462-473`).</li><li>Clock-driven logic: due dates are stored (`internal/desk/desk.go:89`) but nothing fires on them. Only `private.go:152` counts due-soon tasks.</li></ul> |
| **Sources** | <ul><li>Project folders, read-only (`internal/app/tools.go:515`).</li><li>Foreman ledger (`internal/projects/projects.go:230-246,413`).</li><li>Work items and tasks, whose move (`internal/app/work.go:66,148-174`).</li><li>`needs()` (`internal/app/dashboard.go:65`).</li><li>Wiki, reports and the action log.</li><li>The repo since the last mark (`changes.go:107`).</li><li>Broker connections, a mock only (`internal/broker/broker.go:17-37`).</li><li>The private-project wall (`internal/app/private.go:31`).</li></ul> | <ul><li>**Sources declared on the spec.** The run's read scope narrows to them, and they feed staleness and change triggers.</li><li>**External sources:** calendar (needed for "tomorrow's meetings"), email, Jira. Web access is on the backlog.</li><li>**Fingerprints**, so a run can be skipped when nothing changed.</li></ul> |
| **Spec / template** | <ul><li>SKILL.md loader with variables generated from the code (`internal/app/skills.go:18-40`).</li><li>A project-owned file overrides the default skill (`internal/app/closeout.go:165-174`). This is exactly the pattern needed.</li><li>Task brief (`desk.go:46-60`).</li></ul> | <ul><li>A workflow format (backlog 2026-11-13).</li><li>Parameters.</li><li>An output template (the doc's sections).</li><li>`on` / `output` / `delivery` fields.</li></ul> |
| **Agent run, scoped profile** | <ul><li>`KindTask`: ephemeral and read-only. It has `plan_set`, `report_write` and `task_finish` (`tools.go:141-145`), and `wiki_propose`.</li><li>Run kinds (`internal/app/agents.go:19-25,122-160`) and tools per kind (`tools.go:272`).</li><li>Loop and model per kind (`cmd/nexus-core/main.go:254-266`).</li><li>Private → local model only (`agents.go:40-43,225`).</li></ul> | <ul><li>**A profile per workflow:** tool subset, model, turn and token cap, timeout.</li><li>**Starting without a press:** only a `draft`, `stopped` or `failed` task can start (`chat.go:404`).</li><li>**Unattended mode:** a turn that ends without `task_finish` becomes `needs-input` (`chat.go:436-446`). With nobody there, it should record the question instead.</li></ul> |
| **Output** | <ul><li>Report versions: never overwritten, earlier ones kept (`tools.go:875-909`).</li><li>Wiki pages with history and proposals (`internal/wiki/wiki.go:193,241`).</li><li>Review threads on docs (`internal/review/review.go:132`).</li><li>Widget catalog (`internal/app/home.go:31-44`) and plugin widgets.</li><li>Work proposals (`work_propose`).</li></ul> | <ul><li>**An output slot:** one doc with a fixed identity (`desk/<project>/docs/weekly-status.md`) whose versions are runs. Report paths carry the task id.</li><li>**A "doc" widget kind**, to keep a running doc on Home.</li><li>**Draft outputs** (email). There are no outward channels at all.</li></ul> |
| **Diff from last version** | <ul><li>Versions are kept but never compared.</li><li>The agent-side diff (`changes.go:107`).</li><li>The customizer's draft diff (`internal/app/customizer.go:71`).</li><li>Wiki proposals are shown as a diff.</li></ul> | <ul><li>A version diff per slot.</li><li>**Your read marker**, so it can show "changed since you last read".</li><li>The previous version passed in as input, so the run edits it rather than regenerating it. That keeps the doc stable and costs less.</li></ul> |
| **Delivery / notification** | <ul><li>Needs you, which is pull (`dashboard.go:65`).</li><li>Running (`dashboard.go:106`).</li><li>The in-app toast (`chat.go:472`).</li><li>The top-bar count.</li><li>`notify` as a node capability (`docs/design/frontends.md`).</li></ul> | <ul><li>A policy per workflow: silent, badge, needs-you or notify.</li><li>An interrupt budget, quiet hours, a morning bundle.</li><li>Outward delivery as a draft you press to send.</li></ul> |
| **Run record / journal** | <ul><li>Action log entries (`internal/actionlog/actionlog.go:13-21`).</li><li>The Journal rail merges foreman events, the log and tasks (`web/src/main.ts:644-652`).</li></ul> | <ul><li>"Why did this fire": the trigger, an input fingerprint, model, tokens, duration, and the result or "nothing new".</li><li>Run history per workflow.</li><li>An inventory of everything scheduled.</li></ul> |
| **Unattended secrets** | <ul><li>Secure entry returns a `secret:…` handle (`internal/vault/vault.go:108-155`).</li><li>The broker attaches credentials (`tools.go:246`).</li></ul> | <ul><li>Handles live in memory only (`vault.go:8-10`), so they're gone after a restart.</li><li>`broker_call` is home and project only (`Kinds: HP`), not task.</li><li>No grant tied to a job, a connection and a read-only path list.</li></ul> |
| **An awake core** | One local binary that runs when the app runs. | Jobs that can wait for a later start ("ready by"), and catch-up after sleep. See placement below. |

## 2. Fit with existing plans

**Testing "running doc = workflow + trigger + output":** it holds, with two amendments.

- **Workflows** (backlog 2026-11-13: command + agent profile + skill). This is the spec. A workflow with no `on:` is a command, so the same file runs on demand or on a schedule. That matches GitHub Actions' `on:` plus `workflow_dispatch`.
  - **Amendment 1: the output slot is its own block.** A workflow run produces a report. A running doc is one doc that runs update, and it carries a freshness contract (`stale_after`). That contract belongs to the output, not the workflow.
  - **Amendment 2: sources must be declared in the spec.** They narrow the run's scope (grants only narrow down the tiers), they drive change triggers and staleness, and they let the core apply the private wall before the run starts.
- **Triggers and suggestions** (backlog 2026-11-17) is the same trigger primitive. Its rules carry over unchanged:
  - triggers are data, composed by you and the agent;
  - runs read and propose only;
  - outward actions are confirmed presses;
  - the interrupt budget applies;
  - every trigger is visible, pausable and logged;
  - the skill works on small models.

  The task-mgmt nudges are tiny workflows whose output is a needs-you proposal: "idle 5 days → draft a nudge" (`prototypes/tasks-mock/v2/data.js:209`), and the SEC-431 watcher (`data.js:66`).
- **Web access** (backlog) is a source type. Its rules apply as written to unattended runs: allowlisted fetches, text marked untrusted, snapshots for citations, a journal entry per fetch. Untrusted text plus nobody present is the strongest reason to keep runs propose-only. Snapshots give a running doc its citations.
- **Task-mgmt (fm/task-mgmt):**
  - **"Needs me"** is the content of nightly prep: `needs()`, whose move (`work.go:148-174`), due dates, blocked tasks that became ready.
  - **Focus** is the order of the morning view.
  - **Nudge** is a draft output.
  - Its **open question 1** ("should agent tasks start on their own?", `work-items-and-tasks.md`, Open questions) is the same decision as tension A. Decide it once.
- **Plans for work items** (backlog): a workflow can be a plan template. A `gate` step is the press that sends a draft.
- **Guardrails as code** (backlog 2026-11-18): a job's scope ("may write only its slot; may read only these sources") can be compiled into core-enforced rules at the one choke point. A small model then can't overstep, and a user rule can only tighten.
- **The scheduler in the core** (`where-we-are.md` § Architecture): the core owns the scheduler, the jobs and the run records. Each run goes to an agent runtime picked by placement and trust. The scheduler lives in whichever core instance is always on.

## 3. Tensions with the settled design

### A. "Standing agents and pre-approved autonomy levels" are withdrawn

**Stance:** a scheduled workflow isn't a standing agent. It is a **standing order you wrote**, carried out each time by a fresh ephemeral task agent. These rules keep it inside the design, and each is security-motivated, in line with "boundaries only for security":

1. **Your spec, your press.** Agents draft specs. Only your **Keep** turns a trigger on (the Eitri pattern, `customizer.go:154`). An agent never enables, widens or re-times a job.
2. **Ephemeral run.** Each run is a fresh session. Its only memory is its own last output, plus a domain note if the spec declares one (the sensitive-agent pattern, `sensitive-agents.md` §5).
3. **Narrow, fixed scope.** It reads only the declared sources. It writes only **its own output slot** (Nexus-owned and versioned, like the project agent's direct wiki edits). Anything else is a proposal: tasks, work items, wiki pages.
4. **Propose-only outward.** There's no `term_propose`, no broker writes, no sending. Email and posts are drafts.
5. **No asking when nobody's there.** A question goes into the doc ("Open question for you: …") and into Needs you. The next run reads your answer.
6. **Everything in the journal**, with "why did this fire". Every job sits in one visible list with a pause, plus a global pause.
7. **Capped:** turns, tokens and time per run.

**Decided by the user (2026-11-20):**
- **D1. A run writes its own slot directly: yes.** The slot is versioned and Nexus-owned, and it's never your list, so a refresh needs no press.
- **D2. A run may start other runs, if that is deterministically configured: yes.** Chaining is part of the spec you kept, for example `then: [meeting-briefs]`, or a trigger `after: nightly-prep`. The core starts each run that is next in the chain, and each one runs under its own spec, scope and caps. **An agent deciding to start a run is not allowed yet.** If a run thinks more work is needed, it proposes a task or job, and you start it.
- **D3. Nothing beyond propose-only: no.** Nothing auto-sends a status email or auto-closes stale items. Outward actions and changes to your lists stay a press each time.
- **D4. Task-mgmt's open question 1 is settled by D1 and D2:**
  - work starts on its own only inside its own output and scope;
  - one run starts another only through a configured chain;
  - an agent never decides to start a run.

### B. "Pull in, push rarely" (the interrupt budget)

- **The default delivery is `silent`.** The doc updates in place, with a "new since you read" dot on the doc and its widget. A running doc is pull by nature.
- **`needs-you` only when a decision is waiting:** proposals, a draft to send, a question. Nightly outputs **bundle into one "Morning" item**, at most one count in the top bar.
- **Deliver on arrival, not at run time.** Nightly prep shows when you first open Nexus (attention or session start, `frontends.md`), not as a 2 AM toast.
- **`notify` (OS push) is opt-in per job,** within a global budget (for example 2 a day) and outside quiet hours. Recommended only for watchers you mark urgent.
- **Failures stay quiet.** They show on the output itself (the stale banner) and in the job list. They reach Needs you only for jobs set to `needs-you`, after the retry fails.

### C. "Stays local" and the security invariants

- **Outputs are Nexus-owned docs in the data dir.** Nothing is published to third-party doc services (`where-we-are.md`: no Claude Docs), and nothing is written to project folders (AGENTS.md).
- **Sending is yours.** A status email is a draft in Needs you with **Send / Copy / Open in mail**, like `term_propose`: you press it every time. Until there's a mail integration, "Copy" or `mailto:` is enough, and it still avoids copy-paste work.
- **Secrets reach a job without the agent:**
  - At setup, while you're present, secure entry stores a credential in the vault **persistently**, as a vault item, not an in-memory handle.
  - It's bound to a **grant**: job + connection + allowlisted read-only paths.
  - The run gets a `source_read`-style tool. The broker attaches the credential, and the agent sees only data. Secure tools keep returning `DATA_DELIVERED_OUT_OF_BAND`.
  - If the vault is locked (the PA §8/§12 node/lock boundary), the run doesn't fail open. It waits in the deferred **"needs your machine / unlock"** queue, and the doc shows "waiting for unlock".
- **Private projects:** a job that reads a private project runs on a local model only (`agents.go:225`). Its output stays inside the wall. A cross-project digest shows only counts and due days, as `private.go` already does.
- **Sensitive domains** (email, finance): the job *is* a sensitive agent, with no way out. Its output is pings and draft todos only.
- **Tests:** extend `make e2e` with a scheduled run over the canary `.env` and a canary broker key. The leak check must stay green.

### D. Placement (configuration, not design)

| Placement of the scheduler | Implies |
|---|---|
| **Desktop core** (today) | Runs only when the machine is awake and the core is up. So specs say **`ready_by: 08:00`**, not `at: 02:00`: the core runs the job as soon as it can before the deadline, or on first wake (anacron / systemd `Persistent=`). A local GPU competes with you, so prefer idle time. |
| **Homelab core** | On time, every time, and the Spark serves models. Repos on the desktop are reached **through the node**. When the desktop is off, a run either waits ("needs your machine") or runs on what it can reach and marks the doc **partial: repo X unavailable**. |
| **Provider / cloud** | Always on. The trust model limits sources and models: no private projects, no trusted-only domains. |
| **Split** (recommended shape) | The scheduler sits in the always-on core. Each run is routed to a runtime that satisfies its sources' reachability and its trust requirement. The job list shows "runs on homelab · needs desktop for nexus". |

## 4. What else the same blocks enable (ranked by value to you)

1. **Morning prep: "what needs me today/tomorrow".** Due and overdue tasks, blockers cleared overnight, whose move, finished reports, stale items. Internal sources only, so it ships first.
2. **Weekly review of open work.** Idle or stale work items with **close / park / split** proposals as a checklist. It addresses the five lingering workstreams in `work-items-and-tasks.md`.
3. **A running status doc per project** (weekly or daily), with an email draft.
4. **Source watchers → capture or proposals.** The SEC-431 pattern: a new ticket comment becomes two proposed tasks. Very high value for work, but it waits on Jira/GitHub sources.
5. **Wiki freshness.** When wiki-check flags a page stale (`tools_wiki.go:205`), a run proposes the update. It's cheap and almost all built.
6. **Meeting prep briefs.** High value, but blocked on a calendar source.
7. **End-of-day handoff.** One "where I left off" note per workstream, from "since your last turn" and the transcript, triggered at a time or when idle. It makes resuming fast.
8. **Deadline and idle nudges:** "due in 3 days, not started" and "IT-882 idle 5 days → draft a nudge".
9. **Refreshing digest widgets:** plugin widgets (activity, homelab disks through the broker) on a schedule instead of on open.
10. **Re-checking parked items:** "bring back when X merges / after date".
11. **Personal recurring docs:** trip countdown checklists, renovation weekly digest, gift dates.
12. *(Dev)* A nightly `make eval-home` against the local model, with a running doc of skill pass rates.

## 5. How to implement it well

### Spec format: the workflow file

It's a SKILL.md (agentskills.io frontmatter plus body), with a `nexus:` block. It's stored in the data dir: global in `workflows/`, per project in `desk/<project>/workflows/` (the `closeout.md` precedent). Shipped defaults are skills. Schedules use **fixed phrases** (`hourly`, `daily 07:00`, `weekdays 17:30`, `weekly fri 15:00`), not cron, so small models get them right.

**Weekly status report**

```markdown
---
name: weekly-status
description: Nexus weekly status, always current; a draft email on Fridays.
nexus:
  scope: project:nexus
  sources: [work, journal:7d, repo, reports:since-last]
  on:
    schedule: weekly fri 15:00
    changed: [work, repo]        # also refresh, at most every 6h
    ready_by: fri 16:00
  output:
    doc: status/weekly.md        # desk/nexus/status/weekly.md, versioned
    widget: home                 # optional "doc" widget on Home
    draft: email                 # Fridays: a draft in Needs you, never sent
  delivery: silent               # the draft alone reaches Needs you
  stale_after: 8d
  profile: { model: task, max_turns: 25, max_tokens: 150k }
  may_propose: [tasks]
---
# Weekly status: {{project}}, week of {{week}}

Edit the previous version; don't rewrite it. Keep each section ≤5 bullets.

## Done this week
## In progress (whose move, next step)
## Blocked / waiting on
## Next week
## What changed since last version   <!-- one line, for the reader -->
```

**Nightly prep**

```markdown
---
name: nightly-prep
description: What needs me tomorrow, across every project.
nexus:
  scope: home
  sources: [needs, work:all, due:2d, reports:since-last, journal:1d]
  on:
    schedule: daily 02:00
    ready_by: 07:30              # desktop asleep → runs on first wake
  output:
    doc: home/tomorrow.md
    widget: home
  delivery: morning              # one bundled item on arrival, no push
  stale_after: 20h
  profile: { model: local, max_turns: 20 }
  may_propose: [tasks, park, close]   # a checklist you tick
  then: [meeting-briefs]       # D2: a configured chain the core runs next;
                               # this agent never decides to start a run
---
# Tomorrow, {{date}}

1. **Needs me first:** at most 5, ordered by due date, then whose move.
2. **Became ready overnight:** blockers that cleared.
3. **Finished while you were away:** reports to review.
4. **Stale:** idle more than 7 days. Propose close, park or split. Never do it.
5. **Meetings:** *(once a calendar source exists)*
```

### Creating one by talking

You say "Every Friday, keep a status report for Nexus I can paste into an email". Then:

1. The project agent (or the home agent, for cross-project jobs) uses a **`workflows` skill** with one tool per action: `workflow_draft`, `workflow_try`, `workflow_keep`, `workflow_pause` and `workflow_list`.
2. It asks **one question per turn** with answers to click (which day? silent or Needs you?), as `ask_user` already does.
3. It runs **Try it now**: a dry run into a draft version.
4. It shows the doc, the next five run times in plain words, the sources and the cost of that run.
5. **Keep** turns it on.

As with every skill: a worked example per common request, errors that say how to fix the call, `TestSkillExamples`, and new `eval-home` cases ("a plain question about the schedule changes nothing").

### Versions and "what changed"

- **The slot:** `slot.json` holds the versions, their run ids, the source fingerprints, your read marker and the last good version.
- **Never overwrite** (the `report_write` pattern, `tools.go:883-896`).
- **Each run gets the previous version as input** and edits it.
- **The core computes the diff.** The UI highlights "changed since you last read". The agent's one-line "what changed" is the run summary.
- **Skip if unchanged:** when the source fingerprints match the last run, log "checked: nothing new" (the mock's watcher log) and spend nothing.

### Failure and staleness

- **Freshness is computed by the core,** not claimed by the agent: now minus the last good run, against `stale_after`.
- **A stale doc says so at the top:** "**Stale:** last updated Tue 15:02. Friday's run failed (model endpoint unreachable). **Run now**".
- **Partial runs say what was missing.**
- **A failed run never replaces the last good version.** It retries once with backoff, then waits.

### Cost and model choice

- **Per-job `profile`:** a role (`task`, `local`) routed by trust, plus turn and token caps.
- **Refresh-type jobs default to the local model;** synthesis can use a bigger one.
- **Every run logs model, tokens and time.** The job list shows a monthly total per job.
- **Skipping unchanged runs** is the biggest saver.

### Testing

- **Run now:** the same path as the schedule.
- **Dry run:** writes a scratch version, moves no markers and delivers nothing.
- **Preview schedule** in plain words.
- **"Why did this fire"** in the journal.
- **For the core:** a fake clock (`-test-hooks`) so e2e can fire schedules, and the leak check over a scheduled run.

### Build order

| # | Block | Size | Unblocks |
|---|---|---|---|
| 1 | Workflow spec loader + **Run now** through the existing task path (`From: "workflow:<name>"`) | S | Workflows backlog item; manual running docs |
| 2 | **Output slot:** stable path, versions, read marker, diff view, stale banner, previous version as input, "doc" widget kind | M | Running docs you refresh by hand |
| 3 | **Scheduler in the core:** persisted jobs, `ready_by` and catch-up, pause and global pause, fake clock; unattended profile (no ask, caps); journal "why fired"; configured chains (`then:` / `after:`) with a cycle check when you press Keep | M | **Nightly prep and weekly status (internal sources)** |
| 4 | **Delivery policy:** silent, badge, needs-you, morning bundle; opt-in notify with a budget | S | Respecting the interrupt budget |
| 5 | **`workflows` skill + tools + eval cases;** Try / Keep | S | Creating jobs by talking, on small models |
| 6 | **Change triggers + skip-if-unchanged** (generalize the existing hashes and snapshots) | M | Always-current docs at low cost; wiki freshness |
| 7 | **Draft outputs** (email draft with Copy / `mailto` / Send press) | S–M | Status emails, nudges |
| 8 | **Unattended credentials:** persistent vault items, job-bound read-only grants, broker read for the task kind, lock behaviour | L | External sources |
| 9 | **External sources:** calendar, Jira/GitHub, web access | L each | Meeting prep, watchers |
| 10 | **Placement:** scheduler on an always-on core, runs routed through nodes | L | On-time nightly runs (part of the core split) |

Steps 1 to 5 deliver both of your examples on internal sources.

## 6. Lessons from elsewhere

*From general knowledge; no fresh survey was done in this workstream.*

- **cron, anacron and systemd timers.** Borrow: catch-up after sleep (`Persistent=true`), jitter, and an inventory of timers. Avoid: silent failure and output nobody reads.
- **GitHub Actions `schedule` + `workflow_dispatch`, and Claude Code routines.** Borrow: the spec is a file, the same file runs on demand or on a schedule, and the run history has logs. Avoid: scheduled agents that open PRs or post comments without asking, cost you can't see, and cloud runs that need your repo on a git server.
- **Zapier and n8n.** Borrow: trigger → steps as data, per-run history with inputs and outputs, "test this step" with sample data. Avoid: sprawl of forgotten automations (show "last useful run", offer "nothing in 30 days: remove?"), brittle chains, and outward actions without review.
- **Notion AI autofill, Linear project updates and Pulse.** Borrow: Linear has the human post the update, with a reminder and a prefilled draft. That's our "agent drafts, you send". Pulse is a pull digest. Avoid: AI-filled content that doesn't show it's generated or stale. Always show "generated · as of".
- **Daily digest emails and "morning briefs".** These get ignored when pushed. Show them when you arrive.
- **Dependabot and Renovate.** Group and schedule updates to cut noise. Same idea as the morning bundle.

## Follow-ups (not fixed)

- Vault handles live in memory only (`vault.go:8-10`), so secrets entered for a job are lost after a restart. It's fine for the prototype but blocks unattended runs.
- Task agents can't use the broker (`tools.go:246`, `Kinds: HP`). Check this is intended, rather than an oversight, before adding job grants.
- `taskTurnEnded` (`chat.go:436-446`) assumes someone is there to answer. Unattended runs need a different end state.
- `notifyTask` is a UI broadcast, not an event. Triggers like "task finished" need an internal event bus.
- Naming: "workflow" is on the backlog, and "routine" is Claude Code's term. Keep "workflow" plus "running doc" to avoid confusion.

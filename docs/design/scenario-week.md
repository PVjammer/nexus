# Scenario: one week, start to finish

*2026-11-05, workstream pa-extensible-ui. A hypothetical walkthrough to find where the ideal app works and where it breaks. It is not a v1 plan.*

## Framing updates from the user (2026-11-05)

- **Native desktop app**, not a browser tab. The browser pane is for *following along*: the agent's links open right there, and tabs are grouped by context. It doesn't need your logins or your extensions.
- **Layout is "herdr/tmux in UI form", tabbed like VS Code.** Splits hold groups of tabs. Example arrangement: chat or terminal on the left, and the right is one of ~4 apps (tabbed, or picked by the agent). Switching to editor + browser should be quick, and you shouldn't have to keep rearranging.
- **Not only a project-management tool.** It should also cover the homelab, a vacation, Christmas shopping, research, a renovation, and home and family todos. Some of that data is sensitive. Don't overtune it for software projects.
- **Positioning:** it brings AI and apps closer together while you keep using both. *Not OpenClaw* (an autonomous agent doing your life for you), *not Clippy* (AI shoved into every app).

## The general model this scenario assumes

To avoid overtuning for projects, the core objects are generic:

- **Space:** any context. Examples: a software project, the homelab, "Spring trip", "Bathroom reno", "Christmas 2026", "Home". A space has knowledge (a brief, links, people, decisions), items, tabs, layouts, and a **visibility default** (e.g. `Home` = local model only).
- **Item:** a note, task, or question. An item lives in one space and can be **linked** into others. Linking is not moving. Each item has a state: `inbox → proposed → active → waiting(on whom) → done | someday`.
- **Apps:** surfaces that open as tabs: chat, terminal, browser, editor/Markdown, diff/review, table, diagram, plus extensions. Software-project features (workers, MR review) are apps a space enables. They are not part of the core model.
- **Layout:** a split tree of tab groups. A space remembers its arrangement. One group can be marked as the **agent target**, where the agent opens links and results.
- **Agent:** pairs in whatever space you're in. Its model access follows that space's visibility.

## Cast of spaces (personal instance)

| Space | Kind | Visibility default | Apps enabled |
|---|---|---|---|
| **Orchestrator** (the agent-orchestrator / Nexus idea) | big software project | shared (remote model OK) | terminal, editor, diff, workers, browser, diagrams |
| **Homelab** | infra project | shared, but secrets on the data plane | terminal (via node, L1), browser, table |
| **Spring trip** | life plan | shared | browser, table, map/doc |
| **Bathroom reno** | life project | **local model only** (money, contractor PII) | table, docs/PDF, browser |
| **Christmas 2026** | life list | mixed: per-person lists; **partner's gifts hidden** | table, browser |
| **Home** (family todos) | ongoing | local model only | list |
| **Research** | ad hoc | shared | browser, doc |

---

## Monday: the dump

You open the scratch notepad (global hotkey) and paste a week's backlog of notes. Here are 18 lines, verbatim in your voice:

```
1  orchestrator: workers should be able to pause and hand back to me mid-task
2  orchestrator: roster of what each worker knows (mycelium-ish) so the lead isn't blind
3  orchestrator: rank the inbox by urgency, not arrival
4  PA core needs a home on the homelab - compose or k3s?
5  GPU box for local models - 2x used 3090 vs one new card? noise matters
6  vault backups aren't verified. restore test!!
7  Mom's bday Dec 12 - book dinner
8  kids xmas ideas - ask what they're into lately
9  partner wants a stand mixer (DON'T put on the shared list)
10 spring break somewhere warm, kid friendly, ~4k all in
11 check passports before booking anything international
12 bathroom: get 3 quotes, tile options, timeline before summer
13 garage door is grinding
14 are heat pumps worth it in our climate?
15 that thing from the podcast about agent memory - find it
16 work: on-call summary bot for the team
17 orchestrator demo for the meetup in Jan?
18 cancel the streaming service we never use
```

### What the app does
1. **The capture is saved instantly** as one scratch note (write now, file later).
2. **The agent proposes a filing for each line**, using space knowledge, in a triage table that opens in the right-hand group:

| # | Proposed | Why | Your action |
|---|---|---|---|
| 1–3 | Orchestrator, as **ideas**, not tasks | design-level, needs thinking | accept |
| 4 | Homelab, **linked** to PA | infra decision that blocks PA hosting | accept the link |
| 5 | Homelab | hardware decision; also touches budget | accept, ⚠ see B3 |
| 6 | Homelab, **task**, priority high | concrete, risky if ignored | accept |
| 7 | Home, task, due Dec 1 (book ahead) | dated | accept |
| 8 | Christmas, task "ask kids" | | accept |
| 9 | Christmas, **private to you** | your note says "don't share" | ⚠ B2: it has to get this right |
| 10–11 | Spring trip (new space?) | no space exists yet | accept "create space" |
| 12 | Bathroom reno | | accept |
| 13 | Home | | accept |
| 14 | Research, linked to Bathroom reno? | uncertain | ⚠ B1: you change it to Research only |
| 15 | Research | too vague to act on | ⚠ B5: stays in the inbox with a question |
| 16 | **Work**, which is a different instance | | ⚠ B10: crosses an instance boundary |
| 17 | Orchestrator, a milestone with a date | creates a deadline that pulls in 1–3 | accept |
| 18 | Home, a 2-minute task | | do it now: the agent opens the cancel page in the browser tab, and you click |

3. **No decomposition yet.** The agent offers it for #6, #10 and #12, and suggests *brainstorm first* for #1–3 (pair, don't delegate).

**Layout:** chat on the left; the triage table on the right, with a tab for the raw scratch note.

**Tools touched:** today that's notes app → mental sorting → Todoist/Jira/whatever → calendar. In the app it's one table. *This is where the app wins clearly.*

---

## Tuesday: think and research

### Orchestrator brainstorm (#1–3, #17)
- **Layout:** you switch to the Orchestrator space and its layout comes back: chat on the left; right group tabs `design.md` (editor), `diagram`, `browser`.
- You and the agent talk through "pause and hand back". The agent drafts options as a **comparison table plus a sequence diagram**. You edit the doc directly while it watches, and it updates the diagram when you change the design. (Two seats on one doc.)
- You ask it to check how herdr, Claude Code and OpenAI's agents handle resume. It opens 4 docs pages as tabs in the right group and returns **quotes with links**. You click through two of them to confirm. *Following along works.*
- **Outcome:** 2 decisions written down as yours; #1 is decomposed into 5 tasks (accepted 4, deferred 1 to `someday`); #2 is linked to a cross-project item ("mycelium read API"); #3 is merged into #1's design.
- ⚠ **B4:** the agent proposed 11 tasks for #1 first. Keeping the backlog small has to be a design rule, not something you police.

### GPU research (#5)
- Homelab space: the agent researches, and the browser tabs (reviews, noise measurements, listings) **stay in the Homelab space**. The result is a table: options × price × noise × VRAM × power, with a source on every cell.
- Used listings are on marketplace sites you're logged in to (to see prices or message sellers). The agent's browser has no logins, so you open those in your real browser. ⚠ **B6.**
- ⚠ **B3:** the decision depends on money (the Christmas budget, the reno). That's budget data from a local-only space affecting a shared space.

### Heat pump research (#14)
Research space: a short report pane with sources. It's done in 20 minutes and filed. Cheap, and a clear win.

---

## Wednesday: do things

### Orchestrator: pair on code
- **Layout:** you switch to *editor + terminal* (a saved arrangement: editor on the left, terminal and diff stacked on the right).
- A worker (coding agent) runs in the background in its own sandbox. Its terminal is a tab, so you can **follow along** or ignore it. A badge shows when it needs you.
- It finishes, and the diff opens in the review tab. You review it together: it annotates, you comment or edit, and you accept 3 of 4 hunks.
- The worker hits a limit: the sandbox it runs in (agent-lockdown) needs a change. **A cross-project follow-up is created and linked** (B7: it leaves this space).

### Homelab: restore test (#6)
- The terminal runs through the homelab **node at L1**. The agent can read backup logs and propose commands; you run the destructive ones. A restore to a scratch dir succeeds, with one gap: `.config` is excluded.
- **Follow-up item:** fix the backup include list. Linked to the PA space, because the PA's store will live here.
- Secrets (the backup encryption key) are on the data plane: the agent sees "key present ✓", never the value. *This is the security model doing its job without friction.*

### Bathroom reno (#12)
- Three contractor quote PDFs arrive by **email**. You drag them in (or a mail integration pulls them). Local model only: the agent extracts line items into a comparison table and flags that quote B leaves out demolition.
- The tile options are in vendors' browser tabs inside this space.
- ⚠ **B8:** contacting contractors, scheduling visits and paying a deposit all happen in email, a phone call and a bank. The app only *follows* those steps. You have to report outcomes back ("visit booked Thu 2pm"), and if that takes effort, the space goes stale.

---

## Thursday: life logistics

### Spring trip (#10, #11)
- The agent researches 3 destinations against your constraints (warm, kid friendly, $4k). Result: a table plus a map/doc tab, with links open in the space.
- **#11 blocks booking:** the agent asks for passport expiry dates. That's sensitive. Where do the values live? → the data plane: you type them into a form pane, and the agent only learns "passport 2 expires in 5 months: renew first". A new task appears, with a dependency.
- ⚠ **B9:** **calendar conflict.** The contractor's proposed timeline overlaps spring break. A dependency across spaces (reno ↔ trip) has to show up *somewhere*, or it gets missed.
- Booking happens on airline and hotel sites in your real browser. The confirmation needs to come back into the space (B8 again).

### Christmas (#8, #9)
- You're in a store on Saturday and want the list. **It's a desktop app.** ⚠ **B11: there's no mobile access.** The life spaces need at least read and capture on a phone.
- The shared family list (the kids' ideas, which you might share with your partner) **must not include the stand mixer**. Visibility per item, not just per space. ⚠ B2.
- Price tracking for 5 items: a background job, with a nudge when the price drops below your target (within the interrupt budget).

---

## Friday: review and fallout

### Weekly review
The agent builds a review from the event log:
- done: 9
- active: 7
- waiting: 3 (contractor, kids' answers, the passport form)
- new follow-ups: 6
- stale: 1 (#15, the podcast)

One screen, per space, then rolled up. **This is the part that most directly helps with ADHD.**

### Ripples across spaces (generated this week)
```
Orchestrator ──needs──▶ agent-lockdown sandbox change   (other project)
Orchestrator ──needs──▶ mycelium read API                (other project)
Homelab ──blocks──▶ PA core hosting                       (#4 decision still open)
Homelab GPU ──competes with──▶ Christmas + reno budget    (local-only money)
Reno timeline ──conflicts──▶ Spring trip dates
Restore test ──creates──▶ backup include fix ──protects──▶ PA store
Work idea #16 ──exported to──▶ work instance (by hand)
```

### Backlog health
- Proposed 31, accepted 19, someday 8, rejected 4. Without a cap and the `someday` bucket, the backlog would have grown about 50% in one week (B4).

---

## Where it works

1. **Triage of a messy dump into spaces.** One table replaces several apps and the mental sorting. It's the biggest single win.
2. **Follow-along research.** Sources open as tabs in the space, answers come with quotes, and nothing piles up in your main browser.
3. **Resuming context.** Switching spaces brings back the layout, the tabs and the thread. This answers the context-switching pain directly.
4. **Pairing on docs and code.** The agent drafts and you decide; two seats on a doc; diff review together.
5. **Weekly review from the event log.** This comes free once everything goes through one place.
6. **Sensitive data on the data plane** (backup keys, passports, money) causes little friction in practice.

## Where it breaks (and what each break implies)

| # | Break | Design implication |
|---|---|---|
| **B1** | Items span spaces (heat pump → research and reno?) | Items **link** across spaces instead of being copied or moved. Filing is a suggestion you can override in one click. |
| **B2** | One private item in a shared list (the mixer) | Visibility **per item**, inheriting from the space. Shared views and exports filter by viewer. |
| ~~B3~~ | *Withdrawn: the budget notes were invented for this scenario, not a stated need.* | — |
| **B4** | Decomposition blows up the backlog | Built-in limits: proposals stay `proposed` until accepted, a per-space limit on active items, and `someday` by default for anything beyond the next steps. The agent proposes the *next 3*, not the whole tree. |
| **B5** | Vague captures ("that podcast thing") | An inbox state with an agent question and an expiry. Stale items surface in the review and never block. |
| **B6** | Logged-in sites (marketplaces, booking, banks) | The agent's browser has no logins **by design**. Add a **"send to my browser" handoff** and a **capture-back** from your real browser (a small browser extension or share target) to get results back into the space. |
| **B7** | Work spills into other projects | Cross-space and cross-project links that appear in both spaces' inboxes, and dependencies that show as "blocked by". |
| **B8** | Real-world steps happen outside the app (calls, email, payments) | Make reporting results back nearly free: quick replies on `waiting` items, an email/calendar integration that recognizes confirmations, and the agent asking "did the visit get booked?" within the interrupt budget. **This is the most common break in the life spaces.** |
| **B9** | Conflicts between spaces (dates, money, time) | A shared **calendar/timeline** across spaces. Probably a core app, not an extension. |
| **B10** | Personal ↔ work instance crossing | An explicit, labelled **export to the other instance**. It never syncs automatically. |
| **B11** | Life happens on a phone | **Mobile capture and read access** for at least lists and the inbox (a thin client on the same core contract, or a messaging bridge). Desktop-only covers projects but not family life. |
| B12 | Cold start for life spaces (no repo docs to learn from) | Space knowledge comes from conversation and decisions over time. Templates (trip, renovation, gifts) bootstrap a new space. |
| B13 | Agent opening tabs while you're reading something else | The agent opens new tabs **in the target group, in the background with a badge**, unless you've allowed it to take focus. Focus-steal is a per-space setting. |

## What this suggests for the design

1. **The core is general:** spaces, items with links, tabs and layouts, an event log, visibility. Project-management features (workers, MR review, decomposition into code tasks) are **apps a space enables**, built on that core. That avoids overtuning.
2. **Integration priority for the personal instance** comes out as: **email and calendar** first (B8, B9), then a capture-back from your real browser (B6), mobile capture (B11), Git hosting (projects), and Slack mostly for the work instance.
3. **Keeping the backlog small is a product rule** (B4), not a setting. It's the difference between an ADHD aid and an ADHD trap.
4. **Layout model:** a split tree of tab groups, per-space saved arrangements, a marked **agent target group**, and background-open with badges (B13). Keyboard switching between arrangements is enough; no manual rearranging.
5. **Visibility has three levels:** instance > space > item, plus sensitive domains (finances, identity docs) referenced by handle.
6. **The "follows along" boundary is the right one,** but the app needs good **handoff out** and **capture back** at that boundary, or the spaces go stale.

## Next steps
- Turn this scenario into clickable layout mocks (spike 1): the Monday triage table, the Orchestrator arrangement, and the Friday review are the three screens worth mocking.
- Run a second scenario with the **work instance** (Slack request → task → MR review → verify → report back) to test the project-heavy path and B10 from the other side.

---

## Plan status for each break (2026-11-05)

Honest status. **Mechanism**: we know how, needs building. **Direction**: we know what, not yet how. **Open**: no plan yet.

| # | Break | Status | Plan, or what's missing |
|---|---|---|---|
| B1 | Items span spaces | Mechanism | links, not moves; one-click refile |
| B2 | Private item in a shared list | Direction | labels per item that inherit from the space. Missing: how labels propagate into exports, notifications and the agent's context |
| ~~B3~~ | withdrawn | — | it came from invented scenario notes |
| B4 | Backlog blow-up | Mechanism | the `proposed` state, a cap on active items per space, the agent proposes the next 3 steps, `someday` by default. Thresholds need tuning on real use |
| B5 | Vague captures | Mechanism | inbox state, an agent question, expiry, shown in the review |
| B6 | Logged-in sites | Direction | send-to-my-browser plus capture-back. Open: a browser extension vs an OS share target vs a clipboard watcher |
| B7 | Work spilling into other projects | Mechanism | the same links as B1, plus `blocked by` |
| B8 | Real-world steps outside the app | **Open, and the biggest** | best candidate: standing email/calendar sensitive agents that propose `waiting → done` updates from confirmations. Unproven |
| B9 | Conflicts between spaces | Direction | a core timeline/calendar app; needs a calendar integration |
| B10 | Personal ↔ work crossing | Mechanism | an explicit labelled export, no sync |
| B11 | Mobile | **Open** | options: a thin mobile client on the same contract, a PWA, or a messaging bridge. None chosen; it affects whether the core is reachable off-LAN |
| B12 | Cold start for life spaces | Direction | space templates plus knowledge learned from decisions |
| B13 | Agent stealing focus | Mechanism | background open in the target group with a badge; focus-steal is a per-space setting |

Six have a mechanism, four have a direction (B3 withdrawn), and two are open (B8, B11). Those two are the ones most specific to life spaces.

## Correction (2026-11-05): the goal is less friction, not more autonomy

*This replaces the earlier "accelerated variant", which pushed toward standing agents and pre-approved autonomy. The user's correction: "I'm still me and the agent is still the agent." The goal is less friction at the handoffs and less opacity in what the agent does. Some copy and paste is natural.*

Five tests for any feature:
1. **No box-out.** I can see the state of the work (tasks, status, what the agent did and why) without asking the agent. Every agent output lands as an artifact I can open, read, edit and comment on directly, not just as chat text.
2. **Direct action.** Anything the agent can do in the app, I can do myself through the UI. The agent is never the only way in.
3. **Handoff without copy-paste, both ways.**
   - *Me → agent:* point at things: select text, rows, terminal output or a region of a page and "ask about this"; drag something into chat; **comment on an artifact** (a comment is a request).
   - *Agent → me:* links that open in place; commands that land **pre-filled at my terminal prompt, not run**; diffs that apply in my editor; questions anchored to the exact spot.
4. **I choose what it sees.** No ambient listening or indexing (calls, texts). Sources come in only where I bring them, per space. Each pane shows what the agent can see.
5. **Review is a first-class loop.** I mark up any artifact. The agent answers in the thread and edits in suggestion mode, and I accept or reject. A **walkthrough mode** lets it "present" (I follow its scroll and highlights as it explains), and I can take over at any point. This is the two-seat model applied to review.

**Live evidence, from this design session:** four documents written, none read yet. Reviewing them means opening files separately and copying quotes back into chat. That's B14 below, and the strongest argument for test 5. Pushback on myself: the agent should also produce **smaller, reviewable increments**, not long documents in one go.

| # | Break | Status | Plan |
|---|---|---|---|
| B14 | Agent output piles up unreviewed; marking it up is clumsy | Direction | comment threads on every artifact, edits in suggestion mode, walkthrough mode, smaller increments |

Standing agents and pre-approved action classes are **dropped from the core**. Sensitive agents (`sensitive-agents.md`) stay **opt-in per source**: you choose to run email triage; it is never ambient.

# tasks-mock (throwaway)

A clickable mock of how Nexus could handle work items and tasks. **Mock data only**: nothing is wired to the Nexus core, desk or foreman. Workstream task-mgmt, 2026-11-16. Design: [`docs/design/work-items-and-tasks.md`](../../docs/design/work-items-and-tasks.md).

## Run it

Open `index.html` in a browser. Or serve it:

```bash
python3 -m http.server -d prototypes/tasks-mock 8770   # then http://localhost:8770
```

No build, no dependencies (vanilla JS; fonts load from Google Fonts with system fallbacks).

## What's in it

It's Monday morning and four work items are in flight at once, plus a few personal todos:

| Work item | Source | Shape |
|---|---|---|
| `#14` Export my pantry as CSV | GitHub issue | a feature in a simple app |
| `NEX-12` Jira integration for Nexus | Jira | coordination: IT access, design approval, build, MR review |
| `ARCH-7` Replace our job runner with Temporal? | team tracker | a technology evaluation with a working doc and a spike |
| `SEC-431` Security approval: reporting service | Jira (security team) | rounds of back-and-forth, tracked by a watcher |

**Three home screens over the same task set** (tabs at the top):

- **A · Inbox:** one ranked list of what needs you. Context sits beside it: what changed since Friday, agents working, who you're waiting on, todos.
- **B · Day plan:** a proposed plan for the day that you reorder or trim, then one task at a time in focus.
- **C · Work items:** a card per work item showing whose move it is, progress toward done and the next action. Backlog items from your trackers sit below with Start.

**Board** (the last tab) is the To do / In progress / Blocked / In review / Done board that runs underneath all three. You never have to open it.

**Make something happen** (top right) plays events from the outside world so you can watch cards move on their own: the agent finishes the spike or the plugin build, IT grants access, the platform lead approves, security replies with round 3, the agent finishes something else.

## A 10-minute script

1. **A · Inbox.** Read "Since you were here". Every card that moved has a reason.
2. Open **Review MR !41**. It's a landing-style packet: summary, checks, files, diff and proposed follow-ups (start now / queue / drop). Approve it.
3. Open **Answer security: where are secrets stored**. Every claim in the draft shows its source; one is flagged as not verified. Edit it, then Save.
4. **Post both replies to SEC-431** is now unblocked. Post asks you to confirm, because the security team will see it. Afterwards SEC-431 is their move.
5. **Make something happen → Security replies (round 3).** Open SEC-431 (its card in C, or the link in any of its tasks). The raw comment sits beside the asks the watcher derived from it. Pause the watcher.
6. **B · Day plan.** Reorder, push one to tomorrow, press **Start my day** and work through the focus cards.
7. **Make something happen → IT grants access.** "Test on live Jira" doesn't start on its own: it uses your credentials on a live system, so it waits for your Start.
8. **C · Work items.** Open **What's left** on the pantry card. Approve MR !23, check it on your phone (try **Found a problem**), then **Land**. When every done-when item is met, a **Close #14 in GitHub** task appears.
9. Open any agent task and press **Take it**: the agent stops and leaves its branch and notes. Then **Hand back**.
10. From C, press **Start** on `REP-88`. With 4 work items already in progress, it suggests parking NEX-12 (waiting on IT for 6 days). Park it, then trigger **IT grants access** and watch NEX-12 come back by itself.
11. Add a todo with no project. Ask the agent to help with the hotel todo, then trigger **The agent finishes the hotel search**.

## What to compare between A, B and C

| Question | A · Inbox | B · Day plan | C · Work items |
|---|---|---|---|
| What's the unit on the home screen? | a task | a task, in your chosen order | a work item |
| How do you start the day? | scan and pick | accept a plan, then go | scan whose move it is |
| Where do you act? | a drawer over the list | inline, one at a time | a drawer, or the work item's page |
| What's left on a work item? | one click away | one click away | on the card |
| Risk | the list feels like an inbox to clear | rigid when things land mid-day | your own tasks are spread across cards |

## Ideas taken from foreman's `landing` workstream

- **Done means landed.** A code task is done when its work merges (landed), not when the agent stops. A work item is done when every done-when item is met; closing it in the source is one confirmed press.
- **One packet, one decision.** Agent results arrive as a packet (summary, checks, files, diff, follow-ups) with Approve / Keep working / Take it.
- **Follow-ups:** start now / queue / drop, chosen in the packet.
- **Keep working** reuses the same session and branch with your note.
- **Opening an MR, posting a comment or merging always takes a press.** In the mock, every outward action has a confirm step that names who will see it.

## Not in the mock

Real sources, real agents, persistence (reload resets it), mobile layout polish, and keyboard shortcuts.

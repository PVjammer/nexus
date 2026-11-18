---
name: onboarding
description: Set up a project's work with the user, and shape a vague work item into a clear one with tasks. Use when the user asks to set up the project, to find what work there is, or to shape a work item (the Shape button sends "Shape ...").
---

# Onboarding and shaping

A new project has no work items yet. Setting it up is a short conversation. **The user decides everything; you find, suggest and ask.** Keep each message to a few lines.

Two rules hold throughout:
- **One question per turn.** `ask_user` is the last thing you do in a turn. After it, stop: the answer is the user's next message. A second question in the same turn is refused.
- **Suggestions aren't work yet.** What you propose sits in a checklist until the user ticks it. Don't say it's "on the board" or "added" until you're told what they accepted.

## Setting up a project ("Set up this project")

1. **Scan.** Call `project_scan`. It starts with the README, then lists what looks like work, each with where it is: unmerged branches, uncommitted changes, unchecked checklists, plan sections (todo, roadmap, next steps, open questions), TODO comments and design docs. If a doc looks like a plan, `file_read` it. **Find out where things stand first:** the README's status, and the newest results, report or changelog. A plan that says "waiting on approval" may be long done; when docs disagree, the newest one wins, and you say so in one line.
2. **Propose.** Group related findings into **3 to 8 work items**: the things a person would say they're working on, not every TODO. A branch with commits is usually one item ("Finish the auth refactor"). Many TODOs in one area are one item. Skip trivia. Call `work_propose` once with all of them. Give each:
   - a short `title` starting with a verb;
   - a one-line `why`, and `evidence` copied from the scan;
   - a `brief`: what it is and what's in scope, in 1-2 lines, from what you read;
   - `done_when`: 2-4 short checks that say it's finished, from the docs where they say so.

   Every work item should say what it is and when it's done even before anyone works on it. Then tell the user in one line that they're in the checklist.
3. **Ask what else.** `ask_user`: "What else do you want to do with this project?" with options like "Nothing else for now". End your turn. Whatever they name, add with `work_item_add` (their words, not a proposal).
4. **Ask what's in progress.** Once you're told what they accepted, `ask_user` with `multi: true`: "Which of these are you working on now?", the accepted items as options. Start each one they pick with `work_item_set` state `in-progress`. If the result says they're over their in-progress limit, mention it and suggest one to leave for later; don't change it yourself.
5. **Shape each item in progress** (below), one at a time. Ask before moving to the next: "Shape the next one?"

If the scan finds nothing, skip to step 3.

## Shaping a work item ("Shape ...")

A work item is shaped when it has a short brief, what done looks like, and its next tasks in order.

1. Read what there is: the item's brief, done-when and evidence, and the files it points at.
2. Ask **at most 3 questions**, one per turn, each with 2 to 4 likely answers to click. Keep the question under 200 characters: put any background in your message before it. Ask only what you can't find out yourself. Good questions: what does done look like (if the draft is unsure); is there a deadline; who else is involved or are you waiting on anyone; what's out of scope.
3. Save the answers with `work_item_set`: `brief` (1 to 3 lines in the user's words) and `done_when` (2 to 5 short checks).
4. Propose the next **2 to 5 tasks** with `work_propose` and `item`: concrete next actions ("Write the CSV writer for orders", "Ask Sam for the export format"), each assigned to `you`, `agent` or a person's name. When a task can only start after another, say so with `after` (the earlier task's title): it waits, out of the user's way, until that one is done. The user ticks the ones they want.

If the user says "good enough" or "just break it down", stop asking and go to step 3 with what you have.

## Examples

Setting up: scan, then propose what you found, each with a draft brief and done-when.

```json
[{"tool": "project_scan", "args": {}},
 {"tool": "work_propose", "args": {"proposals": [
   {"title": "Finish the auth refactor", "why": "branch auth-v2 has 6 commits not on main", "evidence": ["branch auth-v2"],
    "brief": "Move sessions to short-lived tokens (branch auth-v2).", "done_when": ["auth-v2 merged into main", "Old session tokens are rejected"]},
   {"title": "Write the plan's next steps", "why": "the plan's Next steps section lists them", "evidence": ["docs/plan.md:3"],
    "brief": "Turn the three next steps in docs/plan.md into work.", "done_when": ["Each next step is done or dropped", "docs/plan.md updated"]}],
  "reason": "found while setting up the project"}}]
```

Asking what else, with answers to click. Nothing comes after it in the turn.

```json
[{"tool": "ask_user", "args": {"question": "What else do you want to do with this project?", "options": ["Nothing else for now", "Fix the slow startup", "Write docs"]}}]
```

The user said "I also want to add dark mode, I'm on it now".

```json
[{"tool": "work_item_add", "args": {"title": "Add dark mode", "brief": "The user wants a dark theme; working on it now.", "start": true, "reason": "the user named it"}}]
```

Asking which items are in progress.

```json
[{"tool": "ask_user", "args": {"question": "Which of these are you working on now?", "options": ["Add CSV export", "Finish the auth refactor", "None yet"], "multi": true}}]
```

The user picked "Add CSV export".

```json
[{"tool": "work_item_set", "args": {"item": "Add CSV export", "state": "in-progress", "reason": "the user is working on it"}}]
```

Shaping: one question in this turn.

```json
[{"tool": "ask_user", "args": {"question": "What does done look like for CSV export?", "options": ["Orders only", "Orders and customers", "Everything in the admin"]}}]
```

The user answered "Orders only". Save what you learned, then propose tasks in order: the button can't go in before the writer exists, and finance checks the result last.

```json
[{"tool": "work_item_set", "args": {"item": "Add CSV export", "brief": "Export orders to CSV from the admin page. Finance needs it for month end.", "done_when": ["Orders export from the admin page", "Finance has checked a real export"], "reason": "from the user's answers"}},
 {"tool": "work_propose", "args": {"item": "Add CSV export", "proposals": [
   {"title": "Ask finance which columns they need", "assignee": "you"},
   {"title": "Write the CSV writer for orders", "assignee": "agent", "after": ["Ask finance which columns they need"]},
   {"title": "Add the Export button to the admin page", "assignee": "you", "after": ["Write the CSV writer for orders"]},
   {"title": "Have finance check a real export", "assignee": "you", "after": ["Add the Export button to the admin page"]}],
  "reason": "next steps for CSV export"}}]
```

## When a call fails

The error says what to fix, for example `give 2-5 options to click` or `no work item "x". Call work_items for refs`. Fix that one thing and call again. `you already asked ... this turn` means stop now and wait for the answer.

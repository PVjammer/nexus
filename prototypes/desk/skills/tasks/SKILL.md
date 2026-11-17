---
name: tasks
description: Track the user's work with the work_* tools. Work items are the things a project is working toward (on the project's list in Nexus, or read from a connected source that is never changed); tasks are concrete next actions toward them, kept only in Nexus. Use when the user asks what's left, what needs them, to break work down, to start or park a work item, or to record who is doing what.
---

# Tasks

**Work items** are what a project is working toward: "Add CSV export", "Finish the auth refactor". Most are on the project's list in Nexus; some may be read from a connected source, which you never change. Nexus keeps four things about each one: its **state** (`in-progress`, `parked`, `not-started`), a short **brief**, what **done** looks like (`done_when`), and its **tasks**.

A **task** is one concrete next action: "Review MR !41", "Ask IT for API access", "Check the export on your phone". Not "work on the integration". A task with no work item is a plain **todo**.

Tasks sit on a board with these statuses: {{statuses}}. Each has an **assignee**: `you` (the user), `agent`, or a person's name ("IT", "Platform lead"), which records that you're waiting on them. A task in `review` has a **reviewer** (the user by default).

## When to use this

- "What needs me?": `needs_you`. It's the user's whole Needs you list (questions waiting on them, finished agent tasks, proposals to review, and their tasks), not just tasks. Lead with the one that matters most.
- "What's left on X?": `work_items`, then `work_tasks` for that item. Answer in a short list: what's the user's, what's the agent's, who you're waiting on.
- "Break X down", "what are the next steps?" or "plan X": **add** 2 to 5 tasks with `work_task_add` (the next actions only, not a whole plan), then list them in one line each. Don't just describe steps in chat: the point is that they end up on the board.
- "Start X", "park X until Y" or "drop X for now": `work_item_set`.
- The user says something happened ("IT granted access", "I reviewed it"): move that task with `work_task_update`.
- If starting a work item goes over the user's in-progress limit, the result says so and names good candidates to park. Suggest one; don't park it yourself. "Set my limit to 3" is `work_limit` (home agent).
- Steps and waits are **tasks** on an existing work item (`work_task_add`), never new work items. `work_item_add` is only for a new goal the user names ("I also want to add dark mode").
- Don't add tasks for a question. Answer it, and offer: "Want me to add that as a task?"

## How

1. `work_items` shows refs (like `item:i12`). Use a ref, or the work item's exact title.
2. Keep titles short and concrete, starting with a verb.
3. If a task can't start until another is done, give `blocked_by` with that task's id. It moves to `todo` by itself when the other is done.
4. Something you're waiting on outside Nexus is a task assigned to that person: status `doing`, assignee `IT`.
5. Every change needs `reason`: a few words from the user's request.
6. You can refer to a task by its id (`k12`) or its exact title.

## Examples

"What are the next steps on CSV export?"

```json
[{"tool": "work_tasks", "args": {"item": "Add CSV export"}},
 {"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Decide which columns the export has", "reason": "next step on CSV export"}},
 {"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Draft the export docs", "assignee": "agent", "reason": "next step on CSV export"}}]
```

"We're waiting on IT for API access, and the live test can't start until then."

```json
[{"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Grant API access (IT-882)", "assignee": "IT", "status": "doing", "reason": "waiting on IT"}},
 {"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Test against the live API", "blocked_by": ["Grant API access (IT-882)"], "reason": "needs the access first"}}]
```

"IT granted access."

```json
[{"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Grant API access (IT-882)", "assignee": "IT", "status": "doing", "reason": "waiting on IT"}},
 {"tool": "work_task_update", "args": {"id": "Grant API access (IT-882)", "status": "done", "reason": "IT granted access"}}]
```

"The agent finished the draft; I need to review it."

```json
[{"tool": "work_task_add", "args": {"item": "Add CSV export", "title": "Draft the export docs", "assignee": "agent", "status": "doing", "reason": "agent drafting"}},
 {"tool": "work_task_update", "args": {"id": "Draft the export docs", "status": "review", "reviewer": "you", "reason": "draft ready for review"}}]
```

"Park CSV export until the format is decided. Done means: format agreed, export shipped."

```json
[{"tool": "work_item_set", "args": {"item": "Add CSV export", "state": "parked", "park_note": "until the format is decided", "done_when": ["Format agreed with finance", "Export shipped"], "reason": "park until the format is decided"}}]
```

"Remind me to book the hotel by Friday."

```json
[{"tool": "work_task_add", "args": {"title": "Book the conference hotel", "due": "2026-11-20", "reason": "todo for Friday"}}]
```

"What needs me?"

```json
[{"tool": "needs_you", "args": {}}]
```

## When a call fails

The error says what to fix, for example `status must be one of: todo, doing, blocked, review, done` or `no work item "x". Call work_items for refs`. Fix that one thing and call again.

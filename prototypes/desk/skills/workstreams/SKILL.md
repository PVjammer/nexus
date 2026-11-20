---
name: workstreams
description: Orchestrate a project's workstreams. Every work item in progress has its own agent and conversation; use this to see where one stands, ask it something, or tell it to do something. Use when the user asks about one piece of work in progress, or wants something done on it.
---

# Workstreams

Every work item in progress has a **workstream**: its own agent and its own conversation with the user. You are the **orchestrator**. Your turn context lists each workstream on one line: whose move it is, its next task, and what it last said.

**Anything the user asks about a workstream, you can do.** Never tell them to go and ask the workstream instead. Pick the cheapest way:

| The user wants | Do this |
|---|---|
| where it stands, in general | answer from its line in your context; it's usually enough |
| something specific it said or decided | `ws_transcript` with a `query`, or `ws_ask` with the question |
| several agents' views, or a discussion | `ask_agents` (see the mentions skill) |
| the latest of its conversation | `ws_transcript` with no query (the newest 20 messages) |
| it to do something | `ws_message` with what to do; it does it in its own conversation |
| a change to its tasks or items | do it yourself with the `work_*` tools; its agent is told at its next turn |

- **Read in pieces.** `ws_transcript` returns a window and says how to get more ("from=21 for the next"). Search before you page.
- **`ws_ask`** asks the workstream's agent itself, from a side session: it answers with everything it knows, keeps working meanwhile, and remembers being asked. Call it as `ws_ask` with `item` and `question`. It takes a minute, so use it for questions a search can't answer ("why did it pick Postgres?").
- **`ws_message` waits for its turn.** If the workstream agent is mid-turn, your message waits until the turn ends. The user can interrupt it. Tell the user it's queued, in one line.
- **Changes reach the agents.** When you, the user or a quick ask change a workstream's tasks, or files change in the project, its agent gets a short "since your last turn" note. You get one too: start from it.

## Examples

The user asks "did the CSV export settle on the columns yet?". Search its conversation.

```json
[{"tool": "ws_transcript", "args": {"item": "Add CSV export", "query": "columns"}}]
```

The user says "tell the CSV export to use semicolons". Message the workstream.

```json
[{"tool": "ws_message", "args": {"item": "Add CSV export", "text": "The user wants semicolons as the separator, not commas. Update the plan and the tasks."}}]
```

The user asks "what's the latest on CSV export?". Read the newest messages.

```json
[{"tool": "ws_transcript", "args": {"item": "Add CSV export"}}]
```

## When a call fails

`isn't in progress` means its workstream is asleep: say so, and offer to start the work item. `no work item` lists refs: use one, or the exact title.

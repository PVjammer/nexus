---
name: closeout
description: Close out a work item the user marked done. Write its report, propose what's left as new work items, and list loose ends in the repo as commands for the user. Used by the closeout task Nexus starts when a work item is done. A project can replace these steps with its own desk/<project>/closeout.md.
---

# Closing out a work item

The user said this work item is done. Your brief has everything Nexus knows about it: the brief, the done-when checks (with evidence if an agent claimed it), its tasks, its earlier report and the loose ends in the repo. **Don't reopen or redo the work.** You wrap it up so nothing gets lost. Nexus archives the item itself; you never delete anything.

1. **Read only what you need.** If the brief says the work item has a workstream conversation, search it with `ws_transcript` (a `query` such as "decid" or "chose") for the decisions. If there's an earlier report or a design doc, `file_read` or `review_read` it. Skip this step if the brief is enough.
2. **Write the report** with `report_write`, short, with these sections:
   - **Done:** what was finished, in 2 to 5 bullets, from the done-when checks and the tasks done.
   - **Decisions:** what was decided along the way, if you know any. Leave the section out if you don't.
   - **What's left:** the tasks still open, and anything the done-when checks didn't cover.
   - **Loose ends:** each unmerged branch or uncommitted change from the brief, with the command the user could run (`git merge auth-v2`, `git branch -d auth-v2`, `git stash`). You can't run them, and Nexus never changes the repo. Write "none" if there are none.
3. **Propose what's left** with `work_propose`, leaving `item` out, so they arrive as new work items in the user's checklist. Only propose things worth doing later, at most 5, each with a `why` that says it's left over from this work item. Skip this step if nothing is left.
4. **Finish** with `task_finish`: status `done`, and a one-sentence summary for the user ("Closed out CSV export: 2 follow-ups suggested, branch export-v1 not merged").

## Examples

Finding the decisions in the workstream's conversation.

```json
[{"tool": "ws_transcript", "args": {"item": "Add CSV export", "query": "decid"}}]
```

Writing the report.

```json
[{"tool": "report_write", "args": {"content": "# Closed out: Add CSV export\n\n## Done\n- Orders export from the admin page\n- Finance checked a real export\n\n## What's left\n- Customers export (task still open)\n\n## Loose ends\n- Branch `export-v1` isn't merged: `git merge export-v1`, then `git branch -d export-v1`\n"}}]
```

Proposing what's left, then finishing.

```json
[{"tool": "work_propose", "args": {"proposals": [
   {"title": "Add the customers export", "why": "left over from Add CSV export: the task was still open",
    "brief": "Export customers to CSV, like the orders export.", "done_when": ["Customers export from the admin page"]}],
  "reason": "what's left after closing out Add CSV export"}},
 {"tool": "task_finish", "args": {"status": "done", "summary": "Closed out Add CSV export: 1 follow-up suggested, branch export-v1 not merged."}}]
```

## When a call fails

The error says what to fix. Fix that one thing and call again. If `work_propose` says an item was skipped (already there), that's fine: carry on.

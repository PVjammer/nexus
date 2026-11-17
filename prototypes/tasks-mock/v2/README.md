# tasks-mock v2: a desktop made of widgets (throwaway)

The same Monday and the same four work items as v1 (`../`), rebuilt around one idea: **every box on screen is a widget, and a widget is data + scope + shape.** Mock data only.

## Run it

```bash
python3 -m http.server -d prototypes/tasks-mock/v2 8771   # then http://localhost:8771
```

Or open `index.html`. No build, no dependencies.

## What changed from v1

- **No variant tabs.** Home is a layout of widgets: Needs you, Since you were here, Work items, Not in progress, Todos, Agents working, Waiting on others. The v1 inbox and cards are now just widgets on it.
- **Spaces.** The top-left switcher goes to Home, a project, or a workspace. A project space uses the same widgets narrowed to that project. A workspace uses them narrowed to one work item.
- **Clicking a work item opens its workspace**: What's left (grouped by whose move), the agent session, Done when, and whatever the agent opened (MR, thread, design doc, working doc). Looking doesn't resume a suspended session; Resume is its own press.
- **The Tasks app** (Apps menu, or the "need you" count) holds the default views: Needs you, Today, Work items, Board, Todos, Since you were here. You never have to go there; every view is also a widget (⋯ → Pin to Home).
- **Start and Park appear wherever a work item does**: cards, lists, the workspace header and the task panel.
- **Today is an agenda you pick from**, not a forced sequence. After you finish a task opened from a list, the panel offers what's next in that list. Close it to choose anywhere.
- **The agent arranges with the same parts.** Ask it to plan your day, strip your home down, keep SEC-431's thread on Home, or show the tasks as a board. What it adds is marked "agent", has Undo, and "What is this?" shows its spec.
- **The "need you" count in the top bar can't be removed**, so no layout can bury something waiting on you.

## A 10-minute script

1. **Home.** Open **Review MR !41** from Needs you and approve it. The panel then offers what's next in Needs you; take **Answer security**, then **Post both replies**.
2. Click the **SEC-431** card. You're in its workspace. Note that the session is still suspended until you press **Resume**.
3. In the Agent widget, press **What's left?** (the short answer, not 700 words), then **Show the tasks as a board**, then **Undo** in the toast.
4. **Make something happen → Security replies (round 3).** The Thread widget shows the raw comment beside the asks. Pause the watcher.
5. On the Thread widget: **⋯ → Pin to Home**. On any agent-marked widget, press the **agent** badge to see why it's there.
6. Back on **Home**: try **Plan my day**, then **What's left to close NEX-12?** → **Pin this as a widget**, then **Strip my home to the essentials** → **Undo**.
7. **Customize**: move a widget, change its width, **+ Add a widget** (try Waiting on others, scoped to Nexus), remove one.
8. **⋯ → Show as** on Needs you: switch it to a board and back.
9. Switch to the **Pantry app** project space, open `#14`, and land it: approve, phone check, Land, then Close from the Done when widget.
10. **Apps → Tasks → Board**, and narrow it with **Show**.
11. From **Not in progress**, **Start** `REP-88`. Accept parking NEX-12, then trigger **IT grants access** and watch NEX-12 come back.

## Not in the mock

Drag-and-drop (Customize uses arrows), persistence (reload resets), real agents and sources, other Nexus apps (terminal, browser, review).

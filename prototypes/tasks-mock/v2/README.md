# tasks-mock v2: a desktop made of widgets (throwaway)

The same Monday and the same four work items as v1 (`../`), rebuilt around one idea: **every box on screen is a widget, and a widget is data + scope + shape.** Mock data only.

## Run it

```bash
python3 -m http.server -d prototypes/tasks-mock/v2 8771   # then http://localhost:8771
```

Or open `index.html`. No build, no dependencies.

## What's in it

- **Home starts as one prompt box** (the home agent) with **Customize manually** in the bottom-right corner. The agent answers in place ("What needs me?"), and "Keep this on screen" turns an answer into a widget. "Set up my usual dashboard" adds the v1-style widgets under the box.
- **Customize manually** opens a bar for the current space:
  - **columns** (1 to 4);
  - **tiling**: Flow (the page scrolls) or Tile (widgets fill the screen like a tiling window manager);
  - **+ Add a widget** and **Reset**.

  Each widget gets controls to move it, change its width and height, or remove it.
- **The chat is a widget too**, in two styles: Minimal (a prompt box with the last reply) or Conversation (the full thread). Switch with ⋯ → Show as.
- **Chats** (top bar) lists every agent conversation, including the home agent, project agents and each workspace's session. They're kept whether or not they're on a screen. You can read and continue any of them there, or put one on the current screen.
- **Spaces.** The top-left switcher goes to Home, a project (the same widgets narrowed to that project), or a work item's **workspace**.
- **Clicking a work item opens its workspace**: What's left (grouped by whose move), the agent session, Done when, and whatever the agent opened (MR, thread, design doc, working doc). Looking doesn't resume a suspended session; Resume is its own press.
- **The task panel** (the drawer) has **Open in workspace ↗**. It takes you to the workspace with the task docked at the top, next to the session and the doc, for tasks that need digging into.
- **The Tasks app** (Apps menu, or the "need you" count) holds the default views: Needs you, Today, Work items, Board, Todos, Since you were here. You never have to go there.
- **Start and Park appear wherever a work item does.** Today is a list you pick from. After a task you opened from a list, the panel offers what's next in that list.
- **The agent arranges with the same parts.** What it adds is marked "agent", has Undo, and "What is this?" shows its spec.
- **The "need you" count in the top bar can't be removed.**

## A 10-minute script

1. **Home** is one box. Press **What needs me?**, then **Keep this on screen**.
2. Open **Review MR !41**, then **Open in workspace ↗**. The task is docked in SEC-431's workspace next to the agent session and the thread. Approve it there, then × to put it away.
3. In the session, press **What's left?** and **Show the tasks as a board**, then **Undo**.
4. Open **Chats**, then **Home agent**. Friday's conversation is there. Ask "plan my day" from the panel, or put the conversation on this workspace.
5. Go back **Home** and press **Set up my usual dashboard**.
6. Press **Customize manually**.
   - Set 4 columns and switch Tile on and off.
   - Make a widget wider and taller, and move it.
   - **+ Add a widget**, choose Agent, set the style to Conversation, and add it.
   - Remove the prompt box, then press **Done**.
7. On the chat widget, **⋯ → Show as** Minimal.
8. Answer security from Needs you; after **Save answer**, the panel offers what's next. Post both replies, then **Make something happen → Security replies (round 3)**.
9. Switch to the **Pantry app** project, open `#14` and land it: approve, phone check, Land, then Close.
10. **Apps → Tasks → Work items**, **Start** `REP-88`, accept parking NEX-12, then trigger **IT grants access**.

## Not in the mock

Drag-and-drop (Customize uses arrows), persistence (reload resets), real agents and sources, other Nexus apps (terminal, browser, review).

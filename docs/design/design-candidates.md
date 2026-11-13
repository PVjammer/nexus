# PA "everything app": candidate designs for the ideal app

*2026-11-05, workstream pa-extensible-ui. Brainstorm input, not decisions. This is the target app, not v1.*

## What the user said, as requirements

- **The meta-pain is context switching and organization** (ADHD). Both projects and tasks are central. What the app knows about a project should help organize incoming data and draft tasks.
- **Pair, don't delegate.** Doing everything agentically has drawbacks: you lose context and hand off the critical thinking. The app should keep you in the work, let the agent speed it up, and let you take the wheel at any point. Your work and the agent's work are closely paired.
- **The user works with other apps mainly through this one.** Editor and browser tabs matter more than Slack right now.
- **Pretty and structured** (diagrams, structured data, good views) rather than text in a terminal.
- **Cross-platform:** Linux and Mac required, Windows via WSL desirable. Omarchy/Hyprland is an optional integration, not the foundation.
- **Background work** keeps running when the window is closed, and agents can run locally or remotely.
- **Separate instances** for work and personal.
- **Not limited by other repo projects.** foreman can become this app's project-management engine, or this app can be "foreman 2.0".

## Design principles that follow

1. **The agent's default output is something you shape.** It produces a draft, options with tradeoffs, an annotated diff, or proposed subtasks, and you make the decisions. Decisions are recorded as yours. This is the guard against handing off the critical thinking.
2. **Every surface has a visible "wheel".** On any surface (editor, browser tab, terminal, diff, doc, extension pane) it is always clear who is driving: *you*, *the agent*, or *you, with the agent suggesting*. Switching is one gesture. The agent's actions on a surface are visible, attributed and undoable.
3. **Context sticks to work, not to windows.** Tabs, terminals, notes, sources and agent runs belong to a task. Resuming a task restores all of them.
4. **Capture is instant; organization comes later.** A global hotkey captures on every OS. Project knowledge files the item afterwards (the write-now, file-later lesson).
5. **The app pulls information in and pushes rarely.** It fetches from sources itself, and nudges go through an interrupt budget, so it never becomes another inbox to forget.
6. **No box-out, opt-in context** (added 2026-11-05): everything the agent produces is an artifact you can read, edit and comment on directly; anything it can do, you can do in the UI; it sees only what you bring in. See the five tests in scenario-week.md.
7. **Sensitive data defaults to the data plane, and the node/lock boundary holds** (pa_design_doc §2.5, §8, §12, unchanged).

## Shared foundation (true for every candidate)

**Data model**
- **Instance:** `work` or `personal`. Each has its own core, credentials and store. One app can switch between them, and nothing crosses between them unless you export it explicitly with a label.
- **Project knowledge:** goals, repos, people, links, conventions, decisions, glossary. It is a living brief, partly written by you and partly learned (with provenance). It drives filing, task drafting and agent context.
- **Task:** a tree (decomposition), status, source links (with quoted text), an owner (me / agent / pair), acceptance criteria, and attached surfaces.
- **Surface:** editor buffer, browser tab, terminal, diff/MR, note/doc, widget, or extension pane. Each has an adapter that declares what the agent can read, what it can do or propose, and a visibility label.
- **Event log:** every action by either driver, with who did it, where, which task, and undo information. Timelines, status reports and "what happened while I was away" are projections of this log.

**Processes:** a core daemon (local or remote) owns the store, scheduler, broker, workstream engine and agents. Desktop clients connect to it. One option is a Tauri shell (Rust, the system webview on each OS); the Hyprland/Quickshell client is another client on the same contract. On Windows, the app runs on Windows and the core and terminals run in WSL.

**Workstream engine:** an interface. foreman's model (brief → worker → report → inbox) is the first implementation, and it can be replaced.

**Extensions:** tiers 1 / 2a / 2b / 3 (report §f), all built on the surface-adapter contract.

**Two hard parts every candidate inherits**
- **The browser.** *(Downgraded 2026-11-05: the user only needs to follow along in it, so an embedded webview without logins is enough. Its remaining needs are handing off to your real browser and capturing results back, see scenario-week.md B6.)* The original concern: it has to be good enough that you actually work in it. The system webview (Tauri) has no browser extensions and weak password-manager support. CEF/Chromium is the real thing but heavy. A third option is driving your own Chrome/Firefox through CDP or WebDriver BiDi with tab groups per task. This is the biggest engineering risk and needs its own spike.
- **The two-seat editor.** If the agent is a real pair, edits have to be concurrent, not "last writer wins". A CRDT buffer (Yjs/`yrs`, or Automerge, both with Rust cores) puts the agent in as a peer with its own cursor. CodeMirror 6 has Yjs bindings and is light and hackable. Neovim (embedded) and ACP (to Zed and others) are alternatives for users who already have an editor.

---

## Candidate 1: Project Workbench (IDE-shaped)

```
┌ projects ┬──────────── tabs: editor | browser | terminal | diff ───────────┬ pair ─────┐
│ ▸ pa     │                                                                 │ agent on  │
│   tasks  │            focused surface (you or agent driving)               │ THIS tab: │
│   notes  │                                                                 │ suggest,  │
│   files  │                                                                 │ explain,  │
│ ▸ baish  │                                                                 │ take over │
└──────────┴────────── status: workstreams running · needs-you (2) ──────────┴───────────┘
```

- **Unit:** the project is the window, and tasks are the tree in the sidebar. Tabs are scoped to the selected task, so switching tasks swaps the whole tab set.
- **Pairing:** a sidebar agent bound to the focused tab. It can suggest inline or take the wheel on that tab.
- **Strengths:** familiar, editor- and browser-first, and dense. Closest to how you work today.
- **Weaknesses:** the cross-project overview is weak, and it can still turn into tab soup within a task. Pretty, structured views compete with the editor for space.
- **Architecture stress:** the surface adapters and the browser. Little new in the data model.

## Candidate 2: Mission Control (zoomable task canvas)

```
 all projects ──zoom──▶ project map (tasks as cards, links, status) ──zoom──▶ task canvas
                                                                  ┌──────────────────────┐
                                                                  │ [slack src] [notes]  │
                                                                  │ [diagram]  [browser] │
                                                                  │ [diff]    [terminal] │
                                                                  └─ card ⇢ focus mode ──┘
```

- **Unit:** the task is a spatial canvas of its surfaces. Zooming out shows the project map, and zooming out again shows every project.
- **Pairing:** the agent places and updates cards (research results, diagrams, draft subtasks). You arrange, edit or reject them. Expanding a card opens a full-screen focus mode.
- **Strengths:** spatial memory and "pretty pictures" by default. The overview addresses the organization pain directly.
- **Weaknesses:** canvases get messy, heavy editing on a canvas is awkward (so focus mode matters), and it is the most expensive to build well.
- **Architecture stress:** layout state is per-task and server-side so it syncs. Widgets and tier-3 views do most of the work.

## Candidate 3: Project Journal (a stream of blocks as the spine)

```
 pa ▸ journal                                          threads: #extensible-ui #sandbox
 ─────────────────────────────────────────────────────────────────────────────────────
 09:12 you    captured: "feature ask from Jo" [slack ↗]           → task #42 (drafted)
 09:14 agent  decomposition proposal (3 subtasks)   [accept] [edit] [reject]
 09:20 agent  research: 4 sources, quotes verified  [open browser block]
 09:41 you    ▣ editor: pkg/broker.go  (live block, pinned)
 10:05 agent  diff ready · tests pass · 2 notes     [review together]
```

- **Unit:** a chronological stream per project (as in Warp or a notebook). Every action by either side is a block. Tasks are threads through the stream. Any block (editor, browser, terminal, diff) expands into a live surface and can be pinned.
- **Pairing:** the most transparent of the four. Your blocks and the agent's are interleaved, so you never lose track of what it did or why. Status updates and "what happened while I was away" come free.
- **Strengths:** context is never lost, it fits the event log exactly, and the history is useful in itself.
- **Weaknesses:** long-lived artifacts (a file you edit all day) don't suit a stream, so you need pinned surfaces, which drift toward candidate 1. Weaker overview across projects.
- **Architecture stress:** the event log is the primary store. Blocks must be live references, not snapshots.

## Candidate 4: Two-Seat Cockpit (the interaction model as the product)

- **Unit:** any surface, with two seats: you and the agent. Each has a cursor and a presence indicator, as in multiplayer docs. Modes: *follow the agent* (watch it drive), *agent follows me* (it watches and suggests), and *split* (it works in a forked copy that you merge).
- **Organization layer:** deliberately thin (a projects/tasks list plus a needs-you queue). The bet is that if pairing is excellent, organization can stay simple.
- **Strengths:** delivers "hop in and take the wheel" and "don't hand off the thinking" most directly.
- **Weaknesses:** it barely addresses the organization and ADHD pain by itself, and it is the hardest per surface (a CRDT editor, a browser you can share control of, shared terminal input).
- **Architecture stress:** a CRDT for each surface type, plus "who's driving" as a core concept.

---

## Comparison and recommendation

| | 1 Workbench | 2 Mission Control | 3 Journal | 4 Two-Seat |
|---|---|---|---|---|
| Context switching | good within a project | **best** overview | **best** continuity | weak |
| Pairing / take the wheel | good | ok | good (transparent) | **best** |
| Pretty / structured | ok | **best** | good | ok |
| Editor + browser first | **best** | weak (focus mode) | ok (pinned blocks) | good |
| Cost to build well | med | high | med | high |

These are **layers, not rivals**. My candidate ideal is a composite:

- **4's two-seat model as the interaction rule on every surface.** Principle 2 is not optional, whichever layout wins.
- **1's workbench as the working mode**, because editor and browser are the priority.
- **3's journal as the memory**: every project has a stream, and the workbench can show it as a side panel or as the default resume view.
- **2's map as the zoom-out** for the overview across tasks and projects. It is the second phase, not the main screen.

If you want only one, pick **1 + 3** (workbench plus journal). It covers editor/browser-first, context continuity and pairing transparency at moderate cost.

## Spikes for the ideal design (exploration, not v1)

1. **Layout feel test.** Build clickable mocks of candidates 1–3 with your real projects as data. Question: which one actually reduces context switching for you? This is cheap and should come first.
2. **Two-seat editor.** CodeMirror 6 plus Yjs, with the agent as a peer that has its own cursor; follow/suggest/split modes. Question: does concurrent pairing feel better than a sidebar agent, and how should agent edits be shown?
3. **Browser strategy.** Compare the system webview, CEF and driving your own browser over CDP/BiDi with per-task tab groups. Questions: which one would you actually use as your browser, and which gives the agent structured access with the private-page rule?
4. **Journal as the store.** An event log with tasks, status and resume views projected from it. Question: can one log serve history, undo, status reports and agent context?
5. **Project knowledge bootstrap.** Repo docs plus notes give a project brief, and incoming requests are then filed into it and decomposed. Question: does project knowledge make filing and decomposition good enough that you accept them without much editing?

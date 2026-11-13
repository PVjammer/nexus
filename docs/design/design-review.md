# Design review of the Nexus UX prototype (2026-11-12)

*After your first hands-on testing ("definitely not in love yet", "doesn't really capture what I'm looking for"). This review is critical on purpose, and most of the criticism is of my own choices.*

## The diagnosis in one paragraph

I built the four must-haves as features and put them on a generic frame: a tiling window manager with a chat column on the left. That frame is what every AI IDE already is: a chatbot beside some panes. Nobody designed what the screen is *for*. The candidate designs recommended **workbench + journal**, and the prototype is neither. There's no project or task rail, and no journal, since the action log is a hidden pane. I also skipped the spike the candidates doc put first, clickable layout mocks with your real data, and went straight to machinery. So the prototype shows the mechanisms working (secure tool, handles, sandboxed plugins, review threads), but it can't tell you whether the *app* is right. The mechanisms carry forward. The frame doesn't.

## Where it misses what you said you want

| What you asked for | What the prototype does | Gap |
|---|---|---|
| **Help with context switching and organization** (the meta-pain, ADHD) | Spaces are just named layouts. There are no items, no capture, no inbox, no triage and no resume. | **The biggest miss.** The scenario's clearest win (Monday triage) and the part that most directly helps ADHD (the Friday review) are both absent. |
| **A command-centre feel** (why you picked the name Nexus) | No overview. You can't see what's running, what needs you, or what happened in other spaces. | A command centre is a place you look *from*. This is a place you work *in*, one pane at a time. |
| **Pair, don't delegate; two seats** | The agent is a chat transcript. Its work shows up as badges, toasts and tool-call lines in chat. | You can't watch it work in the pane: no presence, no "agent is editing row 4", no follow mode. Pairing happens in chat, not on the work. |
| **No box-out: outputs are artifacts, not chat text** | Most answers land as chat text. Artifacts exist (sheets, reviews, plugins), but chat is the spine. | The journal idea (every action by either side is a block you can open) would make the event log the spine, not the chat. |
| **The agent arranges and fills the workspace** | The agent edits a tiling tree, and you manage its target group, pins, visibility chips and "undo agent". | It turns the agent's freedom into chores for you. See the next section. |
| **"You shouldn't have to keep rearranging"** | Secure panes open as new splits. Presets reshuffle everything. Three narrow columns appear after a few requests (see the screenshots). | Layout sprawls by default. |
| **Foreman 2.0, workers vs the pair agent** | No workers at all. | Fine for a UX spike, but a command centre without the workers' runs and reports is missing half its content. |

## The layout and visibility model is wrong, not just buggy

You found the core problem with one question: *can the agent see a private pane in its own group?* It can't. So "agent's group" and "what the agent sees" are two independent controls, and that leaves nonsense states. Related problems:

- **Too much chrome for one idea.** Every group shows "target here", and every tab shows a pin icon and a visibility chip. You have to learn three concepts (target, pin, visibility) to know what the agent can touch.
- **Private isn't fully private.**
  - The turn context still tells the agent "N more panes are private", which is a leak.
  - Arrange presets and undo move private panes the agent can't see.
- **The agent's area is one tab stack, while its placements create stray splits.** Its work spreads into one-tab columns instead of filling a region it owns.
- **The terminal breaks "you choose what it sees".** The agent reads terminal output whenever the pane is visible, and the scrollback is where secrets turn up. baish already settled the better model: context comes only from explicit pipes.

**Proposed rule: place is permission.**
- The screen has **your side** and **the shared side** (the agent's desk).
- Anything on the shared side, the agent can see and arrange freely, as tabs or tiles in its own region.
- Your side is yours. The agent sees titles at most, and never moves anything there.
- **Sharing means dragging a pane across.** The pane changes side visibly, and the change is logged.
- Secure panes always open on your side, locked.
- The terminal is always on your side. You share output deliberately (select and send, or a pipe through baish later). The agent can still propose commands, because a proposal reveals nothing.

This deletes the visibility chip, the target-group concept and most of the pins, and every state left makes sense.

## What is worth keeping

- **Secure tool and secure entry.** The value goes to your pane and the agent gets a fixed ack or a handle. The demos work, and the leak check makes the guarantee testable.
- **Plugins vibed on the spot:** the sandbox, the bridge, grants asked once, keep or discard. This felt right in the live runs.
- **The review loop:** threads anchored to text, suggestions you accept, and anchors that survive outside edits. It fixes the original review pain.
- **Sheets as the shared-data surface,** with view-only sort and filter and stale-edit refusal.
- **Under the hood:**
  - the core polices every tool, so the agent loop can be swapped
  - the agent never steals focus
  - the action log records denied actions too

## What a better frame might look like (hypotheses to mock, not decisions)

1. **Home is the command centre.**
   - Spaces as tiles, each with what needs you, what's running (the agent, workers) and what changed since you last looked.
   - A global inbox fed by quick capture.
   - You resume a space from here.
2. **A space is a workbench with a journal.**
   - A rail with the space's items and tasks.
   - Your side and the shared side (place is permission).
   - The journal as the spine: your actions and the agent's are interleaved blocks. Each output is a block you open, accept or comment on. Chat is one kind of block, not the whole column.
3. **The agent is visible on the work, not just in chat.**
   - Presence in shared panes: a highlighted row, a cursor in a doc, "reading welcome.md".
   - A follow mode while it walks you through something.
4. **Capture and triage are first-class.** The Monday dump becomes a triage table you accept line by line. It's the scenario's strongest moment and costs little to prototype.

## Recommended next step

**Stop adding features. Mock the frame first**, which is the spike I skipped.
- Make clickable mocks of three screens, using your real projects and notes as data:
  - **Home** (command centre)
  - **A space** (workbench, journal, the two sides)
  - **Monday triage**
- Make two variants for the layout question.
- Click through them, pick one, and only then rebuild the prototype's frame around it, reusing the mechanisms above.

Separately, I'll make three small fixes in the current prototype now, if you want, so it stops being misleading while you test:
1. Remove the private-pane count from the turn context.
2. Remove agent terminal reads (`term_read` and the excerpt), keeping propose.
3. Exclude private panes from presets and undo.

## Questions for you

These decide the mocks:

1. **What did you picture when you opened it?** If the first screen had been right, what would it have shown?
2. **Command centre:** is it an overview you glance at (spaces, needs-you, running work), or the place you work all day?
3. **Chat:** do you want chat as the main channel to the agent, or should the agent be mostly ambient on the work, with chat secondary?
4. **Organization:** is capture, triage and resume what's missing, or is something else bigger?

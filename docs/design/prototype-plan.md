# UX prototype plan (2026-11-06; built: see `prototypes/ux/`)

**Purpose:** feel how the app works day to day. **It tests the UI/UX only.** Every backend choice below is the simplest thing that makes the UX real, and none of them is settled.

## In scope

| # | Feature | What you can do | Must-have? |
|---|---|---|---|
| 1 | **Dynamic layout** | a split tree of tab groups; you drag, split and close. The agent has tools to open, split, move, fill and close panes, and it targets one group (opening there in the background with a badge). Layouts save per space. | yes |
| 2 | **Secure channel for sensitive data** | **(a) secure input:** you type a value and the agent gets a handle. **(b) secure tool:** you ask the agent for something sensitive, its tool fetches it, the value goes straight to your pane and the model gets a fixed ack, never the value. **(c)** a per-pane visibility label, with an "agent sees" chip on every pane. | yes |
| 3 | **First-class plugin: Sheets** (a local Google-Sheets-style CSV editor) | you edit cells, sort and filter; the agent reads and edits the same file. Sort and filter are view-only and the file is the source of truth. *Discord was deferred: an app can't post as you, and a bot only sees servers where you can add it.* | yes |
| 4 | **Vibe a plugin on the spot** | ask "make me a pane that does X". The agent writes the plugin's manifest plus HTML/JS. It runs in a sandboxed iframe with a default-deny CSP, talks through a `postMessage` bridge (tools/call, update context) and asks you once for each permission. It hot-reloads, and you can keep it or throw it away. | yes |
| 5 | **Review pane** | renders Markdown or sandboxed HTML. You select text to start a comment thread, and the agent replies or proposes edits that you accept or reject. Comments go in sidecar files. | yes (it fixes today's review pain) |
| 6 | Terminal | xterm.js on a pty. You drive it; the agent proposes commands. | likely |
| 7 | Follow-along browser | the agent's links open in a native webview window grouped by space. | stretch |
| 8 | Spaces and quick capture | a few spaces, a capture hotkey, triage of captured items | stretch |
| 9 | Action log view | what both of you did, per space | stretch |

## Placeholders (explicitly not decisions)

- **Agent loop:** pi in `--mode rpc` behind an agent-backend interface, forked or modified if it gets in the way. Our own Go or Rust loop stays open.
- **Core:** one local Go process. The shell talks to it only through a websocket contract, so where it runs stays open.
- **Shell:** Wails, chosen for speed with a Go core. Because the frontend only talks to the contract, it can be swapped for Tauri.
- **Tools:** called in-process behind an interface shaped like a node, so the boundary we design for in the full app isn't contradicted.
- **Not covered:** storage, sync, the trust model, sensitive agents, remote agents, extension tiers 2a and 3.
- **Evidence:** the POC is used as evidence and reference (the callback ack, rev-based editors, pi RPC), not as a code base.

## Milestones (each ends with a 10-minute "try this" script)

- **M0:** shell, layout engine, chat with pi, and the agent's layout tools. *Try:* "set up a research layout for X", then rearrange it yourself.
- **M1:** review pane and terminal. *Try:* review this workstream's documents in it, with me answering your comments.
- **M2:** secure channel and visibility labels. *Try:* hand over a secret and check that the transcript only has the handle.
- **M3:** the first-class plugin.
- **M4:** vibing a plugin. *Try:* make a widget on the spot, approve its permissions, keep it.
- **Then:** stretch items, in whatever order the earlier milestones suggest.

## How you use it

- `make dev` launches the app. On WSL it runs through WSLg; macOS comes later, Windows-native isn't planned for the prototype.
- Use it in place of a herdr pane for reading foreman reports and design documents (M1 onward).
- Give feedback as comments in the review pane. I read the sidecar files and reply there. That way the prototype gets tested on its own feedback loop.

## Test and demo plans for the non-obvious features

Each one has a **demo** (what you do) and **pass** criteria. Security checks plant a **canary**, a unique fake secret like `CANARY-7f3a`, and then grep every place it could leak: the pi session/transcript, the core's model-request log, the action log, the comment files. *Leak check* below means "the canary appears in none of those".

### Layout driven by the agent
- **Demo:** "Set up a layout for reviewing prototype-plan.md with a terminal." While it works, keep typing in another pane.
- **Pass:**
  - It opens panes in its target group with a badge, and your focus never moves.
  - Undo reverts its layout change in one step.
  - Closing a pane it opened works the same as closing one you opened.
  - Switching spaces and back restores the layout.
- **Negative:** ask it to close a pane you've pinned. It has to ask first.

### Secure input (2a)
- **Demo:** "I need to give you my homelab API token." A secret-entry pane opens; type the canary.
- **Pass:** the agent confirms it has a handle and can use it in a tool call (a mock API echoes back "auth ok"). Leak check passes.

### Secure tool (2b)
- **Setup:** a test vault file holding fake items: a Wi-Fi password, a passport number, a recovery code, all canaries. A secure MCP tool reads it.
- **Demo:** "What's the Wi-Fi password?"
- **Pass:**
  - The value appears in a pane addressed to you, with a copy button and a timeout before it hides.
  - The agent replies only with the delivery ack, something like "delivered to your pane".
  - Leak check passes.
- **Negative checks:**
  1. Ask "now read it back to me in chat". It can't, because it never had the value.
  2. Make the pane "agent can see" and ask again. The value now enters context only through that explicit, logged change.
  3. Ask "summarize everything in my vault". It gets item names (or handles), never values.

### Visibility labels (2c)
- **Demo:** open a Markdown note, set it to "agent can't see", then ask "what's in my open panes?"
- **Pass:**
  - That pane is listed by title only, or not at all, and its content isn't in the model request log.
  - The chip shows the state at a glance.

### Sheets plugin
- **Demo:** open `pantry.csv` from Files, edit a cell, then click **Best before** to sort. Ask "what's about to expire this month? Sort so those are on top."
- **Pass:**
  - Your edit lands in the file.
  - The agent's edit appears live, with a flash.
  - The agent's sort shows up as a change to the view (the header arrow), not as an edit to the file.
- **Stale check:** an edit made against an old revision is refused with a "reload" banner instead of overwriting.
- **Deferred:** Discord. An app can't post as you, and a bot only sees servers where you can add it.

### Plugin vibed on the spot
- **Demo:** "Make me a pane that shows my homelab's disk usage as bars", with a mock endpoint providing the data.
- **Pass:**
  - The pane appears, and you're asked once to grant `connect: homelab-mock`.
  - It reloads live when you ask for changes.
  - "Keep it" saves it to the plugin list, where it survives a restart.
- **Negative checks:**
  1. Have the plugin try `fetch` to an unlisted host. The CSP blocks it.
  2. Have it call a tool it didn't declare. The bridge refuses.
  3. Have it read another pane's content. It can't.

  Each refusal shows up in the action log.

### Review pane
- **Demo:** open `where-we-are.md`, select a sentence, comment "too vague, give an example".
- **Pass:**
  - The agent replies in the thread.
  - Its edit arrives as a suggestion. Accept it and the file changes; reject it and the file stays as it was.
  - Edit the file outside the app: the comment stays anchored, or is flagged as orphaned.
  - Opening an HTML file renders it sandboxed.

### Terminal
- **Demo:** "Check why the build fails."
- **Pass:**
  - Its command appears as a proposal in your terminal and runs only when you press Enter.
  - The output streams back into its context, and the action log shows who ran what.

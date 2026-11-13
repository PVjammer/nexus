# Nexus UX prototype

A working prototype of the **experience**: you and an agent side by side on the same panes. It tests UI/UX only. **Every backend choice here is a placeholder, not a decision** (see [the plan](../../docs/design/prototype-plan.md)).

## Run it

```bash
cd projects/nexus/prototypes/ux
make deps          # once: web dependencies
make dev           # core on 127.0.0.1:8740; prints a URL ending in #token=…
```

For the **native window**, install the GTK and WebKit headers once, then:

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
make app           # builds the shell, starts the core if needed, opens the window
```

Until then, `make dev` and opening the printed URL in any browser gives you the same UI. On WSL, a Windows browser reaches `127.0.0.1` too.

- **Model:** pi's default, which is your homelab `spark/Qwen3.8-27B`. To use another: `make dev CORE_FLAGS='-model ollama/qwen3-64k -root docs=../../docs'`.
- **Roots:** the file browser and review pane can open `workspace/` (sample files, seeded on first run) and `docs/` (this repo's design docs). Add more with `-root name=path`.
- **Start fresh:** `make reset` deletes `~/.local/share/nexus-ux`.

## What's in it

| Feature | Where to look |
|---|---|
| **Layout the agent can arrange** | Split tree of tab groups. Drag a tab onto an edge to split; drag dividers to resize. The agent opens things in *its* group (marked with the agent icon) in the background with a badge, and never moves your focus. **Undo agent** reverts its last layout change; **Arrange** applies presets. Pinned tabs make the agent ask first. |
| **Secure channel** | (a) **Secure tool:** "what's the Wi-Fi password?" The value goes straight to a secure pane and the agent gets only `DATA_DELIVERED_OUT_OF_BAND`. (b) **Secure entry:** the agent asks for a token, you type it into a secure pane, and it gets a handle like `secret:ab12cd34` to use through the broker. (c) **Visibility chip** on every pane: *agent sees this*, *title only* or *private*. Making a secure pane visible needs a confirmation and is logged. |
| **Built-in plugin: Sheets** | CSV files you and the agent both edit: click a cell to edit, click a header to sort, type to filter. Sort and filter are view-only; the file is the source of truth. Agent edits flash, and stale edits are refused with a reload banner. |
| **Plugins on the spot** | "Make me a pane that shows my homelab's disk usage as bars." The agent writes one HTML file. It runs in a sandboxed iframe with no network (CSP), reaches data only through `window.nexus` and only for declared permissions you grant, and hot-reloads. **Keep** it or it disappears on restart. |
| **Review pane** | Markdown or text: select text, then **Comment**, with *ask agent* to have it reply in the thread. Its edits arrive as suggestions you **Accept** or **Reject**. Anchors survive edits made outside the app, or are flagged as orphaned. HTML renders sandboxed (no comments on HTML yet). |
| **Terminal** | Your shell. The agent can only **propose** a command (Run / Edit / Dismiss) and read the output if the pane is visible to it. |
| **Also** | Follow-along browser pane (some sites refuse to frame; use ↗), spaces (top bar), a files pane, and the **action log** (who did what, including everything that was blocked). |

## How the agent is wired

- **pi** runs as `pi --mode rpc` with its built-in tools **off**. It loads `pi/nexus-tools.ts`, which fetches the tool list from the core and proxies every call back.
- **The core defines and polices all tools** (`internal/app/tools.go`), so swapping the agent loop changes nothing else.
- **Each user message is prefixed with a `<nexus-context>` block** describing what's on screen. It's built only from panes the agent is allowed to see.
- **Seams to watch:** `internal/agent` (the agent-loop interface), the websocket contract (`internal/app/client.go`, `web/src/core.ts`), and the tool list.

## Tests

- **`make test`** runs Go tests for the invariants: the secure tool never returns the value, handles work through the broker, the agent never takes your focus, undo, pinned panes, visibility, the suggest/accept flow, and plugin permission validation.
- **`make e2e`** runs 39 checks against a live core, acting as both the UI and the agent. It finishes with a leak check: the test vault holds fake `CANARY-…` values, and the check confirms none of them reaches the action log, state, comment files or the agent wire log.
- **Leak check by hand:** `grep -r CANARY ~/.local/share/nexus-ux` should find nothing. That covers pi's session transcript in `pi-sessions/` and every line exchanged with pi in `agent-wire.log`.

The demo scripts for each feature are in [prototype-plan.md](../../docs/design/prototype-plan.md#test-and-demo-plans-for-the-non-obvious-features).

## Known gaps (prototype)

- **macOS:** builds the same way (WKWebView) but hasn't been tried. Windows-native isn't planned; use WSL.
- **Undo** covers the agent's layout changes, not its edits to file contents; edits are in the action log.
- **Comments** don't work on HTML documents yet.
- **The browser pane** can't show sites that refuse framing.
- **Secrets** live in the core's memory for the session only.
- **No sensitive agents, no remote agent, no trust model.** All deferred by design.

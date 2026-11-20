# One core, many frontends

*Workstream core-decouple, 2026-11-20. **Concepts, not built.** It answers "what does a backend look like that a desktop GUI, a web app, a TUI, a CLI, a Hyprland HUD and mobile can all sit on?" It follows [where-we-are § Architecture](where-we-are.md#architecture-direction-not-decided), which already says the desktop shell is one client of the core among several. The research behind it, with file:line evidence from `prototypes/desk`, is in `.foreman/ws/core-decouple/findings.md`.*

## The idea

- **The core is the app. A frontend is a view of it.** Anything that is a record, a decision or a policy lives in the core. A frontend renders, arranges and supplies what its device can do: keys, notifications, a clipboard, a terminal.
- **One core, partial frontends.** No frontend has to use everything the core can do. A TUI drops HTML widgets, a HUD drops almost everything, and both are still first-class clients of the same core. What a frontend can't do, it says so through its capabilities, and the core routes around it.
- **Placement stays configuration.** Nothing here assumes the core runs on the same machine as any frontend.

```
  Desktop GUI   Web   TUI   CLI   HUD   Mobile        ← frontends: render · arrange · input
        └──────────┴─────┴─────┴─────┴──────┘
          one protocol: hello · subscribe · command · answer · attend
  ┌──────────────────────────────────────────────────────────────────┐
  │ CORE  entities · commands · policy · event log · read models     │
  │       agent runtimes (behind the AgentBackend seam) call the     │
  │       same commands, as tools                                    │
  └──────────────────────────────────────────────────────────────────┘
          nodes: your machines — terminal, files, open a URL, notify
```

## The concepts

| Concept | What it is | Owner |
|---|---|---|
| **Entities** | Projects, workstreams, work items, captures, tasks, documents (reports, wiki pages, notes, sheets), threads, episodes, widgets. Each has a stable ref (`task:t4`, `doc:homelab/wiki/plan`). | Core |
| **Commands** | The only way anything changes. Frontends and agents (as tools) use the same commands, so there is one policy path. Every command carries its actor: you, an agent or a plugin. | Core |
| **Event log** | Every command produces events. Frontends subscribe to them. History, the journal and "what happened" are queries over it. | Core |
| **Read models** | Views the core computes and frontends only display: needs-you, running, journal, project summary, review anchors resolved to ranges. No frontend recomputes them, so two frontends never disagree. | Core |
| **Policy** | Agent scopes, the private-project wall, visibility, plugin grants, the secure channel. Enforced on entities and commands, never on what's drawn. | Core |
| **Asks** | One primitive for "the user decides". Examples: a filing to confirm, a wiki proposal, an edit suggestion, a permission, a secret to enter, a command to run, a task to start. An ask names the capability it needs, and any session that has it can answer. **Needs-you is the list of open asks.** | Core |
| **Sessions** | One connected frontend. It has an identity, a scoped and revocable token, and declared **capabilities**. | Core tracks; frontend declares |
| **Attention** | What you're looking at, reported by each session: workspace, ref, selection. The agent's turn context uses the most recently active session's attention. The quick window's "ask from where you are" is this idea already. | Frontend reports; core uses |
| **Surfaces** | What's open in a workspace (home, project or workstream). Each entry has a ref, a visibility (full, title, none), an opener and a badge. Items the agent opens land in an **agent tray**. Surfaces have no geometry. | Core |
| **Arrangement** | Split trees, tabs, floating windows, zoom, or one column. The core may store it as an opaque blob for the frontend but never reads it. | Frontend |
| **Nodes** | Things that happen on a device: your terminal, your files, opening a URL, notifications. They belong to the machine you're at, not to wherever the core runs. | Node or frontend |

### Capabilities

| Capability | Means the session can… | Without it |
|---|---|---|
| `html` | render agent HTML and HTML documents in a sandbox the core serves | shows sanitized text or source |
| `plugins` | run sandboxed plugins and widgets | lists them with "open in the desktop app" |
| `secure-display` | show a vault value privately | secrets are delivered to another session, or held until one connects |
| `secure-entry` | take a secret without echoing it | the ask waits for a session that can |
| `terminal` | host or reach your shell, so it can run a proposed command | command proposals show as text to copy |
| `notify` | raise a notification on your device | — |

## Three rules

1. **The agent sees entities, never pixels.** Its context comes from attention, surfaces and policy. Four panes on the desktop and one item in a TUI give the agent the same picture of the same work. Pane visibility becomes surface visibility. It is still enforced in the core, and the selection is checked against the item it came from.
2. **Sensitive things are routed, never broadcast.** A secret goes to one session that has `secure-display`: the one you're attending from, or one you pick. It is never re-sent to other sessions. The same applies to secure entry and command proposals.
3. **Sandboxes come from the core.** Plugin and HTML documents are served by the core from an isolated origin with CSP headers, the way `/raw` already serves HTML review. A frontend that can't honour that doesn't render them. The core never relies on a frontend to build the sandbox correctly.

## The protocol, in five verbs

- `hello {kind, version, capabilities}` → what this session's token allows.
- `subscribe {scope}` (`home`, `project:X`, `workstream:Y`, `doc:…`) → a snapshot, then events.
- `command {name, args, rid}` → a result, or an error with a code. Agents call the same commands as tools.
- `answer {ask, …}` → resolves an ask.
- `attend {workspace, ref?, selection?}` → updates what you're looking at.

Everything a frontend shows is either a subscription or a command result. Nothing is inferred from another client's state.

## How the frontends map

| Frontend | Arrangement | Attention from | Capabilities | Leaves out |
|---|---|---|---|---|
| Desktop GUI | split tree, floats | focused pane plus selection | all | — |
| Web app | tabs or one column | the current page | `html`, `plugins`, `secure-entry` | your terminal, unless a node is attached |
| TUI | list, detail, chat | the selected item and marked lines | `secure-entry`, `secure-display` (a separate pane or a retrieval command), `terminal` (through your tmux) | plugins, HTML, Eitri |
| CLI | none | the command's arguments (`nexus ask homelab …`) | `secure-entry` | nearly everything visual |
| Hyprland HUD | a fixed strip | none (home) | `notify` | secure features, plugins |
| Mobile | a stack of screens | the current screen | `secure-entry`, `notify`, maybe `plugins` | the terminal |

### Terminal frontends and security

- **Secure view (direction set 2026-11-20; details are for the build workstream):** the TUI can show a secret.
  - **Two options:**
    - In a separate pane of its own.
    - By giving you a shell command that fetches the value from the core over your session, so it never passes through the TUI's screen.
  - **The constraints still apply:**
    - Keep the value out of scrollback and tmux history (use the alternate screen).
    - Mask it until you ask, and clear it when you dismiss it.
    - Be careful when tmux `pipe-pane` logging is on or the screen is being shared.
    - The core delivers the value only to the session that asked.
- **Secure entry:** read from the tty with echo off, like `sudo` does. This is safe in any terminal.
- **Command proposals:** the TUI is a pane in your tmux, not a terminal host. You pick one tmux pane as your shell. When you press a key, the TUI types the proposal there without Enter. Nexus never reads panes (no `capture-pane`). Only the TUI process, acting on your key, writes to your terminal. The core never does.

## The gap from `prototypes/desk` (2026-11-20)

- **Already this shape:**
  - The entities, and policy in the core, including the private-project wall.
  - The action log.
  - Agent tools, which act as commands.
  - A browser-free client (`scripts/e2e.mjs`) that drives every feature.
- **Present, not yet named as concepts:**
  - Asks are about seven separate mechanisms: `permPrompt`, `secretCard`, filing proposals, wiki proposals, review suggestions, term proposals and task drafts.
  - Attention exists only in the quick window.
- **Missing:**
  - **Sessions:** today there is one shared token, sent in query strings, and no capabilities.
  - **Routed secrets:** `secureValue` and `secureCard` are broadcast to every client and re-sent on reconnect.
  - **Surfaces vs arrangement:** the core stores the desktop split tree, and focus is shared across windows.
  - **Typed commands and subscriptions:** today the client sends ad-hoc `t:` messages and the core pushes full `state`/`desk` snapshots.
  - **Read models for everything:** the journal and review anchors are still computed in the web client.
  - **Core-served sandboxes:** plugin CSP is set by the web client.
  - **Nodes:** the terminal, `openExternal` and project discovery run on the core host.

**Order of work.**
1. Sessions with capabilities, and routed secrets. Both are small and make a second client safe.
2. Attention.
3. Typed commands, plus a small client library grown from `e2e.mjs`.
4. Splitting surfaces from arrangement. This is the largest change, and best done once a second frontend shows what it needs.

## A TUI spike (proposed)

**Why:** the TUI is the cheapest way to test this design. `e2e.mjs` already shows that a non-browser client can drive the wire. It doesn't show whether the concepts hold: whether a frontend with no panes gives the agent the right context, gets its asks, and never sees a secret it shouldn't.

**Shape:**
- `cmd/nexus-tui` in `prototypes/desk`, against the **same core**.
- It goes through the five verbs, added to the core in their smallest form next to the existing messages.
- It never sends layout messages.

**In:**
- Home: needs-you as asks, running, the inbox.
- Capture.
- Chat with the home, project and workstream agents.
- Work items, dispatch and a task's plan.
- Reading reports, with review threads.
- Command proposals sent to your tmux pane.
- Secure entry, and secure view (a separate pane or a retrieval command; the build workstream decides which).

**Out:**
- Eitri and widgets/plugins.
- HTML panes and the browser.
- Sheet editing.
- Arrangement.

**Done when:**
- A project agent answers about the report you're reading in the TUI using `attend` alone, with no pane opened.
- Two sessions (the desktop and the TUI) don't fight over the agent's context.
- An e2e check shows that only the session that asked for a vault value receives it on the wire; no other session does.
- The list of desk features that needed a web-only path is short, and written down.

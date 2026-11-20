# Where we are (2026-11-05 checkpoint, direction updated 2026-11-12)

*The single current summary. It supersedes anything in the other documents that disagrees with it. Short on purpose.*

## What we're building, in one paragraph

A **self-hosted, native desktop app** (Linux and Mac, Windows via WSL; where the core, agents and models run is configuration) where you and AI agents work side by side on the same visible things. It covers software projects, the homelab, and life (trips, renovation, gifts, home and family todos, research). The aim isn't autonomy. **You stay you, the agent stays the agent.** The app removes the friction of handing work back and forth and the opacity of what the agent did. It is neither OpenClaw (an agent running your life) nor Clippy (AI in everything).

## Direction since the prototype review (2026-11-12)

*This supersedes the layout model below wherever they disagree. Background: `design-review.md` and `the-split.md`.*

- **A desktop for doing project work with AI.** You plan and execute work here, with little need to switch away. It should work for any job and any project done at a desk. Most projects aren't code: much of the work is curating information, managing communication and decomposing tasks.
- **The landing page is a dashboard.**
  - On first boot it's a blank canvas with a chat. You use the chat to onboard projects and build the dashboard.
  - A fixed core is always there: needs you, running, inbox, project cards. Everything else is widgets.
- **Three agent tiers: home agent → project → task.** ("Chief of staff" was a working name and is **not** staying; the name is open.)
  - The **home agent** is global. It files captures, retrieves information and keeps the overview. **It has full access across projects** (corrected 2026-11-12: no isolation between it and projects). It just **isn't the way you work in a project**: to do project work, you go into the project.
  - Below it, agents can be **dispatched as ephemeral agents** with a brief, and they return a report (foreman-style).
  - Open: does the project tier speak, or is it knowledge plus a router?
- **The home agent outside the app** is the cloud version of the agent, reached through hotkeys and toasts. **No local computer use.**
- **Agents work in their own environment, and reach your machine only through scoped grants via the node.**
  - Their environment can be a container or VM on your machine, the homelab or the cloud.
  - You can step into theirs: its terminal, files and previews open as panes. **Every pane shows which machine it's on.**
  - It's "VS Code Remote", not VNC: structured panes, not pixels.
- **One shared system by default, not a machine per task.**
  - Each task gets its own **worktree**.
  - Grants are made **once per project** and inherited, and they only narrow going down the tiers.
  - Full isolation is a per-task setting.
  - **Goal: in a normal week you approve almost nothing.**
- **Code without round trips through a git server.**
  - Local sandbox: the worktree is on your disk.
  - Cloud: you test inside its environment, or Nexus fetches the branch directly.
  - Either way the work comes back as a branch you review.
- **Works fully on one computer** (a rootless container or a VM on the host, using the same contract as the remote setup). This is required for testing.
- **The shared sandbox is a separate exploration after the UX.** The UX must work with or without it.
- **Pain points of the split** and a natural fix for each: `the-split.md`.
- **Built as `prototypes/desk`** (2026-11-12): the home dashboard and home agent, onboarding, capture and filing, project rails over the real foreman ledgers, and dispatch to task agents with reports. Tried live on the user's projects.

## Settled by you

- Native app, not a browser tab. Cross-platform. An Omarchy/Hyprland version is optional, not the foundation.
- **Stays local.** That includes reviewing what the agent produces.
- A general tool, not only project management. Don't overtune it.
- Separate **work** and **personal** instances.
- Layout like herdr/tmux in UI form, tabbed like VS Code. Example: chat or terminal on the left, one of several apps on the right.
- The browser pane is for **following along** (the agent's links open right there, tabs are grouped by context). No logins needed.
- Editor and browser tabs come before Slack.
- No ambient listening or indexing (calls, texts). **You choose** what the agent sees.
- Other repo projects (foreman, etc.) inform the design without constraining it.
- The POC is evidence only. Nothing carries forward by default.
- **Scheduled jobs (2026-11-20, [`running-docs.md`](running-docs.md) §3A):** a run writes its own versioned output slot directly; one run may start another only through a chain configured in the spec you kept, never by an agent's decision; nothing goes beyond propose-only (no auto-sending, no auto-closing).

## Principles (current)

1. **Pair, don't delegate.** The agent drafts, offers options and annotates; you decide, and decisions are recorded as yours.
2. **The five tests** for every feature:
   - no box-out
   - direct action
   - handoff without copy-paste, both ways
   - you choose what it sees
   - review is a first-class loop
3. **Context sticks to work, not windows.** Tabs, notes and sources belong to a space or task; resuming one restores them.
4. **Capture instantly, organize later.**
5. **The app pulls information in and pushes rarely** (interrupt budget).
6. **Sensitive data stays on the data plane, and the node/lock boundary holds** (PA design §8/§12, unchanged).

## The model

- **Instance** (work / personal) → **Spaces** (any context) → **Items** (notes, tasks, questions; linked across spaces, never copied).
- **Apps** open as tabs: chat, terminal, browser, editor/Markdown, **review**, diff, table, diagram. Project features (workers, MR review) are apps a space turns on.
- **Layout:** a split tree of tab groups, saved per space; one group is the agent's target, and it opens there in the background with a badge.
- **Event log:** a record of every action by either side. History, the review and "what happened" are views of it.
- **Agents** carry permission manifests. **Sensitive agents** (opt-in, one per domain, ephemeral, no way out, propose-only) are how email-like sources come in safely.
- **Extensions:** 1 the app · 2a reviewed and hash-pinned · 2b written on the spot (sandboxed, approval for each permission) · 3 declarative widgets. All share the manifest, broker and labels.

## Architecture (direction, not decided)

*Revised 2026-11-06. **Rule: placement is configuration, not design.** Nothing in the UX work may assume where the core, an agent loop or a model runs. The user's goal is flexibility: don't box in or out.*

- **Three placeable parts:**
  - the **core**: store, event log, broker, scheduler
  - **agent runtimes**: the loop
  - **model endpoints**

  Each can run on the desktop, on a homelab server or with a provider. The PA v0.3 target of a hosted core with nodes is one configuration; a fully local one (the prototype) is another.
- **Agent runtimes are pluggable:**
  - our own loop
  - an external harness, such as `claude -p` inside agentbox (the loop is theirs)
  - later, an always-on homelab agent in the Hermes style, reachable over Slack or Discord

  This is the AgentBackend seam from PA §16.
- **Two roles (added 2026-11-06):**
  - The **pair agent** lives in the app. It needs pane context, suggestions, events in the action log and per-call trust routing, so it should be our own loop or one built on a hackable harness (pi first, as in the POC; our own loop on ai-sdk-go if pi gets in the way).
  - **Workers** do bounded background tasks: Claude Code (`claude -p` or the Agent SDK in agentbox), Codex, pi. Each takes a brief and returns a report, foreman-style. They can run locally or on a remote worker platform.
- **Local tools always go through a node, even when the agent runs on the same machine.** The boundary is then identical wherever the loop runs, so moving to a remote agent later is a change of configuration, not of design. It also keeps §8 intact.
- **Trust model (sketch, to be designed later):**
  - Each **endpoint** declares who operates it and what it is trusted with. Examples: `self:desktop`, `self:homelab`, `org:private-cluster`, `provider:anthropic`.
  - Each **space or domain** declares what trust it requires.
  - Routing picks an endpoint that satisfies the requirement.

  Your setup and a company's private cluster or dedicated Azure endpoint are then just different policies. It is the confidentiality half of the FIDES labels.
- **"Stays local" means no third-party app or document services**, such as Claude Docs. Model endpoints are a separate question, governed by the trust model.
- **The desktop shell** is a client of the core: Tauri is the leading option, Wails the Go alternative. Other clients (a Hyprland HUD, mobile) use the same contract. The concepts for that contract (sessions and capabilities, attention, surfaces vs arrangement, asks) are in [`frontends.md`](frontends.md).

### Placement assumptions found and neutralized

| Where | What it said | Now |
|---|---|---|
| `sensitive-agents.md` manifest | `model: local-only` | `model: requires trust ≥ <domain policy>`; local is one way to satisfy it |
| `scenario-week.md` space table | "local model only" for reno and home | read as "trusted endpoint only" |
| this file, earlier revision | "core hosted in the homelab" | one configuration, not the design |
| this file, earlier revision | "agent never types in your terminal" | a node policy that depends on the lock level, not on placement |
| PA §16 | local core for the prototype | fine, it is already framed as configuration |

**Deferred, not decided:**
- The remote-agent shape: one always-on agent that borrows your node while you're in the app, or a multi-agent hand-off between a remote agent and a local session.
- When a node is offline, work waits in a "needs your machine" queue instead of failing.

## Withdrawn or superseded (so they don't resurface)

- "Catalog widgets first". It is now tier 3 only.
- Standing agents and pre-approved autonomy levels.
- Money/budgets as a requirement (I invented it in the scenario).
- The browser as the biggest risk (true only if it had to be your daily browser).
- Slack as the first integration.
- Publishing to Claude Docs, because of the stay-local rule.

## Open, ranked

1. **The local review surface**: what it is and whether it comes first (see the path forward below).
2. **Shell choice:** Tauri vs Wails (decides where Rust appears).
3. **Main layout:** workbench + journal (my lean) vs the alternatives. Best answered with mocks.
4. **Mobile** (B11) and **steps outside the app** (B8). Both are unplanned.
5. The workstream engine: foreman behind an interface, or foreman 2.0.
6. **Trust model** and the **remote-agent shape** (deferred; see Architecture).
6b. **Sandbox and credential mechanisms** (parked 2026-11-12): secure share, placeholders vs capabilities, environment backends. Lean: wrap OpenShell where possible. See `the-split.md`, "Mechanisms discussed".
7. **A name** (the "PA" label no longer fits).

## Document map

| File | What it holds | Read? |
|---|---|---|
| `where-we-are.md` | this summary | start here |
| `design-review.md` | 2026-11-12 review of the UX prototype: what misses, "place is permission", next step is mocks | **read next** |
| `the-split.md` | pain points of the agent environment vs your machine (.env, CLIs, files in and out, prompt fatigue) with fixes | when designing access |
| `design-candidates.md` | 4 layout candidates and the shared foundation | when choosing a layout |
| `scenario-week.md` | the one-week walkthrough, 14 break points with plan status, the five tests | skim the tables |
| `sensitive-agents.md` | the sensitive-agent design, leak paths and a spike | when we get to email |
| `frontends.md` | one core, many frontends (GUI, web, TUI, CLI, HUD, mobile): the concepts, the gap from desk, a TUI spike | when adding a client |
| `running-docs.md` | running docs and nightly jobs: the building blocks (workflow spec + `on:` / `output:` / `delivery:`), the stance on unattended runs, a build order | when building triggers or workflows |
| `report.md` §a–c | POC inventory, related projects, prior art | reference |
| `report.md` §d–f | older revisions of the questions, spikes and extension tiers | history |

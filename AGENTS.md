# AGENTS.md (nexus)

Guidance for agents working in this repo. It is meant to become its own foreman project (`foreman init` at this root).

- **Read first:** `docs/design/where-we-are.md` (current design), then `docs/design/prototype-plan.md` (what the prototype tests).
- **The prototype tests UX only.** Don't treat backend choices in `prototypes/ux` as settled. They are placeholders: pi as the loop, a local Go core, a JSON-file store, a webview shell. Don't design around them.
- **Placement is configuration.** Never assume the core, an agent loop or a model must run on the device.
- **Security invariants (don't loosen):**
  - Secret values never reach the agent, the action log, pane props or the agent wire. Secure tools return only `DATA_DELIVERED_OUT_OF_BAND`.
  - The agent reaches files and terminals only through core tools. pi's built-in tools stay off by default.
  - The agent proposes terminal commands; it never types into your terminal.
  - Plugins run sandboxed (CSP `default-src 'none'`) and get data only through the bridge, for declared and granted permissions.
  - Pane visibility is enforced in the core (turn context and tool reads), not in the UI.
  - These follow PA design §8/§12 (node/lock boundary) in `../personal-assistant/pa_design_doc.md`.
- **Prototypes:** `prototypes/desk` is current; `prototypes/ux` is the earlier frame.
- **Never write to the user's project folders from Nexus.** Agents get them read-only; Nexus's own state stays in its data directory.
- **Before you finish:** run `make test` and `make e2e` in the prototype you changed. The e2e leak check must stay green.
- **Go:** `go 1.22`. Web: vanilla TypeScript bundled with esbuild (`web/src`). No framework, on purpose.

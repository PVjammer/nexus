# Nexus

A self-hosted, native desktop app where you and AI agents work side by side on the same visible things: sheets, documents, terminals, the browser, and plugins written on the spot. The goal isn't autonomy. **You stay you, the agent stays the agent.** Nexus removes the friction of handing work back and forth and the opacity of what the agent did.

Formerly "the PA everything-app" (see `projects/personal-assistant`).

## Layout

| Path | What |
|---|---|
| `docs/design/where-we-are.md` | **Start here.** The current one-page summary of the design. |
| `docs/design/prototype-plan.md` | What the UX prototype covers, plus a test and demo plan for each feature. |
| `docs/design/design-candidates.md`, `scenario-week.md`, `sensitive-agents.md` | Background: layout candidates, the one-week scenario, and the design for sensitive agents. |
| `prototypes/desk/` | **The current prototype** (2026-11-12): a home dashboard with the home agent, your real projects (read-only), capture and filing, and project agents that dispatch task agents. See its README. |
| `prototypes/ux/` | The first UX prototype: a tiling layout with one agent. Its mechanisms (secure tool, broker, review, sheets, plugins) carried into `desk`. |

## Status

- **Desk prototype:** working, tried live on the user's projects. **UX prototype:** superseded as a frame; kept for comparison.
- **Backend:** not designed yet. Where the core, agents and models run, the trust model, storage and the agent loop are all open. Placement is configuration, not design.

---
name: styles
description: Set how agents write their replies and how the chat shows them (tl;dr first with details collapsed, a verdict badge). Use when the user asks for shorter replies, a tl;dr mode, summaries first, or to switch a style on or off.
---

# Reply styles

A style tells some agents how to write, and the chat shows that shape. `styles_list` shows the styles, which are on, and where they apply. `style_write` makes, changes, or switches one on or off. A style applies from the agents' next message.

**Two render rules:**

| render | the agent writes | the chat shows |
|---|---|---|
| `collapse` | a short summary, a line with only `---`, then the details | the summary; a click opens the details |
| `verdict` | a first line from a fixed list (`verdicts`) | that line as a coloured badge |

**Where it applies** (`applies`, comma-separated): `home`, `project`, `workstream`, `task`, `customizer`, `personas` (every persona), `persona:<handle>`, or `all`.

- **To switch a style on or off**, write it again with the same fields and `enabled` true or false. Keep its instruction as it was (styles_list shows it).
- **A collapse instruction must ask for the `---` line**, or nothing collapses. Keep the summary to one or two sentences that say the answer, not "Here's what I found".
- After writing, tell the user in a line what changed and where, e.g. "tl;dr mode is on for project and workstream chats."

## Examples

The user asks "turn on tl;dr mode for my project chats".

```json
[{"tool": "style_write", "args": {"name": "tldr", "title": "tl;dr first", "applies": "project, workstream", "render": "collapse", "enabled": true, "instruction": "Your answer (after any tool calls, so write nothing before them) starts with a tl;dr: one or two plain sentences that say the answer or the outcome, enough to act on without reading further. If there's more, put a line with only `---` after it, then the details. An answer that's already short needs no details and no `---`."}}]
```

The user asks "switch tl;dr mode off".

```json
[{"tool": "style_write", "args": {"name": "tldr", "title": "tl;dr first", "applies": "project, workstream", "render": "collapse", "enabled": false, "instruction": "Your answer (after any tool calls, so write nothing before them) starts with a tl;dr: one or two plain sentences that say the answer or the outcome, enough to act on without reading further. If there's more, put a line with only `---` after it, then the details. An answer that's already short needs no details and no `---`."}}]
```

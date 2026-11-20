# Response styles

*Proposed 2026-11-20; steps 1 to 4 built the same day on branch `personas`. Not a fixed feature: a way to configure how agents write and how their messages render, e.g. a "tl;dr mode".*

## The idea

You configure a **style**: agents write in a shape, and the chat renders that shape. The first style is **tl;dr**: each reply leads with a short summary, the chat shows only that, and clicking the message expands the details.

Like a persona, a style is a file. It has three parts:

- **Where it applies:** every agent, some kinds (project chats, workstreams), or particular personas.
- **An instruction for the agent**, added to its prompt: "Start every reply with a one- or two-line summary, then a line with only `---`, then the details. Short replies need no details."
- **A render rule**, chosen from a small built-in set:
  - `collapse`: show the summary; click to expand the details.
  - `verdict`: a first line from a fixed list becomes a coloured badge. This suits the critic's "Don't do this." / "This has problems." / "This holds up."

  New rules are added in the app over time. Arbitrary code (plugin renderers in the sandbox) can come later if the built-in set runs out.

You make and change styles by asking home or Eitri: "make agent replies tl;dr-first in project chats". A `style_write` tool and skill do the work, like `persona_write`.

## Why a text convention, not JSON

A JSON reply (`{"summary": …, "details": …}`) is tempting but works badly in practice:

- **Streaming.** You can't render half a JSON object well. A text convention shows the summary as it arrives and collapses once the delimiter appears.
- **Small models.** Markdown inside JSON strings means escaped quotes and newlines. Local models break that often, and a broken object shows nothing.
- **The agent's own history.** Agents reread their past replies. Prose with a delimiter reads naturally; JSON makes every past turn noisier.
- **Graceful fallback.** A reply that ignores the convention renders as an ordinary message. Nothing is hidden or lost, and a broken format can never swallow a reply.

JSON is still right for typed answers the app acts on (proposals, question cards), and those already go through tools.

## Details

- **While streaming:** the summary shows as it arrives. Once the details start, they're collapsed behind "more…", with a hint that they're still growing.
- **Pins show the summary.** Clicking a pin expands it.
- **An "expand all" toggle** per chat, remembered per viewer.
- **Context bonus:** when one agent's reply is passed to another (an aside, a merge, the owner's turn after a join), the core can pass only the summary, plus "details: ask". That keeps small contexts small. See [Channels and mentions](channels-and-mentions.md), open question 2.

## Build order

1. The `collapse` rule and one tl;dr style, written by hand, on for project and workstream chats. See how it feels and whether the local models keep to it. Add an eval for it.
2. `style_write` and the skill, so you can make and change styles by asking.
3. The `verdict` rule, with the critic switched over to it.
4. Summary-only handoffs between agents.

*Built 2026-11-20.* Differences from the above:
- **The style is repeated after your message.** On local Qwen, a style note above the message was forgotten after a few tool calls (the tl;dr eval passed about one run in three). Repeating the instruction after your message made it pass every time.
- **The tl;dr goes on the final answer,** after any tool calls. The model was writing it in the preamble before them.
- **Handoffs:** asides and cross-posts carry the summary. The owner's turn after a join gets every reply in full, because it has to act on them.
- **tl;dr ships switched off.** Ask home to turn it on ("turn on tl;dr mode for my project chats"). The critic's verdict badge ships on.

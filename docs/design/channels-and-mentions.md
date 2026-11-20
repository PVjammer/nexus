# Channels and mentions

*Proposed 2026-11-20, for the next design review. Builds on [Workspaces and agents](workspaces-and-agents.md) and the personas POC (branch `personas`).*

## The problem

- **One run is three things.** A run is a conversation log (what you see), a model context (what the agent has been fed) and an identity (role, tools, which projects it can see). Most recent features move pieces of one conversation into another agent's context: the persona aside, "since your last turn", `ws_message` and its queue, `ws_transcript` and `ws_ask`, and episodes that clear the model while folding the chat.
- **Too many agents to talk to.** Home, the orchestrator, one agent per workstream, one per task, Eitri and personas each have their own chat. The point of orchestrators and workstreams is to *not* manage a hundred agents.
- **Asking two agents and acting on both takes three messages.** "@tm-agent, does this affect your work? @dickhead, any issues? Then draft it" means waiting for both replies and typing "OK, draft it".

## Decisions (proposed)

1. **A workspace's chat is its channel.** Home, each project and each workstream has one. A channel has **members** and **one owner**, the agent that answers by default: home's agent, the orchestrator or the workstream agent. Others speak only when @mentioned. Each member has a **cursor** into the log and its own context rule (persistent, episodic or one-off), so the log is kept for you while each agent's session clears on its own schedule.

2. **Workstreams are the lowest level you talk to.** Task agents are the workstream agent's subagents: it sends them off and reads their reports. You can view a task's transcript, but you don't @mention task agents.

3. **Mentioning an agent that has a session forks it, then merges back the result.** This is the turn-branching pattern from ai-sdk-go (`docs/design/turn-branching.md` there): the question and answer go on the trunk, and the scratch work stays on the side.
   - **A fork answers right away**, even while the trunk is mid-turn. It starts from the last finished turn plus the live tail of the current one, and it can only read.
   - **The reply posts in the channel where it was asked**, under the agent's name, and is cross-posted compactly into the agent's own channel ("asked in #nexus: … · answered: …").
   - **The exchange merges into the trunk after its current turn ends**, as an aside. Nothing is cancelled and no Interrupt is needed.
   - **Actions ride on the merge.** A fork never edits anything. If the request needs work ("@csv, also handle UTF-8"), the fork says what it will do and the trunk does it on its next turn. So one @mention covers both questions and instructions.
   - **Stale answers get flagged.** The merge says which turn the fork started from. If the trunk's finished turn changes the picture, it posts a correction where it was asked.
   - **Personas have no session**, so they answer as today: a one-off reader over the channel.

4. **Talking *to* an agent is remembered; asking *about* it isn't.** An @mention or `ws_ask` merges back. The quick window and personas reading a thread don't touch the agent's context.

5. **When you mention other agents, the owner waits for them (the join).** The core sends the message to every mentioned agent in parallel, verbatim. Under your message: "Waiting for @tm-agent, @dickhead… [Go without them]". When all have answered, the owner gets **one turn** with your message and every reply.
   - **The owner may pass.** If your message asked nothing of it ("@critic, thoughts?"), it replies `PASS`. The core hides that and shows a faint "Nexus agent: nothing to add", so a pass doesn't look like a failure.
   - **Feedback is information, not instructions.** After a join, the owner acts only on what you asked of it. It never starts fixing things a reply criticised unless you said to.
   - **Late replies don't start turns.** After a couple of minutes the owner goes ahead with what it has and says who's missing. A late reply lands with a chip: "@tm-agent replied late. [Update the draft]".

6. **Tagging the owner means "you run this".** `@nexus ask @A first, then @B with A's answer, then draft it`: the core sends nothing itself. The owner gets the message, always responds (no PASS), and runs the mentions in the order you asked.
   - Without the tag, a dependency still works, just more slowly: at the join the owner sees B answered without A's input, and can ask B a follow-up before acting.

7. **Agents mention each other through one tool, in rounds.** `ask_agents(rounds: [[A], [B]])`. The model states the plan once; the core runs round 1, then round 2 with round 1's replies included, then returns everything as the tool's result. Every message is posted in the channel, so you see the discussion. This is how the orchestrator talks to workstreams: `ws_ask` and `ws_message` become an @mention. Small local models slip on multi-step discipline, so they only have to get one tool call right.
   - **Limits:** one hop (an agent answering a mention can't mention others), and at most three rounds per turn before the owner comes back to you.

8. **Privacy is membership.** A private project's channels have no outside members. Personas called there run inside the wall, on the project's model. The argument guards and scrubbing stay as a backstop.

9. **Channels are the core's model, not the interface's.** The desk layout is one view of channels; a messaging-style app, or a Slack bridge, could be another. That requires:
   - **"Where am I" comes from the channel.** View and selection become optional extras that a desk client adds.
   - **Agents don't assume panes exist.** The turn context says what the client can show, and layout tools are offered only when it can show them.

## What changes from today

| Today | With channels |
|---|---|
| a chat per agent | a channel per workspace, with an owner and members |
| task agents have their own chats | they're the workstream agent's subagents; transcripts are viewable |
| `ws_ask` uses a fresh reader over the chat text | a fork of the agent, which remembers the exchange afterwards |
| `ws_message`, queue, Interrupt | an @mention; the merge waits for the turn |
| persona aside | the owner's turn after the join, or an aside |
| "since your last turn" | unread messages from the agent's cursor, plus repo changes as now |
| the private wall | channel membership, with guards as a backstop |
| episodes fold the chat | the log stays; each agent's context clears on its own |

## Use cases to check

Each one against the model: simpler, same or harder.

1. Checking on a workstream mid-turn ("where are we?"): **simpler** (a fork answers; the trunk keeps working)
2. Redirecting a workstream ("skip the tests for now"): **same** (actions ride on the merge, or you interrupt)
3. The orchestrator coordinating two workstreams: **simpler** (visible posts, not hidden messages)
4. Plan + "@A, @B, then draft it": **simpler** (one message, the join)
5. Ordered asks (A, then B, then act): **simpler with the tag**; without it, a follow-up round
6. Asking a persona about a workstream's work: **simpler** (it reads the channel)
7. Catching up after a day away: **unclear** (unread counts help; unread anxiety is a real risk)
8. An agent asking you a question: **same** (a card in the channel; Needs you collects them)
9. Agents disagreeing or looping: **harder** (needs the owner rule, one hop, round limits)
10. Long workstreams outgrowing the context: **harder** (cursors and summaries over a shared log)
11. Private projects: **simpler** (membership)
12. Closeout: **simpler** (a summary post, then the channel is archived)

## Build order

The personas POC already has one-off voices, the aside and the @ menu.

1. **The join, for your example message:** parallel fan-out, the owner's turn after it, PASS with the visible pass line, late-reply chips. Evals: pass vs act; don't act on unrequested feedback; tagging always gets a response.
2. **Fork-and-merge for workstreams:** @workstream in a project chat; the fork answers there; the merge after the trunk's turn; the cross-post. On pi first.
3. **Agent-initiated mentions:** the `ask_agents` rounds tool replaces `ws_ask` and `ws_message`. Evals: ordered vs parallel.
4. **The data model:** a Channel with members, cursors and authored messages. Each existing chat becomes a channel whose owner is its agent, with no visible change. Task agents move under their workstream.
5. **Threads** (backlog): a thread is a channel branch; forks and threads share plumbing.
6. **A messaging-style client**, once 4 holds.

Steps 1 to 3 work on today's per-agent chats, so the idea can be tried before the data model changes.

## Open questions

1. **Fork on Claude Code:** `--resume <id> --fork-session` should work. Confirm it, and how a fork behaves while the trunk is mid-turn.
2. **What the owner is fed after a join:** every reply in full, or summaries with the details on demand? Small contexts favour summaries. [Response styles](response-styles.md) would give each reply a tl;dr for free.
3. **Unread counts:** useful for catching up, or a source of Slack-style anxiety? Maybe Needs you stays the only badge.
4. **Personas with memory:** one-off is right for now. Would a per-channel memory make some personas better (a reviewer who remembers what it flagged last week)?
5. **Cost:** the join adds a short turn per mention message. Measure it on the local models before deciding whether a deterministic fast path is needed for persona-only messages.

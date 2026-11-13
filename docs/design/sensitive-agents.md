# Sensitive agents: domain-scoped, ephemeral agents with no way out

*2026-11-05, workstream pa-extensible-ui. User idea, developed here. My recommendation: promote it from backlog into the core design (the build can come later).*

## The idea (user)

Cheap, ephemeral agent sessions, one per sensitive domain (email, finance, photos, kids, and so on). Each one can work only on its task, with specific permissions. Example: an email agent reads your mail and pulls out anything pressing. It can **only ping you** and **draft a todo**. It has no internet access and can't share anything with another agent. Once you approve a todo, other agents can see it. Email access is read-only or propose-only, so it can't go rogue and send sensitive information to someone.

## Why it holds up

- **It breaks the "lethal trifecta"** (Willison, 2025): private data + untrusted content + external communication. An email agent unavoidably has the first two, because every incoming email is content an attacker controls. Removing every channel out (no network, no send, no peer agents) is exactly the fix he recommends. Trying to filter prompt injections is the alternative he calls a failing grade.
- **It is the research direction:** enforce at the system level, not through the model's goodwill. CaMeL (Google DeepMind) keeps the model that reads untrusted data away from the one that holds tools. FIDES (Microsoft) tracks **confidentiality and integrity labels** and enforces policies deterministically. A sensitive agent is a simpler, coarser version of the same idea: the whole session is the quarantine.
- **It reuses machinery we already need.** It runs on the same capability manifest, credential broker and visibility labels as extensions (report §f). *Agents get manifests too.* One capability system covers both.
- **It answers the scenario's open problems:** it makes email and calendar (the #1 integration in the scenario) safe to connect, it is the best candidate for B8 (capturing confirmations back), and it covers B3 (a finance agent that returns aggregates only).

## Shape

```yaml
agent: email-triage
domain: email                 # owns a private domain memory; other agents can't read it
model: local-only             # a remote provider is also a way out (tandem's private-page rule)
lifetime: per-run             # session discarded after the run; only domain memory persists, audited
tools:
  - mail.read:                # through the broker; the agent never holds a token
      scope: inbox, last 7d, exclude labels [legal, medical]   # least privilege on the *data*, not just the API
  - items.propose:            # writes only `proposed` items into its own outbox
      schema: {title<=80, due?, category, source: <email handle>}
  - notify.user:              # fixed templates, no free text ("3 new items to review")
network: none
peers: none                   # no agent-to-agent messages, and no shared scratch
output: proposals only
```

The flow: the agent reads mail, classifies it and proposes a structured todo. You approve it, and **the core** copies it into the target space with a provenance label. Only then can other agents see it.

## Pushback: where it leaks anyway, and what to do about each

1. **Approval is declassification, and it's also how an injection gets laundered.** The approved todo is the one thing that crosses the boundary. A malicious email can try to ride it: a todo reading "Send the passport scan to visa-help@…", approved by a tired you, then picked up by a privileged agent that acts on it.
   - Use **structured proposals**: short titles, fixed fields, and the source referenced by **handle**, not copied.
   - Approved items carry an **integrity label** (`derived-from-untrusted:email`). Privileged agents treat labelled text as data, never as instructions. This is FIDES's integrity side.
   - The approval UI shows exactly what will cross.
2. **"Ping me" is a channel too.** A phone push through a third-party service carries its content off the box. Use template-only notification text, and keep the details in the app.
3. **The model provider is a way out.** A remote model means sending your email to the provider. Use a local model per domain, or a provider you explicitly approve per domain.
4. **Read-only can still mean too much.** A read-only mail scope reads everything. Filter at the broker (labels, date range, senders) so each domain sees only its slice.
5. **Ephemeral vs useful.** A finance agent that forgets your categories every run is annoying. Discard the session but keep the **domain memory**, which only that domain's agents can read and every write to which is logged.
6. **Some tasks need two domains** (bills arrive by email but belong to finance). Never allow direct agent-to-agent traffic. Either declare an explicit combined manifest, or have the email agent emit a *handle* that the core passes to finance after your approval.
7. **Drafting replies** is fine as a proposal. Sending is you, in your mail client, or a gated send with a recipient allowlist. A reply can never attach content from another domain.

## Example domains

email triage · calendar/confirmations · finance (aggregates only) · photos (local-only tagging, never uploads) · kids/school (forms, dates) · health · identity documents (expiry dates only, values on the data plane)

## Spike

**Email triage agent against an injection corpus.** Use a local model, a read-only broker scope, proposal-only output, and a test inbox seeded with malicious emails (exfiltration attempts, poisoned todos).

Measure three things:
- (a) whether data can get out *structurally*: it shouldn't by construction, so verify the sandbox
- (b) whether poisoned content survives into approved todos, and whether the integrity label stops a privileged agent from acting on it
- (c) usefulness: the share of proposed todos you'd accept, and how many pressing items it misses

## Sources
- Willison, *The lethal trifecta for AI agents* (2025-06-16): https://simonwillison.net/2025/jun/16/the-lethal-trifecta/
- Debenedetti et al., *Defeating Prompt Injections by Design* (CaMeL, Google DeepMind): https://css.csail.mit.edu/6.858/2026/readings/camel.pdf ; summary: https://simonwillison.net/2025/apr/11/camel/
- Costa et al., *Securing AI Agents with Information-Flow Control* (FIDES, Microsoft): https://www.microsoft.com/en-us/research/publication/securing-ai-agents-with-information-flow-control/ ; https://arxiv.org/pdf/2505.23643v2

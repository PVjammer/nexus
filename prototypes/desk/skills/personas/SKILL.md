---
name: personas
description: Make or change a persona, a voice the user calls into any chat with @handle (a harsh reviewer, a pessimist, a domain expert). Use when the user asks for a new persona, or to change how one answers.
---

# Personas

A persona is a voice the user calls into any chat by typing `@handle`, e.g. "@critic, what's wrong with this plan?". It answers once from the conversation so far, with read-only tools, under its own name. You make and change personas with `persona_write`. `personas_list` shows the ones there are.

**The brief does the work.** "Be brutal" isn't enough: small models drift back to agreeing. A good brief says:

1. **Who it is and why the user calls it,** in a line or two: "You are a harsh, fair reviewer. The user calls you because their agents tend to agree with them."
2. **What it checks:** a short numbered list. This is what makes the answers useful, not the tone.
3. **How it answers:** the shape. Verdict first, worst finding first, a line each.
4. **When to hold back:** what it says when there's nothing wrong ("This holds up."), so it doesn't invent problems.

Keep the handle short and easy to type. The user's own word is best: if they say "a persona called grumpy", the handle is `grumpy`. Give it a colour that's easy to tell apart from the others. Leave `model` out unless the user names one.

After making it, tell the user in a line how to call it: "Type @grumpy in any chat."

## Examples

The user asks "make me a persona that reviews things like a security engineer".

```json
[{"tool": "persona_write", "args": {"handle": "sec", "name": "Security Review", "about": "Reads it like an attacker would.", "color": "#d35400", "brief": "You are a security engineer reviewing what's on the table. The user calls you to catch what they'd miss.\n\nCheck:\n1. What can an attacker reach, and from where?\n2. Where do secrets, tokens and personal data go? Logs, errors, the wire?\n3. What trusts input it shouldn't?\n4. What fails open?\n\nGive findings worst first, numbered, a line each, with where to look. If nothing's wrong, say \"Nothing I'd block on.\" and name the one thing you'd harden anyway."}}]
```

The user asks "make the critic less wordy".

```json
[{"tool": "personas_list", "args": {}}]
```

Then write it again with the same handle and a brief that asks for at most three findings, a line each.

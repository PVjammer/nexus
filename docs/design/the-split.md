# The split: where "the agent's environment vs your machine" hurts, and the fix for each (2026-11-12)

*Direction, not decisions. For the agreed direction itself, see the 2026-11-12 section in `where-we-are.md`.*

## The setup in one paragraph

Agents work in **their own environment**: a container or VM on your machine, a homelab box, or the cloud. Which one is configuration. They reach **your machine** only through scoped grants, routed through the node. They never use your computer directly: no screen control, no general shell on your host. You can always step into their environment, because its terminal, files and previews open as ordinary Nexus panes. Grants are made once per project and inherited down the tiers. In a normal week, you should approve almost nothing.

## Not just code

Most projects aren't coding work. For a trip, a renovation, a hiring loop or a team project, the agent hardly needs a machine at all. It needs:
- **the project's material:** briefs, items, documents, sheets, sources
- **your accounts:** email, calendar, chat, documents at work
- **a way to hand results to you**

So for most projects the split is about **accounts and documents, not about a machine.** The pain points below cover both kinds of work, and each one is marked *code*, *general* or *both*.

## Running it all on one computer

The local configuration has to behave the same as the remote one. Otherwise testing locally proves nothing.

- **Agent environment:** a rootless container (podman, or a bubblewrap profile like baish's and agent-lockdown's) by default, or a VM (Lima, multipass) when you want a stronger wall. On a Mac, containers run in a VM anyway.
- **The same contract:** the local container talks to the core through the same node interface a cloud agent would use. Moving to the homelab or the cloud changes configuration, not code.
- **Same paths:** mount the project at the **same absolute path** inside the container, and map your user ID into it. Absolute paths in configs then still work, and files the agent creates are owned by you, not by root.

## Pain points and natural fixes

Each fix should feel like the obvious thing to do, not like a security feature.

### Secrets and credentials

| # | Pain | Where | Natural fix |
|---|---|---|---|
| P1 | **`.env` in your repo.** The agent's worktree doesn't have it: a fresh worktree has no untracked files, so tests fail. Copying it in hands the agent the secrets. | code | **Secret references plus a class per secret** (see "P1 in depth" below). At onboarding, Nexus finds `.env` files, moves the values into the vault and leaves a template with references, like `DB_PASSWORD=secret:db-dev`. *Correction (2026-11-12): resolving references into a process's environment (the `op run` pattern) does **not** hide them from an agent that has a shell as the same user. `env`, `/proc/<pid>/environ`, or a test it writes that prints the config all reveal them. The real protection is that the value never enters the agent's environment, or that it doesn't matter if it does.* |
| P2 | **CLIs logged in on your host:** `gh`, `gcloud`, `kubectl`, `aws`, `docker`. The agent's environment has none of them. | code | **Brokered tools.** The node runs the command on the agent's behalf under a project policy (for example, `gh pr view` and `gh pr create` allowed, `gh repo delete` never). Alternatively, the broker holds a scoped token. Never mount `~/.aws` or `~/.kube`. **Never mount the docker socket**, because that is root on your host. |
| P3 | **You paste a secret into chat** out of habit. | both | Nexus recognizes key-like strings when you paste. It offers "send this as a secure entry instead?", and the chat gets the handle. |
| P4 | **SSH keys and signing.** The agent needs to push, or commits need your signature. | code | The agent pushes through a brokered connection, or doesn't push at all and you push after review. The agent's commits name it as the author, and **your signature goes on at merge.** Your keys never leave your machine. |

### P1 in depth: an agent with a shell can read any secret its processes can

The agent writes and runs the code, so anything that code can read, the agent can read too: through `env`, `/proc`, a log line, or a test that prints its config. Separate users or sibling containers don't help, because the agent controls the code that runs there and sees its output. There are three different risks:
- the value **enters the agent's context**, and with it the transcript and the model provider
- the agent **sends it somewhere**
- the agent **misuses it**

So each secret gets a class at onboarding, and the class decides how it's handled:

| Class | Example | Handling | Can the agent see it? |
|---|---|---|---|
| **HTTP credential** | API keys, bearer tokens | The process gets a placeholder. The egress proxy swaps in the real value on requests to that host only (the OpenShell pattern; the sandbox trusts the proxy's CA). | **No.** `env` shows the placeholder. |
| **Other protocol** | a Postgres password | An auth proxy on the node: the app connects to a local port, and the proxy authenticates upstream (as cloud-sql-proxy does). Or a short-lived credential, scoped to the task and created for it. | No for the proxy. Yes for a short-lived credential, which then expires and is scoped to a scratch database. |
| **Local key material** | JWT signing keys, encryption keys | The process needs the actual bytes. Generate **test values per task**. Real keys never enter an agent's environment. | Yes, and that's harmless. |
| **Production anything** | real database or deploy credentials | Not available to agent environments at all. That step is handed to you. | Never present. |

**Defense in depth:**
- The core knows every vault value, so it scans everything headed for the agent's context for them (tool output, terminal output, files the agent reads), including their base64 and hex forms. A match is redacted and logged.
- This catches accidents, like a test that dumps its config. It doesn't stop an agent determined to get around it, so it's a tripwire, not the boundary.
- Egress allowlists limit where a value could be sent. But the model provider is always reachable, which is why the classes above matter more.

### Getting things in

| # | Pain | Where | Natural fix |
|---|---|---|---|
| P5 | **A fresh worktree is broken:** no `node_modules`, venv, build cache, local database or large data files. | code | **A setup recipe per project.** The onboarding agent infers it (`npm ci`, seeding the database) and you approve it once. Shared, read-only package caches make it fast. A new task is ready in seconds, not minutes. |
| P6 | **The files you need are elsewhere:** contractor PDFs in Downloads, screenshots, a spreadsheet in Documents. | general | **Bring them in by dragging, or through a share target.** Each file is copied into the project and labelled where it came from. A project can also be granted a watched folder ("anything I drop in `~/Inbox/reno` goes to Bathroom reno"). Dragging is sharing, and it's logged. |
| P7 | **Information lives in your accounts:** email threads, calendar, Slack, work documents. | general | **Integrations through the broker,** which holds the OAuth tokens. Reading is per source and opt-in, with sensitive agents for email-like sources. No ambient indexing. |
| P8 | **The clipboard.** You copy something on your machine for the agent, and back. | both | Pasting into a Nexus pane *is* sharing it, explicitly. A "copy to my clipboard" button goes the other way. Nexus never watches your clipboard. |
| P9 | **Local hardware and network:** a GPU, a USB device, the homelab on your LAN when the agent runs in the cloud. | both | **Connections through the node** (the homelab-mock broker connection is the prototype of this). A cloud agent reaches your LAN only through your node, and only for granted endpoints. |

### Getting things out

| # | Pain | Where | Natural fix |
|---|---|---|---|
| P10 | **Results have to land in your real apps:** an email draft, a document for work, a sheet for your partner. | general | **Handoff actions on every artifact:** open in my app, save to a folder, copy, or **send as a draft** to your mail or chat. The agent drafts and **you send, as you.** It never posts in your name. |
| P11 | **Testing what the agent built:** a web app on a port inside the container, or a CLI. | code | **Automatic port forwarding** to a preview pane, plus "open in my browser". With a local sandbox the worktree is on your disk, so you can also just run it. For a cloud task, Nexus fetches the branch straight from the sandbox, with no git server. |
| P12 | **Logged-in websites:** bookings, marketplaces, your bank. The agent's browser has no logins, by design. | general | **"Send to my browser"** for the step only you can do, and **capture-back** for the result (B6). The agent prepares, you act, and the confirmation comes back into the project. |

### Working at the same time

| # | Pain | Where | Natural fix |
|---|---|---|---|
| P13 | **You and the agent edit the same thing in two places:** your checkout vs its worktree, or Excel vs its copy of the sheet. | both | **One source of truth per artifact.** Documents and sheets live in Nexus, and both of you edit the same copy (rev-based, as in the prototype). Code goes through branches and review. Exporting a document makes a copy, and Nexus says so. |
| P14 | **Parallel tasks collide:** two dev servers on :8080, or two migrations against the same local database. | code | **Ports and environment per task,** assigned automatically and shown on the task. Full isolation is a per-task setting ("run this one in a box"), not the default. |
| P15 | **The agent needs your machine while it's off** (a cloud task needs a local file). | both | A **"needs your machine" queue.** The task waits and doesn't fail. It shows on the dashboard and resumes when the node is back. |
| P16 | **Ephemeral agents all finish at once:** a storm of toasts. | both | **An interrupt budget.** Results collect in "needs you", and only blockers or anything you flagged interrupt you. |

### Boundaries that must not create work

| # | Pain | Where | Natural fix |
|---|---|---|---|
| P17 | **Prompt fatigue,** so you start approving everything. | both | Grant by scope once per project. Grants only narrow going down the tiers. Ask only when something crosses out of scope, and let you promote an answer to "always for this project". Measure it: count approvals per week in the action log, and treat a high count as a design bug. |
| ~~P18~~ | ~~Sensitive projects leaking up to the chief of staff.~~ | — | *Dropped 2026-11-12: the user doesn't want that isolation. The home agent has full access, and secrets stay protected by the vault and broker, not by walls between projects.* |
| P19 | **Work ↔ personal crossing.** | general | An explicit, labelled export to the other instance. It never syncs automatically (B10). |

## The ones to solve first

These are the points most likely to make the split feel like friction from day one:

1. **P1 + P5 (a new worktree that is broken or missing secrets).** It's the first thing every code task hits. Secret references plus a setup recipe fix both.
2. **P10 (getting results into your real apps).** It's the first thing every general project hits. Handoff actions and drafts-not-sends fix it.
3. **P17 (prompt fatigue).** It decides whether the boundary survives contact with real use.

## Test plan seeds

- **P1:**
  - Onboard a repo with a `.env` that holds a canary value.
  - Run its tests in a task's worktree. They pass.
  - Have the agent run `env`, `cat /proc/*/environ` and a test that prints its config. For an HTTP-class secret, the canary appears in none of them, nowhere in the transcript and nowhere in the action log.
  - For a local-key-class secret, the output shows a value generated for the task, never the real one.
  - Plant a test that logs the real value through a path we didn't block. The redaction tripwire catches it and logs the hit.
- **P5:** a new task's worktree is ready to test in under 30 seconds on a warm cache.
- **P10:** a drafted email reaches your mail client's drafts, and nothing is sent without you.
- **P17:** in a scripted week of normal tasks, you see at most a handful of permission prompts after onboarding.

## Mechanisms discussed 2026-11-12 (parked: documented, not decided)

*Picked up later. The user's lean: **use OpenShell where possible, wrapped to fit our needs.** Not decided.*

### Secure share: find and propose, don't register by hand

- **By default, nothing sensitive comes in.** A fresh worktree has no gitignored files. The risk is the setup recipe copying them across, so that is where discovery runs.
- **Discovery at onboarding.**
  - The core scans ignored and untracked files, by name (`.env*`, `*.pem`, `*.key`, `*.p12`, kubeconfig, `.npmrc`, `.netrc`, `credentials.json`, `*.tfvars`) and by content (gitleaks-style rules).
  - You get one screen, "found N secret files", with a proposed handling for each, and you accept them all at once.
  - **Secure share** is the same action done by hand: drag a file in and choose "share securely".
- **Handling by format:**

| File | What the agent's environment gets | Redaction tripwire |
|---|---|---|
| Structured (dotenv, JSON, YAML, INI, kubeconfig, `.npmrc`, `.netrc`) | the same file with placeholders, each value classed as in P1 | each value is registered |
| Opaque key material (PEM, p12, ssh keys) | nothing. The broker *uses* the key, as ssh-agent and gpg-agent forwarding do, and never shows it | the whole blob plus its base64 body lines |
| Unknown format | not shared, or shared as whole-file opaque | exact content match (weak; it's only a tripwire) |

### Processes get placeholders, agents get capabilities

- **Placeholders** (P1 classes) are for code that must hold a credential to work, such as the app under test.
- **Capabilities** are for the agent doing something. These are the scoped MCP broker plugins from agent-lockdown (`prototypes/agent-lockdown/docs/design.md`, "Chosen direction: an MCP broker with verified plugins"). The agent never holds a credential; a trusted process acts and returns the result.
  - **Scoping by dimension:** the blast-radius dimension (kubecontext, AWS profile, which repo) is a strict allowlist. The operation dimension can be loose.
  - **A project's grants are a list of capabilities with their allowlists.** Tasks inherit them, and narrowing means a tighter allowlist.
  - **One policy, two transports:** core tools for the pair agent, and the MCP socket for workers in a sandbox (`claude -p`, Codex). The core's tool contract stays the internal model.
  - **Authoring on the fly** is a stricter tier than UI plugins, because the validator sits on the trust boundary:
    1. The agent writes it on a branch.
    2. A fuzz harness and tool-specific adversarial tests run.
    3. Only a plugin that passes reaches you. You review the diff and test results in the review pane.
    4. Approval pins it by hash.
  - **Cautions:**
    - Output can carry secrets (`kubectl get secret -o yaml`). Output passes the tripwire, and tests exclude operations that read secrets.
    - A general MCP server you install is a tier 2a extension, not a capability.

### Environment backends: agentbox and OpenShell

| Role | OpenShell's term | Nexus |
|---|---|---|
| Control plane: vault, broker, policy, grants, log, UI | gateway | the Nexus core |
| Enforcement inside each agent environment | supervisor | an **environment backend** |

- **Interface:**
  - create an environment for a task (worktree, policy, credential rules)
  - run a command in it
  - attach a terminal, list ports
  - destroy it
- **agentbox** is the local backend available now:
  - worktree doorway (`--branch`, `--resume`, `--rm`)
  - lock-aware levels
  - sessions you can undo
  - a socket broker
  
  Its gaps: no egress proxy or credential rewrite, Linux only, no remote environments.
- **OpenShell** is a candidate for those gaps:
  - per-binary L7 egress policy
  - credential injection by proxy rewrite
  - MCP tool-name restriction
  - Landlock and seccomp
  - Docker, Podman, Kubernetes and microVM backends, so local, homelab and cloud can share one runtime
  
  Unlike baish, which *is* the loop, Nexus task agents are whole harnesses, which is what OpenShell wraps. **Risks:** new (March 2026), and WSL2 support is experimental.
- **Fallback:** agent-lockdown's injecting proxy on plain-HTTP localhost. It needs no MITM certificate, but the app's base URL has to change.
- **Spike when we return:**
  - OpenShell under Podman on WSL2.
  - One HTTP secret as a placeholder, run against the P1 attack test (`env`, `/proc/*/environ`, a test that prints its config).
  - One kubectl capability through MCP inspection.
- **Cross-project question:** one sandbox story for baish, agentbox and Nexus, instead of three. Belongs in the decision log (`prototypes/notes/project-ideas.md`).

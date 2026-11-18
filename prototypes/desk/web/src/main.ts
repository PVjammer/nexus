// Shell: top bar, the home dashboard, the project view (rail, panes, chat),
// capture, notifications and secure cards.

import { ago, app, connect, desk, h, modal, on, project, promptModal, request, send, setView, space, toast, token, view, type Detail, type Need } from "./core";
import { allViews, getView, position, render } from "./layout";
import { md, statusLabel } from "./panes";
import { ChatPanel, isBusy, questionCard, transcriptOf } from "./chat";
import { ic, kindIc } from "./icons";
import { customizer, drawHome as drawHomeGrid, initHome, registerKind, setRedraw, startEditing, stopEditing, toastAgentChange, type WidgetCtx } from "./home";
import { agentName, openChats, toggleChats } from "./chats";
import { appsButton, initApps } from "./apps";
import * as work from "./work";
import { initCustomizer, toggle as toggleCustomizer } from "./customizer";

if (!token) {
  document.body.innerHTML = `<div style="padding:40px;font-family:system-ui">Missing token. Open the URL that <code>nexus-core</code> printed (it ends in <code>#token=…</code>), or run <code>make app</code>.</div>`;
  throw new Error("no token");
}

const $ = (id: string) => document.getElementById(id)!;
const projects = () => desk?.projects || [];
const isFirstRun = () => !!desk && projects().length === 0;

// ---------------- chats ----------------

const projChat = new ChatPanel("project-chat");
$("project-chat-slot").append(projChat.el);

function bindChats() {
  const p = project();
  if (p) {
    projChat.bind("p:" + p.id, {
      placeholder: `Work with the ${p.name} agent…`,
      empty: () => {
        const fresh = !work.items().some((w) => w.project === p.id);
        return h("div", { class: "msg system chat-empty" },
          `I'm the ${p.name} project agent. I can read this project, open and arrange panes, draft docs and sheets, and draft tasks for background agents. Your folder is read-only to me.`,
          fresh ? h("div", { class: "setup-card" },
            h("b", {}, "Set up this project"),
            h("div", { class: "small" }, "I'll look through it for work (branches, checklists, plans, TODOs, design docs) and suggest a few work items for you to tick. Then I'll ask what else you want to do, and which you're working on now."),
            h("div", { class: "row" }, h("button", { class: "primary", onclick: () => work.startSetup(p.id) }, "Set up this project"),
              h("span", { class: "muted small" }, "Read-only, and you can stop at any point."))) : "");
      },
      context: () => ({ selection: currentSelection() }),
      ctxLine: () => ctxLine(),
    });
  }
}

function focusedPaneId(): string | null {
  const sp = space();
  if (!sp) return null;
  const find = (n: any): any => n.kind === "group" ? (n.id === sp.focused ? n : null) : (n.children || []).map(find).find(Boolean);
  return find(sp.root)?.active || null;
}
function currentSelection(): string {
  const id = focusedPaneId();
  return id ? getView(id)?.selection?.() || "" : "";
}
function ctxLine(): HTMLElement | null {
  const id = focusedPaneId();
  const p = id ? app?.state.panes[id] : null;
  if (!p) return h("span", {}, "Agent sees: the project, its work items and the layout");
  if (p.kind === "terminal") return h("span", {}, "Agent sees: the project and the layout. ", h("b", {}, "Not your terminal."));
  if (p.visibility === "none") return h("span", {}, "Agent sees: the project and the layout (the focused pane is private to you)");
  return h("span", {}, "Agent sees: the project, plus ", h("b", {}, kindIc(p.kind), ` ${p.title}`), " · and your selection");
}

// ---------------- top bar ----------------

function needsCount(pid?: string) {
  return (desk?.needs || []).filter((n) => (!pid || n.project === pid) && n.kind !== "stale").reduce((a, n) => a + (n.source === "proposals" ? 1 : n.count || 1), 0); // a batch of suggestions is one thing to review
}

function drawTop() {
  const nav = $("tabs");
  const home = h("button", { class: view.kind === "home" ? "active" : "", onclick: () => go({ kind: "home" }) }, ic("home"), " Home");
  const n = needsCount();
  $("needs-pill").replaceChildren(...(n ? [h("button", { class: "needs-pill", title: "Always here, whatever is on your home screen", onclick: toggleNeeds }, h("span", { class: "count" }, String(n)), " need you")] : []));
  nav.replaceChildren(home, ...projects().map((p) => h("button", {
    class: view.kind === "project" && view.id === p.id ? "active" : "", title: p.path,
    onclick: () => go({ kind: "project", id: p.id }),
  }, p.name, isBusy("p:" + p.id) ? h("span", { class: "busy-dot" }) : "", needsCount(p.id) ? h("span", { class: "count" }, String(needsCount(p.id))) : "")));
  const run = desk?.running || [];
  $("running").replaceChildren(run.length ? h("span", { class: "running-pill", title: run.map((r) => `${r.title} (${r.project})`).join("\n") }, h("span", { class: "busy-dot" }), ` ${run.length} running`) : "");
  const tb = $("toolbar");
  const sp = space();
  if (!sp) { tb.replaceChildren(); return; }
  const u = app?.undo?.[sp.id];
  tb.replaceChildren(...[
    u ? h("button", { class: "undo-agent", title: `Undo the agent's last layout change: ${u.label} (${u.depth} available)`, onclick: () => send({ t: "undo" }) }, ic("undo"), ` Undo agent`) : null,
    menu("Arrange ▾", () => [
      item("One group (all tabs)", () => send({ t: "preset", name: "single" })),
      item("Side by side", () => send({ t: "preset", name: "side-by-side" })),
      item("Columns", () => send({ t: "preset", name: "columns" })),
      item("Grid", () => send({ t: "preset", name: "grid" })),
    ]),
  ].filter(Boolean) as HTMLElement[]);
}

function go(v: { kind: "home" } | { kind: "project"; id: string }) {
  if (v.kind !== "home") stopEditing();
  setView(v);
  draw();
}

function menu(label: string | HTMLElement, items: () => HTMLElement[]): HTMLElement {
  const wrap = h("div", { class: "menu" });
  const btn = h("button", {}, label);
  let list: HTMLElement | null = null;
  const close = () => { list?.remove(); list = null; document.removeEventListener("mousedown", outside, true); };
  const outside = (e: Event) => { if (!wrap.contains(e.target as any)) close(); };
  btn.addEventListener("click", () => {
    if (list) return close();
    list = h("div", { class: "menu-list" }, ...items());
    list.addEventListener("click", (e) => { if ((e.target as HTMLElement).closest("button")) close(); });
    wrap.append(list);
    document.addEventListener("mousedown", outside, true);
  });
  wrap.append(btn);
  return wrap;
}
const item = (label: string | HTMLElement, f: () => void) => h("button", { onclick: f }, label);

// ---------------- opening things in a project ----------------

function openIn(pid: string, kind: string, props: any = {}, title?: string) {
  const p = project(pid);
  if (!p) return;
  if (view.kind !== "project" || view.id !== pid) go({ kind: "project", id: pid });
  send({ t: "open", space: p.space, kind, props, title });
}

// ---------------- home dashboard ----------------

// Form controls being edited aren't redrawn under you: redraws wait for blur.
function editingIn(el: HTMLElement): boolean {
  const a = document.activeElement as HTMLElement | null;
  return !!a && el.contains(a) && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName);
}
function whenNotEditing(el: HTMLElement, f: () => void): boolean {
  if (!editingIn(el)) return false;
  if (!(el as any)._pending) {
    (el as any)._pending = true;
    el.addEventListener("focusout", () => setTimeout(() => { (el as any)._pending = false; f(); }, 0), { once: true });
  }
  return true;
}

// The home screen is a grid of widgets (home.ts). Here: what each data kind
// draws, and the hooks home.ts needs.

// Stale-workstream nudges are housekeeping, not decisions: they fold into one
// line so the things that actually need you stay on top.
let showStale = false;
function needsList(needs: Need[]): HTMLElement[] {
  const real = needs.filter((n) => n.kind !== "stale");
  const stale = needs.filter((n) => n.kind === "stale");
  const out = real.map(needCard);
  if (stale.length) {
    const byProj = new Map<string, number>();
    for (const n of stale) byProj.set(n.project || "", (byProj.get(n.project || "") || 0) + 1);
    out.push(h("div", { class: "card need k-stale stale-fold" },
      h("div", { class: "card-top" }, h("b", {}, `${stale.length} workstream${stale.length === 1 ? "" : "s"} gone stale`),
        h("span", { class: "muted small grow-r" }, [...byProj].map(([p, n]) => `${project(p)?.name || p} ${n}`).join(" · "))),
      h("div", { class: "muted small" }, "Foreman's nudge: close them out or split what's left. Housekeeping, not urgent."),
      h("div", { class: "row" },
        h("button", { onclick: () => { showStale = !showStale; drawHome(); } }, showStale ? "Hide" : "Show them"),
        h("button", { onclick: () => stale.forEach((n) => send({ t: "dismissNeed", id: n.id })) }, "Clear all here"))));
    if (showStale) out.push(...stale.map(needCard));
  }
  if (!out.length) out.push(h("div", { class: "empty-note" }, "Nothing is waiting on you."));
  return out;
}

/** needTone: what kind of move a need asks of you, for its colour. Your own
 *  move (a task, a question) is amber; looking at an agent's work purple;
 *  confirming done green; something that failed red. */
function needTone(n: Need): string {
  if (n.source === "done") return "done";
  if (n.kind === "failed") return "blocked";
  if (n.kind === "stale") return "none";
  if (n.source === "proposals" || n.source === "wiki" || n.source === "filing" || (n.source === "task" && n.kind !== "needs-input") || n.kind === "review") return "agent";
  return "you";
}

function needCard(n: Need): HTMLElement {
  const p = n.project ? project(n.project) : undefined;
  const acts: HTMLElement[] = [];
  if (n.source === "filing") {
    acts.push(h("button", { class: "primary", onclick: () => document.querySelector(".hw.k-inbox")?.scrollIntoView({ behavior: "smooth" }) }, `Review ${n.count} filing${n.count === 1 ? "" : "s"}`));
  }
  if (n.source === "foreman" && p) {
    if (n.report) acts.push(h("button", { class: "primary", onclick: () => openIn(p.id, "review", { path: n.report }) }, ic("doc"), " Open report"));
    acts.push(h("button", { onclick: () => go({ kind: "project", id: p.id }) }, "Open project"));
    acts.push(h("button", { title: "Clear it here (foreman's own inbox is untouched)", onclick: () => send({ t: "dismissNeed", id: n.id }) }, "Clear"));
  }
  if (n.source === "wiki" && p) {
    const propId = n.id.split(":").slice(2).join(":");
    acts.push(h("button", { class: "primary", onclick: () => reviewProposal(p.id, propId) }, "Review change"));
  }
  if (n.source === "proposals") {
    acts.push(h("button", { class: "primary", onclick: () => work.openProposals(n.id.slice(6)) }, "Review"));
  }
  let answer: HTMLElement | string = "";
  if (n.source === "question" && n.agent) {
    // Answer right here: the same card as in the chat, clicks go to that agent.
    const m = transcriptOf(n.agent).find((x) => x.id === n.msg);
    if (m) answer = questionCard(n.agent, m);
    acts.push(h("button", { onclick: () => openChats(n.agent) }, ic("chat"), " Open the conversation"));
  }
  if (n.source === "done" && n.ref) {
    // An agent says it's done; only you can say so. Each check with its evidence.
    answer = h("ul", { class: "dw claim" }, ...(n.checks || []).map((c) => h("li", {}, c.text ? h("span", {}, c.text) : "", c.evidence ? h("span", { class: "muted small" }, (c.text ? " · " : "") + c.evidence) : h("span", { class: "muted small" }, " · no evidence given"))));
    acts.push(h("button", { class: "primary", onclick: () => request({ t: "workItemDone", item: n.ref, space: "" }).then(() => toast(`Done: ${n.title}. Closing it out.`, "info", { label: "Undo", run: () => send({ t: "workItemReopen", item: n.ref }) })).catch((e) => toast(e.message, "error")) }, ic("check"), " Yes, it's done"));
    acts.push(h("button", { onclick: async () => { const why = await promptModal("Not done yet", "What's missing? (optional: the agent is told)"); if (why !== null) send({ t: "workItemNotDone", item: n.ref, reason: why }); } }, "Not yet"));
    acts.push(h("button", { onclick: () => work.openWorkspace(n.ref!) }, "Open"));
  }
  if (n.source === "work" && n.task) {
    acts.push(h("button", { class: "primary", onclick: () => work.openTask(n.task!) }, n.kind === "review" ? "Review" : "Open"));
    acts.push(h("button", { onclick: () => request({ t: "workTaskUpdate", id: n.task, status: "done", space: "" }).catch((e) => toast(e.message, "error")) }, ic("check"), " Done"));
  }
  if (n.source === "task" && p) {
    acts.push(h("button", { class: n.kind === "needs-input" ? "primary" : "", onclick: () => openIn(p.id, "task", { task: n.task }) }, n.kind === "needs-input" ? "Answer" : "Open task"));
    if (n.report) acts.push(h("button", { class: "primary", onclick: () => openIn(p.id, "review", { path: n.report }) }, ic("doc"), " Report"));
    acts.push(h("button", { onclick: () => send({ t: "taskReviewed", task: n.task }) }, ic("check"), " Done"));
  }
  const kindTag = n.source === "foreman" ? `foreman · ${n.kind}` : n.source === "task" ? `task agent · ${statusLabel[n.kind || ""] || n.kind}` : n.source === "wiki" ? "wiki proposal" : n.source === "proposals" ? "suggestions" : n.source === "question" ? "question" : n.source === "done" ? "done? the agent says so" : n.source === "work" ? (n.detail === "todo" ? "todo" : `task · ${n.kind === "review" ? "review" : "yours"}`) : "capture";
  return h("div", { class: `card need src-${n.source} k-${n.kind || ""} tone-${needTone(n)}` },
    h("div", { class: "card-top" }, h("b", {}, n.title), p ? h("span", { class: "tag" }, p.name) : "", h("span", { class: "muted small grow-r" }, `${kindTag} · ${ago(n.time)}`)),
    n.detail ? h("div", { class: "need-detail" }, n.detail) : "",
    answer,
    acts.length ? h("div", { class: "row" }, ...acts) : "");
}

function captureBox(): HTMLElement {
  const ta = h("textarea", { rows: 1, placeholder: "Capture a thought…", title: "Ctrl+Shift+Space captures from anywhere" }) as HTMLTextAreaElement;
  const go_ = () => { const t = ta.value.trim(); if (!t) return; send({ t: "capture", text: t }); ta.value = ""; ta.rows = 1; toast("Captured. The home agent will propose where it goes."); };
  ta.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); go_(); } });
  ta.addEventListener("input", () => { ta.rows = Math.min(6, Math.max(1, ta.value.split("\n").length)); });
  return h("div", { class: "capture" }, ta, h("button", { class: "primary", onclick: go_ }, "Capture"));
}

function captureCard(c: any): HTMLElement {
  const sel = h("select", {}, ...projects().map((p) => h("option", { value: p.id, selected: c.proposal?.project === p.id }, p.name))) as HTMLSelectElement;
  const kind = h("select", {}, ...["task", "note", "question"].map((k) => h("option", { value: k, selected: (c.proposal?.kind || "task") === k }, k))) as HTMLSelectElement;
  const title = h("input", { value: c.proposal?.title || c.text }) as HTMLInputElement;
  return h("div", { class: `card cap ${c.status}` },
    h("div", { class: "cap-text" }, `“${c.text}”`),
    c.status === "new"
      ? h("div", { class: "muted small" }, h("span", { class: "busy-dot" }), " waiting for the home agent to propose a project…")
      : h("div", { class: "muted small" }, ic("agent"), ` ${c.proposal?.reason || "proposed"}`),
    h("div", { class: "cap-edit" }, title),
    h("div", { class: "row" }, sel, kind,
      h("button", { class: "primary", onclick: () => send({ t: "fileCapture", capture: c.id, project: sel.value, kind: kind.value, title: title.value }) }, c.status === "proposed" ? "Accept" : "File"),
      h("button", { onclick: () => send({ t: "dismissCapture", capture: c.id }) }, "Dismiss")));
}

function projectCard(p: Detail): HTMLElement {
  const ws = (p.workstreams || []).filter((w) => !w.closed);
  const working = ws.filter((w) => w.agentStatus === "working").length;
  const backlog = (p.backlog || []).filter((b) => !b.done).length;
  const wis = work.items().filter((w) => w.project === p.id);
  const doing = wis.filter((w) => w.state === "in-progress").length, later = wis.filter((w) => w.state === "not-started").length;
  const needs = needsCount(p.id);
  const stat = (n: number, label: string, cls = "") => n ? h("span", { class: `stat ${cls}` }, h("b", {}, String(n)), ` ${label}`) : null;
  return h("div", { class: "card proj", onclick: (e: Event) => { if (!(e.target as HTMLElement).closest(".menu")) go({ kind: "project", id: p.id }); } },
    h("div", { class: "card-top" }, h("b", { class: "pname" }, p.name), h("span", { class: "muted small grow-r" }, ago(p.lastActivity)),
      menu("⋯", () => [
        item("Edit brief…", async () => { const b = await promptModal(`Brief for ${p.name}`, "One or two sentences", p.brief || p.readme || ""); if (b !== null) send({ t: "projectBrief", project: p.id, brief: b }); }),
        item("Remove from Nexus (the folder is untouched)", () => send({ t: "projectRemove", project: p.id })),
      ])),
    h("div", { class: "brief" }, p.brief || p.readme || h("span", { class: "muted" }, "No brief yet.")),
    h("div", { class: "stats" },
      stat(needs, "need you", "need"), stat(working, "working", "work"), stat(ws.length, "workstreams"), stat(backlog, "backlog"), stat(doing, "in progress"), stat(later, "not started"),
      p.git ? h("span", { class: "stat git", title: p.git.commits?.[0]?.subject || "" }, ic("code"), ` ${p.git.branch}${p.git.dirty ? ` · ${p.git.dirty} changed` : ""}`) : null,
      !p.exists ? h("span", { class: "stat need" }, "folder missing") : null));
}


const inScope = (c: WidgetCtx) => (x: { project?: string }) => !c.project || x.project === c.project;
const empty = (t: string) => h("div", { class: "empty-note" }, t);
const tag = (pid?: string) => pid ? h("span", { class: "tag" }, project(pid)?.name || pid) : "";

function needOpen(n: Need): (() => void) | null {
  const p = n.project ? project(n.project) : undefined;
  if (n.source === "work" && n.task) return () => work.openTask(n.task!);
  if (n.source === "question" && n.agent) return () => openChats(n.agent);
  if (n.source === "done" && n.ref) return () => work.openWorkspace(n.ref!);
  if (n.source === "task" && p) return () => openIn(p.id, "task", { task: n.task });
  if (n.report && p) return () => openIn(p.id, "review", { path: n.report });
  if (n.source === "wiki" && p) return () => reviewProposal(p.id, n.id.split(":").slice(2).join(":"));
  if (n.source === "filing") return () => document.querySelector(".hw.k-inbox")?.scrollIntoView({ behavior: "smooth" });
  if (p) return () => go({ kind: "project", id: p.id });
  return null;
}
function compactRow(title: string, note: string | HTMLElement, onclick: (() => void) | null, lead?: HTMLElement | string, cls = ""): HTMLElement {
  return h("button", { class: `crow ${cls}`, onclick: onclick || undefined, disabled: !onclick }, lead || "", h("span", { class: "crow-t" }, title), h("span", { class: "muted small crow-n" }, note));
}

registerKind("needs", { draw: (c) => {
  const needs = (desk?.needs || []).filter(inScope(c));
  if (c.shape === "compact") {
    const real = needs.filter((n) => n.kind !== "stale");
    return real.length ? real.map((n) => compactRow(n.title, h("span", {}, tag(n.project), " ", n.kind || n.source), needOpen(n), h("span", { class: `dot tone-${needTone(n)}` }))) : [empty("Nothing is waiting on you.")];
  }
  return needsList(needs);
} });

registerKind("running", { draw: (c) => {
  const run = (desk?.running || []).filter(inScope(c));
  if (!run.length) return [empty("No agents working right now.")];
  if (c.shape === "compact") return run.map((r) => compactRow(r.title, h("span", {}, tag(r.project), " ", r.source === "foreman" ? "foreman" : "task agent"), r.task ? () => openIn(r.project!, "task", { task: r.task }) : null, h("span", { class: "busy-dot" })));
  return run.map((r) => h("div", { class: "card run" },
    h("div", { class: "card-top" }, h("span", { class: "busy-dot" }), h("b", {}, r.title), tag(r.project), h("span", { class: "muted small grow-r" }, r.source === "foreman" ? "foreman" : "task agent")),
    r.detail ? h("div", { class: "muted small" }, r.detail) : "",
    r.task ? h("div", { class: "row" }, h("button", { onclick: () => openIn(r.project!, "task", { task: r.task }) }, "Follow along")) : ""));
} });

const taskCols: [string, string, string[]][] = [["Draft", "draft", ["draft"]], ["Running", "running", ["running"]], ["Needs you", "needs-input", ["needs-input"]], ["Finished", "done", ["done", "failed", "stopped"]]];
registerKind("tasks", { draw: (c) => {
  const ts = (desk?.desk.tasks || []).filter(inScope(c)).filter((t) => !t.reviewed).sort((a, b) => b.updated.localeCompare(a.updated));
  if (!ts.length) return [empty("No dispatched tasks. A project's agent can draft one, or use + Task in a project.")];
  const row = (t: any) => compactRow(t.title, h("span", {}, tag(t.project), " ", ago(t.updated)), () => openIn(t.project, "task", { task: t.id }), h("span", { class: `status st-${t.status}` }, statusLabel[t.status] || t.status));
  if (c.shape === "board") return [h("div", { class: "mini-board" }, ...taskCols.map(([label, cls, sts]) => {
    const col = ts.filter((t) => sts.includes(t.status));
    return h("div", { class: "mb-col" }, h("div", { class: `mb-h st-${cls}` }, label, h("span", { class: "muted small" }, ` ${col.length}`)),
      ...col.map((t) => h("button", { class: "mb-card", onclick: () => openIn(t.project, "task", { task: t.id }) }, h("span", {}, t.title), tag(t.project))));
  }))];
  return ts.map(row);
} });

registerKind("items", { draw: (c) => {
  const its = (desk?.desk.items || []).filter(inScope(c)).filter((i) => i.state === "open");
  if (!its.length) return [empty("No open items.")];
  return its.map((i) => h("div", { class: "crow" },
    c.shape === "list" ? h("input", { type: "checkbox", title: "Done", onclick: () => send({ t: "itemUpdate", item: i.id, state: "done" }) }) : "",
    h("span", { class: `kind k-${i.kind}` }, i.kind), h("span", { class: "crow-t" }, i.title), h("span", { class: "muted small crow-n" }, tag(i.project))));
} });

registerKind("workstreams", { draw: (c) => {
  const rows: HTMLElement[] = [];
  for (const p of projects().filter((p) => !c.project || p.id === c.project)) {
    for (const w of (p.workstreams || []).filter((w) => !w.closed)) {
      const open = w.report ? () => openIn(p.id, "review", { path: `${p.id}/${w.report}` }) : () => go({ kind: "project", id: p.id });
      rows.push(c.shape === "compact"
        ? compactRow(w.name, h("span", {}, c.project ? "" : tag(p.id), ` ${w.reportStatus || ""} · ${ago(w.updated)}`), open, h("span", { class: `status ws-${w.agentStatus}` }, w.agentStatus || "?"))
        : h("div", { class: "card ws-card", onclick: open }, h("div", { class: "card-top" }, h("span", { class: `status ws-${w.agentStatus}` }, w.agentStatus || "?"), h("b", {}, w.name), c.project ? "" : tag(p.id), h("span", { class: "muted small grow-r" }, ago(w.updated))),
          w.summary ? h("div", { class: "muted small" }, w.summary) : ""));
    }
  }
  return rows.length ? rows : [empty("No open foreman workstreams.")];
} });

registerKind("workitems", { draw: (c) => work.workItemCards(c.project, c.shape) });
registerKind("board", { draw: (c) => c.shape === "list" ? work.boardList(c.project) : [work.board(c.project, { hideDone: true })] });
registerKind("todos", { draw: () => work.todos() });

registerKind("inbox", { draw: () => {
  const caps = (desk?.desk.captures || []).filter((c) => c.status === "new" || c.status === "proposed");
  return caps.length ? caps.map(captureCard) : [empty("Nothing to file. Captures land here with the home agent's proposal.")];
} });

registerKind("capture", { create: () => ({ el: captureBox() }) });

registerKind("projects", { draw: (c) => {
  if (c.shape === "list") return projects().map((p) => compactRow(p.name, `${needsCount(p.id) ? needsCount(p.id) + " need you · " : ""}${ago(p.lastActivity)}`, () => go({ kind: "project", id: p.id }), ic("folder")));
  return [h("div", { class: "proj-grid" }, ...projects().map(projectCard), h("button", { class: "card add-card", onclick: addFolder }, ic("plus"), " Add a project"))];
} });

initHome({
  greeting: () => {
    if (isFirstRun()) return h("div", { class: "hero" },
      h("h1", {}, "Let's set up Nexus."),
      h("p", {}, "Nexus is where you plan and do project work with agents. Tell the home agent what you work on: it can scan your workspace for projects (git repos), read them, and arrange this screen with you. Then open each project and press Set up: its agent looks through it for work and asks what you want to do."),
      h("p", { class: "muted" }, "Nexus only reads your project folders. Its own state lives in its data directory."));
    const run = desk?.running || [];
    return h("div", { class: "greet" },
      h("h2", {}, new Date().toLocaleDateString([], { weekday: "long", month: "long", day: "numeric" })),
      h("div", { class: "muted" }, `${projects().length} project${projects().length === 1 ? "" : "s"} · `, h("button", { class: "link", onclick: () => toggleNeeds() }, `${needsCount()} need you`), ` · ${run.length} running`));
  },
  chips: (agent) => {
    if (agent !== "home") return [{ label: "What's left here?" }, { label: "What changed this week?" }];
    if (isFirstRun()) return [{ label: "Find my projects", text: "Find my projects and help me set up." }, { label: "Add a folder myself", run: addFolder }];
    return [{ label: "What needs me?" }, { label: "What's running?" }, { label: "Set up my home screen", text: "Set up my home screen: put what needs me under this box, and what's running beside it." }];
  },
  openChats: (agent) => openChats(agent),
  agentName,
});
setRedraw(() => drawHome());

// The count in the top bar always works, whatever is on the home screen:
// it answers in the prompt box (or opens the home conversation).
function toggleNeeds() {
  if (view.kind !== "home") go({ kind: "home" });
  const l = desk?.desk.layouts?.home?.widgets || [];
  if (l.some((w) => w.kind === "needs" && !w.scope)) { document.querySelector(".hw.k-needs")?.scrollIntoView({ behavior: "smooth" }); return; }
  openChats("home");
  send({ t: "chat", agent: "home", text: "What needs me?", view: "" });
}

function drawHome() {
  document.body.classList.toggle("first-run", isFirstRun());
  if (!desk) { $("home").replaceChildren(h("div", { class: "muted pad" }, "Connecting…")); return; }
  drawHomeGrid($("home"));
}

async function addFolder() {
  const path = await promptModal("Add a project", "Folder path, e.g. ~/Workspace/homelab");
  if (!path) return;
  try { await request({ t: "projectAdd", path: path.replace(/^~\//, homeDirGuess()) }); toast("Added"); } catch (e: any) { toast(e.message, "error"); }
}
function homeDirGuess() {
  const p = projects()[0]?.path || "";
  const m = p.match(/^(\/home\/[^/]+|\/Users\/[^/]+)\//);
  return m ? m[1] + "/" : "~/";
}

// ---------------- project rail ----------------

const openSecs: Record<string, boolean> = (() => { try { return JSON.parse(localStorage.getItem("nexus-rail") || "{}"); } catch { return {}; } })();
function railSec(key: string, title: string, count: number | string, body: () => (HTMLElement | null)[], def = true, extra?: HTMLElement): HTMLElement {
  const open = openSecs[key] ?? def;
  const head = h("div", { class: "rsec-head", onclick: () => { openSecs[key] = !open; try { localStorage.setItem("nexus-rail", JSON.stringify(openSecs)); } catch {} drawRail(); } },
    h("span", { class: "caret" }, open ? "▾" : "▸"), h("b", {}, title), count !== "" ? h("span", { class: "muted small" }, String(count)) : "", h("span", { class: "grow-r" }), extra || "");
  return h("div", { class: "rsec" }, head, open ? h("div", { class: "rsec-body" }, ...(body().filter(Boolean) as HTMLElement[])) : "");
}

function drawRail() {
  const p = project();
  const rail = $("rail");
  if (!p) { rail.replaceChildren(); return; }
  if (whenNotEditing(rail, drawRail)) return;
  const tasks = (desk?.desk.tasks || []).filter((t) => t.project === p.id).sort((a, b) => b.updated.localeCompare(a.updated));
  const items = (desk?.desk.items || []).filter((i) => i.project === p.id);
  const openItems = items.filter((i) => i.state === "open");
  const ws = p.workstreams || [];
  const backlog = (p.backlog || []).filter((b) => !b.done);
  const needs = (desk?.needs || []).filter((n) => n.project === p.id);
  // Work items: goals (in your list, or read from a source). Tasks: the next
  // actions toward them. Agent runs: background agents you sent off with a brief.
  const wis = work.items().filter((w) => w.project === p.id && w.state !== "parked" && w.state !== "done");
  const wTasks = work.tasks().filter((t) => t.project === p.id && t.item && t.status !== "done" && work.items().find((w) => w.ref === t.item)?.state === "in-progress");
  const notes = openItems.filter((i) => i.kind !== "task");
  const addItem = h("input", { placeholder: "Add a work item…" }) as HTMLInputElement;
  addItem.addEventListener("keydown", (e) => { if (e.key === "Enter" && addItem.value.trim()) { work.addWorkItem(p.id, addItem.value.trim()); addItem.value = ""; } });
  const newTask = h("button", { class: "mini", title: "Send a background agent off with a brief", onclick: async (e: Event) => {
    e.stopPropagation();
    const t = await promptModal("New agent run", "What should a background agent look into? (you'll edit the brief next)");
    if (t) send({ t: "taskNew", project: p.id, title: t });
  } }, ic("plus"), " Task");

  rail.replaceChildren(...([
    h("div", { class: "rail-head" },
      h("h2", {}, p.name),
      h("div", { class: "muted small path", title: p.path }, p.path),
      h("div", { class: "brief", title: "Click to edit", onclick: async () => { const b = await promptModal(`Brief for ${p.name}`, "One or two sentences", p.brief || p.readme || ""); if (b !== null) send({ t: "projectBrief", project: p.id, brief: b }); } },
        p.brief || p.readme || h("span", { class: "muted" }, "No brief yet. Click to write one.")),
      p.git ? h("div", { class: "muted small" }, ic("code"), ` ${p.git.branch}${p.git.dirty ? ` · ${p.git.dirty} uncommitted` : " · clean"}${p.git.commits?.[0] ? ` · ${ago(p.git.commits[0].when)}: ${p.git.commits[0].subject}` : ""}`) : null,
      h("div", { class: "row quick" },
        h("button", { onclick: () => send({ t: "open", kind: "terminal" }) }, ic("term"), " Terminal"),
        h("button", { onclick: () => send({ t: "open", kind: "files", props: { path: p.id } }) }, ic("folder"), " Files"),
        h("button", { onclick: () => send({ t: "open", kind: "log" }) }, ic("log"), " Log"))),
    needs.length ? railSec("needs", "Needs you", needs.length, () => needs.map((n) => h("div", { class: "ritem need", onclick: () => {
      if (n.source === "work" && n.task) work.openTask(n.task);
      else if (n.source === "question" && n.agent) openChats(n.agent);
      else if (n.task) send({ t: "open", kind: "task", props: { task: n.task } });
      else if (n.report) send({ t: "open", kind: "review", props: { path: n.report } });
    } }, h("span", { class: `dot tone-${needTone(n)}` }), h("span", { class: "grow" }, h("b", {}, n.title), h("div", { class: "muted small" }, n.detail || "")), h("span", { class: "muted small" }, ago(n.time))))) : null,
    railSec("items", "Work items", wis.length || "", () => [
      ...wis.map((w) => h("div", { class: `ritem tone-${work.itemTone(w)}`, title: [w.brief, (w.doneWhen || []).length ? "Done when: " + (w.doneWhen || []).join("; ") : ""].filter(Boolean).join("\n") || w.title, onclick: () => work.openWorkspace(w.ref) },
        h("span", { class: "grow" }, w.title, w.brief ? h("div", { class: "muted small" }, w.brief) : ""),
        w.state === "in-progress" ? work.moveChip(w)
          : h("button", { class: "mini keep", title: "Put it in progress", onclick: (e: Event) => { e.stopPropagation(); work.startItem(w); } }, "Start"))),
      wis.length ? null : h("div", { class: "muted small" }, "No work items yet. Set up the project, or add one here."),
      addItem,
    ]),
    railSec("wtasks", "Tasks", wTasks.length || "", () => [
      ...wTasks.map((t) => work.taskRow(t)),
      wTasks.length ? null : h("div", { class: "muted small" }, "No open tasks on work items in progress."),
    ]),
    notes.length ? railSec("notes", "Notes and questions", notes.length, () => notes.map((i) => h("div", { class: "ritem" },
      h("input", { type: "checkbox", title: "Done: clear it", onclick: (e: Event) => { e.stopPropagation(); send({ t: "itemUpdate", item: i.id, state: "done" }); } }),
      h("span", { class: "grow" }, h("span", { class: `kind k-${i.kind}` }, i.kind), " ", i.title))), false) : null,
    railSec("tasks", "Agent runs", tasks.filter((t) => !t.reviewed).length || "", () => [
      ...tasks.filter((t) => !t.reviewed).map((t) => h("div", { class: "ritem", onclick: () => send({ t: "open", kind: "task", props: { task: t.id } }) },
        h("span", { class: `status st-${t.status}` }, statusLabel[t.status] || t.status), h("span", { class: "grow" }, t.title))),
      tasks.filter((t) => !t.reviewed).length ? null : h("div", { class: "muted small" }, "No background agents running. Send one off with a brief here, or ask the project agent."),
    ], false, newTask),
    p.foreman ? railSec("ws", "Workstreams", ws.filter((w) => !w.closed).length, () => ws.filter((w) => !w.closed).map((w) => h("div", { class: "ritem", title: w.goal || "", onclick: () => w.report && send({ t: "open", kind: "review", props: { path: `${p.id}/${w.report}` } }) },
      h("span", { class: `status ws-${w.agentStatus}` }, w.agentStatus || "?"),
      h("span", { class: "grow" }, h("b", {}, w.name), h("div", { class: "muted small" }, `report: ${w.reportStatus || "—"} · ${ago(w.updated)}`), w.summary ? h("div", { class: "small" }, w.summary) : "")))) : null,
    p.foreman && backlog.length ? railSec("backlog", "Backlog", backlog.length, () => backlog.slice().reverse().slice(0, 40).map((b) => h("div", { class: "ritem bl" },
      h("span", { class: "grow small" }, b.text),
      h("button", { class: "mini", title: "Dispatch to a background agent", onclick: () => send({ t: "taskNew", project: p.id, title: b.text, from: "backlog" }) }, "→"))), false) : null,
    railSec("wiki", "Wiki", wikiCount(p.id), () => wikiRail(p.id), true),
    (p.docs || []).length ? railSec("docs", "Docs", (p.docs || []).length, () => (p.docs || []).map((d) => h("div", { class: "ritem doc", onclick: () => send({ t: "open", kind: "review", props: { path: `${p.id}/${d}` } }) }, ic("doc"), h("span", { class: "grow" }, d))), false) : null,
    railSec("journal", "Journal", "", () => journal(p), false),
    railSec("addons", "Add-ons", "", () => addonRail(p.id), false),
  ].filter(Boolean) as HTMLElement[]));
}

// ---------------- wiki ----------------

function wikiOf(pid: string) { return desk?.wikis?.[pid] || { pages: [], proposals: [] }; }
function wikiCount(pid: string): string {
  const w = wikiOf(pid);
  const n = (w.pages || []).length, pr = (w.proposals || []).length;
  return n ? `${n}${pr ? ` · ${pr} to review` : ""}` : "empty";
}
function openWiki(pid: string, slug: string) {
  openIn(pid, "review", { path: `wiki/${pid}/${slug}.md` }, slug === "index" ? "Wiki" : `Wiki: ${slug}`);
}
function wikiRail(pid: string): HTMLElement[] {
  const w = wikiOf(pid);
  const pages = w.pages || [];
  const props = w.proposals || [];
  const out: HTMLElement[] = [];
  if (!pages.length) {
    out.push(h("div", { class: "muted small" }, "No wiki yet. The project agent can build one from the README, docs and reports; every agent here then starts from its index."),
      h("button", { class: "primary mini-wide", onclick: () => send({ t: "wikiBuild", project: pid }) }, ic("spark"), " Build the wiki"));
    return out;
  }
  for (const pr of props) out.push(h("div", { class: "ritem need", onclick: () => reviewProposal(pid, pr.id) },
    h("span", { class: "dot k-proposal" }), h("span", { class: "grow" }, h("b", {}, `Change to ${pr.slug}`), h("div", { class: "muted small" }, pr.reason)), h("span", { class: "muted small" }, "review")));
  for (const pg of pages) out.push(h("div", { class: "ritem doc", title: pg.meta.summary || "", onclick: () => openWiki(pid, pg.slug) },
    ic(pg.slug === "index" ? "home" : "doc"),
    h("span", { class: "grow" }, pg.slug === "index" ? h("b", {}, "index") : pg.slug, pg.meta.summary ? h("div", { class: "muted small" }, pg.meta.summary) : ""),
    pg.stale?.length ? h("span", { class: "status st-needs-input", title: `Sources changed since it was verified: ${pg.stale.join(", ")}` }, "stale") : ""));
  return out;
}

// A small line diff for reviewing wiki proposals.
function lineDiff(a: string, b: string): HTMLElement {
  const A = a.split("\n"), B = b.split("\n");
  if (A.length * B.length > 250000) return h("pre", { class: "diff" }, b);
  const dp: number[][] = Array.from({ length: A.length + 1 }, () => new Array(B.length + 1).fill(0));
  for (let i = A.length - 1; i >= 0; i--) for (let j = B.length - 1; j >= 0; j--) dp[i][j] = A[i] === B[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const rows: HTMLElement[] = [];
  let i = 0, j = 0;
  while (i < A.length || j < B.length) {
    if (i < A.length && j < B.length && A[i] === B[j]) { rows.push(h("div", { class: "dl same" }, A[i] || " ")); i++; j++; }
    else if (j < B.length && (i >= A.length || dp[i][j + 1] >= dp[i + 1][j])) { rows.push(h("div", { class: "dl add" }, "+ " + B[j])); j++; }
    else { rows.push(h("div", { class: "dl del" }, "- " + A[i])); i++; }
  }
  return h("div", { class: "diff" }, ...rows);
}

async function reviewProposal(pid: string, id: string) {
  let r: any;
  try { r = await request({ t: "wikiProposal", project: pid, proposal: id }); } catch (e: any) { toast(e.message, "error"); return; }
  const pr = r.proposal;
  modal((close) => h("div", { class: "modal wide" },
    h("h3", {}, `Wiki change: ${pr.slug}`, r.current ? "" : h("span", { class: "tag" }, "new page")),
    h("p", { class: "muted small" }, `From ${pr.by}${pr.task ? ` (task ${pr.task})` : ""} · ${pr.reason}`),
    pr.sources?.length ? h("p", { class: "muted small" }, "Sources: ", pr.sources.join(", ")) : "",
    lineDiff(r.current || "", pr.content),
    h("div", { class: "row" },
      h("button", { onclick: () => { send({ t: "wikiResolve", project: pid, proposal: id, accept: false }); close(); } }, "Reject"),
      h("button", { class: "primary", onclick: () => { send({ t: "wikiResolve", project: pid, proposal: id, accept: true }); close(); } }, "Accept"))));
}

// [[wiki links]] anywhere (chat, documents) open the page in the current project.
document.addEventListener("click", (e) => {
  const a = (e.target as HTMLElement).closest?.("a[href^='#wiki:']") as HTMLAnchorElement | null;
  if (!a) return;
  e.preventDefault();
  let slug = a.getAttribute("href")!.slice(6);
  let pid = view.kind === "project" ? view.id : "";
  if (slug.includes("/")) { [pid, slug] = slug.split("/", 2); }
  if (pid && project(pid)) openWiki(pid, slug);
  else toast("Open a project to follow wiki links", "warn");
}, true);

// ---------------- add-ons ----------------

function addonRail(pid: string): HTMLElement[] {
  const list = desk?.addons?.[pid] || [];
  return list.map((ad) => {
    const st = ad.status;
    const stLine = ad.unavailable ? ad.unavailable
      : ad.id === "codegraph" && ad.on ? (st ? (st.syncing ? "indexing…" : st.error ? `error: ${st.error}` : `${st.files} files indexed · ${ago(st.lastSync)}`) : "indexing soon…")
      : "";
    const cb = h("input", { type: "checkbox", checked: ad.on, disabled: !!ad.unavailable,
      onchange: (e: Event) => send({ t: "addonSet", project: pid, addon: ad.id, on: (e.target as HTMLInputElement).checked }) });
    return h("label", { class: "ritem addon", title: ad.description }, cb,
      h("span", { class: "grow" }, h("b", {}, ad.name), h("span", { class: "muted small" }, ` · ${ad.kind}`),
        h("div", { class: "muted small" }, ad.description), stLine ? h("div", { class: `small ${st?.error ? "err" : "muted"}` }, stLine) : ""));
  });
}

let logEntries: any[] = [];
on("logHistory", (m) => { logEntries = m.entries || []; });
on("log", (m) => { logEntries.push(m.entry); if (logEntries.length > 1500) logEntries.shift(); if (view.kind === "project") scheduleRail(); });

function journal(p: Detail): HTMLElement[] {
  type J = { t: number; who: string; text: string; cls: string };
  const js: J[] = [];
  for (const e of p.events || []) js.push({ t: new Date(e.time).getTime(), who: e.ws ? `foreman · ${e.ws}` : "foreman", text: `${e.kind}: ${e.summary}`, cls: "j-foreman" });
  for (const e of logEntries) if (e.space === p.space) js.push({ t: new Date(e.ts).getTime(), who: e.actor, text: `${e.action}${e.target ? " " + e.target : ""}`, cls: e.actor === "you" ? "j-you" : e.denied ? "j-denied" : "j-agent" });
  for (const t of desk?.desk.tasks || []) if (t.project === p.id && t.status !== "draft") js.push({ t: new Date(t.updated).getTime(), who: "task", text: `${statusLabel[t.status] || t.status}: ${t.title}`, cls: "j-agent" });
  js.sort((a, b) => b.t - a.t);
  return js.slice(0, 40).map((j) => h("div", { class: `jrow ${j.cls}` }, h("span", { class: "muted small" }, ago(new Date(j.t).toISOString())), h("span", { class: "small who" }, j.who), h("span", { class: "small" }, j.text)));
}

let railT = 0;
function scheduleRail() { if (railT) return; railT = requestAnimationFrame(() => { railT = 0; drawRail(); }); }

// ---------------- capture anywhere ----------------

function captureModal() {
  modal((close) => {
    const ta = h("textarea", { rows: 5, placeholder: "Dump it here. One line per thought. The home agent proposes where each goes; you confirm." }) as HTMLTextAreaElement;
    const go_ = () => { const t = ta.value.trim(); if (t) { send({ t: "capture", text: t }); toast("Captured"); } close(); };
    ta.addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) go_(); if (e.key === "Escape") close(); });
    setTimeout(() => ta.focus(), 0);
    return h("div", { class: "modal" }, h("h3", {}, ic("note"), " Capture"), ta,
      h("div", { class: "row" }, h("span", { class: "muted small grow" }, "Ctrl+Enter to save"), h("button", { onclick: close }, "Cancel"), h("button", { class: "primary", onclick: go_ }, "Capture")));
  });
}
document.addEventListener("keydown", (e) => {
  if (e.ctrlKey && e.shiftKey && (e.code === "Space" || e.key === " ")) { e.preventDefault(); captureModal(); }
});
$("capture-btn").addEventListener("click", captureModal);
$("chats-btn").addEventListener("click", () => toggleChats());
initApps({ openIn, openTasks: () => work.openTasksApp() });
work.initWork({
  openProject: (pid) => go({ kind: "project", id: pid }),
  openRun: (pid, task) => openIn(pid, "task", { task }),
  openWorkspace: (ref, task) => {
    if (ref.startsWith("open-report:")) { const path = ref.slice(12); openIn(path.split("/")[0], "review", { path }); return; }
    const w = work.items().find((x) => x.ref === ref);
    const p = w && project(w.project);
    if (!w || !p) { toast("That work item isn't in a project Nexus knows", "warn"); return; }
    openIn(p.id, "workitem", { ref, task: task || "" }, w.title);
  },
});
$("apps-slot").append(appsButton());
initCustomizer({
  view: () => view.kind === "home" ? "home" : `project ${view.kind === "project" ? view.id : ""}`,
  goHome: () => { go({ kind: "home" }); toast(`${customizer().name} finished: try it on the home screen`, "info"); },
  redraw: () => { if (view.kind === "home") drawHome(); },
});

// ---------------- notifications and secure cards ----------------

on("notify", (m) => toast(m.text, "info", m.project && m.task ? { label: "Open", run: () => openIn(m.project, "task", { task: m.task }) } : undefined));

on("secureCard", (m) => {
  let shown = false;
  modal((close) => {
    const val = h("div", { class: "val" }, "•".repeat(12));
    const done = () => { send({ t: "secureDismiss", id: m.id }); close(); };
    return h("div", { class: "modal secure" },
      h("div", { class: "lock" }, ic("lock")), h("h3", {}, m.item), m.reason ? h("div", { class: "muted small" }, `Agent’s reason: ${m.reason}`) : "", val,
      h("div", { class: "row", style: { justifyContent: "center" } },
        h("button", { onclick: () => { shown = !shown; val.textContent = shown ? m.value : "•".repeat(12); } }, "Show / hide"),
        h("button", { onclick: async () => { try { await navigator.clipboard.writeText(m.value); toast("Copied"); } catch { toast("Clipboard unavailable", "warn"); } } }, "Copy"),
        h("button", { class: "primary", onclick: done }, "Done")),
      h("div", { class: "note" }, "Delivered directly to you. The agent got only DATA_DELIVERED_OUT_OF_BAND. Closing this drops it from memory."));
  });
});

on("secretCard", (m) => {
  modal((close) => {
    const input = h("input", { type: "password", autocomplete: "off", placeholder: "Paste or type the secret" }) as HTMLInputElement;
    const submit = () => { if (input.value) { send({ t: "secretSubmit", handle: m.handle, agent: m.agent, value: input.value }); close(); } };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") submit(); });
    setTimeout(() => input.focus(), 0);
    return h("div", { class: "modal secure" }, h("div", { class: "lock" }, ic("key")), h("h3", {}, m.label),
      m.purpose ? h("div", { class: "muted" }, `For: ${m.purpose}`) : "", input,
      h("div", { class: "row" }, h("button", { onclick: close }, "Cancel"), h("button", { class: "primary", onclick: submit }, "Hand over securely")),
      h("div", { class: "note" }, `The agent receives only the handle ${m.handle}.`));
  });
});

// ---------------- permission prompts ----------------

const prompts = new Map<string, () => void>();
on("permPrompt", (m) => {
  const close = modal((close) => h("div", { class: "modal" },
    h("h3", {}, `${m.name} wants a permission`),
    h("p", {}, m.explain),
    h("p", {}, h("code", {}, m.perm)),
    h("p", { class: "muted small" }, "Asked once. You can revoke it from the plugin's header at any time. Every call is recorded in the action log."),
    h("div", { class: "row" },
      h("button", { onclick: () => { send({ t: "permAnswer", prompt: m.prompt, grant: false }); close(); } }, "Deny"),
      h("button", { class: "primary", onclick: () => { send({ t: "permAnswer", prompt: m.prompt, grant: true }); close(); } }, "Allow"))));
  prompts.set(m.prompt, close);
});
on("permDone", (m) => { prompts.get(m.prompt)?.(); prompts.delete(m.prompt); });

// ---------------- drawing ----------------

function draw() {
  if (view.kind === "project" && desk && !project()) setView({ kind: "home" });
  const home = view.kind === "home";
  $("home-view").hidden = !home;
  $("project-view").hidden = home;
  bindChats();
  drawTop();
  if (home) drawHome(); else drawRail();
  render();
  projChat.drawCtx();
}

on("state", () => { render(); drawTop(); projChat.drawCtx(); if (view.kind === "home") drawHome(); });
on("desk", () => draw());
on("busy", () => drawTop());
on("disconnected", () => toast("Lost connection to the core; reconnecting…", "warn"));
on("turnEnd", () => position());
on("pluginChanged", () => { if (view.kind === "home") drawHome(); });
on("homeChanged", (m) => toastAgentChange(m));

// Refresh "x ago" labels now and then.
setInterval(() => { if (view.kind === "home") drawHome(); }, 60_000);

draw();
connect();
// Deep links: #customize opens Customize manually; #chats or #chats=<agent>
// opens the Chats panel (agents can point you at these too).
const hashArgs = new URLSearchParams(location.hash.slice(1));
if (hashArgs.has("home")) on("desk", function once() { if (!(window as any)._homeOpened) { (window as any)._homeOpened = true; go({ kind: "home" }); } });
if (hashArgs.has("eitri")) on("desk", () => { if (!(window as any)._czOpened) { (window as any)._czOpened = true; toggleCustomizer(true); } });
if (hashArgs.has("customize")) on("desk", function once() { startEditing(); drawHome(); });
if (hashArgs.has("project")) on("desk", () => { if (!(window as any)._projOpened && project(hashArgs.get("project")!)) { (window as any)._projOpened = true; go({ kind: "project", id: hashArgs.get("project")! }); } });
if (hashArgs.has("tasks")) on("desk", () => { if (!(window as any)._tasksOpened) { (window as any)._tasksOpened = true; work.openTasksApp(hashArgs.get("tasks") || undefined); } });
if (hashArgs.has("task")) on("desk", () => { if (!(window as any)._taskOpened) { (window as any)._taskOpened = true; work.openTask(hashArgs.get("task")!); } });
if (hashArgs.has("chats")) on("desk", () => { if (!(window as any)._chatsOpened) { (window as any)._chatsOpened = true; openChats(hashArgs.get("chats") || undefined); } });
(window as any).nexusProjectOfSpace = (sid: string) => projects().find((p) => p.space === sid)?.id || "";
(window as any).nexusDebug = { app: () => app, desk: () => desk, views: allViews, md };

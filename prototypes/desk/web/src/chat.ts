// Chat with an agent. Transcripts are kept per agent ("home", "p:<project>",
// "t:<task>"); a ChatPanel shows one of them and can be re-bound.

import { desk, h, on, send } from "./core";
import { proposalChecklist } from "./work";
import { askMsg, attachMentions, isPass, joinStrip, passMsg, personaMsg, replyBody, summaryText, xpostMsg } from "./personas";

export type Msg = { id: number; role: string; text: string; tool?: string; args?: string; result?: string; error?: boolean; done?: boolean; callId?: string; author?: string; from?: string; late?: boolean };

const transcripts = new Map<string, Msg[]>();
const busy = new Map<string, boolean>();
const listeners = new Set<(agent: string, kind: "reset" | "add" | "delta" | "update" | "busy", m?: Msg) => void>();

function list(agent: string): Msg[] {
  let l = transcripts.get(agent);
  if (!l) { l = []; transcripts.set(agent, l); }
  return l;
}
export const isBusy = (agent: string) => !!busy.get(agent);
export const hasTranscript = (agent: string) => (transcripts.get(agent)?.length || 0) > 0;
export const transcriptOf = (agent: string): Msg[] => transcripts.get(agent) || [];
/** Agents with a saved conversation (the core reloads them at startup). */
export const knownAgents = (): string[] => [...transcripts.keys()].filter(hasTranscript);
export type ChatEvent = "reset" | "add" | "delta" | "update" | "busy";
/** onChat calls f for every change to any transcript; returns an unsubscribe. */
export function onChat(f: (agent: string, kind: ChatEvent, m?: Msg) => void): () => void {
  listeners.add(f);
  return () => { listeners.delete(f); };
}

on("chatHistory", (m) => { transcripts.set(m.agent, m.messages || []); busy.set(m.agent, !!m.busy); listeners.forEach((f) => f(m.agent, "reset")); });
on("chat", (m) => { list(m.agent).push(m.msg); listeners.forEach((f) => f(m.agent, "add", m.msg)); });
on("chatDelta", (m) => {
  const msg = list(m.agent).find((x) => x.id === m.id);
  if (!msg) return;
  msg.text += m.text;
  listeners.forEach((f) => f(m.agent, "delta", msg));
});
on("chatUpdate", (m) => {
  const l = list(m.agent);
  const i = l.findIndex((x) => x.id === m.msg.id);
  if (i >= 0) l[i] = m.msg;
  listeners.forEach((f) => f(m.agent, "update", m.msg));
});
on("busy", (m) => { busy.set(m.agent, !!m.busy); if (!m.busy) drafting.delete(m.agent); listeners.forEach((f) => f(m.agent, "busy")); });
// While the model writes a big tool call (a whole page), show how far along it is.
const drafting = new Map<string, number>();
on("drafting", (m) => { if (m.chars) drafting.set(m.agent, m.chars); else drafting.delete(m.agent); listeners.forEach((f) => f(m.agent, "busy")); });
on("chat", (m) => { if (m.msg?.role === "tool") drafting.delete(m.agent); });

const toolLabels: Record<string, string> = {
  vault_reveal: "secure delivery", secret_request: "secure entry", pane_open: "opened a pane", plugin_write: "wrote a plugin",
  term_propose: "proposed a command", review_suggest: "suggested an edit", sheet_edit: "edited a sheet", capture_propose: "proposed a filing",
  project_add: "added a project", widget_write: "built a widget", task_draft: "drafted a task", report_write: "wrote the report",
  task_finish: "finished", file_read: "read", search: "searched", project_get: "looked at a project", project_discover: "scanned for projects",
  project_scan: "looked through the project", work_propose: "suggested work", ask_user: "asked", work_item_add: "added a work item",
  home_add: "added a widget", home_change: "changed a widget", home_remove: "removed a widget", home_grid: "changed the grid", home_show: "looked at the screen",
};

function argsOf(m: Msg): any { try { return JSON.parse(m.args || "{}"); } catch { return {}; } }
/** answered: the user has said something since this message. */
const answered = (agent: string, m: Msg) => transcriptOf(agent).some((x) => x.id > m.id && (x.role === "you" || x.role === "app"));

/** questionCard: the agent's ask_user, with answers to click. Several can
 *  be picked when multi is set. Typing in the chat box answers too. */
export function questionCard(agent: string, m: Msg, context?: () => { view?: string; selection?: string }): HTMLElement {
  const a = argsOf(m);
  const done = answered(agent, m) || !m.done || m.error;
  const picked = new Set<string>();
  const say = (text: string) => { const c = context?.() || {}; send({ t: "chat", agent, text, selection: c.selection || "", view: c.view || "" }); el.classList.add("done"); };
  const opts = (a.options || []) as string[];
  const el: HTMLElement = h("div", { class: `qcard${done ? " done" : ""}` },
    h("div", { class: "q" }, a.question || ""),
    h("div", { class: "chips" }, ...opts.map((o) => h("button", { type: "button", class: "chip", disabled: done,
      onclick: (e: Event) => {
        if (!a.multi) { say(o); return; }
        const b = e.currentTarget as HTMLElement;
        picked.has(o) ? picked.delete(o) : picked.add(o);
        b.classList.toggle("on", picked.has(o));
      } }, o)),
      a.multi && !done ? h("button", { type: "button", class: "primary small", onclick: () => { if (picked.size) say([...picked].join(", ")); } }, "Send") : ""),
    done ? "" : h("div", { class: "muted small" }, a.multi ? "Pick any, then Send. Or type your own answer." : "Click one, or type your own answer."));
  return el;
}

function proposeCard(m: Msg): HTMLElement | null {
  const b = /\(batch (b\d+)\)/.exec(m.result || "");
  return b ? proposalChecklist(b[1], { compact: true }) : null;
}

function renderMsg(m: Msg, agent = "", context?: () => { view?: string; selection?: string }): HTMLElement {
  if (m.role === "tool" && m.tool === "ask_user" && !m.error && agent) return questionCard(agent, m, context);
  if (m.role === "tool" && m.tool === "work_propose" && m.done && !m.error) {
    const c = proposeCard(m);
    if (c) return h("div", { class: "msg tool-card" }, c);
  }
  if (m.role === "tool") {
    const secure = m.tool === "vault_reveal" || m.tool === "secret_request";
    const el = h("div", { class: `msg tool${m.error ? " err" : ""}${secure ? " secure" : ""}${m.done ? "" : " pending"}`, onclick: () => el.classList.toggle("open") },
      h("span", { class: "tname" }, toolLabels[m.tool || ""] ? `${toolLabels[m.tool!]} · ${m.tool}` : m.tool || "tool"),
      m.args && m.args !== "{}" ? h("span", { class: "targs" }, m.args) : "",
      h("div", { class: "tres" }, m.result || (m.done ? "" : "running…")));
    return el;
  }
  if (m.role === "agent" && isPass(m.text)) return passMsg();
  if (m.role === "agent") return h("div", { class: "msg agent" }, replyBody(m.text, agent, m, !m.done));
  if (m.role === "persona") return personaMsg(m, agent);
  if (m.role === "ask") return askMsg(m);
  if (m.role === "xpost") return xpostMsg(m);
  if (m.role === "thinking") {
    const el = h("div", { class: "msg thinking", title: "Model reasoning (click to expand)", onclick: () => el.classList.toggle("open") }, m.text);
    return el;
  }
  if (m.role === "episode") return h("div", { class: "episode-div", title: "This conversation ended and was saved. The agent started fresh, with this summary in mind." }, h("span", {}, "Saved · " + m.text));
  if (m.role === "you") return h("div", { class: "msg you" }, m.text);
  if (m.role === "app") return h("div", { class: "msg app", title: m.text.startsWith("Message from the ") ? "From another agent, through Nexus" : "Sent by Nexus on your behalf" }, m.text);
  return h("div", { class: `msg system${m.error ? " error" : ""}` }, m.text);
}

export type ChatOpts = {
  placeholder?: string;
  empty?: () => HTMLElement | string; // shown when the transcript is empty
  context?: () => { selection?: string; view?: string };
  ctxLine?: () => HTMLElement | string | null; // "what the agent sees" line
  compact?: boolean;
};

/** MiniChat is the minimal style of the chat widget: a prompt box, a few
 *  suggestions, and the latest exchange. The whole conversation is one click
 *  away (onHistory opens it in the Chats panel). */
export type MiniOpts = {
  placeholder?: string;
  chips?: () => { label: string; text?: string; run?: () => void }[];
  intro?: () => HTMLElement | null;
  onHistory?: () => void;
  context?: () => { view?: string; selection?: string };
};
export class MiniChat {
  el: HTMLElement;
  private input: HTMLTextAreaElement;
  private out: HTMLElement;
  private chipsEl: HTMLElement;
  private introEl: HTMLElement;
  private off: () => void;
  private raf = 0;
  constructor(public agent: string, private opts: MiniOpts = {}) {
    this.input = h("textarea", { rows: 1, placeholder: opts.placeholder || "Ask anything" }) as HTMLTextAreaElement;
    const sendBtn = h("button", { type: "submit", class: "primary" }, "Ask");
    const form = h("form", { class: "mini-form" }, this.input, sendBtn);
    form.addEventListener("submit", (e) => { e.preventDefault(); this.submit(); });
    attachMentions(this.input, () => this.agent);
    this.input.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); this.submit(); } });
    this.input.addEventListener("input", () => { this.input.rows = Math.min(6, Math.max(1, this.input.value.split("\n").length)); });
    this.out = h("div", { class: "mini-out" });
    this.chipsEl = h("div", { class: "chips" });
    this.introEl = h("div", { class: "mini-intro" });
    this.el = h("div", { class: "mini-chat" }, this.introEl, form, this.chipsEl, this.out);
    const offChat = onChat((agent, kind) => { if (agent === this.agent) this.schedule(kind === "delta"); });
    const offDesk = on("desk", () => this.schedule(false)); // the join strip
    this.off = () => { offChat(); offDesk(); };
    this.draw();
  }
  destroy() { this.off(); }
  focus() { this.input.focus(); }
  ask(text: string) { this.input.value = text; this.submit(); }
  private submit() {
    const text = this.input.value.trim();
    if (!text) return;
    const c = this.opts.context?.() || {};
    send({ t: "chat", agent: this.agent, text, selection: c.selection || "", view: c.view || "" });
    this.input.value = ""; this.input.rows = 1;
  }
  private schedule(_delta: boolean) {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => { this.raf = 0; this.draw(); });
  }
  draw() {
    const intro = this.opts.intro?.();
    this.introEl.replaceChildren(...(intro ? [intro] : []));
    this.chipsEl.replaceChildren(...(this.opts.chips?.() || []).map((c) => h("button", { type: "button", class: "chip", onclick: () => c.run ? c.run() : this.ask(c.text || c.label) }, c.label)));
    const msgs = transcriptOf(this.agent);
    let i = msgs.length - 1;
    while (i >= 0 && msgs[i].role !== "you" && msgs[i].role !== "episode") i--; // your last question; prompts Nexus sent on its own stay in the history
    const ended = i >= 0 && msgs[i].role === "episode" ? msgs[i] : null; // a fresh conversation: show what the last one was
    const last = i >= 0 && !ended ? msgs.slice(i) : [];
    const kids: HTMLElement[] = [];
    const tools: string[] = [];
    for (const m of last) {
      if (m.role === "you") kids.push(h("div", { class: "msg you" }, m.text));
      else if (m.role === "app") continue;
      else if (m.role === "agent" && isPass(m.text)) kids.push(passMsg());
      else if (m.role === "agent" && m.text.trim()) kids.push(h("div", { class: "msg agent" }, replyBody(m.text, this.agent, m, !m.done)));
      else if (m.role === "persona") kids.push(personaMsg(m, this.agent));
      else if (m.role === "ask") kids.push(askMsg(m));
      else if (m.role === "tool" && m.tool === "ask_user" && !m.error) kids.push(questionCard(this.agent, m, this.opts.context));
      else if (m.role === "tool" && m.tool === "work_propose" && m.done && !m.error && proposeCard(m)) kids.push(proposeCard(m)!);
      else if (m.role === "tool") tools.push((m.error ? "⚠ " : "") + (toolLabels[m.tool || ""] || m.tool || "tool"));
      else if (m.role === "system" && m.error) kids.push(h("div", { class: "msg system error" }, m.text));
    }
    if (tools.length) kids.splice(1, 0, h("div", { class: "mini-tools muted small" }, "Did: " + tools.join(" · ")));
    if (ended) kids.push(h("div", { class: "episode-div" }, h("span", {}, "Saved · " + ended.text)));
    const j = joinStrip(this.agent);
    if (j) kids.push(j);
    if (isBusy(this.agent)) kids.push(h("div", { class: "muted small" }, h("span", { class: "busy-dot" }), " working…", h("button", { type: "button", class: "link", onclick: () => send({ t: "abort", agent: this.agent }) }, "Stop")));
    const talk = (l: Msg[]) => l.filter((m) => m.role === "you" || m.role === "agent" || m.role === "persona").length;
    if (talk(msgs) > talk(last)) kids.push(h("button", { type: "button", class: "link small", onclick: () => this.opts.onHistory?.() }, `Conversation history (${talk(msgs)} messages)`));
    this.out.replaceChildren(...kids);
  }
}

export class ChatPanel {
  el: HTMLElement;
  private log: HTMLElement;
  private input: HTMLTextAreaElement;
  private stop: HTMLButtonElement;
  private status: HTMLElement;
  private ctx: HTMLElement;
  private queue: HTMLElement;
  private pins: HTMLElement;
  private agent = "";
  private opts: ChatOpts = {};
  private els = new Map<number, HTMLElement>();
  private raf = 0;

  constructor(cls = "") {
    this.log = h("div", { class: "chat-log" });
    this.input = h("textarea", { rows: 3 }) as HTMLTextAreaElement;
    this.stop = h("button", { type: "button", hidden: true, onclick: () => send({ t: "abort", agent: this.agent }) }, "Stop") as HTMLButtonElement;
    this.status = h("span", { class: "agent-name" });
    this.ctx = h("div", { class: "chat-ctx" });
    this.queue = h("div", { class: "chat-queue" });
    this.pins = h("div", { class: "chat-pins" });
    const form = h("form", { class: "chat-form" }, this.input,
      h("div", { class: "chat-actions" }, this.status, this.stop, h("button", { type: "submit", class: "primary" }, "Send")));
    form.addEventListener("submit", (e) => { e.preventDefault(); this.submit(); });
    attachMentions(this.input, () => this.agent);
    this.input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); this.submit(); }
    });
    this.el = h("div", { class: `chat ${cls}` }, this.pins, this.log, this.queue, this.ctx, form);
    on("desk", () => { this.drawPins(); this.drawQueue(); });
    listeners.add((agent, kind, m) => {
      if (agent !== this.agent) return;
      if (kind === "reset") this.redraw();
      else if (kind === "add" && m && m.role === "episode") this.redraw();
      else if (kind === "add" && m) this.append(m);
      else if (kind === "update" && m) { const el = this.els.get(m.id); if (el) { const n = this.render(m); el.replaceWith(n); this.els.set(m.id, n); } }
      if (kind === "add" && m && (m.role === "you" || m.role === "app")) this.log.querySelectorAll(".qcard:not(.done)").forEach((q) => q.classList.add("done"));
      else if (kind === "delta") this.flush();
      else if (kind === "busy") this.drawStatus();
    });
  }

  bind(agent: string, opts: ChatOpts = {}) {
    const changed = agent !== this.agent;
    this.agent = agent;
    this.opts = opts;
    this.input.placeholder = opts.placeholder || "Talk to the agent, or @ a persona. Enter to send, Shift+Enter for a new line.";
    if (changed) this.redraw();
    this.drawPins();
    this.drawCtx();
    this.drawStatus();
  }

  focus() { this.input.focus(); }
  setText(t: string) { this.input.value = t; this.input.focus(); }

  drawCtx() {
    const c = this.opts.ctxLine?.();
    this.ctx.hidden = !c;
    if (c) this.ctx.replaceChildren(c);
    this.drawQueue();
  }

  /** Pins are yours: messages you keep in view at the top of the chat so
   *  you don't have to scroll back. The agent never sees them. */
  private pinned(): { msg: number; role: string; text: string }[] {
    return (((desk as any)?.desk?.pins || {})[this.agent] || []) as { msg: number; role: string; text: string }[];
  }
  private drawPins() {
    const ps = this.pinned();
    this.pins.hidden = !ps.length;
    const ids = new Set(ps.map((p) => p.msg));
    this.els.forEach((el, id) => el.classList.toggle("pinned", ids.has(id)));
    if (!ps.length) { this.pins.replaceChildren(); return; }
    this.pins.replaceChildren(...ps.map((p) => h("div", { class: `pin pin-${p.role}` },
      h("span", { class: "pin-ic", title: "Pinned (only you see pins)" }, "📌"),
      h("button", { class: "pin-t", title: "Show it in the conversation", onclick: () => {
        const el = this.els.get(p.msg);
        if (!el) return;
        el.closest("details")?.setAttribute("open", "");
        el.scrollIntoView({ block: "center", behavior: "smooth" });
        el.classList.add("flash"); setTimeout(() => el.classList.remove("flash"), 1200);
      } }, (p.role === "you" ? "You: " : "") + summaryText(p.text)),
      h("button", { class: "ebtn", title: "Unpin", onclick: () => send({ t: "pin", agent: this.agent, msg: p.msg, on: false }) }, "✕"))));
  }

  /** Messages from other agents (or hand-offs) waiting for this agent's
   *  turn to end. You can let it finish, or interrupt. */
  private drawQueue() {
    const q: { from: string; text: string }[] = ((desk as any)?.queues || {})[this.agent] || [];
    const join = joinStrip(this.agent);
    this.queue.hidden = !q.length && !join;
    if (!q.length) { this.queue.replaceChildren(...(join ? [join] : [])); return; }
    this.queue.replaceChildren(...(join ? [join] : []), ...q.map((m) => h("div", { class: "cq-item" },
      h("span", { class: "muted small" }, `Waiting for its turn to end · from the ${m.from === "app" ? "app (a hand-off)" : m.from}`),
      h("div", { class: "small" }, m.text.length > 180 ? m.text.slice(0, 180) + "…" : m.text))),
      h("div", { class: "row" }, h("button", { title: "Stop what it's doing so the message goes now", onclick: () => send({ t: "agentInterrupt", agent: this.agent }) }, "Interrupt?"),
        h("span", { class: "muted small" }, "or let it finish first")));
  }

  private drawStatus() {
    const b = isBusy(this.agent);
    this.stop.hidden = !b;
    const d = drafting.get(this.agent);
    const what = d ? `writing a tool call (${d < 1024 ? d + " B" : Math.round(d / 1024) + " KB"} so far)…` : "working…";
    this.status.replaceChildren(b ? h("span", { class: "busy-dot" }) : "", b ? what : "");
    this.status.title = d ? "The model is still writing the arguments of a tool call, e.g. a whole HTML page. On a local model this can take minutes." : "";
  }

  private submit() {
    const text = this.input.value.trim();
    if (!text || !this.agent) return;
    const c = this.opts.context?.() || {};
    send({ t: "chat", agent: this.agent, text, selection: c.selection || "", view: c.view || "" });
    this.input.value = "";
  }

  private redraw() {
    this.log.replaceChildren();
    this.els.clear();
    const msgs = transcripts.get(this.agent) || [];
    if (!msgs.length && this.opts.empty) {
      const e = this.opts.empty();
      this.log.append(typeof e === "string" ? h("div", { class: "msg system" }, e) : e);
    }
    // Ended episodes fold behind their summary; the current one is shown.
    let start = 0;
    msgs.forEach((m, i) => {
      if (m.role !== "episode") return;
      const part = msgs.slice(start, i);
      start = i + 1;
      const d = h("details", { class: "episode" }, h("summary", {}, h("span", { class: "muted small" }, `Earlier: ${m.text} · ${part.filter((x) => x.role === "you" || x.role === "agent").length} messages`)));
      part.forEach((x) => { const el = this.render(x); this.els.set(x.id, el); d.append(el); });
      this.els.set(m.id, d);
      this.log.append(d);
    });
    msgs.slice(start).forEach((m) => this.append(m, false));
    this.log.scrollTop = this.log.scrollHeight;
  }

  /** render is renderMsg plus a pin button on what was said. */
  private render(m: Msg): HTMLElement {
    const el = renderMsg(m, this.agent, this.opts.context);
    if ((m.role === "you" || m.role === "agent" || m.role === "app" || (m.role === "persona" && m.done && !m.error)) && m.text.trim()) {
      const on = () => this.pinned().some((p) => p.msg === m.id);
      el.classList.add("pinnable");
      el.classList.toggle("pinned", on());
      el.append(h("button", { class: "msg-pin", type: "button", title: "Pin it to the top of this chat (only you see pins)",
        onclick: (e: Event) => { e.stopPropagation(); send({ t: "pin", agent: this.agent, msg: m.id, on: !on() }); } }, "📌"));
    }
    return el;
  }

  private append(m: Msg, stick = true) {
    if (this.els.size === 0) this.log.querySelector(".chat-empty")?.remove();
    const near = this.log.scrollHeight - this.log.scrollTop - this.log.clientHeight < 80;
    const el = this.render(m);
    this.els.set(m.id, el);
    this.log.append(el);
    if (!stick || near) this.log.scrollTop = this.log.scrollHeight;
  }

  private flush() {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => {
      this.raf = 0;
      for (const m of transcripts.get(this.agent) || []) {
        const el = this.els.get(m.id);
        if (!el) continue;
        if (m.role === "agent" && el.dataset.len !== String(m.text.length)) { const n = this.render(m); n.dataset.len = String(m.text.length); el.replaceWith(n); this.els.set(m.id, n); }
        if (m.role === "thinking") el.textContent = m.text;
      }
      this.log.scrollTop = this.log.scrollHeight;
    });
  }
}

// Chat with an agent. Transcripts are kept per agent ("home", "p:<project>",
// "t:<task>"); a ChatPanel shows one of them and can be re-bound.

import { h, on, send } from "./core";
import { md } from "./md";

export type Msg = { id: number; role: string; text: string; tool?: string; args?: string; result?: string; error?: boolean; done?: boolean; callId?: string };

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
};

function renderMsg(m: Msg): HTMLElement {
  if (m.role === "tool") {
    const secure = m.tool === "vault_reveal" || m.tool === "secret_request";
    const el = h("div", { class: `msg tool${m.error ? " err" : ""}${secure ? " secure" : ""}${m.done ? "" : " pending"}`, onclick: () => el.classList.toggle("open") },
      h("span", { class: "tname" }, toolLabels[m.tool || ""] ? `${toolLabels[m.tool!]} · ${m.tool}` : m.tool || "tool"),
      m.args && m.args !== "{}" ? h("span", { class: "targs" }, m.args) : "",
      h("div", { class: "tres" }, m.result || (m.done ? "" : "running…")));
    return el;
  }
  if (m.role === "agent") return h("div", { class: "msg agent", html: md(m.text) });
  if (m.role === "thinking") {
    const el = h("div", { class: "msg thinking", title: "Model reasoning (click to expand)", onclick: () => el.classList.toggle("open") }, m.text);
    return el;
  }
  if (m.role === "you") return h("div", { class: "msg you" }, m.text);
  if (m.role === "app") return h("div", { class: "msg app", title: "Sent by Nexus on your behalf" }, m.text);
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
  context?: () => { view?: string };
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
    this.input.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); this.submit(); } });
    this.input.addEventListener("input", () => { this.input.rows = Math.min(6, Math.max(1, this.input.value.split("\n").length)); });
    this.out = h("div", { class: "mini-out" });
    this.chipsEl = h("div", { class: "chips" });
    this.introEl = h("div", { class: "mini-intro" });
    this.el = h("div", { class: "mini-chat" }, this.introEl, form, this.chipsEl, this.out);
    this.off = onChat((agent, kind) => { if (agent === this.agent) this.schedule(kind === "delta"); });
    this.draw();
  }
  destroy() { this.off(); }
  focus() { this.input.focus(); }
  ask(text: string) { this.input.value = text; this.submit(); }
  private submit() {
    const text = this.input.value.trim();
    if (!text) return;
    send({ t: "chat", agent: this.agent, text, selection: "", view: this.opts.context?.().view || "" });
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
    while (i >= 0 && msgs[i].role !== "you") i--; // your last question; prompts Nexus sent on its own stay in the history
    const last = i >= 0 ? msgs.slice(i) : [];
    const kids: HTMLElement[] = [];
    const tools: string[] = [];
    for (const m of last) {
      if (m.role === "you") kids.push(h("div", { class: "msg you" }, m.text));
      else if (m.role === "app") continue;
      else if (m.role === "agent" && m.text.trim()) kids.push(h("div", { class: "msg agent", html: md(m.text) }));
      else if (m.role === "tool") tools.push((m.error ? "⚠ " : "") + (toolLabels[m.tool || ""] || m.tool || "tool"));
      else if (m.role === "system" && m.error) kids.push(h("div", { class: "msg system error" }, m.text));
    }
    if (tools.length) kids.splice(1, 0, h("div", { class: "mini-tools muted small" }, "Used: " + tools.join(" · ")));
    if (isBusy(this.agent)) kids.push(h("div", { class: "muted small" }, h("span", { class: "busy-dot" }), " working…", h("button", { type: "button", class: "link", onclick: () => send({ t: "abort", agent: this.agent }) }, "Stop")));
    const talk = (l: Msg[]) => l.filter((m) => m.role === "you" || m.role === "agent").length;
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
    const form = h("form", { class: "chat-form" }, this.input,
      h("div", { class: "chat-actions" }, this.status, this.stop, h("button", { type: "submit", class: "primary" }, "Send")));
    form.addEventListener("submit", (e) => { e.preventDefault(); this.submit(); });
    this.input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); this.submit(); }
    });
    this.el = h("div", { class: `chat ${cls}` }, this.log, this.ctx, form);
    listeners.add((agent, kind, m) => {
      if (agent !== this.agent) return;
      if (kind === "reset") this.redraw();
      else if (kind === "add" && m) this.append(m);
      else if (kind === "update" && m) { const el = this.els.get(m.id); if (el) { const n = renderMsg(m); el.replaceWith(n); this.els.set(m.id, n); } }
      else if (kind === "delta") this.flush();
      else if (kind === "busy") this.drawStatus();
    });
  }

  bind(agent: string, opts: ChatOpts = {}) {
    const changed = agent !== this.agent;
    this.agent = agent;
    this.opts = opts;
    this.input.placeholder = opts.placeholder || "Talk to the agent. Enter to send, Shift+Enter for a new line.";
    if (changed) this.redraw();
    this.drawCtx();
    this.drawStatus();
  }

  focus() { this.input.focus(); }
  setText(t: string) { this.input.value = t; this.input.focus(); }

  drawCtx() {
    const c = this.opts.ctxLine?.();
    this.ctx.hidden = !c;
    if (c) this.ctx.replaceChildren(c);
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
    msgs.forEach((m) => this.append(m, false));
    this.log.scrollTop = this.log.scrollHeight;
  }

  private append(m: Msg, stick = true) {
    if (this.els.size === 0) this.log.querySelector(".chat-empty")?.remove();
    const near = this.log.scrollHeight - this.log.scrollTop - this.log.clientHeight < 80;
    const el = renderMsg(m);
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
        if (m.role === "agent" && el.dataset.len !== String(m.text.length)) { el.innerHTML = md(m.text); el.dataset.len = String(m.text.length); }
        if (m.role === "thinking") el.textContent = m.text;
      }
      this.log.scrollTop = this.log.scrollHeight;
    });
  }
}

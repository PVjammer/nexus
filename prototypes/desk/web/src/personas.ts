// Other voices in a chat: personas, workstream agents, project agents, the
// home agent, reached with @handle (mentions.go, personas.go). And reply
// styles (styles.go): tl;dr collapsed, verdict badges. This is how their
// messages look and the @ menu in a composer.

import { desk, h, send } from "./core";
import { md } from "./md";

export type Persona = { handle: string; name: string; about?: string; color?: string; model?: string; builtin?: boolean };
export type Target = { handle: string; name: string; kind: "persona" | "home" | "project" | "workstream"; agent?: string; project?: string; about?: string; color?: string };
type Style = { name: string; title?: string; applies: string[]; render: "collapse" | "verdict"; verdicts?: { text: string; tone: string }[]; enabled: boolean };

export const personas = (): Persona[] => ((desk as any)?.personas || []) as Persona[];
const handles = (): Target[] => ((desk as any)?.handles || []) as Target[];
const styles = (): Style[] => (((desk as any)?.styles || []) as Style[]).filter((s) => s.enabled);
const targetOf = (handle: string) => handles().find((t) => t.handle === handle);

/** The project a chat belongs to ("" for home). */
function chatProject(agent: string): string {
  if (agent.startsWith("p:")) return agent.slice(2);
  if (agent.startsWith("w:")) return (((desk as any)?.workItems || []) as any[]).find((w) => w.ref === agent.slice(2))?.project || "";
  return "";
}

/** Who a chat can mention: like the core's targetsFor, minus itself. */
export function targetsFor(agent: string): Target[] {
  const p = chatProject(agent);
  return handles().filter((t) => t.agent !== agent && !(t.kind === "workstream" && p && t.project !== p));
}

const kindColor = (t?: Target) => t?.color || (t?.kind === "workstream" ? "var(--you)" : t?.kind === "persona" ? "var(--agent)" : "var(--accent)");
const kindLabel: Record<string, string> = { persona: "persona", home: "home agent", project: "project agent", workstream: "workstream" };

// ---- styles ----

const styleKeyOf = (agent: string) => agent === "home" ? "home" : agent.startsWith("p:") ? "project" : agent.startsWith("w:") ? "workstream" : agent.startsWith("t:") ? "task" : agent === "customizer" ? "customizer" : agent;
const applies = (s: Style, key: string) => s.applies.some((x) => x === key || x === "all" || (x === "personas" && key.startsWith("persona:")));
/** The style key of a message: its author's if a mentioned agent wrote it, else the chat's. */
const msgKey = (chat: string, m: { role: string; author?: string; from?: string }) =>
  m.role === "persona" ? (m.from ? styleKeyOf(m.from) : "persona:" + m.author) : styleKeyOf(chat);

const BREAK = /^\s*---\s*$/m;
const strip = (s: string) => s.replace(/^[\s*_"'“”]+|[\s*_"'“”]+$/g, "");

/** A reply's body, shaped by the styles on for whoever wrote it. A reply
 *  that doesn't follow its style renders as usual. */
export function replyBody(text: string, chat: string, m: { role: string; author?: string; from?: string }, streaming = false): HTMLElement {
  const key = msgKey(chat, m);
  const on = styles().filter((s) => applies(s, key));
  let body = text;
  let badge: HTMLElement | null = null;
  const verdict = on.find((s) => s.render === "verdict");
  if (verdict) {
    const nl = body.indexOf("\n");
    const first = strip(nl < 0 ? body : body.slice(0, nl));
    const v = (verdict.verdicts || []).find((x) => strip(x.text).toLowerCase() === first.toLowerCase());
    if (v) { badge = h("span", { class: `verdict v-${v.tone || "warn"}` }, v.text); body = nl < 0 ? "" : body.slice(nl + 1).trimStart(); }
  }
  const out = h("div", { class: "reply-body" });
  if (badge) out.append(badge);
  const cut = on.some((s) => s.render === "collapse") ? BREAK.exec(body) : null;
  if (cut && cut.index > 0) {
    const sum = h("div", { class: "tldr", html: md(body.slice(0, cut.index)) });
    const more = h("div", { class: "tldr-more", html: md(body.slice(cut.index + cut[0].length)) });
    const hint = h("button", { type: "button", class: "link small tldr-toggle", onclick: (e: Event) => { e.stopPropagation(); out.classList.toggle("open"); } },
      streaming ? "more… (still writing)" : "more…");
    out.classList.add("collapsible");
    out.append(sum, hint, more);
    out.addEventListener("click", (e) => { if (!(e.target as HTMLElement).closest("a,button,code,pre")) out.classList.add("open"); });
  } else {
    out.append(h("div", { html: md(body) }));
  }
  return out;
}

/** The summary of a reply (for pins): what's before its "---". */
export const summaryText = (text: string) => { const c = BREAK.exec(text); return c && c.index > 0 ? text.slice(0, c.index).trim() : text; };

// ---- messages from other voices ----

/** A mentioned agent's answer: its name in its colour, then what it said. */
export function personaMsg(m: { author?: string; from?: string; text: string; done?: boolean; error?: boolean; late?: boolean; role: string }, chat = ""): HTMLElement {
  const t = targetOf(m.author || "");
  const p = personas().find((x) => x.handle === m.author);
  const name = t?.name || p?.name || "@" + m.author;
  const kind = t?.kind || (m.from ? "workstream" : "persona");
  const body = !m.done
    ? h("div", { class: "persona-body muted" }, h("span", { class: "busy-dot" }), m.from ? " answering from a side session…" : " thinking…")
    : m.error ? h("div", { class: "persona-body error" }, m.text)
    : replyBody(m.text, chat, m);
  body.classList.add("persona-body");
  const el = h("div", { class: `msg persona${m.error ? " err" : ""}`, title: t?.about || p?.about || "" },
    h("div", { class: "persona-who" }, h("b", {}, name), h("span", { class: "muted small" }, ` @${m.author} · ${kindLabel[kind] || kind}`)),
    body);
  if (m.late && chat) el.append(h("div", { class: "late-chip" }, h("span", { class: "muted small" }, "Came in after the agent went ahead. "),
    h("button", { type: "button", class: "link small", onclick: () => send({ t: "chat", agent: chat, text: `${name} replied late (above). Update your last answer with it.`, selection: "", view: "" }) }, "Update with it")));
  el.style.setProperty("--pc", kindColor(t) );
  if (p?.color) el.style.setProperty("--pc", p.color);
  return el;
}

/** An agent asking another (ask_agents): who, and the question. */
export function askMsg(m: { author?: string; text: string }): HTMLElement {
  const t = targetOf(m.author || "");
  const el = h("div", { class: "msg ask" }, h("span", { class: "muted small" }, "asks "), h("b", {}, "@" + m.author), h("span", {}, " " + m.text));
  el.style.setProperty("--pc", kindColor(t));
  return el;
}

/** In an agent's own chat: what it was asked elsewhere and answered from a side session. */
export function xpostMsg(m: { text: string }): HTMLElement {
  const el = h("div", { class: "msg xpost", title: "Asked in another chat; answered from a side session. Its session learns about it at its next turn." },
    h("div", { class: "muted small" }, "↪ from another chat"), h("div", { html: md(m.text) }));
  el.addEventListener("click", () => el.classList.toggle("open"));
  return el;
}

/** The chat's agent had nothing to add after the replies (it said PASS). */
export const isPass = (text: string) => text.replace(/[^A-Za-z]/g, "").toUpperCase() === "PASS";
export const passMsg = () => h("div", { class: "msg pass muted small" }, "Nothing to add from this chat's agent.");

// ---- the @ menu ----

/** attachMentions gives a composer an @ menu of who the chat can reach.
 *  Call it before the composer's own Enter handler is added: while the menu
 *  is open, Enter and Tab pick from it. */
export function attachMentions(input: HTMLTextAreaElement, chat: () => string) {
  const menu = h("div", { class: "mention-menu", hidden: true });
  let items: Target[] = [];
  let sel = 0;
  let start = -1; // where the "@..." being typed starts
  const place = () => { if (!menu.isConnected) input.parentElement?.append(menu); };
  const close = () => { menu.hidden = true; items = []; start = -1; };
  const draw = () => {
    menu.replaceChildren(...items.map((t, i) => {
      const b = h("button", { type: "button", class: `mention${i === sel ? " on" : ""}`,
        onmousedown: (e: Event) => { e.preventDefault(); pick(t); } },
        h("b", {}, "@" + t.handle), " ", h("span", {}, t.name), h("span", { class: "muted small" }, " · " + (t.about || kindLabel[t.kind])));
      b.style.setProperty("--pc", kindColor(t));
      return b;
    }));
  };
  const pick = (t: Target) => {
    const v = input.value, caret = input.selectionStart ?? v.length;
    input.value = v.slice(0, start) + "@" + t.handle + " " + v.slice(caret);
    const at = start + t.handle.length + 2;
    input.setSelectionRange(at, at);
    close();
    input.focus();
  };
  input.addEventListener("input", () => {
    const caret = input.selectionStart ?? input.value.length;
    const m = /(^|[\s(])@([\w-]*)$/.exec(input.value.slice(0, caret));
    const all = targetsFor(chat());
    if (!m || !all.length) return close();
    const q = m[2].toLowerCase();
    const order = (t: Target) => (t.kind === "workstream" ? 0 : t.kind === "persona" ? 1 : 2);
    items = all.filter((t) => t.handle.startsWith(q) || t.name.toLowerCase().includes(q)).sort((x, y) => order(x) - order(y)).slice(0, 8);
    if (!items.length) return close();
    start = caret - m[2].length - 1;
    sel = Math.min(sel, items.length - 1);
    place();
    menu.hidden = false;
    draw();
  });
  input.addEventListener("keydown", (e) => {
    if (menu.hidden || !items.length) return;
    const stop = () => { e.preventDefault(); e.stopImmediatePropagation(); };
    if (e.key === "ArrowDown" || e.key === "ArrowUp") { stop(); sel = (sel + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length; draw(); }
    else if (e.key === "Enter" || e.key === "Tab") { stop(); pick(items[sel]); }
    else if (e.key === "Escape") { stop(); close(); }
  }, { capture: true });
  input.addEventListener("blur", () => setTimeout(close, 100));
}

/** The join: who the chat's agent is still waiting on, with a way to go on. */
export function joinStrip(agent: string): HTMLElement | null {
  const w: string[] = ((desk as any)?.joins || {})[agent] || [];
  if (!w.length) return null;
  return h("div", { class: "cq-item join" },
    h("span", { class: "busy-dot" }), h("span", { class: "small" }, ` Waiting for ${w.map((x) => "@" + x).join(", ")}… then this chat's agent answers. `),
    h("button", { type: "button", class: "link small", onclick: () => send({ t: "joinGo", agent }) }, "Go without them"));
}

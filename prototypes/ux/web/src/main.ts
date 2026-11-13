// Shell entry: chat dock, top bar, menus, permission prompts.

import { app, connect, h, modal, on, promptModal, send, space, toast, token } from "./core";
import { allViews, getView, position, render } from "./layout";
import { md } from "./panes";
import { ic, kindIc } from "./icons";

if (!token) {
  document.body.innerHTML = `<div style="padding:40px;font-family:system-ui">Missing token. Open the URL that <code>nexus-core</code> printed (it ends in <code>#token=…</code>), or run <code>make app</code>.</div>`;
  throw new Error("no token");
}

// ---------------- chat ----------------

type Msg = { id: number; role: string; text: string; tool?: string; args?: string; result?: string; error?: boolean; done?: boolean; callId?: string };
const logEl = document.getElementById("chat-log")!;
const input = document.getElementById("chat-input") as HTMLTextAreaElement;
const stopBtn = document.getElementById("chat-stop") as HTMLButtonElement;
const nameEl = document.getElementById("agent-name")!;
const msgEls = new Map<number, { el: HTMLElement; msg: Msg }>();
let busy = false;

const toolLabels: Record<string, string> = {
  vault_reveal: "secure delivery", secret_request: "secure entry", pane_open: "opened a pane", plugin_write: "wrote a plugin",
  term_propose: "proposed a command", review_suggest: "suggested an edit", sheet_edit: "edited a sheet",
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
  if (m.role === "you") {
    const text = m.text;
    return h("div", { class: "msg you" }, text);
  }
  return h("div", { class: `msg system${m.error ? " error" : ""}` }, m.text);
}

function addMsg(m: Msg) {
  const el = renderMsg(m);
  msgEls.set(m.id, { el, msg: m });
  const stick = logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 80;
  logEl.append(el);
  if (stick) logEl.scrollTop = logEl.scrollHeight;
}

on("chatHistory", (m) => {
  logEl.replaceChildren();
  msgEls.clear();
  (m.messages || []).forEach(addMsg);
  if (!m.messages?.length) {
    logEl.append(h("div", { class: "msg system" },
      "Hi. I'm the agent. I can arrange panes, work on your sheets and documents with you, propose terminal commands and build plugins on the spot. Secrets go to secure panes and never pass through me.\n\nTry: “open welcome.md and pantry.csv side by side”."));
  }
  setBusy(!!m.busy);
  logEl.scrollTop = logEl.scrollHeight;
});
on("chat", (m) => addMsg(m.msg));
let deltaRaf = 0;
on("chatDelta", (m) => {
  const e = msgEls.get(m.id);
  if (!e) return;
  e.msg.text += m.text;
  if (!deltaRaf) deltaRaf = requestAnimationFrame(() => {
    deltaRaf = 0;
    for (const v of msgEls.values()) {
      if (v.msg.role === "agent" && v.el.dataset.len !== String(v.msg.text.length)) { v.el.innerHTML = md(v.msg.text); v.el.dataset.len = String(v.msg.text.length); }
      if (v.msg.role === "thinking") v.el.textContent = v.msg.text;
    }
    logEl.scrollTop = logEl.scrollHeight;
  });
});
on("chatUpdate", (m) => {
  const e = msgEls.get(m.msg.id);
  if (!e) return;
  const el = renderMsg(m.msg);
  e.el.replaceWith(el);
  msgEls.set(m.msg.id, { el, msg: m.msg });
});
function setBusy(b: boolean) {
  busy = b;
  stopBtn.hidden = !b;
  drawAgentName();
}
on("busy", (m) => setBusy(m.busy));

function drawAgentName() {
  nameEl.replaceChildren(busy ? h("span", { class: "busy-dot" }) : "", app?.agent || "");
}

function currentSelection(): string {
  const sp = space();
  if (!sp || !app) return "";
  const g = findFocused();
  return g ? getView(g)?.selection?.() || "" : "";
}
function findFocused(): string | null {
  const sp = space();
  if (!sp) return null;
  const find = (n: any): any => n.kind === "group" ? (n.id === sp.focused ? n : null) : (n.children || []).map(find).find(Boolean);
  return find(sp.root)?.active || null;
}

document.getElementById("chat-form")!.addEventListener("submit", (e) => {
  e.preventDefault();
  const text = input.value.trim();
  if (!text) return;
  send({ t: "chat", text, selection: currentSelection() });
  input.value = "";
});
input.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); (document.getElementById("chat-form") as HTMLFormElement).requestSubmit(); }
});
stopBtn.addEventListener("click", () => send({ t: "abort" }));

// What the agent sees, always visible above the input.
function drawCtx() {
  const ctx = document.getElementById("chat-ctx")!;
  const id = findFocused();
  const p = id ? app?.state.panes[id] : null;
  if (!p) { ctx.replaceChildren("Agent sees: the layout (no focused pane)"); return; }
  const what = p.visibility === "full" ? "its content" : p.visibility === "title" ? "its title only" : "nothing (private)";
  ctx.replaceChildren("Agent sees focused pane ", h("b", {}, kindIc(p.kind), ` ${p.title}`), `: ${what}`,
    p.visibility === "full" ? " · plus your text selection" : "");
}

// chat dock resize (local only)
{
  const chat = document.getElementById("chat")!;
  const rz = document.getElementById("chat-resizer")!;
  try { const w = localStorage.getItem("nexus-chat-w"); if (w) chat.style.width = w; } catch {}
  rz.addEventListener("mousedown", (e) => {
    e.preventDefault();
    document.body.classList.add("dragging");
    const move = (ev: MouseEvent) => { chat.style.width = `${Math.max(260, Math.min(innerWidth * 0.7, ev.clientX))}px`; position(); };
    const up = () => {
      document.removeEventListener("mousemove", move); document.removeEventListener("mouseup", up);
      document.body.classList.remove("dragging");
      try { localStorage.setItem("nexus-chat-w", chat.style.width); } catch {}
    };
    document.addEventListener("mousemove", move); document.addEventListener("mouseup", up);
  });
}

// ---------------- top bar ----------------

function drawTop() {
  if (!app) return;
  const spaces = document.getElementById("spaces")!;
  spaces.replaceChildren(...app.state.spaces.map((s) => h("button", {
    class: s.id === app!.state.activeSpace ? "active" : "", onclick: () => send({ t: "spaceSwitch", space: s.id }),
    title: "Spaces keep their own layout and panes",
  }, s.name)), h("button", { title: "New space", onclick: async () => { const n = await promptModal("New space", "e.g. Homelab, Spring trip, Bathroom reno"); if (n) send({ t: "spaceCreate", name: n }); } }, "+"));

  const tb = document.getElementById("toolbar")!;
  const undo = app.undo.depth > 0
    ? h("button", { id: "undo-agent", title: `Undo the agent's last layout change (${app.undo.depth} available)`, onclick: () => send({ t: "undo" }) }, ic("undo"), ` Undo agent: ${app.undo.label}`)
    : null;
  tb.replaceChildren(...[undo, menu("Arrange ▾", () => [
    item("One group (all tabs)", () => send({ t: "preset", name: "single" })),
    item("Side by side", () => send({ t: "preset", name: "side-by-side" })),
    item("Columns (one per pane)", () => send({ t: "preset", name: "columns" })),
    item("Grid", () => send({ t: "preset", name: "grid" })),
  ]), menu("Open ▾", () => openItems())].filter(Boolean) as HTMLElement[]);
}

function openItems(): HTMLElement[] {
  const open = (kind: string, props: any = {}, title?: string) => send({ t: "open", kind, props, title });
  const pls = app?.plugins || [];
  return [
    item("Files", () => open("files", { path: "" })),
    item("Welcome guide", () => open("review", { path: "workspace/welcome.md" })),
    item("New sheet…", async () => { const n = await promptModal("New sheet", "file name, e.g. packing-list"); if (n) send({ t: "sheetNew", path: `workspace/${n}` }); }),
    item("Terminal", () => open("terminal")),
    item("Browser…", async () => { let u = await promptModal("Open a page to follow along", "https://…"); if (u) { if (!/^https?:\/\//.test(u)) u = "https://" + u; open("browser", { url: u }); } }),
    item("Action log", () => open("log")),
    sep(),
    h("div", { class: "hint" }, pls.length ? "Plugins" : "No plugins yet: ask the agent to make one"),
    ...pls.map((p) => item(`${p.name}${p.kept ? "" : " (draft)"}`, () => open("plugin", { plugin: p.id }, p.name))),
  ];
}

function menu(label: string, items: () => HTMLElement[]): HTMLElement {
  const wrap = h("div", { class: "menu" });
  const btn = h("button", {}, label);
  let list: HTMLElement | null = null;
  const close = () => { list?.remove(); list = null; document.removeEventListener("mousedown", outside, true); };
  const outside = (e: Event) => { if (!wrap.contains(e.target as any)) close(); };
  btn.addEventListener("click", () => {
    if (list) return close();
    list = h("div", { class: "menu-list" }, ...items());
    list.addEventListener("click", (e) => { if ((e.target as HTMLElement).tagName === "BUTTON") close(); });
    wrap.append(list);
    document.addEventListener("mousedown", outside, true);
  });
  wrap.append(btn);
  return wrap;
}
const item = (label: string, f: () => void) => h("button", { onclick: f }, label);
const sep = () => h("div", { class: "sep" });

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

// ---------------- state ----------------

on("state", () => { render(); drawTop(); drawCtx(); drawAgentName(); });
on("disconnected", () => toast("Lost connection to the core; reconnecting…", "warn"));
on("turnEnd", () => position());

// Plugin html is fetched on demand (it isn't in the state broadcast).
connect();
(window as any).nexusDebug = { app: () => app, views: allViews };

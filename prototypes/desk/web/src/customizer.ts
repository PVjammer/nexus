// The floating window (Ctrl+. or the hammer button). Two modes:
//  - Ask (Ctrl+.): the quick agent, which is the home agent opened from
//    wherever you are, with that screen as its context. It organizes across
//    projects and never writes into the project's or a workstream's
//    conversation; what it changes reaches those agents as a "since your last
//    turn" note. "Continue in …" moves an ask that turned into real work.
//  - Eitri (the hammer): the customizer. You ask for a change to how Nexus
//    looks and then go back to work. Its agent
// builds on a draft; home.ts shows what's under construction, then lets you
// try the result with Keep and Roll back. Each step the agent takes is one
// worker trip through the door, so the animation is the agent's real work,
// and the window lists those steps for anyone who wants the detail.

import { desk, h, on, send } from "./core";
import { MiniChat, onChat, transcriptOf, isBusy } from "./chat";
import { customizer } from "./home";
import { ic } from "./icons";
import { openChats } from "./chats";

const AGENT = "customizer";
const QUICK = "home"; // the quick agent is the home agent, opened from anywhere
let mode: "ask" | "eitri" = "ask";
let quick: MiniChat | null = null;
let askBox: HTMLElement | null = null;
let shopEls: HTMLElement[] = [];
let tabs: HTMLElement | null = null;
type Where = { agent: string; label: string; view: string; selection: string };
let where: () => Where = () => ({ agent: "", label: "", view: "home", selection: "" });
let continueIn: (agent: string, text: string) => void = () => {};
const $ = (id: string) => document.getElementById(id)!;
const name = () => customizer().name || "Eitri";

let open = false;
let target = ""; // widget id the user pointed at
let inspecting = false;
let pos: { x: number; y: number } | null = null;

let mini: MiniChat | null = null;
let win: HTMLElement | null = null;
let lane: HTMLElement | null = null;
let steps: HTMLElement | null = null;
let targetEl: HTMLElement | null = null;
let view = () => "home";

export function initCustomizer(o: { view: () => string; goHome: () => void; redraw: () => void; where: () => Where; continueIn: (agent: string, text: string) => void }) {
  view = o.view;
  where = o.where;
  continueIn = o.continueIn;
  const btn = $("customizer-btn");
  btn.replaceChildren(ic("hammer"), " " + name());
  btn.addEventListener("click", () => toggle(open && mode === "eitri" ? false : true, "eitri"));
  document.addEventListener("keydown", (e) => {
    if (e.ctrlKey && !e.shiftKey && !e.altKey && e.key === ".") { e.preventDefault(); toggle(open && mode === "ask" ? false : true, "ask"); }
    else if (e.key === "Escape" && inspecting) { e.preventDefault(); stopInspect(); }
  });
  onChat((agent, kind) => { if (agent === QUICK && open && mode === "ask") drawAsk(); void kind; });
  onChat((agent, kind, m) => {
    if (agent !== AGENT) return;
    if (kind === "add" && m?.role === "tool") trip(m.tool || "");
    if (kind === "busy" || kind === "add" || kind === "update") drawStatus();
  });
  on("desk", () => { btn.classList.toggle("working", customizer().state !== "idle"); drawStatus(); });
  on("customizer", (m) => {
    (desk as any).customizer = m.customizer;
    btn.classList.toggle("working", m.customizer.state !== "idle");
    drawStatus();
    o.redraw();
    if (m.customizer.state === "ready" && view() !== "home") o.goHome();
  });
}

export function toggle(force?: boolean, m?: "ask" | "eitri") {
  if (m) mode = m;
  open = force ?? !open;
  if (open) show(); else hide();
}

function show() {
  if (!win) build();
  const root = $("customizer-root");
  if (!root.contains(win)) root.append(win!);
  win!.hidden = false;
  drawMode();
  drawStatus();
  if (mode === "ask") { drawAsk(); setTimeout(() => quick!.focus(), 0); }
  else { mini!.draw(); setTimeout(() => mini!.focus(), 0); }
}

function drawMode() {
  if (!win || !tabs) return;
  win.classList.toggle("ask", mode === "ask");
  tabs.querySelectorAll("button").forEach((b) => b.classList.toggle("on", (b as HTMLElement).dataset.mode === mode));
  for (const el of shopEls) el.hidden = mode === "ask";
  askBox!.hidden = mode !== "ask";
}

// ---- Ask: the quick agent ----

function drawAsk() {
  if (!askBox || !quick) return;
  quick.draw();
  const w = where();
  const msgs = transcriptOf(QUICK);
  let i = msgs.length - 1;
  while (i >= 0 && msgs[i].role !== "you") i--;
  const cur = i >= 0 && !msgs.slice(i).some((m) => m.role === "episode") ? msgs.slice(i) : [];
  const answer = cur.filter((m) => m.role === "agent").map((m) => m.text).join("\n").trim();
  const foot = askBox.querySelector(".quick-foot") as HTMLElement;
  foot.replaceChildren(
    h("span", { class: "muted small grow" }, w.label ? `Sees: ${w.label}, and your selection. Its conversation stays out of ${w.label}'s.` : "Sees: the home screen."),
    cur.length && answer && w.agent && !isBusy(QUICK) ? h("button", { class: "small", title: `Send this ask and its answer to ${w.label}'s conversation, and keep going there`, onclick: () => {
      continueIn(w.agent, `From a quick ask: ${cur[0].text}\n\nThe home agent answered: ${answer.slice(0, 1500)}`);
      toggle(false);
    } }, `Continue in ${w.label} →`) : "",
    cur.length && !isBusy(QUICK) ? h("button", { class: "link small", title: "Save this conversation and start a fresh one", onclick: () => send({ t: "episodeEnd", agent: QUICK }) }, "New conversation") : "");
}

function hide() {
  if (win) win.hidden = true;
  stopInspect();
}

function build() {
  quick = new MiniChat(QUICK, {
    placeholder: "Ask, or have it organize something: add a task here, tidy this workspace, check another project",
    chips: () => {
      const w = where();
      return [
        { label: "What needs me?", text: "What needs me right now?" },
        w.agent.startsWith("w:") ? { label: "Add a task here", text: `Add a task to ${w.label}: ` } : { label: "What's running?", text: "What's running right now?" },
        { label: "Another project…", text: "In project " },
      ];
    },
    context: () => { const w = where(); return { view: w.view, selection: w.selection }; },
    onHistory: () => openChats(QUICK),
  });
  askBox = h("div", { class: "quick" }, quick.el, h("div", { class: "quick-foot row" }));
  mini = new MiniChat(AGENT, {
    placeholder: "e.g. make the needs list compact",
    chips: () => [
      { label: "Point at something", run: () => startInspect() },
      { label: "Two columns", text: "Make the home screen two columns" },
      { label: "Just the prompt", text: "Back to just the prompt box" },
    ],
    context: () => ({ view: view(), selection: target }),
  });
  targetEl = h("div", { class: "cz-target" });
  lane = h("div", { class: "lane" });
  steps = h("div", { class: "cz-steps muted small" });
  tabs = h("nav", { class: "seg cz-tabs" },
    h("button", { "data-mode": "ask", title: "Ctrl+. · a quick ask that doesn't add to the conversation you're in", onclick: () => { mode = "ask"; show(); } }, ic("chat"), " Ask"),
    h("button", { "data-mode": "eitri", title: `${name()}: change how Nexus looks`, onclick: () => { mode = "eitri"; show(); } }, ic("hammer"), " " + name()));
  const head = h("header", { class: "cz-head" },
    tabs,
    h("span", { class: "grow-r" }),
    h("button", { class: "ebtn", title: "Close (work carries on)", onclick: () => toggle(false) }, "✕"));
  const shop = h("div", { class: "cz-shop" }, h("div", { class: "door-frame small" }, h("div", { class: "door" }), h("div", { class: "door-light" })), lane);
  shopEls = [shop, targetEl, mini.el, steps];
  win = h("div", { class: "customizer", role: "dialog", "aria-label": "Ask, or " + name() },
    head, askBox, shop, targetEl, mini.el, steps);
  drag(head, win);
}

// The window moves by its header and remembers where you put it (this session).
function drag(handle: HTMLElement, el: HTMLElement) {
  handle.addEventListener("pointerdown", (e) => {
    if ((e.target as HTMLElement).closest("button")) return;
    const r = el.getBoundingClientRect();
    const dx = e.clientX - r.left, dy = e.clientY - r.top;
    const move = (ev: PointerEvent) => {
      pos = { x: Math.max(8, Math.min(innerWidth - r.width - 8, ev.clientX - dx)), y: Math.max(8, Math.min(innerHeight - 60, ev.clientY - dy)) };
      Object.assign(el.style, { left: pos.x + "px", top: pos.y + "px", right: "auto", bottom: "auto" });
    };
    const up = () => { removeEventListener("pointermove", move); removeEventListener("pointerup", up); };
    addEventListener("pointermove", move); addEventListener("pointerup", up);
  });
}

// ---- pointing at a widget ----

function startInspect() {
  inspecting = true;
  document.body.classList.add("cz-inspecting");
  toast("Click a widget on the home screen. Esc cancels.");
  document.addEventListener("click", pick, true);
  document.addEventListener("mouseover", hover, true);
}
function stopInspect() {
  if (!inspecting) return;
  inspecting = false;
  document.body.classList.remove("cz-inspecting");
  document.removeEventListener("click", pick, true);
  document.removeEventListener("mouseover", hover, true);
  document.querySelectorAll(".cz-hover").forEach((x) => x.classList.remove("cz-hover"));
}
function hover(e: Event) {
  document.querySelectorAll(".cz-hover").forEach((x) => x.classList.remove("cz-hover"));
  (e.target as HTMLElement).closest?.(".hw[data-id]")?.classList.add("cz-hover");
}
function pick(e: Event) {
  const cell = (e.target as HTMLElement).closest?.(".hw[data-id]") as HTMLElement | null;
  if ((e.target as HTMLElement).closest?.(".customizer")) return;
  e.preventDefault(); e.stopPropagation();
  stopInspect();
  if (!cell) return;
  target = cell.dataset.id || "";
  drawStatus();
  mini?.focus();
}
function toast(t: string) {
  if (!steps) return;
  steps.textContent = t;
}

// ---- status: what it's doing, and the trips through the door ----

const verbs: Record<string, string> = {
  home_show: "looked at the screen", home_add: "built a widget", home_change: "reshaped a widget", home_remove: "took a widget away",
  home_grid: "changed the grid", home_reset: "cleared the site", home_undo: "undid a step", ask_user: "asked you something",
};

function trip(tool: string) {
  const carry = tool === "home_show" ? "plans" : tool === "home_remove" ? "out" : "in";
  const lanes = [lane, ...document.querySelectorAll<HTMLElement>(".door-tile .lane")].filter(Boolean) as HTMLElement[];
  for (const l of lanes) {
    const w = h("span", { class: `worker ${carry}`, title: verbs[tool] || tool },
      h("span", { class: "hat" }), h("span", { class: "body" }), h("span", { class: "load" }));
    l.append(w);
    setTimeout(() => w.remove(), 2600);
  }
}

function drawStatus() {
  if (!win || !targetEl || !steps) return;
  const cz = customizer();
  const label = (() => {
    if (!target) return null;
    const w = (desk?.desk.layouts?.home?.widgets || []).find((x) => x.id === target);
    return w ? `${(desk?.homeKinds || []).find((k) => k.kind === w.kind)?.label || w.kind} (${w.id})` : target;
  })();
  targetEl.replaceChildren(...(label ? [h("span", { class: "tag" }, ic("target"), " This: ", label), h("button", { class: "link small", onclick: () => { target = ""; drawStatus(); } }, "clear")] : []));
  win.classList.toggle("busy", isBusy(AGENT));
  win.classList.toggle("ready", cz.state === "ready");
  // the steps of the current request
  const msgs = transcriptOf(AGENT);
  let i = msgs.length - 1;
  while (i >= 0 && msgs[i].role !== "you") i--;
  const done = msgs.slice(Math.max(0, i)).filter((m) => m.role === "tool").map((m) => (m.error ? "⚠ " : "") + (verbs[m.tool || ""] || m.tool));
  const state = cz.state === "ready" ? "Ready: try it on the home screen, then Keep or Roll back." : isBusy(AGENT) ? "Working…" : "";
  steps.replaceChildren(...[state ? h("div", {}, state) : null, done.length ? h("div", {}, done.join(" · ")) : null].filter(Boolean) as HTMLElement[]);
}

// The customizer: a small floating window (Ctrl+. or the hammer button) where
// you ask for a change to how Nexus looks and then go back to work. Its agent
// builds on a draft; home.ts shows what's under construction, then lets you
// try the result with Keep and Roll back. Each step the agent takes is one
// worker trip through the door, so the animation is the agent's real work,
// and the window lists those steps for anyone who wants the detail.

import { desk, h, on } from "./core";
import { MiniChat, onChat, transcriptOf, isBusy } from "./chat";
import { customizer } from "./home";
import { ic } from "./icons";

const AGENT = "customizer";
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

export function initCustomizer(o: { view: () => string; goHome: () => void; redraw: () => void }) {
  view = o.view;
  const btn = $("customizer-btn");
  btn.replaceChildren(ic("hammer"), " " + name());
  btn.addEventListener("click", () => toggle());
  document.addEventListener("keydown", (e) => {
    if (e.ctrlKey && !e.shiftKey && !e.altKey && e.key === ".") { e.preventDefault(); toggle(); }
    else if (e.key === "Escape" && inspecting) { e.preventDefault(); stopInspect(); }
  });
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

export function toggle(force?: boolean) {
  open = force ?? !open;
  if (open) show(); else hide();
}

function show() {
  if (!win) build();
  const root = $("customizer-root");
  if (!root.contains(win)) root.append(win!);
  win!.hidden = false;
  drawStatus();
  mini!.draw();
  setTimeout(() => mini!.focus(), 0);
}

function hide() {
  if (win) win.hidden = true;
  stopInspect();
}

function build() {
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
  const head = h("header", { class: "cz-head" },
    h("span", { class: "cz-anvil" }, ic("hammer")),
    h("div", {}, h("b", {}, name()), h("div", { class: "muted small" }, "changes how Nexus looks")),
    h("span", { class: "grow-r" }),
    h("button", { class: "ebtn", title: "Close (work carries on)", onclick: () => toggle(false) }, "✕"));
  win = h("div", { class: "customizer", role: "dialog", "aria-label": name() },
    head,
    h("div", { class: "cz-shop" }, h("div", { class: "door-frame small" }, h("div", { class: "door" }), h("div", { class: "door-light" })), lane),
    targetEl, mini.el, steps);
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

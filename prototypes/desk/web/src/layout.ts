// Layout: a split tree of tab groups per space. The skeleton (splits, tab
// bars, empty group bodies) is rebuilt from state on every change; pane
// elements live in a separate absolutely-positioned layer and are never
// re-parented, so iframes and terminals survive any rearrangement.

import { app, h, send, space, type Node, type Pane } from "./core";
import { createPane, type PaneView } from "./panes";
import { ic, kindIc } from "./icons";

const views = new Map<string, PaneView>();
const layoutEl = () => document.getElementById("layout")!;
const layerEl = () => document.getElementById("pane-layer")!;
let dragPane: string | null = null;


export function getView(id: string) { return views.get(id); }

// ---- floating panes and zoom (per window, remembered locally) ----

type Rect = { x: number; y: number; w: number; h: number };
const floats = new Map<string, Rect>();
const floatFrames = new Map<string, HTMLElement>();
let zoomedPane = "";
try { for (const [k, v] of Object.entries(JSON.parse(localStorage.getItem("nexus-floats") || "{}"))) floats.set(k, v as Rect); } catch {}
const saveFloats = () => { try { localStorage.setItem("nexus-floats", JSON.stringify(Object.fromEntries(floats))); } catch {} };
const floatLayer = () => {
  let el = document.getElementById("float-layer");
  if (!el) { el = h("div", { id: "float-layer" }); document.getElementById("workspace")!.append(el); }
  return el;
};
function clampRect(r: Rect, layer: DOMRect) {
  r.w = Math.max(260, Math.min(r.w, layer.width));
  r.h = Math.max(160, Math.min(r.h, layer.height));
  r.x = Math.max(0, Math.min(r.x, layer.width - 80));
  r.y = Math.max(0, Math.min(r.y, layer.height - 40));
}

export function toggleFloat(id: string) {
  if (floats.has(id)) floats.delete(id);
  else {
    const layer = layerEl().getBoundingClientRect();
    const n = floats.size;
    floats.set(id, { x: layer.width * 0.18 + n * 24, y: layer.height * 0.1 + n * 24, w: layer.width * 0.6, h: layer.height * 0.72 });
    if (zoomedPane === id) zoomedPane = "";
  }
  saveFloats();
  render();
}

export function toggleZoom(id: string) {
  zoomedPane = zoomedPane === id ? "" : id;
  render();
}

function floatFrame(id: string): HTMLElement {
  const title = h("span", { class: "ftitle" });
  const bar = h("div", { class: "fbar" }, ic("float"), title, h("span", { class: "grow" }),
    h("button", { title: "Zoom", onclick: () => toggleZoom(id) }, ic("zoom")),
    h("button", { title: "Dock back into the layout", onclick: () => toggleFloat(id) }, "Dock"),
    h("button", { title: "Close the pane", onclick: () => { floats.delete(id); saveFloats(); send({ t: "close", pane: id }); } }, "✕"));
  const grip = h("div", { class: "fgrip", title: "Resize" });
  const fr = h("div", { class: "float-win" }, bar, grip);
  const drag = (e: MouseEvent, resize: boolean) => {
    if ((e.target as HTMLElement).closest("button")) return;
    e.preventDefault();
    const r = floats.get(id)!;
    const sx = e.clientX, sy = e.clientY, r0 = { ...r };
    document.body.classList.add("dragging");
    const mv = (ev: MouseEvent) => {
      if (resize) { r.w = r0.w + ev.clientX - sx; r.h = r0.h + ev.clientY - sy; }
      else { r.x = r0.x + ev.clientX - sx; r.y = r0.y + ev.clientY - sy; }
      position();
    };
    const up = () => { document.removeEventListener("mousemove", mv); document.removeEventListener("mouseup", up); document.body.classList.remove("dragging"); saveFloats(); };
    document.addEventListener("mousemove", mv); document.addEventListener("mouseup", up);
  };
  bar.addEventListener("mousedown", (e) => drag(e, false));
  grip.addEventListener("mousedown", (e) => drag(e, true));
  fr.addEventListener("mousedown", () => { // bring to front
    const fl = floatLayer(); fl.append(fr);
    const v = views.get(id); if (v) { floats.set(id, floats.get(id)!); }
  });
  return fr;
}

// ---- splits (tmux-like) ----

function splitGroup(g: Node, edge: "right" | "bottom", anchor?: HTMLElement) {
  if ((g.tabs?.length || 0) > 1 && g.active) { send({ t: "move", pane: g.active, placement: `${g.id}:${edge}` }); return; }
  // One tab: tmux opens a new shell in the split; offer that, or another pane.
  const open = (kind: string, props: any = {}, title?: string) => send({ t: "open", kind, props, title, placement: `${g.id}:${edge}` });
  const pid = projectIdOfSpace();
  const items: [string, () => void][] = [
    ["Terminal", () => open("terminal")],
    ["Files", () => open("files", { path: pid })],
    ["Wiki", () => open("review", { path: `wiki/${pid}/index.md` }, "Wiki")],
    ["Action log", () => open("log")],
  ];
  const menu = h("div", { class: "menu-list split-menu" }, h("div", { class: "hint" }, "Open in the new split"),
    ...items.map(([label, f]) => h("button", { onclick: () => { menu.remove(); f(); } }, label)));
  const r = (anchor || document.body).getBoundingClientRect();
  Object.assign(menu.style, { position: "fixed", left: `${Math.min(r.left, innerWidth - 230)}px`, top: `${r.bottom + 4}px`, right: "auto" });
  document.body.append(menu);
  const off = (e: Event) => { if (!menu.contains(e.target as any)) { menu.remove(); document.removeEventListener("mousedown", off, true); } };
  setTimeout(() => document.addEventListener("mousedown", off, true), 0);
}

function projectIdOfSpace(): string {
  const sp = space();
  return (window as any).nexusProjectOfSpace?.(sp?.id) || "";
}

function focusedGroup(): Node | null {
  const sp = space();
  return sp ? findGroup(sp.root, sp.focused) : null;
}

function groupsInOrder(): Node[] {
  const sp = space();
  const out: Node[] = [];
  const walk = (n: Node) => { if (n.kind === "group") out.push(n); else (n.children || []).forEach(walk); };
  if (sp) walk(sp.root);
  return out;
}

function focusDir(dx: number, dy: number) {
  const cur = focusedGroup();
  if (!cur) return;
  const rectOf = (id: string) => document.querySelector(`.group[data-group="${id}"]`)?.getBoundingClientRect();
  const c = rectOf(cur.id);
  if (!c) return;
  let best: Node | null = null, bestD = Infinity;
  for (const g of groupsInOrder()) {
    if (g.id === cur.id) continue;
    const r = rectOf(g.id);
    if (!r) continue;
    const vx = (r.left + r.width / 2) - (c.left + c.width / 2), vy = (r.top + r.height / 2) - (c.top + c.height / 2);
    if ((dx && Math.sign(vx) !== dx) || (dy && Math.sign(vy) !== dy)) continue;
    const d = Math.abs(vx) + Math.abs(vy);
    if (d < bestD) { bestD = d; best = g; }
  }
  if (best) send({ t: "focusGroup", group: best.id });
}

// ---- tmux-style keys: Ctrl+B, then a key ----

let prefixUntil = 0;
const hintEl = () => document.getElementById("prefix-hint") || document.body.appendChild(h("div", { id: "prefix-hint" },
  "Ctrl+B  ·  % split right  ·  \" split down  ·  x close  ·  z zoom  ·  f float  ·  o / arrows move  ·  c terminal"));
window.addEventListener("keydown", (e) => {
  if (!space()) return;
  if (e.ctrlKey && !e.shiftKey && !e.altKey && (e.key === "b" || e.key === "B")) {
    e.preventDefault(); e.stopPropagation();
    prefixUntil = Date.now() + 2000;
    const hint = hintEl(); hint.classList.add("on");
    setTimeout(() => { if (Date.now() >= prefixUntil) hint.classList.remove("on"); }, 2100);
    return;
  }
  if (e.key === "Escape" && zoomedPane && Date.now() > prefixUntil) { zoomedPane = ""; render(); return; }
  if (Date.now() > prefixUntil || ["Shift", "Control", "Alt", "Meta"].includes(e.key)) return;
  prefixUntil = 0;
  hintEl().classList.remove("on");
  const g = focusedGroup();
  const act = g?.active || "";
  const handled = () => { e.preventDefault(); e.stopPropagation(); };
  switch (e.key) {
    case "%": handled(); if (g) splitGroup(g, "right", document.querySelector(`.group[data-group="${g.id}"] .gtools`) as HTMLElement); break;
    case '"': handled(); if (g) splitGroup(g, "bottom", document.querySelector(`.group[data-group="${g.id}"] .gtools`) as HTMLElement); break;
    case "x": handled(); if (act) send({ t: "close", pane: act }); break;
    case "z": handled(); if (act) toggleZoom(act); break;
    case "f": handled(); if (act) toggleFloat(act); break;
    case "c": handled(); send({ t: "open", kind: "terminal" }); break;
    case "o": { handled(); const gs = groupsInOrder(); const i = gs.findIndex((x) => x.id === g?.id); if (gs.length) send({ t: "focusGroup", group: gs[(i + 1) % gs.length].id }); break; }
    case "ArrowLeft": handled(); focusDir(-1, 0); break;
    case "ArrowRight": handled(); focusDir(1, 0); break;
    case "ArrowUp": handled(); focusDir(0, -1); break;
    case "ArrowDown": handled(); focusDir(0, 1); break;
  }
}, true);
export function allViews() { return views; }

export function render() {
  const sp = space();
  if (!app) return;
  if (!sp) { layoutEl().replaceChildren(); syncViews(); requestAnimationFrame(position); return; }
  const root = layoutEl();
  root.replaceChildren(renderNode(sp.root, sp.id));
  syncViews();
  requestAnimationFrame(position);
}

// syncViews keeps a view for every pane in every project, so terminals and
// running tasks in other projects keep going while you look elsewhere.
function syncViews() {
  if (!app) return;
  const panes = app.state.panes;
  for (const [id, v] of views) {
    if (!panes[id]) { v.destroy(); v.el.remove(); views.delete(id); }
  }
  for (const p of Object.values(panes)) {
    let v = views.get(p.id);
    if (!v) {
      v = createPane(p);
      v.el.classList.add("pane");
      v.el.hidden = true; // position() reveals it and fires shown()
      v.el.dataset.pane = p.id;
      v.el.addEventListener("mousedown", () => focusPane(p.id), true);
      layerEl().append(v.el);
      views.set(p.id, v);
    }
    v.update(p);
  }
}

function focusPane(id: string) {
  const sp = space();
  if (!sp || !app) return;
  const p = app.state.panes[id];
  const g = groupOf(sp.root, id);
  if (g && (sp.focused !== g.id || p?.badge)) send({ t: "activate", pane: id });
}

function groupOf(n: Node, pane: string): Node | null {
  if (n.kind === "group") return n.tabs?.includes(pane) ? n : null;
  for (const c of n.children || []) { const g = groupOf(c, pane); if (g) return g; }
  return null;
}

/** position puts each active pane over its group's body. */
export function position() {
  const sp = space();
  if (!app) return;
  if (!sp) { for (const v of views.values()) v.el.hidden = true; return; }
  const layer = layerEl().getBoundingClientRect();
  const shown = new Set<string>();
  const inSpace = (id: string) => app!.state.panes[id]?.space === sp.id;
  const place = (v: PaneView, l: number, t: number, w: number, hgt: number, z = "") =>
    Object.assign(v.el.style, { left: `${l}px`, top: `${t}px`, width: `${w}px`, height: `${hgt}px`, zIndex: z });
  const zoomed = zoomedPane && inSpace(zoomedPane) && views.get(zoomedPane) ? zoomedPane : "";
  document.body.classList.toggle("zoomed", !!zoomed);
  if (zoomed) {
    place(views.get(zoomed)!, 0, 0, layer.width, layer.height, "45");
    shown.add(zoomed);
  } else {
    document.querySelectorAll<HTMLElement>(".gbody[data-group]").forEach((body) => {
      const gid = body.dataset.group!;
      const g = findGroup(sp.root, gid);
      if (!g || !g.active || floats.has(g.active)) return;
      const v = views.get(g.active);
      if (!v) return;
      const r = body.getBoundingClientRect();
      place(v, r.left - layer.left, r.top - layer.top, r.width, r.height);
      shown.add(g.active);
    });
  }
  // floating panes: each sits in a window frame over the layout
  const fl = floatLayer();
  for (const [id, fr] of [...floatFrames]) if (!floats.has(id) || !inSpace(id) || !views.get(id)) { fr.remove(); floatFrames.delete(id); }
  if (!zoomed) for (const [id, rect] of floats) {
    const v = views.get(id);
    if (!v || !inSpace(id)) continue;
    let fr = floatFrames.get(id);
    if (!fr) { fr = floatFrame(id); floatFrames.set(id, fr); fl.append(fr); }
    clampRect(rect, layer);
    Object.assign(fr.style, { left: `${rect.x}px`, top: `${rect.y}px`, width: `${rect.w}px`, height: `${rect.h}px` });
    (fr.querySelector(".ftitle") as HTMLElement).textContent = app!.state.panes[id]?.title || "";
    place(v, rect.x + 1, rect.y + 29, rect.w - 2, rect.h - 30, "41");
    shown.add(id);
  }
  for (const [id, v] of views) {
    const vis = shown.has(id);
    if (v.el.hidden === vis) {
      v.el.hidden = !vis;
      if (vis) v.shown?.();
    } else if (vis) v.resized?.();
  }
}

function findGroup(n: Node, id: string): Node | null {
  if (n.kind === "group") return n.id === id ? n : null;
  for (const c of n.children || []) { const g = findGroup(c, id); if (g) return g; }
  return null;
}

function renderNode(n: Node, spaceId: string): HTMLElement {
  if (n.kind === "group") return renderGroup(n, spaceId);
  const el = h("div", { class: `split ${n.dir === "col" ? "col" : "row"}` });
  const kids = n.children || [];
  const sizes = n.sizes && n.sizes.length === kids.length ? n.sizes : kids.map(() => 1 / kids.length);
  const childEls: HTMLElement[] = [];
  kids.forEach((c, i) => {
    const wrap = h("div", { class: "child", style: { flex: `${sizes[i]} 1 0` } }, renderNode(c, spaceId));
    childEls.push(wrap);
    el.append(wrap);
    if (i < kids.length - 1) el.append(resizer(n, i, childEls, sizes, el));
  });
  return el;
}

function resizer(n: Node, i: number, childEls: HTMLElement[], sizes: number[], splitEl: HTMLElement): HTMLElement {
  const r = h("div", { class: "resizer" });
  r.addEventListener("mousedown", (e) => {
    e.preventDefault();
    r.classList.add("active");
    document.body.classList.add("dragging");
    const horiz = n.dir !== "col";
    const total = horiz ? splitEl.clientWidth : splitEl.clientHeight;
    const start = horiz ? e.clientX : e.clientY;
    const a0 = sizes[i], b0 = sizes[i + 1];
    const cur = [...sizes];
    const move = (ev: MouseEvent) => {
      const d = ((horiz ? ev.clientX : ev.clientY) - start) / total;
      const a = Math.max(0.08, Math.min(a0 + b0 - 0.08, a0 + d));
      cur[i] = a; cur[i + 1] = a0 + b0 - a;
      childEls[i].style.flex = `${cur[i]} 1 0`;
      childEls[i + 1].style.flex = `${cur[i + 1]} 1 0`;
      position();
    };
    const up = () => {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", up);
      r.classList.remove("active");
      document.body.classList.remove("dragging");
      send({ t: "resize", split: n.id, sizes: cur });
    };
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
  });
  return r;
}

function renderGroup(g: Node, spaceId: string): HTMLElement {
  const sp = space()!;
  const panes = app!.state.panes;
  const focused = sp.focused === g.id;
  const isTarget = sp.target === g.id;
  const tabs = h("div", { class: "tabs" });
  for (const id of g.tabs || []) {
    const p = panes[id];
    if (p) tabs.append(renderTab(p, id === g.active));
  }
  const tools = h("div", { class: "gtools" },
    isTarget ? h("span", { class: "tgt", title: "The project agent opens new things here, in the background" }, ic("agent")) : null,
    g.active ? h("button", { title: "Split right (Ctrl+B %)", onclick: (e: Event) => splitGroup(g, "right", e.currentTarget as HTMLElement) }, ic("splitv")) : null,
    g.active ? h("button", { title: "Split down (Ctrl+B \")", onclick: (e: Event) => splitGroup(g, "bottom", e.currentTarget as HTMLElement) }, ic("splith")) : null,
    g.active ? h("button", { title: "Pop out as a floating window (Ctrl+B f)", onclick: () => toggleFloat(g.active!) }, ic("float")) : null,
    g.active ? h("button", { title: "Zoom: fill the workspace (Ctrl+B z, Esc to restore)", onclick: () => toggleZoom(g.active!) }, ic("zoom")) : null,
    g.active ? h("button", { class: "gclose", title: "Close this pane (Ctrl+B x)", onclick: () => send({ t: "close", pane: g.active }) }, "✕") : null,
  );
  tabs.append(tools);
  tabs.addEventListener("dragover", (e) => { if (dragPane) { e.preventDefault(); showHint(body, "center"); } });
  tabs.addEventListener("drop", (e) => { e.preventDefault(); dropOn(g.id, "center"); });
  const body = h("div", { class: "gbody", "data-group": g.id });
  if (g.active && floats.has(g.active)) {
    body.append(h("div", { class: "empty" }, h("div", {}, h("div", {}, "This pane is floating."),
      h("button", { onclick: () => toggleFloat(g.active!) }, "Dock it back"))));
  }
  if (!g.tabs?.length) {
    body.append(h("div", { class: "empty" }, "Nothing open. Pick something from the project rail, or ask the project agent."));
  }
  body.addEventListener("dragover", (e) => {
    if (!dragPane) return;
    e.preventDefault();
    showHint(body, zone(body, e));
  });
  body.addEventListener("dragleave", () => hideHint());
  body.addEventListener("drop", (e) => { e.preventDefault(); dropOn(g.id, zone(body, e)); });
  const el = h("div", { class: `group${focused ? " focused" : ""}${isTarget ? " target" : ""}`, "data-group": g.id }, tabs, body);
  el.addEventListener("mousedown", () => { if (!focused) send({ t: "focusGroup", group: g.id }); });
  return el;
}

function renderTab(p: Pane, active: boolean): HTMLElement {
  const tabEl = renderTabInner(p, active);
  tabEl.addEventListener("auxclick", (e) => { if ((e as MouseEvent).button === 1) { e.preventDefault(); send({ t: "close", pane: p.id }); } });
  if (floats.has(p.id)) tabEl.classList.add("floated");
  return tabEl;
}

function renderTabInner(p: Pane, active: boolean): HTMLElement {
  const visIco = p.visibility === "none" ? ic("lock", "Private to you") : null;
  const el = h("div", {
    class: `tab${active ? " active" : ""}`, draggable: "true",
    title: `${p.title}\nopened by ${p.opener}`,
    onclick: () => send({ t: "activate", pane: p.id }),
  },
    p.badge ? h("span", { class: "badge", title: "The agent changed this" }) : null,
    h("span", { class: "ico" }, kindIc(p.kind)),
    h("span", { class: "title" }, p.title),
    p.opener === "agent" ? h("span", { class: "ico agent-ico", title: "Opened by the agent" }, ic("agent")) : null,
    visIco ? h("span", { class: "ico" }, visIco) : null,
    h("button", { class: "x", title: "Close (middle-click the tab works too)", onclick: (e: Event) => { e.stopPropagation(); send({ t: "close", pane: p.id }); } }, "✕"),
  );
  el.addEventListener("dragstart", (e) => {
    dragPane = p.id;
    e.dataTransfer!.setData("text/plain", p.id);
    e.dataTransfer!.effectAllowed = "move";
    document.body.classList.add("dragging");
  });
  el.addEventListener("dragend", () => { dragPane = null; hideHint(); document.body.classList.remove("dragging"); });
  return el;
}

type Zone = "center" | "left" | "right" | "top" | "bottom";
function zone(body: HTMLElement, e: DragEvent): Zone {
  const r = body.getBoundingClientRect();
  const x = (e.clientX - r.left) / r.width, y = (e.clientY - r.top) / r.height;
  const m = Math.min(x, 1 - x, y, 1 - y);
  if (m > 0.25) return "center";
  if (m === x) return "left";
  if (m === 1 - x) return "right";
  if (m === y) return "top";
  return "bottom";
}

function showHint(body: HTMLElement, z: Zone) {
  const hint = document.getElementById("drop-hint")!;
  const ws = document.getElementById("workspace")!.getBoundingClientRect();
  const r = body.getBoundingClientRect();
  let { left, top, width, height } = { left: r.left - ws.left, top: r.top - ws.top, width: r.width, height: r.height };
  if (z === "left") width /= 2;
  if (z === "right") { left += width / 2; width /= 2; }
  if (z === "top") height /= 2;
  if (z === "bottom") { top += height / 2; height /= 2; }
  Object.assign(hint.style, { left: `${left}px`, top: `${top}px`, width: `${width}px`, height: `${height}px` });
  hint.hidden = false;
}
function hideHint() { document.getElementById("drop-hint")!.hidden = true; }

function dropOn(group: string, z: Zone) {
  const id = dragPane;
  dragPane = null;
  hideHint();
  document.body.classList.remove("dragging");
  if (!id) return;
  send({ t: "move", pane: id, placement: z === "center" ? group : `${group}:${z}` });
}

new ResizeObserver(() => position()).observe(document.getElementById("workspace")!);
window.addEventListener("resize", () => position());

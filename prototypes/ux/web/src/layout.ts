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
export function allViews() { return views; }

export function render() {
  const sp = space();
  if (!app || !sp) return;
  const root = layoutEl();
  root.replaceChildren(renderNode(sp.root, sp.id));
  // sync pane views (all spaces, so terminals in other spaces keep running)
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
  requestAnimationFrame(position);
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
  if (!sp || !app) return;
  const layer = layerEl().getBoundingClientRect();
  const shown = new Set<string>();
  document.querySelectorAll<HTMLElement>(".gbody[data-group]").forEach((body) => {
    const gid = body.dataset.group!;
    const g = findGroup(sp.root, gid);
    if (!g || !g.active) return;
    const v = views.get(g.active);
    if (!v) return;
    const r = body.getBoundingClientRect();
    Object.assign(v.el.style, { left: `${r.left - layer.left}px`, top: `${r.top - layer.top}px`, width: `${r.width}px`, height: `${r.height}px` });
    shown.add(g.active);
  });
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
    isTarget
      ? h("span", { class: "tgt", title: "The agent opens new things in this group (in the background)" }, ic("agent"), " agent's group")
      : h("button", { title: "Make this the group where the agent opens things", onclick: () => send({ t: "setTarget", group: g.id }) }, ic("agent"), " target here"),
    g.active ? h("button", { title: "Split: move the active tab to a new group on the right", onclick: () => send({ t: "move", pane: g.active, placement: `${g.id}:right` }) }, "⇥") : null,
    g.active ? h("button", { title: "Split: move the active tab to a new group below", onclick: () => send({ t: "move", pane: g.active, placement: `${g.id}:bottom` }) }, "⤓") : null,
  );
  tabs.append(tools);
  tabs.addEventListener("dragover", (e) => { if (dragPane) { e.preventDefault(); showHint(body, "center"); } });
  tabs.addEventListener("drop", (e) => { e.preventDefault(); dropOn(g.id, "center"); });
  const body = h("div", { class: "gbody", "data-group": g.id });
  if (!g.tabs?.length) {
    body.append(h("div", { class: "empty" }, isTarget
      ? "The agent's group. Things it opens for you land here, in the background, with a badge."
      : "Empty group. Open something from the Open menu, or drag a tab here."));
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
  const visIco = p.visibility === "none" ? ic("lock", "Private: the agent can't see this") : p.visibility === "title" ? ic("half", "The agent sees the title only") : null;
  const el = h("div", {
    class: `tab${active ? " active" : ""}`, draggable: "true",
    title: `${p.title}\nopened by ${p.opener}${p.visibility !== "full" ? `\nagent sees: ${p.visibility === "none" ? "nothing" : "title only"}` : ""}`,
    onclick: () => send({ t: "activate", pane: p.id }),
  },
    p.badge ? h("span", { class: "badge", title: "The agent changed this" }) : null,
    h("span", { class: "ico" }, kindIc(p.kind)),
    h("span", { class: "title" }, p.title),
    p.opener === "agent" ? h("span", { class: "ico agent-ico", title: "Opened by the agent" }, ic("agent")) : null,
    visIco ? h("span", { class: "ico" }, visIco) : null,
    h("button", { class: `pin${p.pinned ? " on" : ""}`, title: p.pinned ? "Pinned: the agent must ask before closing or moving it" : "Pin (the agent will have to ask before closing or moving it)",
      onclick: (e: Event) => { e.stopPropagation(); send({ t: "pin", pane: p.id, pinned: !p.pinned }); } }, p.pinned ? ic("pinned") : ic("pin")),
    h("button", { class: "x", title: "Close", onclick: (e: Event) => { e.stopPropagation(); send({ t: "close", pane: p.id }); } }, "✕"),
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

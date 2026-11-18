// The home screen: a grid of widgets stored as data in the core (desk
// layouts["home"]). Each widget is a kind from the catalog, drawn in a shape,
// for everything or one project, at a size. You change it here (Customize
// manually) and the home agent changes it with its home_* tools; both go
// through the same core functions, and both can be undone.

import { app, desk, h, modal, project, send, toast, type Layout, type LayoutWidget, type WidgetKind } from "./core";
import { ChatPanel, MiniChat } from "./chat";
import { mountPlugin, pluginControls } from "./panes";
import { ic } from "./icons";

// ---- kinds are drawn by functions main.ts registers ----

export type WidgetCtx = { w: LayoutWidget; scope: string; shape: string; project?: string };
type Persistent = { el: HTMLElement; update?: (c: WidgetCtx) => void; destroy?: () => void };
type KindDef = { draw?: (c: WidgetCtx) => (HTMLElement | null)[]; create?: (c: WidgetCtx) => Persistent };
const kinds = new Map<string, KindDef>();
export function registerKind(kind: string, def: KindDef) { kinds.set(kind, def); }

export type HomeHooks = {
  greeting: () => HTMLElement;          // shown above the prompt when it's alone
  chips: (agent: string) => { label: string; text?: string; run?: () => void }[];
  openChats: (agent?: string) => void;  // the Chats panel
  agentName: (agent: string) => string;
};
let hooks: HomeHooks;
export function initHome(h_: HomeHooks) { hooks = h_; }

// ---- state ----

let editing = false;
export const isEditing = () => editing;
const cells = new Map<string, { el: HTMLElement; head: HTMLElement; body: HTMLElement; key: string; live?: Persistent }>();

// The customizer (customizer.ts) works on a draft. While it builds, the live
// screen shows what's under construction; when it's done, the draft is shown
// in place (a trial) with Keep and Roll back.
type Cz = { name: string; state: "idle" | "building" | "ready"; draft?: Layout; added?: string[]; changed?: string[]; removed?: string[]; grid?: boolean };
export const customizer = (): Cz => ((desk as any)?.customizer || { name: "Eitri", state: "idle" }) as Cz;
const trial = () => customizer().state === "ready" && !!customizer().draft;
const liveLayout = (): Layout => desk?.desk.layouts?.home || { cols: 3, tiling: "flow", widgets: [] };
const layout = (): Layout => trial() ? customizer().draft! : liveLayout();
let revealed = new Set<string>(); // ids whose reveal animation already ran
const widgets = (): LayoutWidget[] => layout().widgets || [];
const catalog = (): WidgetKind[] => desk?.homeKinds || [];
const kindInfo = (k: string) => catalog().find((x) => x.kind === k);
const shapeName: Record<string, string> = { list: "List", compact: "Compact", board: "Board", cards: "Cards", minimal: "Minimal", conversation: "Conversation", box: "Box", frame: "Frame" };

async function homeSend(m: Record<string, any>) {
  send({ ...m, space: "" }); // home changes aren't tied to a project's pane space
}

// ---- drawing ----

export function drawHome(root: HTMLElement) {
  const l = layout();
  const ws = widgets();
  const solo = !editing && ws.length === 1 && ws[0].kind === "chat" && (ws[0].shape || "minimal") === "minimal" && !ws[0].scope;
  const cols = solo ? 1 : Math.max(1, Math.min(4, l.cols || 3));
  const tile = !solo && l.tiling === "tile";

  let wrap = root.querySelector<HTMLElement>(":scope > .home-wrap");
  if (!wrap) { wrap = h("div", { class: "home-wrap" }); root.replaceChildren(wrap); }
  const cz = customizer();
  const building = cz.state === "building";
  wrap.className = `home-wrap${solo ? " solo" : ""}${editing ? " editing" : ""}${tile ? " tile" : ""}${trial() ? " trial" : ""}${building && cz.grid ? " rebuilding" : ""}`;
  if (cz.state === "idle") revealed = new Set();

  let tbar = wrap.querySelector<HTMLElement>(":scope > .trial-bar");
  const nt = trial() ? trialBar(cz) : building ? buildBar(cz) : null;
  if (nt) { tbar ? tbar.replaceWith(nt) : wrap.prepend(nt); } else tbar?.remove();

  let bar = wrap.querySelector<HTMLElement>(":scope > .edit-bar");
  if (editing) { const nb = editBar(); bar ? bar.replaceWith(nb) : wrap.prepend(nb); } else bar?.remove();

  let greet = wrap.querySelector<HTMLElement>(":scope > .home-greet");
  const g = solo ? h("div", { class: "home-greet" }, hooks.greeting()) : null;
  if (g) { greet ? greet.replaceWith(g) : wrap.insertBefore(g, wrap.querySelector(".home-grid")); } else greet?.remove();

  let grid = wrap.querySelector<HTMLElement>(":scope > .home-grid");
  if (!grid) { grid = h("div", { class: "home-grid" }); wrap.append(grid); }
  grid.style.setProperty("--cols", String(cols));
  grid.className = `home-grid${tile ? " tile" : ""}`;

  // Cells are kept by widget id so plugin frames and chat boxes aren't torn
  // down on every data refresh.
  const want = new Set(ws.map((w) => w.id));
  for (const [id, c] of cells) if (!want.has(id)) { c.live?.destroy?.(); c.el.remove(); cells.delete(id); }
  const order: HTMLElement[] = [];
  const marks = new Set([...(cz.added || []), ...(cz.changed || [])]);
  ws.forEach((w, i) => {
    const el = drawCell(w, i, ws.length, cols, tile, solo);
    el.classList.toggle("under-construction", building && (marks.has(w.id) || (cz.removed || []).includes(w.id)));
    if (trial() && marks.has(w.id) && !revealed.has(w.id)) { revealed.add(w.id); el.classList.add("reveal"); setTimeout(() => el.classList.remove("reveal"), 1400); }
    el.classList.toggle("built", trial() && marks.has(w.id));
    order.push(el);
  });
  // New widgets don't exist on the live screen yet: a door where they're being built.
  if (building) for (const id of cz.added || []) {
    const dw = (cz.draft?.widgets || []).find((x) => x.id === id);
    order.push(doorTile(id, dw ? kindInfo(dw.kind)?.label || dw.kind : "a widget", Math.min(Math.max(1, dw?.w || 1), cols)));
  }
  const add = editing ? (grid.querySelector<HTMLElement>(".add-tile") || h("button", { class: "add-tile", onclick: addWidgetModal }, ic("plus"), " Add a widget")) : null;
  if (add) order.push(add); else grid.querySelector(".add-tile")?.remove();
  const same = order.length === grid.children.length && order.every((el, i) => grid!.children[i] === el);
  if (!same) for (const el of order) grid.append(el); // re-appending moves; only when the order changed

  let empty = wrap.querySelector<HTMLElement>(":scope > .home-empty");
  if (!ws.length && !editing) {
    const e = h("div", { class: "home-empty" }, "Your home screen is empty. Press ", h("b", {}, "Customize manually"), ", or open ", h("b", {}, "Chats"), " to ask the home agent.");
    empty ? empty.replaceWith(e) : wrap.append(e);
  } else empty?.remove();

  let corner = wrap.querySelector<HTMLElement>(":scope > .corner-btn");
  if (!editing && !trial()) { if (!corner) wrap.append(h("button", { class: "corner-btn", title: "Change this screen yourself", onclick: () => { editing = true; drawHome(root); } }, ic("grid"), " Customize manually")); }
  else corner?.remove();
}

// ---- the customizer's states on this screen ----

const doors = new Map<string, HTMLElement>();
function doorTile(id: string, what: string, span: number): HTMLElement {
  let d = doors.get(id);
  if (!d) {
    d = h("section", { class: "hw door-tile", "data-door": id, title: `${customizer().name} is building ${what}` },
      h("div", { class: "tape" }),
      h("div", { class: "door-scene" },
        h("div", { class: "door-frame" }, h("div", { class: "door" }), h("div", { class: "door-light" })),
        h("div", { class: "lane" })),
      h("div", { class: "door-label" }, ic("hammer"), ` Building ${what}…`));
    doors.set(id, d);
  }
  d.style.gridColumn = `span ${span}`;
  return d;
}

function summary(cz: Cz): string {
  const parts: string[] = [];
  const n = (k: "added" | "changed" | "removed", verb: string) => { const c = (cz[k] || []).length; if (c) parts.push(`${verb} ${c} widget${c === 1 ? "" : "s"}`); };
  n("added", "added"); n("changed", "changed"); n("removed", "removed");
  if (cz.grid) parts.push("a new grid");
  return parts.join(", ") || "a change";
}

function buildBar(cz: Cz): HTMLElement {
  return h("div", { class: "trial-bar building" }, h("span", { class: "busy-dot" }), ic("hammer"), h("b", {}, ` ${cz.name} is working`),
    h("span", { class: "muted small" }, ` on ${summary(cz)}. Keep working; you'll get to try it before it stays.`),
    h("span", { class: "grow-r" }),
    h("button", { class: "link small", onclick: () => send({ t: "customizerRollback", space: "" }) }, "Cancel"));
}

function trialBar(cz: Cz): HTMLElement {
  for (const [id, el] of doors) { el.remove(); doors.delete(id); }
  return h("div", { class: "trial-bar" }, ic("hammer"), h("b", {}, ` Trying ${cz.name}'s change:`),
    h("span", { class: "muted small" }, `${summary(cz)}. It's live here so you can use it; nothing is kept until you say.`),
    h("span", { class: "grow-r" }),
    h("button", { onclick: () => send({ t: "customizerRollback", space: "" }) }, "Roll back"),
    h("button", { class: "primary", onclick: () => send({ t: "customizerKeep", space: "" }) }, ic("check"), " Keep"));
}

function drawCell(w: LayoutWidget, i: number, n: number, cols: number, tile: boolean, solo: boolean): HTMLElement {
  const info = kindInfo(w.kind);
  const shape = w.shape || info?.shapes[0] || "";
  const scope = w.scope || "";
  const key = `${w.kind}|${shape}|${scope}|${w.plugin || ""}`;
  let c = cells.get(w.id);
  if (!c || c.key !== key) {
    c?.live?.destroy?.();
    const head = h("header", { class: "hw-head" });
    const body = h("div", { class: "hw-body" });
    const el = h("section", { class: "hw", "data-id": w.id }, head, body);
    c?.el.replaceWith(el);
    c = { el, head, body, key };
    cells.set(w.id, c);
  }
  const ctx: WidgetCtx = { w, scope, shape, project: scope.startsWith("p:") ? scope.slice(2) : undefined };
  c.el.className = `hw k-${w.kind} s-${shape}${w.by === "agent" ? " by-agent" : ""}${editing ? " editing" : ""}`;
  const span = Math.min(Math.max(1, w.w || 1), cols);
  c.el.style.gridColumn = `span ${span}`;
  c.el.style.gridRow = tile ? `span ${Math.max(1, w.h || 1)}` : "";
  c.el.style.height = !tile && w.h ? `${w.h * 220}px` : "";
  c.el.classList.toggle("fixed", tile || !!w.h);
  c.head.hidden = solo;
  c.head.replaceChildren(...head(w, info, i, n));

  const def = kinds.get(w.kind);
  if (def?.create) {
    if (!c.live) { c.live = def.create(ctx); c.body.replaceChildren(c.live.el); }
    else c.live.update?.(ctx);
  } else if (def?.draw) {
    if (!editingInside(c.body)) c.body.replaceChildren(...(def.draw(ctx).filter(Boolean) as HTMLElement[]));
  } else c.body.replaceChildren(h("div", { class: "muted small" }, `Unknown widget kind “${w.kind}”.`));
  return c.el;
}

function editingInside(el: HTMLElement) {
  const a = document.activeElement as HTMLElement | null;
  return !!a && el.contains(a) && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName);
}

function title(w: LayoutWidget, info?: WidgetKind): string {
  if (w.kind === "chat") return hooks.agentName(w.scope || "home");
  if (w.kind === "plugin") return (app?.plugins || []).find((p) => p.id === w.plugin)?.name || w.plugin || "Plugin";
  return info?.label || w.kind;
}

function head(w: LayoutWidget, info: WidgetKind | undefined, i: number, n: number): (HTMLElement | string)[] {
  const out: (HTMLElement | string)[] = [h("b", {}, title(w, info))];
  if (w.scope && w.kind !== "chat") out.push(h("span", { class: "tag" }, project(w.scope.slice(2))?.name || w.scope.slice(2)));
  if (w.by === "agent") out.push(h("button", { class: "by-agent-tag", title: w.why ? `Added by the agent: ${w.why}` : "Added by the agent", onclick: () => whatIsThis(w) }, ic("agent"), " agent"));
  out.push(h("span", { class: "grow-r" }));
  if (w.kind === "plugin" && w.plugin && !editing) out.push(...pluginControls(w.plugin));
  if (editing) {
    const btn = (label: string, tip: string, f: () => void, dis = false) => h("button", { class: "ebtn", title: tip, disabled: dis, onclick: f }, label);
    out.push(
      btn("←", "Move earlier", () => homeSend({ t: "homeChange", id: w.id, position: i }), i === 0),
      btn("→", "Move later", () => homeSend({ t: "homeChange", id: w.id, position: i + 2 }), i === n - 1),
      h("span", { class: "dim" }, btn("−", "Narrower", () => homeSend({ t: "homeChange", id: w.id, w: Math.max(1, w.w - 1) }), w.w <= 1), `w${w.w}`,
        btn("+", "Wider", () => homeSend({ t: "homeChange", id: w.id, w: Math.min(4, w.w + 1) }), w.w >= Math.min(4, layout().cols))),
      h("span", { class: "dim" }, btn("−", "Shorter", () => homeSend({ t: "homeChange", id: w.id, h: Math.max(0, (w.h || 0) - 1) }), !w.h), w.h ? `h${w.h}` : "h·",
        btn("+", "Taller", () => homeSend({ t: "homeChange", id: w.id, h: Math.min(4, (w.h || 0) + 1) }), (w.h || 0) >= 4)),
      btn("✕", "Remove from the home screen", () => homeSend({ t: "homeRemove", id: w.id })));
  }
  out.push(widgetMenu(w, info));
  return out;
}

function widgetMenu(w: LayoutWidget, info?: WidgetKind): HTMLElement {
  const wrap = h("div", { class: "menu" });
  const btn = h("button", { class: "ebtn", title: "About this widget" }, "⋯");
  let list: HTMLElement | null = null;
  const close = () => { list?.remove(); list = null; document.removeEventListener("mousedown", outside, true); };
  const outside = (e: Event) => { if (!wrap.contains(e.target as any)) close(); };
  btn.addEventListener("click", () => {
    if (list) return close();
    const kids: HTMLElement[] = [];
    if (info && info.shapes.length > 1) {
      kids.push(h("div", { class: "hint" }, "Show as"));
      kids.push(h("div", { class: "seg" }, ...info.shapes.map((s) => h("button", { class: (w.shape || info.shapes[0]) === s ? "on" : "", onclick: () => { homeSend({ t: "homeChange", id: w.id, shape: s }); close(); } }, shapeName[s] || s))));
    }
    if (info?.scoped) {
      const sel = h("select", { onchange: (e: Event) => { homeSend({ t: "homeChange", id: w.id, scope: (e.target as HTMLSelectElement).value }); close(); } },
        h("option", { value: "", selected: !w.scope }, w.kind === "chat" ? "Home agent" : "Everything"),
        ...(desk?.projects || []).map((p) => h("option", { value: "p:" + p.id, selected: w.scope === "p:" + p.id }, w.kind === "chat" ? `${p.name} agent` : p.name))) as HTMLSelectElement;
      kids.push(h("div", { class: "hint" }, w.kind === "chat" ? "Agent" : "Show"), h("div", { class: "pad-x" }, sel));
    }
    kids.push(h("div", { class: "sep" }),
      h("button", { onclick: () => { close(); whatIsThis(w); } }, "What is this?"),
      h("button", { class: "danger", onclick: () => { close(); homeSend({ t: "homeRemove", id: w.id }); } }, "Remove from the home screen"));
    list = h("div", { class: "menu-list" }, ...kids);
    wrap.append(list);
    document.addEventListener("mousedown", outside, true);
  });
  wrap.append(btn);
  return wrap;
}

function editBar(): HTMLElement {
  const l = layout();
  const seg = (vals: (string | number)[], cur: string | number, f: (v: any) => void, label = (v: any) => String(v)) =>
    h("span", { class: "seg" }, ...vals.map((v) => h("button", { class: v === cur ? "on" : "", onclick: () => f(v) }, label(v))));
  const u = desk?.homeUndo;
  const reset = (to: string, label: string) => h("button", { onclick: () => homeSend({ t: "homeReset", to }) }, label);
  return h("div", { class: "edit-bar", role: "toolbar", "aria-label": "Customize the home screen" },
    h("b", {}, "Customizing your home screen"),
    h("span", { class: "eg" }, h("span", { class: "muted small" }, "Columns"), seg([1, 2, 3, 4], l.cols, (v) => homeSend({ t: "homeGrid", cols: v }))),
    h("span", { class: "eg", title: "Flow: widgets take the height they need and the page scrolls. Tile: widgets fill the screen like a tiling window manager." },
      h("span", { class: "muted small" }, "Tiling"), seg(["flow", "tile"], l.tiling, (v) => homeSend({ t: "homeGrid", tiling: v }), (v) => (v === "flow" ? "Flow" : "Tile"))),
    h("span", { class: "eg" },
      h("button", { onclick: addWidgetModal }, ic("plus"), " Add a widget"),
      h("button", { disabled: !u?.depth, title: u?.label ? `Undo: ${u.label}` : "Nothing to undo", onclick: () => homeSend({ t: "homeUndo" }) }, ic("undo"), " Undo"),
      h("span", { class: "muted small" }, "Start from:"), reset("prompt", "Just the prompt"), reset("dashboard", "Dashboard"),
      h("button", { class: "primary", onclick: () => { editing = false; redraw(); } }, "Done")));
}

let redrawFn: () => void = () => {};
export function setRedraw(f: () => void) { redrawFn = f; }
function redraw() { redrawFn(); }
export function stopEditing() { editing = false; }
export function startEditing() { editing = true; }

// ---- add a widget ----

function addWidgetModal() {
  const kindsList = catalog().filter((k) => !k.internal);
  const plugins = app?.plugins || [];
  let pick = "";
  modal((close) => {
    const opts = h("div", { class: "add-opts" });
    const list = h("div", { class: "catalog" });
    const drawOpts = () => {
      if (!pick) { opts.replaceChildren(h("div", { class: "muted small" }, "Pick what it shows.")); return; }
      if (pick.startsWith("plugin:")) {
        opts.replaceChildren(h("div", { class: "row" }, h("button", { class: "primary", onclick: () => { homeSend({ t: "homeAdd", kind: "plugin", plugin: pick.slice(7) }); close(); } }, "Add")));
        return;
      }
      const k = kindInfo(pick)!;
      const shape = h("select", {}, ...k.shapes.map((s) => h("option", { value: s }, shapeName[s] || s))) as HTMLSelectElement;
      const scope = h("select", {}, h("option", { value: "" }, k.kind === "chat" ? "Home agent" : "Everything"),
        ...(desk?.projects || []).map((p) => h("option", { value: "p:" + p.id }, k.kind === "chat" ? `${p.name} agent` : p.name))) as HTMLSelectElement;
      const width = h("select", {}, ...[1, 2, 3, 4].map((n) => h("option", { value: String(n), selected: n === Math.min(k.defaultW, layout().cols) }, n === 4 ? "Full width" : `${n} column${n > 1 ? "s" : ""}`))) as HTMLSelectElement;
      opts.replaceChildren(h("div", { class: "row" },
        k.shapes.length > 1 ? h("label", {}, "Shape ", shape) : null,
        k.scoped ? h("label", {}, k.kind === "chat" ? "Agent " : "Show ", scope) : null,
        h("label", {}, "Width ", width),
        h("button", { class: "primary", onclick: () => {
          const m: any = { t: "homeAdd", kind: k.kind, shape: shape.value, w: Number(width.value) };
          if (k.scoped) m.scope = scope.value;
          homeSend(m); close();
        } }, "Add")));
    };
    const drawList = () => list.replaceChildren(
      ...kindsList.map((k) => h("button", { class: `cat${pick === k.kind ? " on" : ""}`, onclick: () => { pick = k.kind; drawList(); drawOpts(); } }, h("b", {}, k.label), h("span", { class: "muted small" }, k.shows))),
      ...plugins.map((p) => h("button", { class: `cat${pick === "plugin:" + p.id ? " on" : ""}`, onclick: () => { pick = "plugin:" + p.id; drawList(); drawOpts(); } }, h("b", {}, ic("plugin"), " ", p.name), h("span", { class: "muted small" }, p.description || "Custom widget (plugin)"))));
    drawList(); drawOpts();
    return h("div", { class: "modal wide" }, h("h3", {}, "Add a widget"),
      h("p", { class: "muted small" }, "Or ask the home agent in the prompt box: “put what needs me under the prompt”."),
      list, opts, h("div", { class: "row" }, h("span", { class: "grow" }), h("button", { onclick: close }, "Cancel")));
  });
}

// ---- what is this? ----

export function whatIsThis(w: LayoutWidget) {
  const info = kindInfo(w.kind);
  const by = w.by === "agent" ? `The home agent${w.why ? `: “${w.why}”` : ""}` : w.by === "default" ? "Part of the default home screen" : "You";
  const scope = w.scope ? (project(w.scope.slice(2))?.name || w.scope) : (w.kind === "chat" ? "the home agent" : "everything");
  const spec = { kind: w.kind, shape: w.shape, scope: w.scope || "everything", w: w.w, h: w.h || "auto", ...(w.plugin ? { plugin: w.plugin } : {}) };
  modal((close) => h("div", { class: "modal" },
    h("h3", {}, title(w, info)),
    h("dl", { class: "spec" },
      h("dt", {}, "Shows"), h("dd", {}, info?.shows || w.kind),
      h("dt", {}, "For"), h("dd", {}, scope),
      h("dt", {}, "Shape"), h("dd", {}, shapeName[w.shape || ""] || w.shape || "—"),
      h("dt", {}, "Size"), h("dd", {}, `${w.w} column${w.w > 1 ? "s" : ""} wide, ${w.h ? `${w.h} row${w.h > 1 ? "s" : ""} tall` : "as tall as it needs"}`),
      h("dt", {}, "Added by"), h("dd", {}, by, w.added ? h("span", { class: "muted small" }, ` · ${new Date(w.added).toLocaleString()}`) : "")),
    h("pre", { class: "spec-json" }, JSON.stringify(spec, null, 2)),
    h("p", { class: "muted small" }, "Every widget is this: what it shows, for what, in which shape, at which size. The home agent builds with exactly these parts, so anything it adds can be read, changed or removed here."),
    h("div", { class: "row" }, h("button", { class: "danger", onclick: () => { homeSend({ t: "homeRemove", id: w.id }); close(); } }, "Remove it"), h("span", { class: "grow" }), h("button", { class: "primary", onclick: close }, "OK"))));
}

// ---- built-in persistent kinds: chat and plugin ----

registerKind("chat", {
  create: (c) => {
    const agent = c.scope || "home";
    if (c.shape === "conversation") {
      const panel = new ChatPanel("hw-chat");
      panel.bind(agent, {
        placeholder: agent === "home" ? "Ask anything, or tell me how to set up this screen…" : `Message the ${hooks.agentName(agent)}…`,
        empty: () => h("div", { class: "msg system chat-empty" }, agent === "home" ? "The home agent: ask across your projects, file captures, or arrange this screen." : `The ${hooks.agentName(agent)}.`),
        context: () => ({ view: "" }),
      });
      return { el: panel.el };
    }
    const mini = new MiniChat(agent, {
      placeholder: agent === "home" ? "Ask anything, or tell me how to set up this screen" : `Ask the ${hooks.agentName(agent)}`,
      chips: () => hooks.chips(agent),
      onHistory: () => hooks.openChats(agent),
      context: () => ({ view: "" }),
    });
    return { el: mini.el, update: () => mini.draw(), destroy: () => mini.destroy() };
  },
});

registerKind("plugin", {
  create: (c) => {
    const holder = h("div", { class: "widget-frame" });
    const m = mountPlugin(holder, "widget:" + c.w.id, c.w.plugin || "");
    let hash = (app?.plugins || []).find((p) => p.id === c.w.plugin)?.hash || "";
    return {
      el: holder,
      update: () => { const pl = (app?.plugins || []).find((p) => p.id === c.w.plugin); if (pl && pl.hash !== hash) { hash = pl.hash; m.load(); } },
      destroy: () => m.destroy(),
    };
  },
});

export function toastAgentChange(m: { what: string; why?: string }) {
  toast(`Home agent ${m.what}${m.why ? ` (${m.why})` : ""}`, "info", { label: "Undo", run: () => homeSend({ t: "homeUndo" }) });
}

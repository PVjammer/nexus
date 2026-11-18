// Shared plumbing: the websocket contract to the core, app state, helpers.

export type Node = {
  kind: "split" | "group"; id: string; dir?: "row" | "col"; sizes?: number[];
  children?: Node[]; tabs?: string[]; active?: string;
};
export type Space = { id: string; name: string; root: Node; target: string; focused: string };
export type Pane = {
  id: string; space: string; kind: string; title: string; props: Record<string, any>;
  opener: "you" | "agent"; visibility: "full" | "title" | "none"; visLocked?: boolean; pinned?: boolean; badge?: boolean;
};
export type PluginInfo = {
  id: string; name: string; description?: string; permissions: string[]; grants: Record<string, string>;
  kept: boolean; version: number; hash: string;
};
export type AppState = {
  state: { spaces: Space[]; activeSpace: string; panes: Record<string, Pane> };
  undo: Record<string, { depth: number; label: string }>;
  plugins: PluginInfo[]; roots: string[]; agent: string;
};

// ---- the desk: projects (read from your folders) and what Nexus owns ----
export type Workstream = { name: string; kind?: string; goal?: string; agentStatus?: string; reportStatus?: string; summary?: string; updated: string; closed?: boolean; report?: string; harness?: string };
export type FEvent = { time: string; ws?: string; kind: string; summary: string };
export type Detail = {
  id: string; name: string; path: string; brief?: string; space: string; added: string;
  exists: boolean; readme?: string; foreman: boolean; workstreams?: Workstream[]; archived?: number;
  backlog?: { text: string; done?: boolean }[]; inbox?: { id: string; time: string; ws?: string; kind: string; summary: string }[];
  events?: FEvent[]; git?: { branch: string; dirty: number; commits?: { hash: string; when: string; subject: string }[] };
  docs?: string[]; lastActivity: string;
};
export type Capture = { id: string; text: string; created: string; status: string; proposal?: { project: string; kind: string; title: string; reason?: string }; item?: string };
export type Item = { id: string; project: string; kind: string; title: string; body?: string; state: string; source: string; created: string };
export type Task = { id: string; project: string; title: string; brief: string; status: string; summary?: string; report?: string; reports?: string[]; from?: string; reviewed?: boolean; created: string; updated: string };
export type Need = { id: string; source: string; project?: string; title: string; detail?: string; kind?: string; time: string; report?: string; task?: string; count?: number; agent?: string; msg?: number };
export type Running = { source: string; project?: string; title: string; detail?: string; since: string; task?: string; agent?: string };
// The home screen is a layout of widgets, stored as data in the core.
export type LayoutWidget = { id: string; kind: string; shape?: string; scope?: string; plugin?: string; w: number; h?: number; by: "you" | "agent" | "default"; why?: string; added: string };
export type Layout = { cols: number; tiling: "flow" | "tile"; widgets: LayoutWidget[] | null };
export type WidgetKind = { kind: string; label: string; shows: string; shapes: string[]; scoped: boolean; defaultW: number; internal?: boolean };
export type DeskState = {
  desk: { captures: Capture[] | null; items: Item[] | null; tasks: Task[] | null; layouts: Record<string, Layout> | null; dismissed: Record<string, boolean> | null; workTasks?: any[] | null };
  workItems?: any[];
  homeKinds?: WidgetKind[]; homeUndo?: { depth: number; label: string };
  projects: Detail[] | null; needs: Need[] | null; running: Running[] | null;
  wikis?: Record<string, { pages: WikiPage[] | null; proposals: WikiProposal[] | null }>;
  addons?: Record<string, AddonState[] | null>;
};
export type WikiPage = { slug: string; title: string; meta: { summary?: string; sources?: string[]; verified?: string; updatedBy?: string }; updated: string; size: number; stale?: string[] };
export type WikiProposal = { id: string; project: string; slug: string; content: string; reason: string; sources?: string[]; by: string; task?: string; created: string; status: string };
export type AddonState = { id: string; name: string; description: string; kind: string; on: boolean; unavailable?: string; status?: { lastSync: string; files: number; error?: string; syncing?: boolean } };
export let desk: DeskState | null = null;

// The view is local to this window: home, or one project.
export type View = { kind: "home" } | { kind: "project"; id: string };
export let view: View = { kind: "home" };
try { const v = JSON.parse(localStorage.getItem("nexus-desk-view") || "null"); if (v && (v.kind === "home" || v.kind === "project")) view = v; } catch {}
export function setView(v: View) {
  view = v;
  try { localStorage.setItem("nexus-desk-view", JSON.stringify(v)); } catch {}
  emit("view", v);
}
export function project(id?: string): Detail | undefined {
  const pid = id ?? (view.kind === "project" ? view.id : "");
  return desk?.projects?.find((p) => p.id === pid);
}

type Handler = (m: any) => void;
const handlers = new Map<string, Set<Handler>>();
let ws: WebSocket | null = null;
let queue: string[] = [];
export let app: AppState | null = null;
export const token = new URLSearchParams(location.hash.slice(1)).get("token") || localStorage.getItem("nexus-token") || "";
if (token) { try { localStorage.setItem("nexus-token", token); } catch {} }

export function on(t: string, f: Handler): () => void {
  if (!handlers.has(t)) handlers.set(t, new Set());
  handlers.get(t)!.add(f);
  return () => handlers.get(t)!.delete(f);
}

export function emit(t: string, m: any) {
  handlers.get(t)?.forEach((f) => { try { f(m); } catch (e) { console.error(t, e); } });
}

export function send(m: Record<string, any>) {
  // Layout and pane messages act on the project you're looking at.
  if (m.space === undefined) { const sp = space(); if (sp) m = { ...m, space: sp.id }; }
  const s = JSON.stringify(m);
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(s);
  else queue.push(s);
}

let rid = 0;
const waiting = new Map<string, (m: any) => void>();
/** request sends a message and resolves with the reply carrying the same rid. */
export function request(m: Record<string, any>): Promise<any> {
  const id = "r" + ++rid;
  return new Promise((res, rej) => {
    waiting.set(id, (r) => (r.t === "error" ? rej(new Error(r.error)) : res(r)));
    send({ ...m, rid: id });
  });
}

export function connect() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`);
  ws.onopen = () => { emit("connected", {}); queue.forEach((s) => ws!.send(s)); queue = []; };
  ws.onclose = () => { emit("disconnected", {}); setTimeout(connect, 1000); };
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.rid && waiting.has(m.rid)) { waiting.get(m.rid)!(m); waiting.delete(m.rid); return; }
    if (m.t === "state") {
      // Go omits empty maps; normalize so panes can always read props.
      for (const p of Object.values((m as AppState).state.panes)) p.props = p.props || {};
      app = m as AppState;
    }
    if (m.t === "desk") desk = m as DeskState;
    if (m.t === "error") toast(m.error, "error");
    if (m.t === "toast") toast(m.text, m.level);
    emit(m.t, m);
  };
}

/** space is the layout space of the project you're looking at (none at home). */
export function space(): Space | undefined {
  if (view.kind !== "project") return undefined;
  const p = project();
  return p ? app?.state.spaces.find((s) => s.id === p.space) : undefined;
}

// ---- tiny DOM helper ----
type Attrs = Record<string, any>;
export function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs: Attrs = {}, ...kids: (Node_ | string | null | undefined | false)[]): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === "class") el.className = v;
    else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
    else if (k === "html") el.innerHTML = v;
    else if (v === true) el.setAttribute(k, "");
    else el.setAttribute(k, String(v));
  }
  for (const c of kids) if (c !== null && c !== undefined && c !== false) el.append(c as any);
  return el;
}
type Node_ = globalThis.Node;

export function toast(text: string, level = "info", action?: { label: string; run: () => void }) {
  const el = h("div", { class: `toast ${level}` }, text,
    action ? h("button", { class: "toast-act", onclick: () => { action.run(); el.remove(); } }, action.label) : "");
  document.getElementById("toasts")!.append(el);
  setTimeout(() => el.remove(), level === "error" || action ? 8000 : 4000);
}

export function modal(build: (close: () => void) => HTMLElement): () => void {
  const root = document.getElementById("modal-root")!;
  const back = h("div", { class: "backdrop" });
  const close = () => back.remove();
  back.append(build(close));
  root.append(back);
  return close;
}

export function confirmModal(title: string, body: string, ok = "Continue", danger = false): Promise<boolean> {
  return new Promise((res) => {
    modal((close) => h("div", { class: "modal" },
      h("h3", {}, title), h("div", { html: body }),
      h("div", { class: "row" },
        h("button", { onclick: () => { close(); res(false); } }, "Cancel"),
        h("button", { class: danger ? "primary danger" : "primary", onclick: () => { close(); res(true); } }, ok))));
  });
}

export function promptModal(title: string, placeholder: string, value = ""): Promise<string | null> {
  return new Promise((res) => {
    const input = h("input", { placeholder, value, style: { width: "100%" } }) as HTMLInputElement;
    modal((close) => {
      const done = (v: string | null) => { close(); res(v); };
      input.addEventListener("keydown", (e) => { if (e.key === "Enter") done(input.value.trim() || null); if (e.key === "Escape") done(null); });
      setTimeout(() => input.focus(), 0);
      return h("div", { class: "modal" }, h("h3", {}, title), input,
        h("div", { class: "row" }, h("button", { onclick: () => done(null) }, "Cancel"),
          h("button", { class: "primary", onclick: () => done(input.value.trim() || null) }, "OK")));
    });
  });
}

export const isDark = () => matchMedia("(prefers-color-scheme: dark)").matches;
export const esc = (s: string) => s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]!));

export function ago(iso: string | undefined): string {
  if (!iso) return "never";
  const t = new Date(iso).getTime();
  if (!t || t < 0) return "never";
  const d = (Date.now() - t) / 1000;
  if (d < 60) return "just now";
  if (d < 3600) return `${Math.floor(d / 60)}m ago`;
  if (d < 172800) return `${Math.floor(d / 3600)}h ago`;
  return `${Math.floor(d / 86400)}d ago`;
}

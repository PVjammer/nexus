// Pane renderers, one per kind. Each pane has a header with its own tools and
// an "agent sees" chip, so what the agent can see is visible on every pane.

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { ic } from "./icons";
import { app, desk, esc, h, isDark, on, request, send, toast, token, type Pane } from "./core";

export type PaneView = {
  el: HTMLElement;
  update(p: Pane): void;
  destroy(): void;
  shown?(): void;
  resized?(): void;
  selection?(): string;
};

export { md } from "./md";
import { md } from "./md";
import { ChatPanel } from "./chat";
import { workItemView } from "./work";

// ---- shared frame ----

// Each pane says where it lives when that matters: your machine (the agent
// can't see it), private to you (secure panes), or the agent's workspace.
function placeTag(p: Pane): [string, string, string] | null {
  if (p.kind === "terminal") return ["mine", "your machine · the agent can't read it", "monitor"];
  if (p.kind === "secure-view" || p.kind === "secret-input") return ["none", "private to you", "lock"];
  if (p.kind === "task") return ["agent", "agent workspace · read-only on your project", "agent"];
  return null;
}

function frame(p: Pane) {
  const head = h("div", { class: "phead" });
  const tools = h("div", { class: "grow", style: { display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" } });
  const chip = h("span", { class: "vis" });
  head.append(tools, chip);
  const body = h("div", { class: "pbody" });
  const el = h("div", {}, head, body);
  const setVis = (np: Pane) => {
    const t = placeTag(np);
    chip.hidden = !t;
    if (t) { chip.className = `vis ${t[0]}`; chip.replaceChildren(ic(t[2]), " " + t[1]); }
  };
  setVis(p);
  return { el, head, tools, body, setVis };
}

const basic = (f: ReturnType<typeof frame>, extra: Partial<PaneView> = {}): PaneView => ({
  el: f.el, update: (p) => f.setVis(p), destroy: () => {}, ...extra,
});

// ---- registry ----

export function createPane(p: Pane): PaneView {
  switch (p.kind) {
    case "sheet": return sheetPane(p);
    case "review": return reviewPane(p);
    case "terminal": return terminalPane(p);
    case "markdown": return markdownPane(p);
    case "html": return htmlPane(p);
    case "browser": return browserPane(p);
    case "plugin": return pluginPane(p);
    case "files": return filesPane(p);
    case "log": return logPane(p);
    case "secure-view": return secureViewPane(p);
    case "secret-input": return secretInputPane(p);
    case "task": return taskPane(p);
    case "workitem": return workItemPane(p);
  }
  const f = frame(p);
  f.body.append(h("div", { class: "pad muted" }, `Unknown pane kind ${p.kind}`));
  return basic(f);
}

// ---- a work item: its tasks and done-when, with one task docked ----

function workItemPane(p: Pane): PaneView {
  const f = frame(p);
  f.tools.append(h("span", { class: "muted small" }, "Work item · tasks live only in Nexus"));
  const v = workItemView(String(p.props.ref), p.props.task ? String(p.props.task) : undefined);
  f.body.classList.add("pad");
  f.body.append(v.el);
  let task = p.props.task;
  return { el: f.el, update: (np) => { f.setVis(np); if (np.props.task !== task) { task = np.props.task; v.focus(String(task || "")); } }, destroy: v.destroy };
}

// ---- markdown / html (agent-made content) ----

function markdownPane(p: Pane): PaneView {
  const f = frame(p);
  f.body.classList.add("pad");
  const doc = h("div", { class: "md" });
  f.body.append(doc);
  f.tools.append(h("span", { class: "muted small" }, p.opener === "agent" ? "Written by the agent" : "Note"));
  let last = "";
  const upd = (np: Pane) => {
    f.setVis(np);
    const c = String(np.props.content || "");
    if (c !== last) { doc.innerHTML = md(c); last = c; }
  };
  upd(p);
  return { el: f.el, update: upd, destroy: () => {}, selection: () => selIn(doc) };
}

const htmlCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:";

function htmlPane(p: Pane): PaneView {
  const f = frame(p);
  f.tools.append(h("span", { class: "muted small" }, "Sandboxed HTML (no scripts, no network)"));
  const fr = h("iframe", { class: "fill", sandbox: "" }) as HTMLIFrameElement;
  f.body.append(fr);
  let last = "";
  const upd = (np: Pane) => {
    f.setVis(np);
    const c = String(np.props.content || "");
    if (c === last) return;
    last = c;
    const base = `<style>:root{color-scheme:light dark}body{font-family:system-ui;margin:14px;color:${isDark() ? "#e3e7ee" : "#1d2330"};background:${isDark() ? "#1a1e26" : "#fff"}}</style>`;
    fr.srcdoc = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${htmlCSP}">${base}</head><body>${c}</body></html>`;
  };
  upd(p);
  return { el: f.el, update: upd, destroy: () => {} };
}

function selIn(el: HTMLElement): string {
  const s = getSelection();
  if (!s || s.isCollapsed || !s.anchorNode || !el.contains(s.anchorNode)) return "";
  return s.toString();
}

// ---- sheet (the built-in "local Sheets" app) ----

type Sheet = { path: string; rev: string; headers: string[]; rows: string[][] };
type View = { sortCol: number; sortDesc?: boolean; filter?: string; filterCol?: number };

function sheetPane(p: Pane): PaneView {
  const f = frame(p);
  const path = String(p.props.path);
  let sheet: Sheet | null = null;
  let view: View = { sortCol: -1, filter: "", filterCol: -1, ...(p.props.view || {}) };
  let editing = false;
  const filter = h("input", { placeholder: "Filter rows…", style: { width: "160px" } }) as HTMLInputElement;
  const info = h("span", { class: "muted small" });
  const stale = h("div", { class: "banner warn", hidden: true });
  const addRow = h("button", { title: "Add a row", onclick: () => ops([{ op: "addRow", cells: [] }]) }, "+ Row");
  const addCol = h("button", { title: "Add a column", onclick: async () => {
    const name = prompt("Column name");
    if (name) ops([{ op: "addCol", value: name }]);
  } }, "+ Column");
  f.tools.append(filter, addRow, addCol, info);
  const table = h("table", { class: "sheet" });
  f.body.append(stale, table);
  let ft: any;
  filter.addEventListener("input", () => {
    clearTimeout(ft);
    ft = setTimeout(() => send({ t: "sheetView", pane: p.id, view: { ...view, filter: filter.value } }), 250);
  });

  const ops = (o: any[]) => { if (sheet) send({ t: "sheetOps", path, rev: sheet.rev, ops: o }); };

  const load = async () => {
    try { const r = await request({ t: "sheetLoad", path }); setSheet(r.sheet, false); }
    catch (e: any) { table.replaceChildren(h("tr", {}, h("td", { class: "muted" }, String(e.message)))); }
  };

  const setSheet = (s: Sheet, flash: boolean) => {
    const prev = sheet;
    sheet = s;
    stale.hidden = true;
    if (!editing) draw(flash && prev ? changedRows(prev, s) : new Set());
  };

  const changedRows = (a: Sheet, b: Sheet) => {
    const out = new Set<number>();
    b.rows.forEach((r, i) => { if (!a.rows[i] || a.rows[i].join("\u0001") !== r.join("\u0001")) out.add(i); });
    return out;
  };

  const draw = (changed: Set<number>) => {
    if (!sheet) return;
    const s = sheet;
    let idx = s.rows.map((_, i) => i);
    const q = (view.filter || "").toLowerCase();
    if (q) idx = idx.filter((i) => s.rows[i].some((c, j) => (view.filterCol ?? -1) < 0 || j === view.filterCol ? c.toLowerCase().includes(q) : false));
    if (view.sortCol >= 0 && view.sortCol < s.headers.length) {
      const c = view.sortCol;
      const num = idx.every((i) => s.rows[i][c] === "" || !isNaN(Number(s.rows[i][c])));
      idx.sort((a, b) => {
        const x = s.rows[a][c], y = s.rows[b][c];
        const r = num ? Number(x || 0) - Number(y || 0) : x.localeCompare(y, undefined, { numeric: true });
        return view.sortDesc ? -r : r;
      });
    }
    info.textContent = `${idx.length}${idx.length !== s.rows.length ? ` of ${s.rows.length}` : ""} rows${view.sortCol >= 0 || q ? " · view only, file unchanged" : ""}`;
    const head = h("tr", {}, h("th", { class: "rn" }, "#"));
    s.headers.forEach((name, c) => {
      const ind = view.sortCol === c ? (view.sortDesc ? " ▼" : " ▲") : "";
      head.append(h("th", { title: "Click to sort (view only). Double-click to rename.",
        onclick: () => {
          const nv = { ...view };
          if (nv.sortCol !== c) { nv.sortCol = c; nv.sortDesc = false; } else if (!nv.sortDesc) nv.sortDesc = true; else nv.sortCol = -1;
          send({ t: "sheetView", pane: p.id, view: nv });
        },
        ondblclick: () => { const v = prompt("Rename column", name); if (v && v !== name) ops([{ op: "renameCol", col: c, value: v }]); },
      }, h("div", { class: "h" }, name, h("span", { class: "sortind" }, ind))));
    });
    const rows = idx.map((i) => {
      const tr = h("tr", { class: changed.has(i) ? "changed" : "" },
        h("td", { class: "rn" }, h("span", { class: "num" }, String(i)),
          h("button", { class: "del", title: "Delete row", onclick: () => ops([{ op: "deleteRow", row: i }]) }, "✕")));
      s.rows[i].forEach((val, c) => {
        const cell = h("div", { class: "c", title: val }, val);
        const td = h("td", {}, cell);
        td.addEventListener("click", () => editCell(td, i, c, val));
        tr.append(td);
      });
      return tr;
    });
    table.replaceChildren(h("thead", {}, head), h("tbody", {}, ...rows));
  };

  const editCell = (td: HTMLElement, row: number, col: number, val: string) => {
    if (editing) return;
    editing = true;
    const input = h("input", { value: val }) as HTMLInputElement;
    td.replaceChildren(input);
    input.focus();
    input.select();
    let done = false;
    const finish = (commit: boolean) => {
      if (done) return;
      done = true;
      editing = false;
      if (commit && input.value !== val) ops([{ op: "set", row, col, value: input.value }]);
      draw(new Set());
    };
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") finish(true);
      if (e.key === "Escape") finish(false);
      if (e.key === "Tab") { e.preventDefault(); finish(true); }
    });
    input.addEventListener("blur", () => finish(true));
  };

  const offs = [
    on("sheet", (m) => { if (m.sheet?.path === path) setSheet(m.sheet, m.by === "agent"); }),
    on("sheetStale", (m) => {
      if (m.sheet?.path !== path) return;
      stale.hidden = false;
      stale.replaceChildren("The file changed since you loaded it, so your edit wasn't applied.",
        h("button", { onclick: () => setSheet(m.sheet, false) }, "Reload"));
    }),
    on("fileChanged", (m) => { if (m.path === path && !editing) load(); }),
    on("connected", () => load()),
  ];
  load();
  return {
    el: f.el,
    update: (np) => {
      f.setVis(np);
      const nv = { sortCol: -1, filter: "", filterCol: -1, ...(np.props.view || {}) };
      if (JSON.stringify(nv) !== JSON.stringify(view)) {
        view = nv;
        if (document.activeElement !== filter) filter.value = view.filter || "";
        draw(new Set());
      }
    },
    destroy: () => offs.forEach((o) => o()),
  };
}

// ---- review (documents with anchored comments and suggestions) ----

type Anchor = { exact: string; prefix?: string; suffix?: string };
type Thread = {
  id: string; anchor: Anchor; status: string;
  comments: { id: string; author: string; text: string }[];
  suggestions?: { id: string; author: string; find: string; replace: string; note?: string; status: string }[];
};

function reviewPane(p: Pane): PaneView {
  const f = frame(p);
  const path = String(p.props.path);
  const isHTML = /\.html?$/i.test(path);
  const doc = h("div", { class: "doc md" });
  const side = h("div", { class: "threads" });
  const wrap = h("div", { class: "review" }, doc, side);
  f.body.classList.add("review-host");
  f.body.append(wrap);
  const count = h("span", { class: "muted small" });
  const showResolved = h("input", { type: "checkbox" }) as HTMLInputElement;
  f.tools.append(count, h("label", { class: "muted small" }, showResolved, " show resolved"),
    isHTML ? h("span", { class: "muted small" }, "HTML rendered sandboxed (scripts and network blocked). Comments work on Markdown and text.") : "");
  showResolved.addEventListener("change", () => drawThreads());
  const editable = /^(wiki|desk)\//.test(path);
  let editing = false;
  if (editable) {
    const editBtn = h("button", { title: "Edit this page (the previous version is kept)", onclick: () => toggleEdit() }, "Edit");
    f.tools.append(editBtn);
  }
  const toggleEdit = () => {
    if (editing) { editing = false; drawDoc(); return; }
    editing = true;
    const ta = h("textarea", { class: "doc-editor", spellcheck: "true" }) as HTMLTextAreaElement;
    ta.value = content;
    const save = async () => {
      try { await request({ t: "fileSave", path, content: ta.value }); content = ta.value; editing = false; drawDoc(); toast("Saved"); }
      catch (e: any) { toast(e.message, "error"); }
    };
    ta.addEventListener("keydown", (e) => { if ((e.ctrlKey || e.metaKey) && e.key === "s") { e.preventDefault(); save(); } if (e.key === "Escape") { editing = false; drawDoc(); } });
    doc.replaceChildren(h("div", { class: "editbar" }, h("span", { class: "muted small" }, "Editing · Ctrl+S saves · Esc cancels"),
      h("button", { onclick: () => { editing = false; drawDoc(); } }, "Cancel"), h("button", { class: "primary", onclick: save }, "Save")), ta);
    setTimeout(() => ta.focus(), 0);
  };
  let content = "";
  let threads: Thread[] = [];
  let selThread = "";
  let orphans = new Set<string>();

  const load = async () => {
    try {
      const r = await request({ t: "reviewLoad", path });
      content = r.content;
      threads = r.doc?.threads || [];
      drawDoc();
    } catch (e: any) { doc.textContent = String(e.message); }
  };

  const drawDoc = () => {
    if (editing) return;
    if (isHTML) {
      doc.classList.remove("md");
      doc.style.padding = "0";
      doc.replaceChildren(h("iframe", { sandbox: "", src: `/raw/${path}?token=${encodeURIComponent(token)}&v=${Date.now()}` }));
    } else if (/\.md$/i.test(path)) {
      doc.innerHTML = md(content);
    } else {
      doc.replaceChildren(h("pre", { style: { whiteSpace: "pre-wrap" } }, content));
    }
    highlight();
    drawThreads();
  };

  const highlight = () => {
    orphans = new Set();
    if (isHTML) return;
    for (const t of threads) {
      if (t.status === "resolved" && !showResolved.checked) continue;
      const ok = markAnchor(doc, t.anchor, t.id, t.comments[0]?.author === "agent");
      if (!ok) orphans.add(t.id);
    }
    doc.querySelectorAll<HTMLElement>("mark[data-thread]").forEach((m) => {
      m.addEventListener("click", () => select(m.dataset.thread!));
    });
  };

  const select = (id: string) => {
    selThread = id;
    doc.querySelectorAll("mark.sel").forEach((m) => m.classList.remove("sel"));
    doc.querySelectorAll(`mark[data-thread="${id}"]`).forEach((m) => m.classList.add("sel"));
    doc.querySelector(`mark[data-thread="${id}"]`)?.scrollIntoView({ block: "center", behavior: "smooth" });
    drawThreads();
    side.querySelector(`[data-thread="${id}"]`)?.scrollIntoView({ block: "nearest" });
  };

  const drawThreads = () => {
    const vis = threads.filter((t) => t.status !== "resolved" || showResolved.checked);
    const open = threads.filter((t) => t.status !== "resolved").length;
    count.textContent = `${open} open thread${open === 1 ? "" : "s"}`;
    if (!vis.length) {
      side.replaceChildren(h("div", { class: "muted small" }, isHTML ? "No comments." : "Select text in the document to start a comment thread."));
      return;
    }
    side.replaceChildren(...vis.map((t) => {
      const reply = h("input", { placeholder: "Reply…" }) as HTMLInputElement;
      const ask = h("input", { type: "checkbox", checked: true }) as HTMLInputElement;
      const sendReply = () => {
        if (!reply.value.trim()) return;
        send({ t: "reviewReply", path, thread: t.id, text: reply.value.trim(), ask: ask.checked });
        reply.value = "";
      };
      reply.addEventListener("keydown", (e) => { if (e.key === "Enter") sendReply(); });
      return h("div", { class: `thread${t.id === selThread ? " sel" : ""}${t.status === "resolved" ? " resolved" : ""}`, "data-thread": t.id },
        orphans.has(t.id) ? h("div", { class: "orphan" }, "⚠ Anchor not found: the text changed. The thread is kept.") : "",
        h("div", { class: "q", onclick: () => select(t.id) }, `“${t.anchor.exact}”`),
        ...t.comments.map((c) => h("div", { class: "cmt" },
          h("div", { class: `who ${c.author}` }, c.author === "agent" ? h("span", {}, ic("agent"), " agent") : "you"),
          h("div", { class: "body" }, c.text))),
        ...(t.suggestions || []).map((s) => h("div", { class: "sugg" },
          h("div", { class: "st" }, `Suggested edit · ${s.status}`),
          s.note ? h("div", { class: "small" }, s.note) : "",
          h("del", {}, s.find), h("ins", {}, s.replace),
          s.status === "pending" ? h("div", { class: "acts" },
            h("button", { class: "primary", onclick: () => send({ t: "suggestion", path, thread: t.id, sugg: s.id, accept: true }) }, "Accept"),
            h("button", { onclick: () => send({ t: "suggestion", path, thread: t.id, sugg: s.id, accept: false }) }, "Reject")) : "")),
        h("div", { class: "replybox" }, reply, h("button", { onclick: sendReply }, "↵")),
        h("div", { class: "acts" },
          h("label", { class: "muted small", title: "Also ask the agent to respond" }, ask, " ask agent"),
          h("span", { class: "grow" }),
          t.status === "resolved"
            ? h("button", { onclick: () => send({ t: "reviewStatus", path, thread: t.id, status: "open" }) }, "Reopen")
            : h("button", { onclick: () => send({ t: "reviewStatus", path, thread: t.id, status: "resolved" }) }, "Resolve")));
    }));
  };

  // selecting text offers a comment button
  let cbtn: HTMLElement | null = null;
  const clearBtn = () => { cbtn?.remove(); cbtn = null; };
  doc.addEventListener("mouseup", () => {
    setTimeout(() => {
      clearBtn();
      if (isHTML) return;
      const s = getSelection();
      if (!s || s.isCollapsed || !doc.contains(s.anchorNode)) return;
      const exact = s.toString().trim();
      if (exact.length < 2) return;
      const r = s.getRangeAt(0).getBoundingClientRect();
      const anchor = anchorFor(doc, s.getRangeAt(0), exact);
      cbtn = h("button", { class: "cbtn primary", style: { left: `${r.right + 6}px`, top: `${r.top - 4}px` },
        onmousedown: (e: Event) => e.preventDefault(),
        onclick: () => { clearBtn(); openComposer(anchor, r); } }, ic("chat"), " Comment");
      document.body.append(cbtn);
    }, 0);
  });
  document.addEventListener("mousedown", (e) => { if (cbtn && e.target !== cbtn) clearBtn(); });

  const openComposer = (anchor: Anchor, r: DOMRect) => {
    const ta = h("textarea", { rows: 3, placeholder: "Comment, question or requested change…" }) as HTMLTextAreaElement;
    const ask = h("input", { type: "checkbox", checked: true }) as HTMLInputElement;
    const pop = h("div", { class: "cpop", style: { left: `${Math.min(r.left, innerWidth - 320)}px`, top: `${Math.min(r.bottom + 6, innerHeight - 170)}px` } },
      h("div", { class: "small muted" }, `On “${anchor.exact.slice(0, 80)}${anchor.exact.length > 80 ? "…" : ""}”`), ta,
      h("div", { class: "row" }, h("label", { class: "small" }, ask, " Ask the agent to respond"), h("span", { class: "grow", style: { flex: "1" } }),
        h("button", { onclick: () => pop.remove() }, "Cancel"),
        h("button", { class: "primary", onclick: () => {
          if (!ta.value.trim()) return;
          send({ t: "reviewComment", path, anchor, text: ta.value.trim(), ask: ask.checked });
          pop.remove();
        } }, "Comment")));
    ta.addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) (pop.querySelector("button.primary") as HTMLButtonElement).click(); if (e.key === "Escape") pop.remove(); });
    document.body.append(pop);
    ta.focus();
  };

  const offs = [
    on("review", (m) => {
      if (m.path !== path) return;
      const prevIds = new Set(threads.map((t) => t.id));
      threads = m.doc?.threads || [];
      const fresh = threads.find((t) => !prevIds.has(t.id));
      // content may have changed (accepted suggestion) — reload source
      request({ t: "reviewLoad", path }).then((r) => { content = r.content; drawDoc(); if (fresh) select(fresh.id); }).catch(() => {});
    }),
    on("fileChanged", (m) => { if (m.path === path) load(); }),
    on("connected", () => load()),
  ];
  load();
  return { el: f.el, update: (np) => f.setVis(np), destroy: () => { offs.forEach((o) => o()); clearBtn(); }, selection: () => selIn(doc) };
}

/** Build a TextQuoteSelector-style anchor from a DOM range. */
function anchorFor(root: HTMLElement, range: Range, exact: string): Anchor {
  const full = root.textContent || "";
  const pre = document.createRange();
  pre.selectNodeContents(root);
  pre.setEnd(range.startContainer, range.startOffset);
  const start = (pre.toString()).length;
  const lead = range.toString().indexOf(exact);
  const s = start + Math.max(0, lead);
  return { exact, prefix: full.slice(Math.max(0, s - 32), s), suffix: full.slice(s + exact.length, s + exact.length + 32) };
}

/** Highlight an anchor in rendered text; returns false if orphaned. */
function markAnchor(root: HTMLElement, a: Anchor, id: string, agent: boolean): boolean {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  let full = "";
  const starts: number[] = [];
  while (walker.nextNode()) { const t = walker.currentNode as Text; starts.push(full.length); nodes.push(t); full += t.data; }
  const norm = (s: string) => s.replace(/\s+/g, " ");
  // whitespace-insensitive search: map normalized index -> original index
  const map: number[] = [];
  let nf = "";
  for (let i = 0; i < full.length; i++) {
    const ws = /\s/.test(full[i]);
    if (ws && nf.endsWith(" ")) continue;
    map.push(i); nf += ws ? " " : full[i];
  }
  const ex = norm(a.exact.trim());
  if (!ex) return false;
  let best = -1, bestScore = -1;
  for (let i = nf.indexOf(ex); i >= 0; i = nf.indexOf(ex, i + 1)) {
    const pre = norm(a.prefix || ""), suf = norm(a.suffix || "");
    let score = 0;
    if (pre && nf.slice(0, i).endsWith(pre.slice(-12))) score += 2;
    if (suf && nf.slice(i + ex.length).startsWith(suf.slice(0, 12))) score += 1;
    if (score > bestScore) { best = i; bestScore = score; }
  }
  if (best < 0) return false;
  const s = map[best], e = map[best + ex.length - 1] + 1;
  for (let k = nodes.length - 1; k >= 0; k--) {
    const ns = starts[k], ne = ns + nodes[k].data.length;
    if (ne <= s || ns >= e) continue;
    let node = nodes[k];
    const from = Math.max(0, s - ns), to = Math.min(node.data.length, e - ns);
    if (to <= from) continue;
    if (to < node.data.length) node.splitText(to);
    if (from > 0) node = node.splitText(from);
    if (!node.data.trim()) continue;
    const m = document.createElement("mark");
    m.dataset.thread = id;
    if (agent) m.className = "agent";
    node.parentNode!.insertBefore(m, node);
    m.append(node);
  }
  return true;
}

// ---- terminal ----

function terminalPane(p: Pane): PaneView {
  const f = frame(p);
  const banner = h("div", { class: "banner agent", hidden: true });
  const hostEl = h("div", { class: "term-host" });
  const holder = h("div", { style: { position: "relative", flex: "1", minHeight: "0" } }, hostEl);
  f.body.style.display = "flex";
  f.body.style.flexDirection = "column";
  f.body.append(banner, holder);
  f.tools.append(h("span", { class: "muted small" }, "Your shell. The agent can only propose commands; you run them."));
  const term = new Terminal({ fontFamily: "ui-monospace, Menlo, Consolas, monospace", fontSize: 12.5, cursorBlink: true, convertEol: false,
    theme: { background: "#14171d" }, allowProposedApi: true });
  const fit = new FitAddon();
  term.loadAddon(fit);
  let opened = false;
  let termId = "";
  const doFit = () => {
    if (!opened || f.el.hidden) return;
    try { fit.fit(); if (termId) send({ t: "termResize", term: termId, rows: term.rows, cols: term.cols }); } catch {}
  };
  const open = () => {
    if (opened) return;
    opened = true;
    term.open(hostEl);
    requestAnimationFrame(doFit);
    send({ t: "termAttach", pane: p.id, rid: "term-" + p.id });
  };
  term.onData((d) => { if (termId) send({ t: "termInput", term: termId, data: d }); });
  const offs = [
    on("termReady", (m) => {
      if (m.pane !== p.id) return;
      termId = m.term;
      term.reset();
      if (m.scrollback) term.write(m.scrollback);
      doFit();
    }),
    on("termOut", (m) => { if (m.term === termId) term.write(m.data); }),
    on("termExit", (m) => { if (m.term === termId) { term.write("\r\n[process exited — close and reopen the pane]\r\n"); } }),
    on("connected", () => { if (opened) send({ t: "termAttach", pane: p.id }); }),
  ];
  const drawProposal = (np: Pane) => {
    const pr = np.props.proposal;
    if (!pr) { banner.hidden = true; return; }
    banner.hidden = false;
    banner.replaceChildren(h("b", {}, ic("agent"), " Proposed:"), h("code", {}, pr.command), pr.why ? h("span", { class: "muted" }, pr.why) : "",
      h("span", { style: { flex: "1" } }),
      h("button", { class: "primary", onclick: () => { send({ t: "termProposal", pane: p.id, action: "run" }); term.focus(); } }, "Run ⏎"),
      h("button", { title: "Type it into the prompt without running, so you can edit it", onclick: () => { send({ t: "termProposal", pane: p.id, action: "edit" }); term.focus(); } }, "Edit"),
      h("button", { onclick: () => send({ t: "termProposal", pane: p.id, action: "dismiss" }) }, "Dismiss"));
    requestAnimationFrame(doFit);
  };
  drawProposal(p);
  return {
    el: f.el,
    update: (np) => { f.setVis(np); drawProposal(np); },
    destroy: () => { offs.forEach((o) => o()); term.dispose(); },
    shown: () => { open(); requestAnimationFrame(doFit); },
    resized: () => doFit(),
  };
}

// ---- browser (follow along) ----

function browserPane(p: Pane): PaneView {
  const f = frame(p);
  const url = h("input", { value: String(p.props.url || "") }) as HTMLInputElement;
  const fr = h("iframe", { class: "fill", sandbox: "allow-scripts allow-same-origin allow-forms allow-popups", referrerpolicy: "no-referrer" }) as HTMLIFrameElement;
  f.tools.append(h("div", { class: "urlbar" }, url,
    h("button", { title: "Open in your real browser", onclick: () => send({ t: "openExternal", url: url.value }) }, "↗ Open in your browser")));
  const note = h("div", { class: "banner warn small" }, "Follow-along view, with no logins. Some sites refuse to load inside a pane; use ↗ for those.");
  f.body.style.display = "flex"; f.body.style.flexDirection = "column";
  const holder = h("div", { style: { position: "relative", flex: "1" } }, fr);
  f.body.append(note, holder);
  let cur = "";
  const go = (u: string) => { if (u && u !== cur) { cur = u; fr.src = u; url.value = u; } };
  url.addEventListener("keydown", (e) => { if (e.key === "Enter") { let u = url.value.trim(); if (!/^https?:\/\//.test(u)) u = "https://" + u; go(u); } });
  go(String(p.props.url || ""));
  return { el: f.el, update: (np) => { f.setVis(np); go(String(np.props.url || "")); }, destroy: () => {} };
}

// ---- plugins (vibed on the spot) ----

const pluginCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; media-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'";

function bridgeScript(theme: string) {
  return `<script>(function(){var pending={},n=0;
function rpc(m,p){return new Promise(function(res,rej){var id="q"+(++n);pending[id]={res:res,rej:rej};parent.postMessage({nexusBridge:1,id:id,method:m,params:p},"*");});}
window.addEventListener("message",function(e){if(e.source!==parent)return;var d=e.data;if(!d||!d.nexusReply)return;var p=pending[d.id];if(!p)return;delete pending[d.id];if(d.error)p.rej(new Error(d.error));else p.res(d.result);});
document.addEventListener("securitypolicyviolation",function(e){parent.postMessage({nexusBridge:1,id:"csp"+(++n),method:"csp_violation",params:{directive:e.effectiveDirective||e.violatedDirective,blocked:e.blockedURI}},"*");});
window.nexus={theme:${JSON.stringify(theme)},fetch:function(c,p){return rpc("fetch",{connection:c,path:p});},call:function(t,a){return rpc("call",{tool:t,args:a||{}});},setContext:function(t){return rpc("setContext",{text:String(t)});}};
})();</script>`;
}

function buildPluginDoc(html: string) {
  const head = `<meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${pluginCSP}"><meta name="color-scheme" content="light dark">${bridgeScript(isDark() ? "dark" : "light")}`;
  if (/<head[^>]*>/i.test(html)) return html.replace(/<head[^>]*>/i, (m) => m + head);
  if (/<html[^>]*>/i.test(html)) return html.replace(/<html[^>]*>/i, (m) => m + "<head>" + head + "</head>");
  return `<!doctype html><html><head>${head}</head><body>${html}</body></html>`;
}

const pluginFrames = new Map<Window, string>(); // contentWindow -> pane id

window.addEventListener("message", (e) => {
  const d = e.data;
  if (!d || !d.nexusBridge) return;
  const pane = pluginFrames.get(e.source as Window);
  if (!pane) return; // not one of our plugin frames
  send({ t: "bridge", pane, req: d.id, method: d.method, params: d.params || {} });
});

on("bridgeReply", (m) => {
  for (const [win, pane] of pluginFrames) {
    if (pane === m.pane) win.postMessage({ nexusReply: 1, id: m.req, result: m.result, error: m.error }, "*");
  }
});

/** mountPlugin runs a plugin in a sandboxed iframe inside holder; key routes
 * its bridge calls ("widget:<id>" for dashboard widgets, else a pane id). */
export function mountPlugin(holder: HTMLElement, key: string, pid: string) {
  let fr: HTMLIFrameElement | null = null;
  let hash = "";
  const load = async (force = false) => {
    const pl = (app?.plugins || []).find((x) => x.id === pid);
    if (!pl || (!force && pl.hash === hash)) return;
    hash = pl.hash;
    try {
      const r = await request({ t: "pluginLoad", plugin: pid });
      if (fr) { pluginFrames.delete(fr.contentWindow!); fr.remove(); }
      fr = h("iframe", { class: "fill", sandbox: "allow-scripts" }) as HTMLIFrameElement;
      holder.append(fr);
      pluginFrames.set(fr.contentWindow!, key);
      fr.srcdoc = buildPluginDoc(r.html);
    } catch (e: any) { toast(`plugin ${pid}: ${e.message}`, "error"); }
  };
  load();
  return { load, destroy: () => { if (fr) { pluginFrames.delete(fr.contentWindow!); fr.remove(); fr = null; } } };
}

export function pluginControls(pid: string): HTMLElement[] {
  const pl = (app?.plugins || []).find((x) => x.id === pid);
  if (!pl) return [h("span", { class: "muted" }, "Plugin removed")];
  return [
    pl.kept ? h("span", { class: "ok small", title: "Kept: survives restarts" }, "✓ kept")
      : h("span", { class: "draft", title: "Draft: made on the spot; gone after a restart unless you keep it" }, "DRAFT"),
    pl.kept ? h("button", { onclick: () => send({ t: "pluginKeep", plugin: pid, keep: false }) }, "Unkeep")
      : h("button", { class: "primary", onclick: () => send({ t: "pluginKeep", plugin: pid, keep: true }) }, "Keep"),
    !pl.kept ? h("button", { class: "danger", onclick: () => send({ t: "pluginDelete", plugin: pid }) }, "Discard") : "",
    ...pl.permissions.map((perm) => {
      const g = pl.grants[perm] || "";
      return h("span", { class: `perm ${g || "ask"}`, title: g === "granted" ? "Granted. Click to revoke." : g === "denied" ? "Denied. Click to reset (it will ask again)." : "Will ask on first use",
        style: { cursor: g ? "pointer" : "default" }, onclick: () => g && send({ t: "pluginRevoke", plugin: pid, perm }) },
        `${g === "granted" ? "✓" : g === "denied" ? "✕" : "?"} ${perm}`);
    }),
  ].filter(Boolean) as HTMLElement[];
}

function pluginPane(p: Pane): PaneView {
  const f = frame(p);
  const pid = String(p.props.plugin);
  const holder = h("div", { style: { position: "relative", height: "100%" } });
  f.body.append(holder);
  let fr: HTMLIFrameElement | null = null;
  let hash = "";
  const drawHead = () => {
    const pl = (app?.plugins || []).find((x) => x.id === pid);
    if (!pl) { f.tools.replaceChildren(h("span", { class: "muted" }, "Plugin removed")); return; }
    f.tools.replaceChildren(
      h("b", {}, pl.name), h("span", { class: "muted small" }, `v${pl.version}`),
      pl.kept ? h("span", { class: "ok small", title: "Kept: survives restarts" }, "✓ kept")
        : h("span", { class: "draft", title: "Draft: vibed on the spot; gone after a restart unless you keep it" }, "DRAFT"),
      pl.kept ? h("button", { onclick: () => send({ t: "pluginKeep", plugin: pid, keep: false }) }, "Unkeep")
        : h("button", { class: "primary", onclick: () => send({ t: "pluginKeep", plugin: pid, keep: true }) }, "Keep"),
      !pl.kept ? h("button", { class: "danger", onclick: () => send({ t: "pluginDelete", plugin: pid }) }, "Discard") : "",
      ...pl.permissions.map((perm) => {
        const g = pl.grants[perm] || "";
        return h("span", { class: `perm ${g || "ask"}`, title: g === "granted" ? "Granted. Click to revoke." : g === "denied" ? "Denied. Click to reset (it will ask again)." : "Will ask on first use",
          style: { cursor: g ? "pointer" : "default" }, onclick: () => g && send({ t: "pluginRevoke", plugin: pid, perm }) },
          `${g === "granted" ? "✓" : g === "denied" ? "✕" : "?"} ${perm}`);
      }),
      pl.permissions.length === 0 ? h("span", { class: "muted small" }, "no permissions") : "",
      h("button", { title: "Reload", onclick: () => { hash = ""; load(); } }, "↻"),
    );
  };
  const load = async () => {
    const pl = (app?.plugins || []).find((x) => x.id === pid);
    if (!pl || pl.hash === hash) return;
    hash = pl.hash;
    try {
      const r = await request({ t: "pluginLoad", plugin: pid });
      if (fr) { pluginFrames.delete(fr.contentWindow!); fr.remove(); }
      fr = h("iframe", { class: "fill", sandbox: "allow-scripts" }) as HTMLIFrameElement;
      holder.append(fr);
      pluginFrames.set(fr.contentWindow!, p.id);
      fr.srcdoc = buildPluginDoc(r.html);
    } catch (e: any) { toast(`plugin ${pid}: ${e.message}`, "error"); }
  };
  drawHead();
  load();
  return {
    el: f.el,
    update: (np) => { f.setVis(np); drawHead(); load(); },
    destroy: () => { if (fr) pluginFrames.delete(fr.contentWindow!); },
  };
}

// ---- files ----

function filesPane(p: Pane): PaneView {
  const f = frame(p);
  let cwd = String(p.props.path || "");
  const home = cwd;
  const crumbs = h("span", { class: "crumbs" });
  f.tools.append(crumbs, h("button", { onclick: () => list() }, "↻"));
  const ul = h("ul", { class: "list" });
  f.body.append(ul);
  const list = async () => {
    crumbs.replaceChildren(h("button", { onclick: () => { cwd = home; list(); } }, ic("folder")));
    let acc = "";
    for (const part of cwd.split("/").filter(Boolean)) {
      acc = acc ? `${acc}/${part}` : part;
      if (home && acc.length <= home.length && home.startsWith(acc)) continue;
      const target = acc;
      crumbs.append(" / ", h("button", { onclick: () => { cwd = target; list(); } }, part));
    }
    try {
      const r = await request({ t: "filesList", path: cwd });
      ul.replaceChildren(...(r.entries || []).map((e: any) => {
        const name = e.path.split("/").pop();
        const ext = (name.split(".").pop() || "").toLowerCase();
        const kind = ext === "csv" ? "sheet" : ["md", "txt", "html", "htm", "json", "yaml", "yml", "go", "ts", "toml"].includes(ext) ? "review" : "";
        return h("li", {
          onclick: () => {
            if (e.dir) { cwd = e.path; list(); return; }
            if (!kind) { toast(`No viewer for .${ext} yet`); return; }
            send({ t: "open", kind, props: { path: e.path } });
          },
        }, h("span", {}, ic(e.dir ? "folder" : kind === "sheet" ? "sheet" : kind ? "doc" : "note")), h("span", {}, name), !e.dir ? h("span", { class: "muted small" }, `${Math.ceil(e.size / 1024)} KB`) : "");
      }));
      if (!r.entries?.length) ul.replaceChildren(h("li", { class: "muted" }, "(empty)"));
    } catch (err: any) { ul.replaceChildren(h("li", { class: "muted" }, err.message)); }
  };
  list();
  return basic(f);
}

// ---- action log ----

const logEntries: any[] = [];
const logListeners = new Set<() => void>();
on("logHistory", (m) => { logEntries.length = 0; logEntries.push(...(m.entries || [])); logListeners.forEach((f) => f()); });
on("log", (m) => { logEntries.push(m.entry); if (logEntries.length > 1000) logEntries.shift(); logListeners.forEach((f) => f()); });

function logPane(p: Pane): PaneView {
  const f = frame(p);
  const filt = h("select", {}, h("option", { value: "" }, "everyone"), h("option", { value: "you" }, "you"), h("option", { value: "agent" }, "agents"),
    h("option", { value: "plugin" }, "plugins"), h("option", { value: "denied" }, "blocked only")) as HTMLSelectElement;
  const scope = h("select", {}, h("option", { value: "here" }, "this project"), h("option", { value: "all" }, "everywhere")) as HTMLSelectElement;
  scope.addEventListener("change", () => draw());
  f.tools.append(h("span", { class: "muted small" }, "Everything you and the agents did."), filt, scope);
  const box = h("div", {});
  f.body.append(box);
  const draw = () => {
    const v = filt.value;
    const isAgent = (a: string) => a.includes("agent");
    const items = logEntries.filter((e) => (scope.value === "all" || e.space === p.space || !e.space) &&
      (!v || (v === "denied" ? e.denied : v === "plugin" ? String(e.actor).startsWith("plugin") : v === "agent" ? isAgent(String(e.actor)) : e.actor === v))).slice(-300).reverse();
    box.replaceChildren(...items.map((e) => {
      const a = String(e.actor);
      const cls = a === "you" ? "a-you" : a.includes("agent") ? "a-agent" : a.startsWith("plugin") ? "a-plugin" : "a-system";
      return h("div", { class: `log-entry${e.denied ? " denied" : ""}` },
        h("span", { class: "muted" }, new Date(e.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })),
        h("span", { class: cls }, a),
        h("span", {}, e.denied ? ic("block") : "", ` ${e.action}`, e.target ? h("b", {}, ` ${e.target}`) : "", e.detail ? h("span", { class: "d" }, ` — ${e.detail}`) : ""));
    }));
    if (!items.length) box.replaceChildren(h("div", { class: "muted pad", style: { padding: "12px" } }, "Nothing yet."));
  };
  filt.addEventListener("change", draw);
  logListeners.add(draw);
  draw();
  return { el: f.el, update: (np) => f.setVis(np), destroy: () => logListeners.delete(draw) };
}

// ---- secure panes ----

const secureVals = new Map<string, string>();
const secureListeners = new Map<string, () => void>();
on("secureValue", (m) => { secureVals.set(m.pane, m.value); secureListeners.get(m.pane)?.(); });

function secureViewPane(p: Pane): PaneView {
  const f = frame(p);
  f.body.classList.add("pad");
  f.tools.append(h("span", { class: "ok small" }, "Delivered out of band, directly to you"));
  let revealed = false;
  let timer: any;
  const box = h("div", { class: "secure" });
  f.body.append(box);
  const draw = () => {
    const v = secureVals.get(p.id);
    const reason = p.props.reason ? h("div", { class: "muted small" }, `Agent’s reason: ${p.props.reason}`) : "";
    if (v === undefined) {
      box.replaceChildren(h("div", { class: "lock" }, ic("lock")), h("div", { class: "muted" }, "The value isn't held in memory anymore (the core restarted). Ask again."));
      return;
    }
    box.replaceChildren(
      h("div", { class: "lock" }, ic("lock")), h("h3", {}, String(p.props.item)), reason,
      h("div", { class: "val" }, revealed ? v : "•".repeat(Math.min(16, Math.max(8, v.length)))),
      h("div", { style: { display: "flex", gap: "8px", justifyContent: "center" } },
        h("button", { onclick: () => { revealed = !revealed; clearTimeout(timer); if (revealed) timer = setTimeout(() => { revealed = false; draw(); }, 20000); draw(); } }, revealed ? "Hide" : "Show (20 s)"),
        h("button", { class: "primary", onclick: async () => { try { await navigator.clipboard.writeText(v); toast("Copied. Clipboard clears in 30 s"); setTimeout(() => navigator.clipboard.writeText("").catch(() => {}), 30000); } catch { toast("Clipboard unavailable; use Show", "warn"); } } }, "Copy")),
      h("div", { class: "note" }, "The agent received only an acknowledgement (DATA_DELIVERED_OUT_OF_BAND), never this value. It isn't written to the action log or the agent transcript. Closing this pane drops it from memory."));
  };
  secureListeners.set(p.id, draw);
  draw();
  return { el: f.el, update: (np) => { f.setVis(np); p = np; }, destroy: () => { secureListeners.delete(p.id); secureVals.delete(p.id); clearTimeout(timer); } };
}

function secretInputPane(p: Pane): PaneView {
  const f = frame(p);
  f.body.classList.add("pad");
  f.tools.append(h("span", { class: "ok small" }, "Secure entry: the agent gets a handle, never the value"));
  const box = h("div", { class: "secure" });
  f.body.append(box);
  const draw = (np: Pane) => {
    if (np.props.done) {
      box.replaceChildren(h("div", { class: "lock ok" }, ic("check")), h("h3", {}, String(np.props.label)),
        h("div", {}, "Entered. The agent can use it only as ", h("code", {}, String(np.props.handle)), ", through the broker."),
        h("div", { class: "note" }, "The value lives in the core's memory only, for this session."));
      return;
    }
    const input = h("input", { type: "password", autocomplete: "off", placeholder: "Paste or type the secret" }) as HTMLInputElement;
    const submit = () => { if (input.value) { send({ t: "secretSubmit", pane: p.id, value: input.value }); input.value = ""; } };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") submit(); });
    box.replaceChildren(h("div", { class: "lock" }, ic("key")), h("h3", {}, String(np.props.label)),
      np.props.purpose ? h("div", { class: "muted" }, `The agent wants it for: ${np.props.purpose}`) : "",
      input, h("button", { class: "primary", onclick: submit }, "Hand over securely"),
      h("div", { class: "note" }, `The agent will receive only the handle ${np.props.handle}. Never paste secrets into chat.`));
  };
  draw(p);
  return { el: f.el, update: (np) => { f.setVis(np); draw(np); }, destroy: () => {} };
}

// ---- dispatched task (brief → task agent → report) ----

export const statusLabel: Record<string, string> = {
  draft: "draft", running: "running", "needs-input": "has a question", done: "done", failed: "failed", stopped: "stopped",
};

function taskPane(p: Pane): PaneView {
  const f = frame(p);
  const id = String(p.props.task);
  f.body.classList.add("task-host");
  const top = h("div", { class: "task-top" });
  const chat = new ChatPanel("compact");
  const body = h("div", { class: "task-body" });
  f.body.append(top, body);
  let lastStatus = "";
  let saveT: any;
  // Once there's a report, the pane shows it inline; the transcript is a click away.
  let tab: "report" | "transcript" | "" = "";
  let shownReport = "";
  let pickedReport = "";
  const reportBox = h("div", { class: "md task-report pad" });
  const loadReport = async (path: string) => {
    shownReport = path;
    try { const r = await request({ t: "reviewLoad", path }); if (shownReport === path) reportBox.innerHTML = md(r.content); }
    catch (e: any) { reportBox.replaceChildren(h("div", { class: "muted" }, e.message)); }
  };
  const offFile = on("fileChanged", (m) => { if (m.path && m.path === shownReport) loadReport(shownReport); });
  const task = () => desk?.desk.tasks?.find((t) => t.id === id);
  const draw = () => {
    const t = task();
    if (!t) { f.tools.replaceChildren(h("span", { class: "muted" }, "This task was discarded.")); top.replaceChildren(); body.replaceChildren(); return; }
    f.tools.replaceChildren(
      h("span", { class: `status st-${t.status}` }, statusLabel[t.status] || t.status),
      t.status === "draft" ? h("button", { class: "primary", onclick: () => send({ t: "taskStart", task: id }) }, ic("send"), " Start") : "",
      t.status === "draft" ? h("button", { class: "danger", onclick: () => send({ t: "taskDiscard", task: id }) }, "Discard") : "",
      t.status === "running" || t.status === "needs-input" ? h("button", { onclick: () => send({ t: "taskStop", task: id }) }, "Stop") : "",
      t.status === "failed" || t.status === "stopped" ? h("button", { onclick: () => send({ t: "taskStart", task: id }) }, "Run again") : "",
      t.report ? h("button", { title: "Open the report as a document you can comment on", onclick: () => send({ t: "open", kind: "review", props: { path: pickedReport || t.report } }) }, ic("doc"), " Comment on it") : "",
      (t.status === "done" || t.status === "failed") && !t.reviewed ? h("button", { title: "Clear it from Needs you", onclick: () => send({ t: "taskReviewed", task: id }) }, ic("check"), " Reviewed") : "",
    );
    if (t.status === "draft") {
      if (lastStatus === "draft" && top.querySelector("textarea") === document.activeElement) return; // don't clobber typing
      const title = h("input", { class: "task-title", value: t.title }) as HTMLInputElement;
      const brief = h("textarea", { class: "task-brief", spellcheck: "true" }) as HTMLTextAreaElement;
      brief.value = t.brief;
      const save = () => { clearTimeout(saveT); saveT = setTimeout(() => send({ t: "taskUpdate", task: id, title: title.value, brief: brief.value }), 300); };
      title.addEventListener("input", save);
      brief.addEventListener("input", save);
      top.replaceChildren(
        h("div", { class: "muted small" }, "A task agent will work this brief in the background. It can read the project and write a report; it can't change anything. Edit the brief, then Start."),
        title, brief);
      body.replaceChildren();
    } else {
      const plan = (t as any).plan as { text: string; status: string; note?: string }[] | undefined;
      top.replaceChildren(
        plan?.length ? h("ol", { class: "task-plan" }, ...plan.map((s) => h("li", { class: `ps-${s.status}` },
          h("span", { class: "pmark" }, s.status === "done" ? "✓" : s.status === "doing" ? "▸" : s.status === "skipped" ? "–" : "○"),
          h("span", {}, s.text), s.note ? h("span", { class: "muted small" }, ` — ${s.note}`) : ""))) : "",
        h("details", { class: "task-brief-ro" }, h("summary", {}, h("b", {}, t.title), h("span", { class: "muted small" }, " · brief")), h("div", { class: "md", html: md(t.brief) })),
        t.summary ? h("div", { class: `task-summary st-${t.status}` }, t.summary) : "");
      if (!tab) tab = t.report ? "report" : "transcript";
      if (!t.report) tab = "transcript";
      const versions = [...(t.reports || []), t.report].filter(Boolean) as string[];
      if (!pickedReport || !versions.includes(pickedReport)) pickedReport = t.report || "";
      const sw = h("div", { class: "task-tabs" },
        t.report ? h("button", { class: tab === "report" ? "on" : "", onclick: () => { tab = "report"; draw(); } }, ic("doc"), " Report") : "",
        h("button", { class: tab === "transcript" ? "on" : "", onclick: () => { tab = "transcript"; draw(); } }, ic("chat"), " Transcript"),
        tab === "report" && versions.length > 1 ? h("select", { onchange: (e: Event) => { pickedReport = (e.target as HTMLSelectElement).value; draw(); } },
          ...versions.map((v, i) => h("option", { value: v, selected: v === pickedReport }, `version ${i + 1}${v === t.report ? " (latest)" : ""}`))) : "");
      top.append(sw);
      if (tab === "report") {
        if (!body.contains(reportBox)) body.replaceChildren(reportBox);
        if (shownReport !== pickedReport) loadReport(pickedReport);
        lastStatus = t.status;
        return;
      }
      if (!body.contains(chat.el)) body.replaceChildren(chat.el);
      chat.bind("t:" + id, {
        placeholder: t.status === "needs-input" ? "Answer the task agent…" : "Message the task agent (it's working in the background)",
        empty: () => h("div", { class: "msg system chat-empty" }, "Starting the task agent…"),
      });
    }
    lastStatus = t.status;
  };
  const off = on("desk", draw);
  draw();
  return { el: f.el, update: (np) => { f.setVis(np); draw(); }, destroy: () => { off(); offFile(); clearTimeout(saveT); } };
}

export { esc };

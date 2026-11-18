// The Tasks app: work items (read from their sources) and tasks (kept only in
// Nexus) on the usual board. Here: the task panel (one place to act on a
// task), the Tasks app window, the renderers home widgets reuse, and the
// work-item view that a project workspace opens as a pane.

import { ago, desk, h, modal, on, project, promptModal, request, toast } from "./core";
import { ic } from "./icons";

export type WorkTask = {
  id: string; item?: string; project?: string; title: string; status: string; assignee: string; reviewer?: string;
  blockedBy?: string[]; due?: string; note?: string; by: string; why?: string; created: string; updated: string;
};
export type WorkItem = {
  ref: string; project: string; title: string; source: string; sourceState?: string; detail?: string; report?: string;
  state: "in-progress" | "parked" | "not-started"; parkNote?: string; doneWhen?: string[]; open: number; done: number;
  move: string; moveNote?: string; updated: string; brief?: string; unshaped?: boolean;
};
export type Proposal = {
  id: string; batch: string; project: string; item?: string; title: string; why?: string; evidence?: string[];
  assignee?: string; status: string; agent: string; created: string; brief?: string; doneWhen?: string[]; after?: string[];
};

const STATUSES: [string, string][] = [["todo", "To do"], ["doing", "In progress"], ["blocked", "Blocked"], ["review", "In review"], ["done", "Done"]];
const statusName = Object.fromEntries(STATUSES);

export const tasks = (): WorkTask[] => ((desk?.desk as any)?.workTasks || []) as WorkTask[];
export const items = (): WorkItem[] => ((desk as any)?.workItems || []) as WorkItem[];
const itemOf = (ref?: string) => items().find((w) => w.ref === ref);
export const proposals = (): Proposal[] => ((desk?.desk as any)?.proposals || []) as Proposal[];
const onboarded = (pid: string) => !!((desk?.desk as any)?.onboard || {})[pid];
const taskOf = (id: string) => tasks().find((t) => t.id === id);

export function needsMe(t: WorkTask) {
  if (t.status === "todo" || t.status === "doing") return t.assignee === "you";
  if (t.status === "review") return t.reviewer === "you" || (!t.reviewer && t.assignee !== "you");
  return false;
}
function waitingOn(t: WorkTask) {
  if ((t.status === "todo" || t.status === "doing") && t.assignee !== "you" && t.assignee !== "agent") return t.assignee;
  if (t.status === "review" && t.reviewer && t.reviewer !== "you") return t.reviewer;
  return "";
}
const live = (t: WorkTask) => !t.item || !itemOf(t.item) || itemOf(t.item)!.state === "in-progress";

type Hooks = { openWorkspace: (ref: string, task?: string) => void; openProject: (pid: string) => void };
let hooks: Hooks;
export function initWork(h_: Hooks) { hooks = h_; }
export const openWorkspace = (ref: string, task?: string) => hooks.openWorkspace(ref, task);
/** addWorkItem adds a work item in your words (Enter in the rail). */
export const addWorkItem = (pid: string, title: string) => act({ t: "workItemAdd", project: pid, title });

async function act(m: Record<string, any>): Promise<any> {
  try { return await request({ ...m, space: "" }); } catch (e: any) { toast(e.message, "error"); return null; }
}
const update = (id: string, m: Record<string, any>) => act({ t: "workTaskUpdate", id, ...m });

// ---- starting work: the in-progress limit ----

const wipLimit = () => ((desk as any)?.wipLimit ?? 4) as number;

/** startItem brings a work item into progress. Over the limit, it suggests
 *  parking one first: ones waiting on others or blocked come first. */
export function startItem(w: WorkItem) {
  const lim = wipLimit();
  const ip = items().filter((x) => x.state === "in-progress" && x.ref !== w.ref);
  const go = () => act({ t: "workItemSet", item: w.ref, state: "in-progress" });
  if (lim <= 0 || ip.length < lim) { go(); return; }
  const rank = (x: WorkItem) => ({ waiting: 0, blocked: 1, none: 2, ready: 3, agent: 4, you: 5 } as any)[x.move] ?? 6;
  modal((close) => h("div", { class: "modal" },
    h("h3", {}, `You have ${ip.length} work items in progress`),
    h("p", {}, `Your limit is ${lim}. Park one before starting `, h("b", {}, w.title), "? Parked work keeps its tasks; they're just out of your lists until you bring it back."),
    ...ip.slice().sort((a, b) => rank(a) - rank(b)).map((x) => h("div", { class: "crow" },
      h("span", { class: "crow-t" }, x.title), projTag(x.project), moveChip(x),
      h("button", { onclick: () => { act({ t: "workItemSet", item: x.ref, state: "parked", parkNote: x.move === "waiting" ? (x.moveNote || "") : "" }); go(); close(); } }, "Park this and start"))),
    h("div", { class: "row" }, h("button", { class: "link small", onclick: () => { close(); openTasksApp("items"); } }, "Change the limit"),
      h("span", { class: "grow" }), h("button", { onclick: close }, "Cancel"), h("button", { class: "primary", onclick: () => { go(); close(); } }, "Start anyway"))));
}

function limitSetting(): HTMLElement {
  const inp = h("input", { type: "number", min: "0", max: "20", value: String(wipLimit()), style: { width: "56px" },
    onchange: (e: Event) => act({ t: "workLimit", limit: Number((e.target as HTMLInputElement).value) }) });
  return h("div", { class: "muted small limit" }, "Suggest parking one when more than ", inp, " are in progress (0 = never).");
}

// ---- small pieces ----

const projTag = (pid?: string) => pid ? h("span", { class: "tag" }, project(pid)?.name || pid) : "";
export function whoChip(t: WorkTask): HTMLElement {
  if (t.status === "done") return h("span", { class: "wchip done" }, "Done");
  if (needsMe(t) && live(t)) return h("span", { class: "wchip you" }, t.status === "review" ? "Your review" : "Yours");
  if (t.assignee === "agent" && t.status === "doing") return h("span", { class: "wchip agent" }, "Agent working");
  const w = waitingOn(t);
  if (w) return h("span", { class: "wchip wait" }, `Waiting: ${w}`);
  if (t.status === "blocked") return h("span", { class: "wchip blocked" }, "Blocked");
  return h("span", { class: "wchip" }, statusName[t.status] || t.status);
}
export const moveChip = (w: WorkItem) => h("span", { class: `wchip ${w.move === "waiting" ? "wait" : w.move}` }, w.moveNote || w.move);

function blockedText(t: WorkTask): string {
  return (t.blockedBy || []).map(taskOf).filter((x) => x && x.status !== "done").map((x) => x!.title).join(", ");
}

export function taskRow(t: WorkTask, opts: { showItem?: boolean } = {}): HTMLElement {
  const w = itemOf(t.item);
  const sub = t.status === "blocked" && blockedText(t) ? `after ${blockedText(t)}` : opts.showItem !== false ? (w ? w.title : "todo") : (t.note || "");
  return h("button", { class: "trow", onclick: () => openTask(t.id) },
    h("span", { class: "trow-main" }, h("span", { class: "trow-t" }, t.title), h("span", { class: "muted small" }, sub)),
    t.due ? h("span", { class: `due${t.due <= today() ? " late" : ""}` }, dueLabel(t.due)) : "",
    projTag(t.project), whoChip(t));
}
const today = () => new Date().toISOString().slice(0, 10);
function dueLabel(d: string) {
  const days = Math.round((new Date(d + "T12:00").getTime() - new Date(today() + "T12:00").getTime()) / 864e5);
  return days < 0 ? `${-days}d late` : days === 0 ? "today" : days === 1 ? "tomorrow" : new Date(d + "T12:00").toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
}

async function addTask(item?: WorkItem, pid?: string) {
  const title = await promptModal(item ? `New task for ${item.title}` : "New todo", item ? "The next concrete action, e.g. Review MR !41" : "e.g. Book the conference hotel");
  if (!title) return;
  await act({ t: "workTaskAdd", title, item: item?.ref, project: item ? undefined : pid });
}

// ---- setting up and shaping (the project agent does the talking) ----

/** startSetup asks the project agent to look through the project for work. */
export async function startSetup(pid: string) {
  if (await act({ t: "onboardStart", project: pid }) === null) return;
  hooks.openProject(pid);
}

/** shape asks the project agent to ask a few questions, then propose tasks. */
export async function shape(w: WorkItem) {
  if (await act({ t: "shapeStart", item: w.ref }) === null) return;
  hooks.openWorkspace(w.ref);
}
const shapeBtn = (w: WorkItem, primary = false) => h("button", { class: primary ? "primary" : "", title: "The project agent asks a few questions (click an answer), then proposes the next tasks", onclick: () => shape(w) }, ic("agent"), " Shape");
const shapeChip = () => h("span", { class: "wchip shape", title: "In progress, but nobody has said what done looks like, or there's no task yet" }, "Needs shaping");

/** proposalChecklist is one batch of suggestions to tick. Ticked ones are
 *  added; the rest are dismissed and won't be suggested again. */
export function proposalChecklist(batch: string, o: { onDone?: () => void; compact?: boolean } = {}): HTMLElement {
  const el = h("div", { class: "proposals" });
  const picked = new Set<string>();
  let init = false;
  const draw = () => {
    const all = proposals().filter((p) => p.batch === batch);
    const open = all.filter((p) => p.status === "open");
    if (!init) { open.forEach((p) => picked.add(p.id)); init = true; }
    if (!all.length) { el.replaceChildren(); return; }
    const forItem = all[0].item ? itemOf(all[0].item) : undefined;
    if (!open.length) {
      const n = all.filter((p) => p.status === "accepted").length;
      const where = forItem ? "on the board" : `in ${project(all[0].project)?.name || all[0].project}'s work items, under Not started`;
      el.replaceChildren(h("div", { class: "muted small" }, ic("check"), ` Reviewed: ${n} added ${where}${all.length - n ? `, ${all.length - n} dismissed` : ""}.`,
        !forItem && n ? h("button", { class: "link small", onclick: () => openTasksApp("items") }, " Show them") : ""));
      return;
    }
    const go = async (start: boolean) => {
      const accept = open.filter((p) => picked.has(p.id)).map((p) => p.id);
      const dismiss = open.filter((p) => !picked.has(p.id)).map((p) => p.id);
      if (await act({ t: "proposalsResolve", accept, dismiss, start }) !== null) o.onDone?.();
    };
    el.replaceChildren(
      h("div", { class: "prop-head" }, h("b", {}, forItem ? `Next tasks for ${forItem.title}` : `Suggested work for ${project(all[0].project)?.name || all[0].project}`),
        h("span", { class: "muted small" }, ` · from the ${all[0].agent.startsWith("p:") ? "project agent" : "agent"}. Ticked ones go on your list${forItem ? "" : " (they don't start until you start them)"}; unticked ones are dismissed.`)),
      ...open.map((p) => {
        const cb = h("input", { type: "checkbox", checked: picked.has(p.id), onchange: (e: Event) => { (e.target as HTMLInputElement).checked ? picked.add(p.id) : picked.delete(p.id); } });
        return h("label", { class: "prop-row" }, cb,
          h("span", { class: "prop-main" }, h("span", { class: "prop-t" }, p.title, p.item && p.assignee ? h("span", { class: "tag" }, p.assignee === "you" ? "You" : p.assignee === "agent" ? "Agent" : p.assignee) : ""),
            p.brief ? h("span", { class: "small" }, p.brief) : p.why && !o.compact ? h("span", { class: "muted small" }, p.why) : "",
            (p.doneWhen || []).length ? h("span", { class: "muted small" }, h("b", {}, "Done when: "), (p.doneWhen || []).join(" · ")) : "",
            (p.after || []).length ? h("span", { class: "muted small" }, `after ${(p.after || []).join(", ")}`) : "",
            p.brief && p.why && !o.compact ? h("span", { class: "muted small" }, p.why) : "",
            (p.evidence || []).length && !o.compact ? h("span", { class: "prop-ev" }, ...(p.evidence || []).slice(0, 3).map((e) => h("code", {}, e))) : ""));
      }),
      h("div", { class: "row" },
        h("button", { class: "primary", onclick: () => go(false) }, forItem ? "Add selected" : "Add to my list"),
        forItem ? "" : h("button", { title: "Add them and put them in progress", onclick: () => go(true) }, "Add and start"),
        h("span", { class: "grow-r" }),
        h("button", { class: "link small", onclick: () => { picked.clear(); go(false); } }, "None of these")));
  };
  draw();
  const off = on("desk", () => { if (!el.isConnected && init) { off(); return; } draw(); });
  return el;
}

/** openProposals shows a batch's checklist in a dialog (from Needs you). */
export function openProposals(batch: string) {
  modal((close) => h("div", { class: "modal wide" }, proposalChecklist(batch, { onDone: close }), h("div", { class: "row" }, h("span", { class: "grow" }), h("button", { onclick: close }, "Close"))));
}

/** setupList: projects with no work items yet, each with a Set up button. */
function setupList(pid?: string): HTMLElement[] {
  const ps = (desk?.projects || []).filter((p) => (!pid || p.id === pid) && !items().some((w) => w.project === p.id) && !proposals().some((q) => q.project === p.id && q.status === "open"));
  if (!ps.length) return [];
  return [h("div", { class: "hint" }, "No work items yet"),
    ...ps.map((p) => h("div", { class: "crow" }, h("span", { class: "crow-t" }, p.name),
      h("span", { class: "muted small crow-n" }, onboarded(p.id) ? "set-up conversation started" : "the project agent looks through it for work"),
      h("button", { onclick: () => startSetup(p.id) }, onboarded(p.id) ? "Open" : "Set up")))];
}

function pendingChecklists(pid?: string, ref?: string): HTMLElement[] {
  const batches = [...new Set(proposals().filter((p) => p.status === "open" && (!pid || p.project === pid) && (!ref || p.item === ref)).map((p) => p.batch))];
  return batches.map((b) => h("div", { class: "card" }, proposalChecklist(b)));
}

// ---- the task panel (drawer) ----

let drawerTask: string | null = null;
export function openTask(id: string) { drawerTask = id; drawDrawer(); }
function closeDrawer() { drawerTask = null; drawDrawer(); }

function drawDrawer() {
  const root = document.getElementById("work-drawer")!;
  const t = drawerTask ? taskOf(drawerTask) : null;
  if (!t) { root.replaceChildren(); return; }
  if (root.contains(document.activeElement) && /^(INPUT|TEXTAREA|SELECT)$/.test((document.activeElement as HTMLElement).tagName)) return;
  root.replaceChildren(h("div", { class: "drawer-bg", onclick: closeDrawer }), h("aside", { class: "drawer task-drawer" }, taskPanel(t, { onClose: closeDrawer })));
}

export function taskPanel(t: WorkTask, o: { onClose?: () => void; docked?: boolean } = {}): HTMLElement {
  const w = itemOf(t.item);
  const title = h("input", { class: "tp-title", value: t.title, onchange: (e: Event) => update(t.id, { title: (e.target as HTMLInputElement).value }) }) as HTMLInputElement;
  const seg = h("div", { class: "seg wide" }, ...STATUSES.map(([s, l]) => h("button", { class: t.status === s ? "on" : "", onclick: () => update(t.id, { status: s }) }, l)));
  const other = h("input", { placeholder: "Someone else…", value: t.assignee !== "you" && t.assignee !== "agent" ? t.assignee : "", style: { width: "140px" },
    onchange: (e: Event) => { const v = (e.target as HTMLInputElement).value.trim(); if (v) update(t.id, { assignee: v }); } });
  const who = h("div", { class: "row" }, h("span", { class: "seg" },
    h("button", { class: t.assignee === "you" ? "on" : "", onclick: () => update(t.id, { assignee: "you" }) }, "You"),
    h("button", { class: t.assignee === "agent" ? "on" : "", onclick: () => update(t.id, { assignee: "agent" }) }, "Agent")), other);
  const siblings = tasks().filter((x) => x.id !== t.id && x.status !== "done" && x.item === t.item && !(t.blockedBy || []).includes(x.id));
  const waits = h("div", { class: "row" },
    ...(t.blockedBy || []).map((id) => { const b = taskOf(id); return h("span", { class: "tag" }, b ? `${b.title}${b.status === "done" ? " ✓" : ""}` : id, " ",
      h("button", { class: "link", title: "Doesn't wait on this", onclick: () => update(t.id, { blockedBy: (t.blockedBy || []).filter((x) => x !== id) }) }, "✕")); }),
    siblings.length ? h("select", { onchange: (e: Event) => { const v = (e.target as HTMLSelectElement).value; if (v) update(t.id, { blockedBy: [...(t.blockedBy || []), v], status: t.status === "done" ? "done" : "blocked" }); } },
      h("option", { value: "" }, "+ waits on…"), ...siblings.map((x) => h("option", { value: x.id }, x.title))) : "");
  const due = h("input", { type: "date", value: t.due || "", onchange: (e: Event) => update(t.id, { due: (e.target as HTMLInputElement).value }) });
  const note = h("textarea", { rows: 2, placeholder: "A note (only here)", onchange: (e: Event) => update(t.id, { note: (e.target as HTMLTextAreaElement).value }) }, t.note || "");
  const quick: HTMLElement[] = [];
  if (t.status !== "done") quick.push(h("button", { class: "primary", onclick: () => update(t.id, { status: "done" }) }, ic("check"), " Done"));
  if (t.assignee === "agent" && t.status !== "done") {
    quick.push(h("button", { title: "You do it from here; the agent stops", onclick: () => update(t.id, { assignee: "you", status: "doing" }) }, "Take it"));
    if (t.project) quick.push(h("button", { title: "Draft a brief for a background task agent", onclick: () => { act({ t: "taskNew", project: t.project, title: t.title, from: "work:" + t.id }); toast("Drafted a background task; it's in the project's Tasks to edit and start"); } }, ic("agent"), " Draft for a task agent"));
  }
  if (t.assignee === "you" && t.status !== "done") quick.push(h("button", { title: "Give it to the agent", onclick: () => update(t.id, { assignee: "agent" }) }, "Hand to the agent"));
  if (t.status === "review") quick.push(h("button", { onclick: () => update(t.id, { status: "doing" }) }, "Needs more work"));
  return h("div", { class: `tpanel${o.docked ? " docked" : ""}` },
    h("div", { class: "tp-top" }, projTag(t.project), w ? h("span", { class: "tag" }, w.title) : h("span", { class: "tag" }, "todo"), whoChip(t),
      h("span", { class: "grow-r" }),
      w && !o.docked ? h("button", { class: "primary", title: "Dig in: the project's workspace with this task and its work item", onclick: () => { o.onClose?.(); hooks.openWorkspace(w.ref, t.id); } }, "Open in workspace ↗") : "",
      o.onClose ? h("button", { class: "ebtn", title: o.docked ? "Put it away" : "Close", onclick: o.onClose }, "✕") : ""),
    title,
    t.status === "blocked" && blockedText(t) ? h("div", { class: "muted small" }, `Waits on: ${blockedText(t)}. It moves to To do when that's done.`) : "",
    h("div", { class: "row quick" }, ...quick),
    h("dl", { class: "spec" },
      h("dt", {}, "Status"), h("dd", {}, seg),
      h("dt", {}, "Who"), h("dd", {}, who),
      t.status === "review" ? h("dt", {}, "Reviewer") : "", t.status === "review" ? h("dd", {}, h("input", { value: t.reviewer || "you", onchange: (e: Event) => update(t.id, { reviewer: (e.target as HTMLInputElement).value }) })) : "",
      h("dt", {}, "Waits on"), h("dd", {}, waits),
      h("dt", {}, "Due"), h("dd", {}, due),
      h("dt", {}, "Note"), h("dd", {}, note)),
    h("div", { class: "muted small tp-foot" },
      `Added by ${t.by}${t.why ? ` (${t.why})` : ""} · ${ago(t.created)}. Lives only in Nexus${w ? `; ${w.source} isn't changed` : ""}. `,
      h("button", { class: "link danger", onclick: () => { act({ t: "workTaskRemove", id: t.id }); o.onClose?.(); } }, "Delete")));
}

// ---- renderers (Tasks app and home widgets) ----

export function needsList(pid?: string): HTMLElement[] {
  const ts = tasks().filter((t) => needsMe(t) && live(t) && (!pid || t.project === pid)).sort((a, b) => (a.due || "9").localeCompare(b.due || "9"));
  return ts.length ? ts.map((t) => taskRow(t)) : [h("div", { class: "empty-note" }, "No tasks waiting on you.")];
}

export function board(pid?: string, opts: { hideDone?: boolean } = {}): HTMLElement {
  const ts = tasks().filter((t) => live(t) && (!pid || t.project === pid));
  const recentDone = (t: WorkTask) => Date.now() - new Date(t.updated).getTime() < 7 * 864e5;
  return h("div", { class: "tboard" }, ...STATUSES.filter(([s]) => !(opts.hideDone && s === "done")).map(([s, l]) => {
    const col = ts.filter((t) => t.status === s && (s !== "done" || recentDone(t)));
    return h("div", { class: "tcol" }, h("div", { class: "tcol-h" }, l, h("span", { class: "muted small" }, ` ${col.length}`)),
      ...col.map((t) => h("button", { class: "tcard", onclick: () => openTask(t.id) }, h("span", {}, t.title),
        h("span", { class: "tcard-m" }, itemOf(t.item) ? h("span", { class: "muted small" }, itemOf(t.item)!.title) : h("span", { class: "muted small" }, "todo"), whoChip(t)))));
  }));
}

export function boardList(pid?: string): HTMLElement[] {
  const ts = tasks().filter((t) => live(t) && t.status !== "done" && (!pid || t.project === pid));
  return ts.length ? ts.map((t) => taskRow(t)) : [h("div", { class: "empty-note" }, "No open tasks.")];
}

export function workItemCards(pid?: string, shape = "cards", o: { noMore?: boolean } = {}): HTMLElement[] {
  const ws = items().filter((w) => w.state === "in-progress" && (!pid || w.project === pid));
  // What isn't started is still yours: say how much there is, one click away.
  const later = items().filter((w) => w.state === "not-started" && (!pid || w.project === pid)).length;
  const more = later && !o.noMore ? h("button", { class: "link small wi-more", onclick: () => openTasksApp("items") }, `${later} more not started →`) : "";
  if (!ws.length) return [h("div", { class: "empty-note" }, later ? `Nothing in progress. ${later} work item${later === 1 ? "" : "s"} waiting to start: ` : "No work items yet. Open a project and press Set up: its agent looks through it for work.", more)];
  if (shape === "list") return [...ws.map((w) => h("button", { class: "crow", onclick: () => hooks.openWorkspace(w.ref) },
    h("span", { class: "crow-t" }, w.title), projTag(w.project), moveChip(w))), ...(more ? [more as HTMLElement] : [])];
  return [h("div", { class: "wi-grid" }, ...ws.map(workItemCard)), ...(more ? [more as HTMLElement] : [])];
}

function nextTask(w: WorkItem) {
  const ts = tasks().filter((t) => t.item === w.ref && t.status !== "done");
  return ts.find(needsMe) || ts.find((t) => t.assignee === "agent" && t.status === "doing") || ts.find((t) => !!waitingOn(t)) || ts[0];
}

function workItemCard(w: WorkItem): HTMLElement {
  const n = nextTask(w);
  const total = w.open + w.done;
  return h("div", { class: "card wi-card" },
    h("div", { class: "card-top" }, projTag(w.project), h("span", { class: "muted small" }, w.source), h("span", { class: "grow-r" }), moveChip(w)),
    h("b", { class: "wi-title", onclick: () => hooks.openWorkspace(w.ref) }, w.title),
    w.brief ? h("div", { class: "muted small wi-brief" }, w.brief) : "",
    w.unshaped ? h("div", { class: "row" }, shapeChip(), h("span", { class: "muted small" }, !(w.doneWhen || []).length ? "What does done look like?" : "No tasks yet.")) : "",
    total ? h("div", { class: "wi-prog" }, h("div", { class: "bar" }, h("span", { style: { width: `${Math.round((w.done / total) * 100)}%` } })), h("span", { class: "muted small" }, `${w.done} of ${total} tasks done`)) : "",
    n ? h("button", { class: "wi-next", onclick: () => openTask(n.id) }, h("span", { class: "muted small" }, "Next"), whoChip(n), " ", n.title)
      : w.unshaped ? "" : h("div", { class: "muted small" }, "No next action yet."),
    h("div", { class: "row" },
      w.unshaped ? shapeBtn(w, true) : "",
      h("button", { onclick: () => hooks.openWorkspace(w.ref) }, "Open workspace"),
      h("button", { onclick: () => addTask(w) }, ic("plus"), " Task"),
      h("button", { title: "Hide it and its tasks until you bring it back", onclick: async () => { const note = await promptModal(`Park ${w.title}`, "Until… (optional), e.g. IT grants access"); if (note !== null) act({ t: "workItemSet", item: w.ref, state: "parked", parkNote: note || "" }); } }, "Park")));
}

export function notInProgress(pid?: string): HTMLElement[] {
  const parked = items().filter((w) => w.state === "parked" && (!pid || w.project === pid));
  const later = items().filter((w) => w.state === "not-started" && (!pid || w.project === pid));
  // Each one says what it is and when it's done, without opening it.
  const row = (w: WorkItem, btn: HTMLElement) => h("div", { class: "crow wi-row" },
    h("span", { class: "wi-row-main" },
      h("button", { class: "link crow-t", title: "Open its workspace", onclick: () => hooks.openWorkspace(w.ref) }, w.title),
      w.brief ? h("span", { class: "small" }, w.brief) : w.detail ? h("span", { class: "muted small" }, w.detail) : "",
      (w.doneWhen || []).length ? h("span", { class: "muted small" }, h("b", {}, "Done when: "), (w.doneWhen || []).join(" · ")) : h("span", { class: "muted small" }, "No done-when yet: Shape it when you start it.")),
    h("span", { class: "muted small crow-n" }, projTag(w.project), w.parkNote ? `parked ${w.parkNote}` : ""), btn);
  return [
    parked.length ? h("div", { class: "hint" }, "Parked") : null,
    ...parked.map((w) => row(w, h("button", { onclick: () => startItem(w) }, "Bring back"))),
    later.length ? h("div", { class: "hint" }, "Not started (from your backlogs and lists)") : null,
    ...later.map((w) => row(w, h("button", { onclick: () => startItem(w) }, "Start"))),
    !parked.length && !later.length ? h("div", { class: "empty-note" }, "Nothing waiting to start.") : null,
  ].filter(Boolean) as HTMLElement[];
}

export function todos(withAdd = true): HTMLElement[] {
  const ts = tasks().filter((t) => !t.item && t.status !== "done");
  const form = h("form", { class: "todo-add" });
  const inp = h("input", { placeholder: "Add a todo, no project needed" }) as HTMLInputElement;
  const due = h("input", { type: "date" }) as HTMLInputElement;
  form.append(inp, due, h("button", { type: "submit" }, "Add"));
  form.addEventListener("submit", (e) => { e.preventDefault(); if (inp.value.trim()) { act({ t: "workTaskAdd", title: inp.value.trim(), due: due.value || undefined }); inp.value = ""; } });
  return [...(ts.length ? ts.map((t) => h("div", { class: "crow" },
    h("input", { type: "checkbox", title: "Done", onclick: () => update(t.id, { status: "done" }) }),
    h("button", { class: "link crow-t", onclick: () => openTask(t.id) }, t.title),
    t.due ? h("span", { class: `due${t.due <= today() ? " late" : ""}` }, dueLabel(t.due)) : "", whoChip(t))) : [h("div", { class: "empty-note" }, "No todos.")]),
    ...(withAdd ? [form] : [])];
}

// ---- the Tasks app window ----

let appOpen = false;
let tab = "needs";
let scope = "";
export function openTasksApp(t?: string) { appOpen = true; if (t) tab = t; drawApp(); }
function closeApp() { appOpen = false; drawApp(); }

function drawApp() {
  const root = document.getElementById("work-app")!;
  if (!appOpen) { root.replaceChildren(); return; }
  if (root.contains(document.activeElement) && /^(INPUT|TEXTAREA|SELECT)$/.test((document.activeElement as HTMLElement).tagName)) return;
  const pid = scope || undefined;
  const tabs: [string, string][] = [["needs", "Needs you"], ["board", "Board"], ["items", "Work items"], ["todos", "Todos"]];
  const nNeeds = tasks().filter((t) => needsMe(t) && live(t) && (!pid || t.project === pid)).length;
  let body: (HTMLElement | null)[] = [];
  if (tab === "needs") body = needsList(pid);
  if (tab === "board") body = [h("div", { class: "row" }, h("button", { onclick: () => addTask(undefined, pid) }, ic("plus"), " Todo"), h("span", { class: "muted small" }, "Cards move when work happens; open one to change it.")), board(pid)];
  if (tab === "items") body = [...pendingChecklists(pid), h("h4", {}, `In progress (${items().filter((w) => w.state === "in-progress").length})`), limitSetting(), ...workItemCards(pid, "cards", { noMore: true }), h("h4", {}, "Not in progress"), ...notInProgress(pid), ...setupList(pid),
    h("div", { class: "row" }, h("button", { onclick: async () => { const p = pid || (desk?.projects || [])[0]?.id; if (!p) return; const t = await promptModal(`New work item in ${project(p)?.name}`, "What you want done, e.g. Add CSV export"); if (t) act({ t: "workItemAdd", project: p, title: t }); } }, ic("plus"), " Work item"),
      pid ? h("button", { title: "The project agent looks through it again for work it hasn't suggested before", onclick: () => startSetup(pid) }, "Find more work") : "")];
  if (tab === "todos") body = todos();
  root.replaceChildren(h("div", { class: "work-app" },
    h("div", { class: "wa-head" }, h("b", {}, ic("board"), " Tasks"),
      h("nav", { class: "seg" }, ...tabs.map(([k, l]) => h("button", { class: tab === k ? "on" : "", onclick: () => { tab = k; drawApp(); } }, l, k === "needs" && nNeeds ? h("span", { class: "count" }, String(nNeeds)) : ""))),
      h("select", { onchange: (e: Event) => { scope = (e.target as HTMLSelectElement).value; drawApp(); } },
        h("option", { value: "" }, "Every project"), ...(desk?.projects || []).map((p) => h("option", { value: p.id, selected: scope === p.id }, p.name))),
      h("span", { class: "grow-r muted small" }, "The default views. Each is also a widget for your home screen."),
      h("button", { class: "ebtn", title: "Close", onclick: closeApp }, "✕")),
    h("div", { class: "wa-body" }, ...(body.filter(Boolean) as HTMLElement[]))));
}

// ---- the work item view (a workspace pane) ----

export function workItemView(ref: string, focus?: string) {
  const el = h("div", { class: "wi-view" });
  let focused = focus || "";
  const draw = () => {
    if (el.contains(document.activeElement) && /^(INPUT|TEXTAREA|SELECT)$/.test((document.activeElement as HTMLElement).tagName)) return;
    const w = itemOf(ref);
    if (!w) { el.replaceChildren(h("div", { class: "muted pad" }, `Work item ${ref} isn't listed by its source any more.`)); return; }
    const ts = tasks().filter((t) => t.item === ref);
    const groups: [string, WorkTask[]][] = [
      ["Yours", ts.filter((t) => t.status !== "done" && needsMe(t))],
      ["Agent working", ts.filter((t) => t.status === "doing" && t.assignee === "agent")],
      ["Waiting on others", ts.filter((t) => t.status !== "done" && !needsMe(t) && !!waitingOn(t))],
      ["Blocked", ts.filter((t) => t.status === "blocked")],
      ["Not started", ts.filter((t) => t.status === "todo" && !needsMe(t) && !waitingOn(t))],
      ["Done", ts.filter((t) => t.status === "done")],
    ];
    const ft = focused ? taskOf(focused) : undefined;
    el.replaceChildren(
      h("div", { class: "wi-head" },
        h("div", {}, h("h3", {}, w.title), h("div", { class: "muted small" }, `${w.source}${w.sourceState ? ` · ${w.sourceState}` : ""} · Nexus reads it and never changes it`)),
        h("span", { class: "grow-r" }), w.unshaped ? shapeChip() : "", moveChip(w),
        w.unshaped && ts.length ? shapeBtn(w) : "",
        w.report ? h("button", { onclick: () => hooks.openWorkspace("open-report:" + w.report) }, ic("doc"), " Report") : "",
        w.state === "in-progress" ? h("button", { onclick: async () => { const note = await promptModal(`Park ${w.title}`, "Until… (optional)"); if (note !== null) act({ t: "workItemSet", item: w.ref, state: "parked", parkNote: note || "" }); } }, "Park")
          : h("button", { class: "primary", onclick: () => startItem(w) }, w.state === "parked" ? "Bring back" : "Start")),
      w.state === "parked" ? h("div", { class: "banner" }, `Parked${w.parkNote ? ` ${w.parkNote}` : ""}. Its tasks are out of your lists until you bring it back.`) : "",
      ft ? taskPanel(ft, { docked: true, onClose: () => { focused = ""; draw(); } }) : "",
      h("div", { class: "wi-cols" },
        h("div", {},
          h("div", { class: "row" }, h("b", {}, "What's left"), h("span", { class: "grow-r" }), h("button", { onclick: () => addTask(w) }, ic("plus"), " Task")),
          ...groups.filter(([, l]) => l.length).flatMap(([g, l]) => [h("div", { class: "hint" }, `${g} · ${l.length}`), ...l.map((t) => h("button", { class: `trow${t.id === focused ? " on" : ""}`, onclick: () => { focused = t.id; draw(); } },
            h("span", { class: "trow-main" }, h("span", { class: "trow-t" }, t.title), t.status === "blocked" && blockedText(t) ? h("span", { class: "muted small" }, `after ${blockedText(t)}`) : ""), whoChip(t)))]),
          !ts.length ? h("div", { class: "empty-note" }, "No tasks yet. Add the next concrete action, or ", shapeBtn(w, true), " it: the agent asks a few questions, then proposes tasks.") : "",
          ...pendingChecklists(undefined, w.ref)),
        h("div", {},
          h("b", {}, "Brief"),
          w.brief ? h("p", { class: "small" }, w.brief) : h("div", { class: "muted small" }, "Not set yet."),
          h("button", { class: "link small", onclick: async () => { const v = await promptModal("Brief", "What it is, in a line or two: scope, constraints, who's involved", w.brief || ""); if (v !== null) act({ t: "workItemSet", item: w.ref, brief: v }); } }, "Edit"),
          h("b", {}, "Done when"),
          (w.doneWhen || []).length ? h("ul", { class: "dw" }, ...(w.doneWhen || []).map((d) => h("li", {}, d))) : h("div", { class: "muted small" }, "Not set yet."),
          h("button", { class: "link small", onclick: async () => { const v = await promptModal("Done when", "Checks separated by ; e.g. Merged; Deployed; Security signed off", (w.doneWhen || []).join("; ")); if (v !== null) act({ t: "workItemSet", item: w.ref, doneWhen: v.split(";").map((x) => x.trim()).filter(Boolean) }); } }, "Edit"),
          w.detail ? h("p", { class: "muted small" }, w.detail) : "")));
  };
  draw();
  const off = on("desk", draw);
  return { el, focus: (id: string) => { focused = id; draw(); }, destroy: off };
}

on("desk", () => { drawDrawer(); drawApp(); });
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (drawerTask) closeDrawer(); else if (appOpen) closeApp();
});

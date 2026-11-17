// Apps: the capabilities you and the agents share (tasks, wiki, docs,
// sheets, terminal, browser, the log, and plugins). This launcher is how you
// open them by hand. Agents use the same apps through tools, with your
// permission, and can open them for you (a project agent's pane_open).

import { app, desk, h, project, promptModal, send, toast, view } from "./core";
import { ic } from "./icons";

export type AppsHooks = {
  openIn: (pid: string, kind: string, props?: any, title?: string) => void;
  openTasks: () => void;
};

type AppDef = {
  id: string; name: string; icon: string; desc: string;
  project?: boolean;                 // opens inside a project
  open: (pid: string) => void;
  soon?: string;
};

let hooks: AppsHooks;
export function initApps(h_: AppsHooks) { hooks = h_; }

const apps = (): AppDef[] => [
  { id: "tasks", name: "Tasks", icon: "board", desc: "Work items, tasks and boards across your projects", open: () => hooks.openTasks() },
  { id: "wiki", name: "Wiki", icon: "doc", desc: "Each project's shared memory, read by every agent there", project: true,
    open: (pid) => {
      const pages = desk?.wikis?.[pid]?.pages || [];
      if (!pages.length) { send({ t: "wikiBuild", project: pid }); toast("No wiki yet: the project agent is building one"); hooks.openIn(pid, "log"); return; }
      hooks.openIn(pid, "review", { path: `wiki/${pid}/index.md` }, "Wiki");
    } },
  { id: "docs", name: "Docs", icon: "files", desc: "Browse a project's files; open a document to read and comment on it", project: true,
    open: (pid) => hooks.openIn(pid, "files", { path: pid }) },
  { id: "sheets", name: "Sheets", icon: "sheet", desc: "Small CSV sheets you and the agent both edit", project: true,
    open: async (pid) => {
      const name = await promptModal("New sheet", "Name, e.g. shopping-list");
      if (!name) return;
      hooks.openIn(pid, "log");
      send({ t: "sheetNew", space: project(pid)?.space, path: `desk/${pid}/${name.replace(/[^\w.-]+/g, "-")}` });
    } },
  { id: "terminal", name: "Terminal", icon: "term", desc: "Your shell in the project folder. Agents can propose commands; they can't read it", project: true,
    open: (pid) => hooks.openIn(pid, "terminal") },
  { id: "browser", name: "Browser", icon: "globe", desc: "Follow along with links the agent finds", project: true,
    open: async (pid) => { const url = await promptModal("Open a page", "https://…"); if (url) hooks.openIn(pid, "browser", { url }); } },
  { id: "log", name: "Action log", icon: "log", desc: "Everything you and the agents did, and what was refused", project: true,
    open: (pid) => hooks.openIn(pid, "log") },
];

/** The top-bar button and its popover. */
export function appsButton(): HTMLElement {
  const wrap = h("div", { class: "menu apps-menu" });
  const btn = h("button", { title: "Apps: what you and the agents can use" }, ic("apps"), " Apps");
  let pop: HTMLElement | null = null;
  let choosing: AppDef | null = null;
  const close = () => { pop?.remove(); pop = null; choosing = null; document.removeEventListener("mousedown", outside, true); };
  const outside = (e: Event) => { if (!wrap.contains(e.target as any) && !(e.target as HTMLElement).closest?.(".backdrop")) close(); };
  const launch = (a: AppDef) => {
    if (a.soon) { toast(a.soon, "warn"); return; }
    if (!a.project) { close(); a.open(""); return; }
    const pid = view.kind === "project" ? view.id : "";
    if (pid) { close(); a.open(pid); return; }
    if ((desk?.projects || []).length === 1) { close(); a.open(desk!.projects![0].id); return; }
    choosing = a; draw();
  };
  const draw = () => {
    if (!pop) return;
    if (choosing) {
      const a = choosing;
      pop.replaceChildren(h("div", { class: "apps-head" }, h("button", { class: "link", onclick: () => { choosing = null; draw(); } }, "← Apps"), h("b", {}, `${a.name}: which project?`)),
        ...(desk?.projects || []).map((p) => h("button", { class: "app-proj", onclick: () => { close(); a.open(p.id); } }, ic("folder"), " ", p.name)));
      return;
    }
    const plugins = app?.plugins || [];
    pop.replaceChildren(
      h("div", { class: "apps-grid" }, ...apps().map((a) => h("button", { class: `app-tile${a.soon ? " soon" : ""}`, title: a.desc, onclick: () => launch(a) },
        h("span", { class: "app-ic" }, ic(a.icon)), h("b", {}, a.name), h("span", { class: "muted small" }, a.soon ? "coming next" : a.desc)))),
      plugins.length ? h("div", { class: "apps-sec" }, h("div", { class: "hint" }, "From plugins"),
        ...plugins.map((p) => h("div", { class: "app-plugin" }, ic("plugin"), h("span", { class: "grow" }, h("b", {}, p.name), p.description ? h("div", { class: "muted small" }, p.description) : ""),
          h("button", { title: "Put it on your home screen", onclick: () => { send({ t: "homeAdd", kind: "plugin", plugin: p.id, space: "" }); toast(`${p.name} is on your home screen`); close(); } }, "Add to home"),
          view.kind === "project" ? h("button", { onclick: () => { close(); hooks.openIn(view.kind === "project" ? view.id : "", "plugin", { plugin: p.id }, p.name); } }, "Open here") : ""))) : "",
      h("p", { class: "muted small apps-foot" }, "Apps are what you and the agents share. Agents use them through tools, with your permission, and can open them for you. Plugins add more."));
  };
  btn.addEventListener("click", () => {
    if (pop) return close();
    pop = h("div", { class: "menu-list apps-pop" });
    wrap.append(pop);
    draw();
    document.addEventListener("mousedown", outside, true);
  });
  wrap.append(btn);
  return wrap;
}

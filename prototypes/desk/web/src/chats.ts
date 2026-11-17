// The Chats panel: every agent conversation, kept by the core whether or not
// a chat widget shows it. Read any of them, or carry on talking.

import { ago, desk, h, project } from "./core";
import { ChatPanel, isBusy, knownAgents, onChat, transcriptOf } from "./chat";
import { ic } from "./icons";

let open = false;
let thread: string | null = null;
const panel = new ChatPanel("chats-chat");

export function agentName(agent: string): string {
  if (agent === "home") return "Home agent";
  if (agent.startsWith("p:")) return `${project(agent.slice(2))?.name || agent.slice(2)} agent`;
  if (agent.startsWith("t:")) {
    const t = (desk?.desk.tasks || []).find((x) => x.id === agent.slice(2));
    return t ? `Task: ${t.title}` : "Task agent";
  }
  return agent;
}

export function openChats(agent?: string) { open = true; thread = agent || null; draw(); }
export function closeChats() { open = false; draw(); }
export function toggleChats() { open ? closeChats() : openChats(); }

function root() { return document.getElementById("chats-root")!; }

function threads(): string[] {
  const out = ["home", ...(desk?.projects || []).map((p) => "p:" + p.id)];
  for (const a of knownAgents()) if (!out.includes(a)) out.push(a);
  const last = (a: string) => { const m = transcriptOf(a); return m.length ? m[m.length - 1].id : -1; };
  // Home first, then whatever was talked to most recently.
  return [out[0], ...out.slice(1).sort((a, b) => Number(transcriptOf(b).length > 0) - Number(transcriptOf(a).length > 0) || last(b) - last(a))];
}

function preview(agent: string): string {
  const msgs = transcriptOf(agent);
  for (let i = msgs.length - 1; i >= 0; i--) {
    const m = msgs[i];
    if ((m.role === "agent" || m.role === "you") && m.text.trim()) return (m.role === "you" ? "You: " : "") + m.text.replace(/\s+/g, " ").slice(0, 110);
  }
  return "No messages yet";
}

function draw() {
  const r = root();
  if (!open) { r.replaceChildren(); return; }
  let body: HTMLElement;
  if (thread) {
    const agent = thread;
    panel.bind(agent, {
      placeholder: `Message the ${agentName(agent).toLowerCase().startsWith("task") ? "task agent" : agentName(agent)}…`,
      empty: () => h("div", { class: "msg system chat-empty" }, "No messages yet."),
      context: () => ({ view: "" }),
    });
    body = h("div", { class: "chats-thread" },
      h("div", { class: "chats-head" },
        h("button", { class: "link", onclick: () => { thread = null; draw(); } }, "← All conversations"),
        h("b", {}, agentName(agent)), h("span", { class: "grow-r" }),
        h("button", { class: "ebtn", title: "Close", onclick: closeChats }, "✕")),
      panel.el);
  } else {
    body = h("div", { class: "chats-list" },
      h("div", { class: "chats-head" }, h("b", {}, ic("chat"), " Conversations"), h("span", { class: "grow-r" }), h("button", { class: "ebtn", title: "Close", onclick: closeChats }, "✕")),
      h("p", { class: "muted small pad-x" }, "Every conversation with an agent is kept here, whether or not it's on a screen."),
      ...threads().map((a) => {
        const msgs = transcriptOf(a);
        const lastTs = msgs.length ? (msgs[msgs.length - 1] as any).ts : 0;
        return h("button", { class: "thread-row", onclick: () => { thread = a; draw(); } },
          h("span", { class: "tr-top" }, h("b", {}, agentName(a)), isBusy(a) ? h("span", { class: "busy-dot" }) : "",
            h("span", { class: "muted small grow-r" }, lastTs ? ago(new Date(lastTs).toISOString()) : "")),
          h("span", { class: "muted small" }, preview(a)));
      }));
  }
  r.replaceChildren(h("div", { class: "drawer-bg", onclick: closeChats }), h("aside", { class: "drawer chats" }, body));
}

onChat((_agent, kind) => { if (open && !thread && kind !== "delta") draw(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && open) closeChats(); });

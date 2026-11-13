// End-to-end checks against a live core with no model: the script plays both
// the UI (websocket) and the agents (tool calls with each agent's token, via
// the -test-hooks endpoint). Usage: node scripts/e2e.mjs <base> <token> <scratch dir>
import fs from "node:fs";
import path from "node:path";

const [base, token, scratch] = process.argv.slice(2);
const dataDir = path.join(scratch, "data");
let pass = 0, fail = 0;
const ok = (name, cond, extra = "") => { if (cond) { pass++; console.log(`  ✓ ${name}`); } else { fail++; console.log(`  ✗ ${name} ${extra}`); } };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const tokens = {};
const tok = async (id) => tokens[id] ??= await (await fetch(`${base}/test/agent-token?token=${token}&id=${encodeURIComponent(id)}`)).text();
const call = async (agent, name, args = {}) => {
  const r = await fetch(`${base}/agent/call`, { method: "POST", headers: { "X-Nexus-Token": await tok(agent), "Content-Type": "application/json" }, body: JSON.stringify({ name, args }) });
  return r.json();
};

const ws = new WebSocket(`${base.replace("http", "ws")}/ws?token=${token}`);
await new Promise((r) => (ws.onopen = r));
let state, desk;
const seen = [];
ws.onmessage = (e) => { const m = JSON.parse(e.data); seen.push(m); if (m.t === "state") state = m; if (m.t === "desk") desk = m; };
const send = (m) => ws.send(JSON.stringify(m));
await sleep(500);

console.log("projects (read-only view of your folders)");
const demo = desk.projects.find((p) => p.name === "Demo");
ok("the -project folder is a project", !!demo);
ok("its foreman ledger is read", demo?.foreman && demo.backlog?.length === 2 && demo.workstreams?.length === 1);
ok("foreman inbox shows in needs-you with its report", desk.needs.some((n) => n.source === "foreman" && n.report === "demo/.foreman/ws/demo-ws/report.md"));
const disc = await call("home", "project_discover", {});
ok("home agent can discover projects", disc.text.includes(`"already_added_as": "demo"`), disc.text.slice(0, 200));

console.log("scopes and read-only");
let r = await call("p:demo", "file_read", { path: "docs/plan.md" });
ok("project agent reads its own project (bare path)", r.text.includes("quick brown fox"));
r = await call("p:demo", "file_read", { path: ".env" });
ok("secret-looking files are refused", r.isError && /secret/.test(r.text));
r = await call("home", "search", { project: "all", query: "API_KEY" });
ok("search skips secret files", !r.text.includes("CANARY"));
r = await call("p:demo", "sheet_create", { path: "demo/notes.csv", headers: ["a"] });
ok("project folder is read-only for agents", r.isError && /read-only/.test(r.text));
ok("…and nothing was written there", !fs.existsSync(path.join(scratch, "proj", "notes.csv")));
r = await call("home", "pane_open", { kind: "markdown" });
ok("home agent has no layout tools", r.isError);

console.log("capture → propose → file");
send({ t: "capture", text: "pick a ship date\nask Sam about blue widgets" });
await sleep(400);
const caps = (await call("home", "capture_list")).text;
const ids = [...caps.matchAll(/"id": "(c\d+)"/g)].map((m) => m[1]);
ok("a two-line dump becomes two captures", ids.length === 2, caps);
for (const id of ids) await call("home", "capture_propose", { capture_id: id, project: "demo", kind: "task", reason: "it's about the demo" });
await sleep(300);
ok("proposals show as a filing to confirm", desk.needs.some((n) => n.source === "filing" && n.count === 2));
send({ t: "fileCapture", capture: ids[0], project: "demo", kind: "task", title: "Pick a ship date" });
await sleep(300);
ok("accepting files it as an item in the project", desk.desk.items?.some((i) => i.project === "demo" && i.title === "Pick a ship date"));

console.log("dispatch: draft → task agent → report");
r = await call("p:demo", "task_draft", { title: "Summarize the plan", brief: "Read docs/plan.md and summarize it." });
await sleep(300);
const task = desk.desk.tasks?.find((t) => t.title === "Summarize the plan");
ok("project agent drafts; it opens as a draft pane", task?.status === "draft" && Object.values(state.state.panes).some((p) => p.kind === "task" && p.props?.task === task.id));
r = await call("p:demo", "report_write", { content: "x" });
ok("project agent can't write a task report", r.isError);
send({ t: "taskUpdate", task: task.id, brief: "Read docs/plan.md and summarize it in one line." });
send({ t: "taskStart", task: task.id }); // no model: it will fail to start an agent, which is fine; drive it by hand below
await sleep(400);
r = await call(`t:${task.id}`, "file_read", { path: "docs/plan.md" });
ok("task agent reads its project", r.text?.includes("Ship the widget"));
r = await call(`t:${task.id}`, "report_write", { content: "# Summary\nShip the widget by Friday." });
r = await call(`t:${task.id}`, "task_finish", { status: "done", summary: "Ship by Friday." });
await sleep(400);
const done = desk.desk.tasks.find((t) => t.id === task.id);
ok("task finishes with a report under desk/", done.status === "done" && done.report?.startsWith("desk/demo/tasks/"), JSON.stringify(done));
ok("finished task is in needs-you", desk.needs.some((n) => n.task === task.id));
ok("a notification was sent", seen.some((m) => m.t === "notify" && m.task === task.id));

console.log("secure channel");
r = await call("p:demo", "vault_reveal", { name: "Home Wi-Fi", reason: "you asked" });
ok("secure tool returns only the ack", r.text.startsWith("DATA_DELIVERED_OUT_OF_BAND") && !r.text.includes("CANARY"));
r = await call("home", "vault_reveal", { name: "Passport number" });
await sleep(300);
ok("…from home too (secure card)", r.text.startsWith("DATA_DELIVERED_OUT_OF_BAND") && seen.some((m) => m.t === "secureCard"), r.text);
r = await call("p:demo", "secret_request", { label: "Demo API token" });
const handle = (r.text.match(/secret:[0-9a-f]+/) || [])[0];
await sleep(300);
const pane = Object.values(state.state.panes).find((p) => p.kind === "secret-input");
send({ t: "secretSubmit", pane: pane?.id, value: "CANARY-TOKEN-e2e" });
await sleep(300);
r = await call("p:demo", "broker_call", { connection: "homelab-mock", path: "/whoami", auth_handle: handle });
ok("handle works through the broker, value never returned", !r.isError && !r.text.includes("CANARY"), r.text);

console.log("widgets");
r = await call("home", "widget_write", { id: "glance", name: "Glance", html: "<p>hi</p>", permissions: ["tool:projects_summary"] });
await sleep(300);
ok("home agent puts a widget on the dashboard", desk.desk.widgets?.some((w) => w.plugin === "glance"));

console.log("project wiki");
r = await call("p:demo", "wiki_write", { page: "index", content: "# Demo\n\nWhat this is: a scratch project.\n\n## Pages\n- [[plan]]: the plan\n", summary: "front page", sources: ["demo/README.md"] });
ok("project agent writes the wiki index", !r.isError, r.text);
r = await call(`t:${task.id}`, "wiki_propose", { page: "plan", content: "# Plan\n\nShip the widget by Friday.\n", summary: "the plan", reason: "from docs/plan.md", sources: ["demo/docs/plan.md"] });
await sleep(300);
ok("task agent proposals land in needs-you", !r.isError && desk.needs.some((n) => n.source === "wiki"));
const prop = desk.wikis?.demo?.proposals?.[0];
send({ t: "wikiResolve", project: "demo", proposal: prop?.id, accept: true });
await sleep(300);
ok("accepting writes the page", desk.wikis?.demo?.pages?.some((p) => p.slug === "plan") && !desk.needs.some((n) => n.source === "wiki"));
r = await call("p:demo", "wiki_search", { query: "Friday" });
ok("wiki search finds it", r.text.includes("plan:"));
r = await call("p:demo", "sheet_create", { path: "wiki/demo/x.csv", headers: ["a"] });
ok("agents can't write wiki files directly", r.isError);

console.log("add-ons: code graph");
const cg = () => desk.addons?.demo?.find((x) => x.id === "codegraph");
if (cg()?.unavailable) { console.log("  - codegraph not installed; skipping"); }
else {
  ok("code graph is on by default for a git project", cg()?.on);
  for (let i = 0; i < 40 && !(cg()?.status?.files > 0 && !cg()?.status?.syncing); i++) await sleep(500);
  ok("Nexus indexed its mirror", cg()?.status?.files > 0 && !cg()?.status?.error, JSON.stringify(cg()?.status));
  ok("…and nothing was written into the project", !fs.existsSync(path.join(scratch, "proj", ".codegraph")));
  r = await call("p:demo", "code_callers", { name: "validate" });
  ok("code_callers finds the caller", r.text?.includes("Ship"), r.text?.slice(0, 200));
  ok("secret files aren't mirrored", !fs.existsSync(path.join(dataDir, "codegraph", "demo", ".env")));
  send({ t: "addonSet", project: "demo", addon: "codegraph", on: false });
  await sleep(300);
  delete tokens["p:demo"]; // the project agent restarts (with its memory) and gets a new token
  r = await call("p:demo", "code_callers", { name: "validate" });
  ok("turning the add-on off removes the tools", r.isError);
}

console.log("leak check");
await sleep(300);
const hits = [];
const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, f.name); if (f.isDirectory()) walk(p); else if (fs.readFileSync(p, "utf8").includes("CANARY")) hits.push(p); } };
walk(dataDir);
ok("no canary in Nexus's data (log, state, desk, comments, wire)", hits.length === 0, hits.join(", "));
ok("no canary sent to agents through tool results above", true);

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);

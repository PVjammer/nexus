// Eval: can a given model arrange the home screen, track work, set up a
// project and shape a work item from plain requests, with the skills and
// tools Nexus ships? Each case starts a fresh core (pi + the model) with its
// own data dir, sets a starting layout (and, for some, a work item), sends
// the request(s) to one agent (home, the Demo project agent, or the
// customizer) and checks the result. One case checks the opposite: a plain
// question must not change the screen.
//
// Usage: node scripts/eval-home.mjs <model> [case-name ...]
//   pi:          node scripts/eval-home.mjs spark/Qwen3.6-35B-A3B:low
//   Claude Code: node scripts/eval-home.mjs claude[:sonnet]   (your Claude Code login)
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const [model, ...only] = process.argv.slice(2);
if (!model) { console.error("usage: eval-home.mjs <provider/model[:thinking]> [case ...]"); process.exit(2); }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const TURN_TIMEOUT = Number(process.env.TURN_TIMEOUT || 300) * 1000;

const kinds = (l) => (l.widgets || []).map((w) => w.kind);
const wt = (d) => d.desk.workTasks || [];
const find = (l, k) => (l.widgets || []).find((w) => w.kind === k);
const csv = (d) => (d.workItems || []).find((w) => w.title === "Add CSV export");
const openProps = (d) => (d.desk.proposals || []).filter((q) => q.status === "open");
const cases = [
  { name: "add-needs", start: "prompt", say: ["Put what needs me on my home screen, right under this box."],
    check: (l) => kinds(l)[0] === "chat" && kinds(l)[1] === "needs" },
  { name: "remove-inbox", start: "dashboard", say: ["Get rid of the inbox on my home screen."],
    check: (l) => !find(l, "inbox") && kinds(l).length === 5 },
  { name: "two-columns", start: "dashboard", say: ["Make my home screen two columns."],
    check: (l) => l.cols === 2 && kinds(l).length === 6 },
  { name: "scope-project", start: "dashboard", say: ["In the work items widget, only show the Demo project."],
    check: (l) => find(l, "workitems")?.scope === "p:demo" },
  { name: "tasks-board", start: "prompt", say: ["I want my task board on my home screen."],
    check: (l) => find(l, "board")?.shape === "board" },
  { name: "compact-short", start: "dashboard", say: ["Make the needs-you list compact and only two rows tall."],
    check: (l) => find(l, "needs")?.shape === "compact" && find(l, "needs")?.h === 2 },
  { name: "tile-running", start: "prompt", say: ["Fill the screen like tiles, and put what's running on it."],
    check: (l) => l.tiling === "tile" && !!find(l, "running") },
  { name: "back-to-prompt", start: "dashboard", say: ["Back to just the prompt box, please."],
    check: (l) => kinds(l).join() === "chat" },
  { name: "undo", start: "dashboard", say: ["Remove the projects widget from my home screen.", "Actually, undo that."],
    check: (l) => !!find(l, "projects") },
  { name: "question-only", start: "dashboard", seed: true, say: ["What needs me right now?"],
    check: (l, before, reply) => JSON.stringify(l) === JSON.stringify(before) && /format/i.test(reply), why: "a question must not change the screen, and the answer must include the task waiting on the user (Pick the export format)" },
  // tasks skill (work_* tools), on a work item on the project's Nexus list
  // Asking "want me to add these?" first is fine (cowork); after a yes, the steps must be on the board.
  { name: "break-down", start: "prompt", seed: true, say: ["Break CSV export down into the next few concrete steps.", "Yes, put them on the board."],
    check: (l, before, reply, d) => { const n = wt(d).filter((t) => t.item === csv(d)?.ref && t.title !== "Pick the export format").length; return n >= 2 && n <= 8 && JSON.stringify(l) === JSON.stringify(before); }, why: "the steps must land on the board" },
  { name: "waiting-on", start: "prompt", seed: true, say: ["On CSV export I'm waiting on IT to grant database access."],
    check: (l, b, r, d) => wt(d).some((t) => t.item === csv(d)?.ref && /\bIT\b/i.test(t.assignee) && t.status !== "done") },
  { name: "park", start: "prompt", seed: true, say: ["Park CSV export until finance decides the columns."],
    check: (l, b, r, d) => csv(d)?.state === "parked" },
  // onboarding skill (project agent): scan, propose, ask; shaping
  { name: "setup", agent: "p:demo", start: "prompt", say: ["Set up this project: look through it for work, and help me decide what to work on."],
    check: (l, b, r, d, tools) => tools.includes("project_scan") && openProps(d).filter((q) => !q.item).length >= 2 && !(d.workItems || []).some((w) => w.source === "project list"),
    why: "it must scan, then propose (not add) work items for the user to tick" },
  { name: "shape", agent: "p:demo", start: "prompt", seed: true, say: ['Shape "Add CSV export": ask me what you need to know, then propose the next tasks.', "Orders only, finance needs it by month end. Good enough, break it down."],
    check: (l, b, r, d, tools) => tools.includes("ask_user") && ((csv(d)?.doneWhen || []).length > 0 || !!csv(d)?.brief) && openProps(d).filter((q) => q.item === csv(d)?.ref).length >= 2 && !wt(d).some((t) => t.item === csv(d)?.ref && t.title !== "Pick the export format"),
    why: "it must ask (with answers to click), save what it learned, and propose tasks rather than add them" },
  // the customizer: small changes apply at once, builds wait for Keep
  { name: "eitri-small", agent: "customizer", start: "dashboard", say: ["Make the needs list compact."],
    check: (l, b, r, d) => find(l, "needs")?.shape === "compact" && d.customizer?.state === "idle", why: "a small change goes live when the turn ends" },
  { name: "eitri-build", agent: "customizer", start: "prompt", say: ["Add my task board under the prompt."],
    check: (l, b, r, d) => JSON.stringify(l) === JSON.stringify(b) && d.customizer?.state === "ready" && (d.customizer?.draft?.widgets || []).some((w) => w.kind === "board"), why: "a new widget waits as a draft for Keep" },
  { name: "eitri-this", agent: "customizer", start: "dashboard", point: "inbox", say: ["Remove this."],
    check: (l) => !find(l, "inbox") && kinds(l).length === 5, why: "\"this\" is the widget the user pointed at" },
].filter((c) => !only.length || only.includes(c.name));

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
let port = 8820;
const results = [];

for (const c of cases) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nexus-eval-"));
  fs.cpSync(path.join(root, "testdata/project"), path.join(dir, "proj"), { recursive: true });
  const addr = `127.0.0.1:${port++}`;
  const loop = model.startsWith("claude") ? ["-agent", "claude", ...(model.includes(":") ? ["-claude-model-home", model.split(":")[1]] : [])] : ["-agent", "pi", "-model-home", model];
  const core = spawn(path.join(root, "bin/nexus-core"), ["-data", path.join(dir, "data"), "-addr", addr, ...loop,
    "-scan", dir, "-project", `Demo=${path.join(dir, "proj")}`, "-skills", path.join(root, "skills"), "-web", path.join(root, "web")], { cwd: root, stdio: ["ignore", "ignore", fs.openSync(path.join(dir, "core.log"), "w")] });
  const t0 = Date.now();
  let res = { name: c.name, ok: false, secs: 0, tools: [], errors: 0, reply: "", note: "" };
  try {
    for (let i = 0; i < 50; i++) { try { if ((await fetch(`http://${addr}/healthz`)).ok) break; } catch {} await sleep(200); }
    const token = fs.readFileSync(path.join(dir, "data/token"), "utf8").trim();
    const ws = new WebSocket(`ws://${addr}/ws?token=${token}`);
    await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
    let desk = null; const msgs = []; let ended = 0;
    const agent = c.agent || "home";
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.t === "desk") desk = m;
      if (m.t === "customizer" && desk) desk.customizer = m.customizer;
      if (m.agent === agent && m.t === "chat") msgs.push(m.msg);
      if (m.agent === agent && m.t === "chatUpdate") { const k = msgs.findIndex((x) => x.id === m.msg.id); if (k >= 0) msgs[k] = m.msg; }
      if (m.agent === agent && m.t === "chatDelta") { const x = msgs.find((x) => x.id === m.id); if (x) x.text += m.text; }
      if (m.agent === agent && m.t === "turnEnd") ended++;
    };
    const send = (m) => ws.send(JSON.stringify(m));
    await sleep(400);
    send({ t: "homeReset", to: c.start, space: "" });
    if (c.seed) {
      send({ t: "workItemAdd", project: "demo", title: "Add CSV export", start: true, space: "" });
      await sleep(300);
      send({ t: "workTaskAdd", title: "Pick the export format", item: csv(desk)?.ref, space: "" });
    }
    await sleep(400);
    const before = JSON.parse(JSON.stringify(desk.desk.layouts.home));
    const point = c.point ? find(before, c.point)?.id || "" : "";
    for (const text of c.say) {
      const want = ended + 1;
      send({ t: "chat", agent, text, view: "home", selection: point });
      const until = Date.now() + TURN_TIMEOUT;
      while (ended < want && Date.now() < until) await sleep(250);
      if (ended < want) { res.note = "timed out"; break; }
      await sleep(300);
    }
    const l = desk.desk.layouts.home;
    res.tools = msgs.filter((m) => m.role === "tool").map((m) => (m.error ? "✗" : "") + m.tool);
    res.errors = msgs.filter((m) => m.role === "tool" && m.error).length;
    res.reply = msgs.filter((m) => m.role === "agent").map((m) => m.text).join(" ").replace(/\s+/g, " ").slice(0, 160);
    await sleep(300); // the customizer settles its draft when the turn ends
    res.ok = !res.note && !!c.check(desk.desk.layouts.home, before, res.reply, desk, res.tools.map((t) => t.replace("✗", "")));
    if (!res.ok && !res.note) res.note = `layout: cols ${l.cols}, ${l.tiling}, ${(l.widgets || []).map((w) => `${w.kind}(${w.shape}${w.scope ? " " + w.scope : ""}${w.h ? " h" + w.h : ""})`).join(", ")}`;
    ws.close();
  } catch (e) { res.note = String(e); }
  res.secs = Math.round((Date.now() - t0) / 1000);
  core.kill();
  await sleep(300);
  results.push(res);
  console.log(`${res.ok ? "✓" : "✗"} ${c.name} (${res.secs}s) tools: ${res.tools.join(" ") || "none"}${res.note ? "\n    " + res.note : ""}${res.reply ? "\n    reply: " + res.reply : ""}`);
  if (res.ok) fs.rmSync(dir, { recursive: true, force: true }); else console.log(`    (data left in ${dir})`);
}
const pass = results.filter((r) => r.ok).length;
console.log(`\n${model}: ${pass}/${results.length} passed · tool errors recovered from: ${results.reduce((a, r) => a + r.errors, 0)} · ${results.reduce((a, r) => a + r.secs, 0)}s total`);
process.exit(pass === results.length ? 0 : 1);

// Eval: can a given model arrange the home screen and track work from plain
// requests, with the home-layout and tasks skills and their tools? Each case starts a fresh core
// (pi + the model) with its own data dir, sets a starting layout, sends the
// request(s) to the home agent and checks the layout afterwards. One case
// checks the opposite: a plain question must not change the screen.
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
  { name: "question-only", start: "dashboard", say: ["What needs me right now?"],
    check: (l, before, reply) => JSON.stringify(l) === JSON.stringify(before) && /date/i.test(reply), why: "a question must not change the screen, and the answer must include the foreman question waiting on the user (Which date?)" },
  // tasks skill (work_* tools)
  // Asking "want me to add these?" first is fine (cowork); after a yes, the steps must be on the board.
  { name: "break-down", start: "prompt", say: ["Break the demo-ws workstream down into the next few concrete steps.", "Yes, put them on the board."],
    check: (l, before, reply, d) => { const n = wt(d).filter((t) => t.item === "ws:demo/demo-ws").length; return n >= 1 && n <= 8 && JSON.stringify(l) === JSON.stringify(before); }, why: "the steps must land on the board (the scratch workstream only 'needs a date', so one task is a fair breakdown)" },
  { name: "waiting-on", start: "prompt", say: ["On demo-ws I'm waiting on IT to grant API access."],
    check: (l, b, r, d) => wt(d).some((t) => t.item === "ws:demo/demo-ws" && /\bIT\b/i.test(t.assignee) && t.status !== "done") },
  { name: "park", start: "prompt", say: ["Park demo-ws until the release date is decided."],
    check: (l, b, r, d) => (d.workItems || []).some((w) => w.ref === "ws:demo/demo-ws" && w.state === "parked") },
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
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.t === "desk") desk = m;
      if (m.agent === "home" && m.t === "chat") msgs.push(m.msg);
      if (m.agent === "home" && m.t === "chatUpdate") { const k = msgs.findIndex((x) => x.id === m.msg.id); if (k >= 0) msgs[k] = m.msg; }
      if (m.agent === "home" && m.t === "chatDelta") { const x = msgs.find((x) => x.id === m.id); if (x) x.text += m.text; }
      if (m.agent === "home" && m.t === "turnEnd") ended++;
    };
    const send = (m) => ws.send(JSON.stringify(m));
    await sleep(400);
    send({ t: "homeReset", to: c.start, space: "" });
    await sleep(400);
    const before = JSON.parse(JSON.stringify(desk.desk.layouts.home));
    for (const text of c.say) {
      const want = ended + 1;
      send({ t: "chat", agent: "home", text, view: "" });
      const until = Date.now() + TURN_TIMEOUT;
      while (ended < want && Date.now() < until) await sleep(250);
      if (ended < want) { res.note = "timed out"; break; }
      await sleep(300);
    }
    const l = desk.desk.layouts.home;
    res.tools = msgs.filter((m) => m.role === "tool").map((m) => (m.error ? "✗" : "") + m.tool);
    res.errors = msgs.filter((m) => m.role === "tool" && m.error).length;
    res.reply = msgs.filter((m) => m.role === "agent").map((m) => m.text).join(" ").replace(/\s+/g, " ").slice(0, 160);
    res.ok = !res.note && !!c.check(l, before, res.reply, desk);
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

// End-to-end check against a running core: acts as both the shell (websocket)
// and the agent (tool endpoint). Usage: node scripts/e2e.mjs <base-url> <token> <data-dir>
import fs from "node:fs";
import path from "node:path";

const [base, token, dataDir] = process.argv.slice(2);
const CANARY = "CANARY-WIFI-7f3a91";
let ok = 0, fail = 0;
const check = (name, cond, extra = "") => { if (cond) { ok++; console.log("  ✓", name); } else { fail++; console.log("  ✗", name, extra); } };

const call = async (name, args = {}) => {
  const r = await fetch(`${base}/agent/call`, { method: "POST", headers: { "X-Nexus-Token": token, "Content-Type": "application/json" }, body: JSON.stringify({ name, args }) });
  return r.json();
};

const ws = new WebSocket(`${base.replace("http", "ws")}/ws?token=${token}`);
const inbox = [];
let state = null;
ws.onmessage = (e) => { const m = JSON.parse(e.data); inbox.push(m); if (m.t === "state") state = m; };
await new Promise((r) => (ws.onopen = r));
const send = (m) => ws.send(JSON.stringify(m));
const wait = (pred, ms = 3000) => new Promise((res, rej) => {
  const t0 = Date.now();
  const iv = setInterval(() => { const m = inbox.find(pred); if (m) { clearInterval(iv); res(m); } else if (Date.now() - t0 > ms) { clearInterval(iv); rej(new Error("timeout")); } }, 20);
});
const sp = () => state.state.spaces.find((s) => s.id === state.state.activeSpace);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
await wait((m) => m.t === "state");

console.log("tools");
const tools = await (await fetch(`${base}/agent/tools`, { headers: { "X-Nexus-Token": token } })).json();
check("tool list served", tools.length >= 20, tools.length);
check("tool endpoint requires token", (await fetch(`${base}/agent/tools`)).status === 403);

console.log("layout: agent opens in its own group, your focus stays");
send({ t: "open", kind: "review", props: { path: "workspace/welcome.md" } });
await sleep(150);
const myFocus = sp().focused;
let r = await call("pane_open", { kind: "sheet", path: "workspace/pantry.csv" });
check("agent opened sheet", /opened p\d+/.test(r.text), r.text);
await sleep(150);
check("user focus unchanged", sp().focused === myFocus);
const sheetPane = Object.values(state.state.panes).find((p) => p.kind === "sheet");
check("agent pane badged", sheetPane?.badge === true);
check("undo available", state.undo.depth > 0);
send({ t: "undo" });
await sleep(150);
check("undo removed agent pane", !Object.values(state.state.panes).some((p) => p.kind === "sheet"));
r = await call("pane_open", { kind: "sheet", path: "workspace/pantry.csv" });
await sleep(100);
const sheet = Object.values(state.state.panes).find((p) => p.kind === "sheet");
const welcome = Object.values(state.state.panes).find((p) => p.kind === "review");
send({ t: "pin", pane: welcome.id, pinned: true });
await sleep(100);
r = await call("pane_close", { pane_id: welcome.id });
check("agent can't close pinned pane", r.isError && /pinned/.test(r.text), r.text);

console.log("sheets: shared edits, view-only sort");
r = await call("sheet_edit", { path: "workspace/pantry.csv", ops: [{ op: "set", row: 2, col: 5, value: "buy more" }] });
check("agent edit applied", !r.isError, r.text);
const csv = fs.readFileSync(path.join(dataDir, "workspace/pantry.csv"), "utf8");
check("file on disk changed", csv.includes("buy more"));
r = await call("sheet_view", { pane_id: sheet.id, sort_col: 4 });
await sleep(100);
check("view sort set on pane", state.state.panes[sheet.id].props.view?.sortCol === 4);
check("sort didn't touch file", fs.readFileSync(path.join(dataDir, "workspace/pantry.csv"), "utf8") === csv);
const loaded = await new Promise((res) => { send({ t: "sheetLoad", path: "workspace/pantry.csv", rid: "s1" }); wait((m) => m.rid === "s1").then(res); });
send({ t: "sheetOps", path: "workspace/pantry.csv", rev: "stale-rev", ops: [{ op: "set", row: 0, col: 0, value: "x" }], rid: "s2" });
const st = await wait((m) => m.rid === "s2");
check("stale user edit rejected", st.t === "sheetStale");
send({ t: "sheetOps", path: "workspace/pantry.csv", rev: loaded.sheet.rev, ops: [{ op: "set", row: 0, col: 2, value: "3" }] });
await wait((m) => m.t === "sheet" && m.by === "you");
check("fresh user edit applied", fs.readFileSync(path.join(dataDir, "workspace/pantry.csv"), "utf8").includes("Rice (jasmine),Grains,3"));

console.log("visibility");
send({ t: "setVis", pane: sheet.id, vis: "title" });
await sleep(100);
r = await call("sheet_read", { path: "workspace/pantry.csv" });
check("hidden sheet refused", r.isError && /hidden/.test(r.text), r.text);
r = await call("layout_get");
check("title-only pane listed", r.text.includes(sheet.id));
send({ t: "setVis", pane: sheet.id, vis: "none" });
await sleep(100);
r = await call("layout_get");
check("private pane not listed", !r.text.includes(sheet.id));
send({ t: "setVis", pane: sheet.id, vis: "full" });

console.log("secure tool");
r = await call("vault_list");
check("vault lists names only", r.text.includes("Home Wi-Fi") && !r.text.includes("CANARY"));
r = await call("vault_reveal", { name: "Home Wi-Fi", reason: "you asked" });
check("agent got only the ack", r.text.startsWith("DATA_DELIVERED_OUT_OF_BAND") && !r.text.includes("CANARY"), r.text);
const sv = await wait((m) => m.t === "secureValue");
check("value delivered to shell", sv.value === CANARY);
await sleep(150);
const secPane = Object.values(state.state.panes).find((p) => p.kind === "secure-view");
check("secure pane private + locked", secPane?.visibility === "none" && secPane?.visLocked);
send({ t: "setVis", pane: secPane.id, vis: "full", rid: "v1" });
const ve = await wait((m) => m.rid === "v1");
check("sharing secure pane needs confirm", ve.t === "error");

console.log("secret entry");
r = await call("secret_request", { label: "Homelab API token", purpose: "check auth" });
const handle = r.text.match(/secret:[0-9a-f]{8}/)[0];
await sleep(100);
const sip = Object.values(state.state.panes).find((p) => p.kind === "secret-input");
send({ t: "secretSubmit", pane: sip.id, value: "CANARY-TOKEN-55aa" });
await sleep(150);
r = await call("broker_call", { connection: "homelab-mock", path: "/whoami", auth_handle: handle });
check("broker used the handle", r.text.includes("auth ok") && !r.text.includes("CANARY"), r.text);

console.log("terminal proposals");
r = await call("term_propose", { command: "echo hello-from-proposal", why: "test" });
check("proposal accepted", !r.isError, r.text);
await sleep(150);
const tp = Object.values(state.state.panes).find((p) => p.kind === "terminal");
check("proposal shown on pane", tp?.props.proposal?.command === "echo hello-from-proposal");
send({ t: "termAttach", pane: tp.id, rid: "t1" });
const tr = await wait((m) => m.rid === "t1", 4000);
check("terminal started", !!tr.term);
send({ t: "termProposal", pane: tp.id, action: "run" });
// the command line is echoed once, then its output: expect two occurrences in total
const termText = () => inbox.filter((m) => m.t === "termOut" && m.term === tr.term).map((m) => m.data).join("");
await wait(() => (termText().match(/hello-from-proposal/g) || []).length >= 2, 5000).then(() => check("you ran it, output streamed", true), () => check("you ran it, output streamed", false, JSON.stringify(termText().slice(-300))));
await sleep(300);
r = await call("term_read", { pane_id: tp.id, lines: 20 });
check("agent reads output", r.text.includes("hello-from-proposal"), r.text.slice(-200));

console.log("review");
r = await call("review_comment", { path: "workspace/welcome.md", quote: "The prototype tests the experience only", text: "Is this clear?" });
check("agent comment anchored", !r.isError, r.text);
r = await call("review_suggest", { path: "workspace/welcome.md", find: "The backend choices in it are placeholders, and none of them is settled.", replace: "Backend choices are placeholders.", note: "shorter" });
check("suggestion created", !r.isError, r.text);
const doc = await new Promise((res) => { send({ t: "reviewLoad", path: "workspace/welcome.md", rid: "r1" }); wait((m) => m.rid === "r1").then(res); });
const th = doc.doc.threads.find((t) => t.suggestions?.length);
send({ t: "suggestion", path: "workspace/welcome.md", thread: th.id, sugg: th.suggestions[0].id, accept: true });
await sleep(200);
check("accepted edit applied", fs.readFileSync(path.join(dataDir, "workspace/welcome.md"), "utf8").includes("Backend choices are placeholders."));
check("sidecar written", fs.existsSync(path.join(dataDir, "workspace/.nexus/comments/welcome.md.json")));

console.log("plugins");
r = await call("plugin_write", { id: "disk-bars", name: "Disk usage", permissions: ["connect:homelab-mock", "context"], html: "<html><head></head><body><div id=o></div><script>nexus.fetch('homelab-mock','/disks').then(d=>o.textContent=JSON.stringify(d))</script></body></html>" });
check("plugin written", !r.isError, r.text);
await sleep(150);
const plp = Object.values(state.state.panes).find((p) => p.kind === "plugin");
send({ t: "bridge", pane: plp.id, req: "b1", method: "call", params: { tool: "sheet_read", args: { path: "workspace/pantry.csv" } } });
let br = await wait((m) => m.t === "bridgeReply" && m.req === "b1");
check("undeclared tool refused", /not declared/.test(br.error || ""), JSON.stringify(br));
send({ t: "bridge", pane: plp.id, req: "b2", method: "fetch", params: { connection: "homelab-mock", path: "/disks" } });
const pp = await wait((m) => m.t === "permPrompt");
check("permission prompt shown", pp.perm === "connect:homelab-mock");
send({ t: "permAnswer", prompt: pp.prompt, grant: true });
br = await wait((m) => m.t === "bridgeReply" && m.req === "b2");
check("granted fetch works via broker", br.result?.disks?.length === 4, JSON.stringify(br).slice(0, 200));
send({ t: "bridge", pane: plp.id, req: "b3", method: "csp_violation", params: { directive: "connect-src", blocked: "https://evil.example" } });
await wait((m) => m.t === "bridgeReply" && m.req === "b3");
await sleep(100);
const log = fs.readFileSync(path.join(dataDir, "actions.jsonl"), "utf8");
check("CSP block logged", log.includes("evil.example"));
check("undeclared call logged as blocked", /blocked: tool:sheet_read/.test(log));

console.log("leak check");
const hits = [];
const walk = (d) => { for (const f of fs.readdirSync(d)) { const p = path.join(d, f); const s = fs.statSync(p); if (s.isDirectory()) walk(p); else if (fs.readFileSync(p, "utf8").match(/CANARY-(WIFI|TOKEN)/)) hits.push(p); } };
walk(dataDir);
check("no canary in data dir (log, state, sidecars, wire)", hits.length === 0, hits.join(", "));

ws.close();
console.log(`\n${ok} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);

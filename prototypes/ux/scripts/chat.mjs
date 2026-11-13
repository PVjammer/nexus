// Send chat messages to a running core and print what the agent does.
// Usage: node scripts/chat.mjs <base-url> <token> "message 1" ["message 2" ...]
const [base, token, ...msgs] = process.argv.slice(2);
const ws = new WebSocket(`${base.replace("http", "ws")}/ws?token=${token}`);
await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
let resolveTurn;
ws.onmessage = (e) => {
  const m = JSON.parse(e.data);
  if (m.t === "chat") {
    const c = m.msg;
    if (c.role === "tool") process.stdout.write(`\n  [tool] ${c.tool} ${c.args || ""}\n`);
    else if (c.role === "agent") process.stdout.write(`\n  agent: ${c.text}`);
    else if (c.role === "system") process.stdout.write(`\n  [system] ${c.text}\n`);
  }
  if (m.t === "chatDelta") process.stdout.write(m.text.replace(/\n/g, "\n         "));
  if (m.t === "chatUpdate" && m.msg.role === "tool") process.stdout.write(`  [result${m.msg.error ? " ERROR" : ""}] ${String(m.msg.result || "").slice(0, 220).replace(/\n/g, " ")}\n`);
  if (m.t === "permPrompt") { console.log(`\n  [permission prompt] ${m.perm} → auto-allow`); ws.send(JSON.stringify({ t: "permAnswer", prompt: m.prompt, grant: true })); }
  if (m.t === "secureValue") console.log(`\n  [secure pane received a value of length ${m.value.length}]`);
  if (m.t === "busy" && !m.busy && resolveTurn) resolveTurn();
};
for (const text of msgs) {
  console.log(`\n> ${text}`);
  const done = new Promise((r) => (resolveTurn = r));
  ws.send(JSON.stringify({ t: "chat", text }));
  await Promise.race([done, new Promise((r) => setTimeout(r, 240000))]);
}
ws.close();

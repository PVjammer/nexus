/**
 * Nexus tool bridge for pi.
 *
 * The tools are defined (and policed) by the Nexus core, not here: at startup
 * this fetches the tool list from the core and registers each as a thin proxy
 * that POSTs the call back. Any other agent loop can do the same, which is the
 * point: the agent loop is swappable, the tools and their policy are not.
 */
import { Type } from "@earendil-works/pi-ai";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const URL_ = process.env.NEXUS_URL ?? "http://127.0.0.1:8740";
const TOKEN = process.env.NEXUS_TOKEN ?? "";

type ToolDef = { name: string; label: string; description: string; parameters: Record<string, unknown> };

export default async function (pi: ExtensionAPI) {
	let defs: ToolDef[] = [];
	for (let attempt = 0; attempt < 20; attempt++) {
		try {
			const r = await fetch(`${URL_}/agent/tools`, { headers: { "X-Nexus-Token": TOKEN } });
			if (r.ok) {
				defs = (await r.json()) as ToolDef[];
				break;
			}
		} catch {
			/* core not listening yet */
		}
		await new Promise((res) => setTimeout(res, 250));
	}
	for (const d of defs) {
		pi.registerTool({
			name: d.name,
			label: d.label,
			description: d.description,
			parameters: Type.Unsafe<Record<string, unknown>>(d.parameters),
			async execute(_id, params, signal) {
				const r = await fetch(`${URL_}/agent/call`, {
					method: "POST",
					headers: { "Content-Type": "application/json", "X-Nexus-Token": TOKEN },
					body: JSON.stringify({ name: d.name, args: params }),
					signal,
				});
				const out = (await r.json()) as { text: string; isError?: boolean };
				if (out.isError) throw new Error(out.text);
				return { content: [{ type: "text", text: out.text }], details: {} };
			},
		});
	}
}

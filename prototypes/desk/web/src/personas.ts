// Personas: voices you call into any chat with @handle. The core runs them
// (personas.go); this is how their answers look and the @ menu in a composer.

import { desk, h } from "./core";
import { md } from "./md";

export type Persona = { handle: string; name: string; about?: string; color?: string; model?: string; builtin?: boolean };

export const personas = (): Persona[] => ((desk as any)?.personas || []) as Persona[];
const personaOf = (handle: string) => personas().find((p) => p.handle === handle);

/** A persona's answer: its name in its colour, then what it said. */
export function personaMsg(m: { author?: string; text: string; done?: boolean; error?: boolean }): HTMLElement {
  const p = personaOf(m.author || "");
  const body = !m.done
    ? h("div", { class: "persona-body muted" }, h("span", { class: "busy-dot" }), " thinking…")
    : m.error ? h("div", { class: "persona-body error" }, m.text)
    : h("div", { class: "persona-body", html: md(m.text) });
  const el = h("div", { class: `msg persona${m.error ? " err" : ""}`, title: p?.about || "" },
    h("div", { class: "persona-who" }, h("b", {}, p?.name || "@" + m.author), h("span", { class: "muted small" }, " @" + m.author)),
    body);
  if (p?.color) el.style.setProperty("--pc", p.color);
  return el;
}

/** attachMentions gives a composer an @ menu of personas. Call it before the
 *  composer's own Enter handler is added: while the menu is open, Enter and
 *  Tab pick from it. */
export function attachMentions(input: HTMLTextAreaElement) {
  const menu = h("div", { class: "mention-menu", hidden: true });
  let items: Persona[] = [];
  let sel = 0;
  let start = -1; // where the "@..." being typed starts
  const place = () => { if (!menu.isConnected) input.parentElement?.append(menu); };
  const close = () => { menu.hidden = true; items = []; start = -1; };
  const draw = () => {
    menu.replaceChildren(...items.map((p, i) => {
      const b = h("button", { type: "button", class: `mention${i === sel ? " on" : ""}`,
        onmousedown: (e: Event) => { e.preventDefault(); pick(p); } },
        h("b", {}, "@" + p.handle), " ", h("span", {}, p.name), p.about ? h("span", { class: "muted small" }, " · " + p.about) : "");
      if (p.color) b.style.setProperty("--pc", p.color);
      return b;
    }));
  };
  const pick = (p: Persona) => {
    const v = input.value, caret = input.selectionStart ?? v.length;
    input.value = v.slice(0, start) + "@" + p.handle + " " + v.slice(caret);
    const at = start + p.handle.length + 2;
    input.setSelectionRange(at, at);
    close();
    input.focus();
  };
  input.addEventListener("input", () => {
    const caret = input.selectionStart ?? input.value.length;
    const m = /(^|[\s(])@([\w-]*)$/.exec(input.value.slice(0, caret));
    const all = personas();
    if (!m || !all.length) return close();
    const q = m[2].toLowerCase();
    items = all.filter((p) => p.handle.startsWith(q) || p.name.toLowerCase().includes(q)).slice(0, 6);
    if (!items.length) return close();
    start = caret - m[2].length - 1;
    sel = Math.min(sel, items.length - 1);
    place();
    menu.hidden = false;
    draw();
  });
  input.addEventListener("keydown", (e) => {
    if (menu.hidden || !items.length) return;
    const stop = () => { e.preventDefault(); e.stopImmediatePropagation(); };
    if (e.key === "ArrowDown" || e.key === "ArrowUp") { stop(); sel = (sel + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length; draw(); }
    else if (e.key === "Enter" || e.key === "Tab") { stop(); pick(items[sel]); }
    else if (e.key === "Escape") { stop(); close(); }
  }, { capture: true });
  input.addEventListener("blur", () => setTimeout(close, 100));
}

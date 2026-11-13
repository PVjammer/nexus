import DOMPurify from "dompurify";
import { marked } from "marked";

export function md(src: string): string {
  // YAML front matter (foreman reports start with it) shows as a small meta line.
  let meta = "";
  const fm = /^---\r?\n([\s\S]*?)\r?\n---\r?\n/.exec(src);
  if (fm) {
    meta = `<div class="frontmatter">${fm[1].split(/\r?\n/).map((l) => l.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]!))).join(" · ")}</div>`;
    src = src.slice(fm[0].length);
  }
  // [[page]] or [[project/page]] wiki links (outside code spans)
  src = src.replace(/\[\[([a-z0-9][a-z0-9/-]{0,80})\]\]/gi, (_m, slug) => `[${slug}](#wiki:${slug.toLowerCase()})`);
  return meta + DOMPurify.sanitize(marked.parse(src, { async: false }) as string, { ADD_ATTR: ["target"] });
}

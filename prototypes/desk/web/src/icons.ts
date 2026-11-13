// Inline SVG icons (currentColor), so the UI doesn't depend on emoji fonts.

const P: Record<string, string> = {
  agent: '<rect x="4" y="7" width="16" height="12" rx="3"/><circle cx="9.5" cy="13" r="1.3" fill="currentColor" stroke="none"/><circle cx="14.5" cy="13" r="1.3" fill="currentColor" stroke="none"/><path d="M12 7V4M10 4h4"/>',
  lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
  key: '<circle cx="8" cy="14" r="4"/><path d="M11 12l8-8M16 7l2 2M14 9l2 2"/>',
  pin: '<path d="M9 4h6l-1 6 3 3H7l3-3z"/><path d="M12 13v7"/>',
  pinned: '<path d="M9 4h6l-1 6 3 3H7l3-3z" fill="currentColor"/><path d="M12 13v7"/>',
  eye: '<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  half: '<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/><path d="M4 18L20 6"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14.5 0 18M12 3c-3 3.5-3 14.5 0 18"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  chat: '<path d="M4 5h16v11H9l-5 4z"/>',
  check: '<path d="M5 12l5 5L20 7"/>',
  block: '<circle cx="12" cy="12" r="9"/><path d="M6 6l12 12"/>',
  sheet: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M3 14h18M9 4v16"/>',
  doc: '<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v4h4M9 12h6M9 16h6"/>',
  term: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M12 15h5"/>',
  note: '<path d="M5 4h14v16H5z"/><path d="M8 8h8M8 12h8M8 16h5"/>',
  code: '<path d="M8 7l-5 5 5 5M16 7l5 5-5 5"/>',
  plugin: '<path d="M9 3v4M15 3v4M6 7h12v5a6 6 0 0 1-12 0zM12 18v3"/>',
  log: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  undo: '<path d="M9 14L4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/>',
  ext: '<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/>',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  home: '<path d="M4 11l8-7 8 7v9H4z"/><path d="M10 20v-6h4v6"/>',
  inbox: '<path d="M4 13l2-8h12l2 8v6H4z"/><path d="M4 13h5l1 2h4l1-2h5"/>',
  bolt: '<path d="M13 3L5 14h6l-1 7 8-11h-6z"/>',
  send: '<path d="M4 12l16-8-6 16-3-7z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  dot: '<circle cx="12" cy="12" r="4" fill="currentColor" stroke="none"/>',
  splitv: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16"/>',
  splith: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 12h18"/>',
  float: '<rect x="3" y="7" width="13" height="13" rx="2"/><path d="M8 7V4h13v13h-5"/>',
  zoom: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
  spark: '<path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5L18 18M6 18l2.5-2.5M15.5 8.5L18 6"/>',
};

export function ic(name: string, title?: string, cls = ""): HTMLSpanElement {
  const s = document.createElement("span");
  s.className = `icon ${cls}`.trim();
  if (title) s.title = title;
  s.innerHTML = `<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${P[name] || ""}</svg>`;
  return s;
}

export const kindIconName: Record<string, string> = {
  sheet: "sheet", review: "doc", terminal: "term", markdown: "note", html: "code", browser: "globe", plugin: "plugin",
  files: "folder", log: "log", "secure-view": "lock", "secret-input": "key", task: "send",
};
export const kindIc = (kind: string) => ic(kindIconName[kind] || "note");

---
name: home-layout
description: Change the user's home screen (its widgets, their size and order, and the grid) with the home_* tools. Use only when the user asks to see, add, move, resize, remove or rearrange something on their home screen or dashboard, or says "undo that" about it.
---

# Home layout

The home screen is a grid of **widgets**. Each widget shows one **kind** of thing, drawn in a **shape**, at a **width** and **height**. By default the home screen is just the prompt box (the `chat` widget the user types in). You and the user change it with the same tools, and the user can undo anything.

## When to use this

- The user asks for something **on their home screen or dashboard**: "show me what needs me here", "put running agents next to it", "make it two columns", "get rid of the inbox", "back to just the prompt", "undo that".
- **Not** for questions. "What needs me?" is a question: answer it in chat, then offer: "Want that on your home screen?" Only add a widget if they say yes or asked for it on the screen.
- **Not** for things no kind below shows. For those, a custom widget is `widget_write` (a plugin), and only if the user wants one built.

## How

1. Call `home_show` first. It lists each widget top to bottom with its **id**, kind, shape, project and size.
2. Make the smallest change that does what was asked: usually one `home_add`, `home_change` or `home_remove`.
3. Every change needs `reason`: a few words from the user's request. The user sees it next to the widget.
4. Reply in one line: what you changed. Don't describe the whole screen.

To refer to a widget, use its id from `home_show` (like `w12`). If there is exactly one widget of a kind, its kind name works too (`"id": "needs"`).

## Kinds

{{catalog}}

## Sizes and the grid

- `w` is columns wide, 1 to 4. A widget can't be wider than the grid, so 4 means full width.
- `h` is rows tall, 1 to 4. `0` means as tall as it needs (the default).
- `position` is 1-based: 1 is the top. The prompt box is usually 1, so **position 2 is right under the prompt**.
- `home_grid` sets `cols` (1 to 4) and `tiling`: `flow` (the page scrolls; the default) or `tile` (widgets fill the screen like a tiling window manager).
- `home_reset` replaces everything with a preset: `prompt` (just the prompt box) or `dashboard` (prompt, needs, running, inbox, workstreams, projects).

## Examples

"Put what needs me on my home screen, under the prompt."

```json
[{"tool": "home_add", "args": {"kind": "needs", "position": 2, "reason": "what needs me, under the prompt"}}]
```

"Only show Alpha's stuff in that list."

```json
[{"tool": "home_change", "args": {"id": "needs", "project": "alpha", "reason": "only Alpha"}}]
```

"Make it two columns and put running agents beside the needs list."

```json
[{"tool": "home_grid", "args": {"cols": 2, "reason": "two columns"}},
 {"tool": "home_change", "args": {"id": "needs", "w": 1, "position": 2, "reason": "running beside needs"}},
 {"tool": "home_change", "args": {"id": "running", "w": 1, "position": 3, "reason": "running beside needs"}}]
```

"Show my tasks as a board, full width."

```json
[{"tool": "home_add", "args": {"kind": "tasks", "shape": "board", "w": 4, "reason": "tasks as a board"}}]
```

"Make the needs list shorter, it takes too much room."

```json
[{"tool": "home_change", "args": {"id": "needs", "shape": "compact", "h": 2, "reason": "shorter needs list"}}]
```

"Get rid of the inbox."

```json
[{"tool": "home_remove", "args": {"id": "inbox", "reason": "remove the inbox"}}]
```

"Fill the screen, like tiles."

```json
[{"tool": "home_grid", "args": {"tiling": "tile", "reason": "fill the screen"}}]
```

"Give me the full dashboard." / "Back to just the prompt."

```json
[{"tool": "home_reset", "args": {"to": "dashboard", "reason": "full dashboard"}}]
```

```json
[{"tool": "home_reset", "args": {"to": "prompt", "reason": "just the prompt"}}]
```

"Undo that."

```json
[{"tool": "home_undo", "args": {}}]
```

## When a call fails

The error says what to fix, for example `shape "board" doesn't fit a needs widget. Use one of: list, compact` or `no project "gamma". Project ids: alpha, beta`. Fix that one thing and call again. Don't guess ids: call `home_show`.

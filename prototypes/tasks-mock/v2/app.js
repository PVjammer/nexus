// Nexus desktop mock v2. One store; every box on screen is a widget = data + scope + shape.
// Spaces (home, a project, a work item's workspace) are layouts of widgets. The Tasks app is
// a set of default views made of the same widgets. Throwaway: vanilla JS, mock data only.
(function () {
  'use strict';

  let S;
  let RENDERED = {};
  const ui = { space: 'home', page: null, drawer: null, from: null, after: null, modal: null, confirm: null, pop: null, menu: null, edit: false, more: {}, chats: null, focusTask: null, appScope: 'all' };

  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const T = (id) => S.tasks.find((t) => t.id === id);
  const W = (id) => S.items.find((w) => w.id === id) || S.backlog.find((w) => w.id === id);
  const P = (id) => S.projects.find((p) => p.id === id);
  const projOf = (t) => (t.wi ? W(t.wi).project : t.project);
  const keyOf = (t) => (t.wi ? W(t.wi).key : 'Personal');
  const mins = (n) => (n >= 60 ? `${Math.floor(n / 60)}h ${n % 60 ? (n % 60) + 'm' : ''}`.trim() : `${n}m`);
  const clockShort = () => S.clock.replace('Mon Nov 16, ', 'Mon ');

  // ---------- the board underneath: same rules as v1 ----------
  const live = (t) => !t.wi || (W(t.wi) && W(t.wi).inProgress);
  function needsMe(t) {
    if (t.status === 'done' || !live(t)) return false;
    if (t.assignee === 'you' && (t.status === 'todo' || t.status === 'doing')) return t.wi ? true : (t.dueIn != null && t.dueIn <= 2);
    if (t.status === 'review' && t.reviewer === 'you') return true;
    if (t.live && t.assignee === 'agent' && t.status === 'todo') return true;
    return false;
  }
  const agentWorking = (t) => live(t) && t.assignee === 'agent' && t.status === 'doing';
  function waitingOthers(t) {
    if (!live(t) || t.status === 'done' || t.status === 'blocked') return false;
    if (t.status === 'review') return !!t.reviewer && t.reviewer !== 'you';
    return t.assignee !== 'you' && t.assignee !== 'agent';
  }
  const waitWho = (t) => (t.status === 'review' ? t.reviewer : t.assignee);
  const met = (c) => c.by.every((id) => T(id) && T(id).status === 'done');
  const wiTasks = (wi) => S.tasks.filter((t) => t.wi === wi);
  function move(wi) {
    const ts = wiTasks(wi); const w = W(wi);
    const mine = ts.filter(needsMe);
    if (w.closed) return { kind: 'done', label: 'Closed' };
    if (!w.inProgress) return { kind: 'ready', label: 'Parked' };
    if (w.complete) return { kind: 'you', label: 'Your move', n: mine.length, note: 'ready to close' };
    if (mine.length) return { kind: 'you', label: 'Your move', n: mine.length };
    const ag = ts.filter(agentWorking);
    if (ag.length) return { kind: 'agent', label: 'Agent working', note: ag[0].title };
    const wt = ts.filter(waitingOthers);
    if (wt.length) return { kind: 'wait', label: `Their move: ${waitWho(wt[0])}`, note: wt[0].waitDays != null ? `${wt[0].waitDays}d` : 'new' };
    return { kind: 'ready', label: 'Ready' };
  }
  function nextAction(wi) {
    const ts = wiTasks(wi);
    return ts.find(needsMe) || ts.find(agentWorking) || ts.find(waitingOthers) || ts.find((t) => t.status === 'blocked');
  }
  function needsList() {
    const all = S.tasks.filter(needsMe);
    const rank = (t) => { const i = S.plan.order.indexOf(t.id); return i < 0 ? 999 : i; };
    return all.sort((a, b) => rank(a) - rank(b));
  }
  function planList() {
    const ids = S.plan.order.filter((id) => T(id) && needsMe(T(id)) && !S.plan.deferred.includes(id));
    const extra = S.tasks.filter((t) => needsMe(t) && !S.plan.order.includes(t.id) && !S.plan.deferred.includes(t.id)).map((t) => t.id);
    return ids.concat(extra).map(T);
  }

  // ---------- mutations ----------
  function feed(who, text, why, wi) { S.feed.unshift({ when: clockShort(), who, text, why, wi, fresh: true }); }
  function toast(text, undo) {
    const id = ++S.seq; S.toasts.push({ id, text, undo: !!undo });
    setTimeout(() => { S.toasts = S.toasts.filter((x) => x.id !== id); render(); }, undo ? 9000 : 4500);
  }
  function flash(id) { S.flash.push(id); setTimeout(() => { S.flash = S.flash.filter((x) => x !== id); render(); }, 1800); }
  const statusLabel = { todo: 'To do', doing: 'In progress', blocked: 'Blocked', review: 'In review', done: 'Done' };
  function setStatus(t, status, who, why) {
    const from = t.status; t.status = status; flash(t.id);
    feed(who, `"${t.title}": ${statusLabel[from]} → ${statusLabel[status]}`, why, t.wi);
  }
  function settle() {
    let changed = true;
    while (changed) {
      changed = false;
      for (const t of S.tasks) {
        if (t.status !== 'blocked' || !t.blockedBy || !t.blockedBy.every((id) => T(id).status === 'done')) continue;
        changed = true;
        if (t.unblockTo) { setStatus(t, t.unblockTo, 'board', 'What it waited on is done.'); t.waitDays = 0; }
        else if (t.assignee === 'agent' && t.live) { setStatus(t, 'todo', 'board', 'Unblocked. It uses your credentials on a live system, so it waits for your start.'); }
        else if (t.assignee === 'agent') { setStatus(t, 'doing', 'agent', 'Unblocked and stays in its own environment, so it started on its own.'); }
        else setStatus(t, 'todo', 'board', 'What it waited on is done.');
      }
    }
    for (const w of S.items) {
      if (!w.inProgress && w.wakeOn && T(w.wakeOn).status === 'done') {
        w.inProgress = true; w.wakeOn = null; w.parkNote = null;
        feed('rule', `${w.key} is back in progress`, 'The thing it was parked on happened.', w.id);
        toast(`${w.key} is back: what it was waiting on happened`);
      }
      if (!w.complete && !w.closed && w.doneWhen.length && w.doneWhen.every(met)) {
        w.complete = true;
        const id = 'close-' + w.id;
        if (!T(id)) S.tasks.push({ id, wi: w.id, title: `Close ${w.key} in ${w.source}`, assignee: 'you', status: 'todo', est: 1, type: 'close', why: 'Every done-when item is met' });
        feed('board', `${w.key}: every done-when item is met`, 'Added a task to close it in the source.', w.id);
        toast(`${w.key} is done. Closing it in ${w.source} is one press.`);
      }
    }
  }
  function addTask(t) { t.id = t.id || 'x' + (++S.seq); S.tasks.push(t); flash(t.id); return t; }
  const waitingOthersRaw = (t) => t.status !== 'done' && t.status !== 'blocked' && ((t.status === 'review' && t.reviewer && t.reviewer !== 'you') || (t.assignee !== 'you' && t.assignee !== 'agent'));
  function ensureInOrder(id) { if (!S.plan.order.includes(id)) S.plan.order.push(id); }
  const anyAgentTask = () => S.tasks.find((t) => agentWorking(t) && !['j6', 'n7', 'hotel'].includes(t.id) && !t.took);

  // ---------- scopes: a widget shows everything, one project, or one work item ----------
  const ctxOf = (sp) => (sp === 'home' ? 'all' : sp);
  const scopeOf = (wd, ctx) => (wd.scope && wd.scope !== 'inherit' ? wd.scope : ctx);
  const inT = (sc) => (t) => sc === 'all' || (sc.startsWith('p:') ? projOf(t) === sc.slice(2) : t.wi === sc.slice(3));
  const inW = (sc) => (w) => sc === 'all' || (sc.startsWith('p:') ? w.project === sc.slice(2) : w.id === sc.slice(3));
  const inF = (sc) => (f) => sc === 'all' || (sc.startsWith('p:') ? (f.wi && W(f.wi) ? W(f.wi).project : 'personal') === sc.slice(2) : f.wi === sc.slice(3));
  const scopeName = (sc) => (sc === 'all' ? 'Everything' : sc.startsWith('p:') ? P(sc.slice(2)).name : W(sc.slice(3)).key);
  const spaceName = (sp) => (sp === 'home' ? 'Home' : sp.startsWith('p:') ? P(sp.slice(2)).name : `${W(sp.slice(3)).key} · ${W(sp.slice(3)).title}`);
  const liveOrScoped = (sc) => (t) => sc.startsWith('wi:') || live(t);

  // ---------- data: named queries over the one store ----------
  const Q = {
    needs: { label: 'Needs you', kind: 'task', shapes: ['list', 'compact', 'board'], rule: 'tasks assigned to you that aren\'t blocked, plus reviews and starts waiting on you', empty: 'Nothing needs you here.', run: (sc) => needsList().filter(inT(sc)) },
    today: { label: 'Today', kind: 'task', shapes: ['agenda', 'list', 'compact'], rule: 'what needs you, in the order you set. "Not today" hides a task until tomorrow', empty: 'Nothing planned.', run: (sc) => planList().filter(inT(sc)) },
    agents: { label: 'Agents working', kind: 'task', shapes: ['compact', 'list', 'board'], rule: 'tasks assigned to an agent and in progress', empty: 'No agent is working here.', run: (sc) => S.tasks.filter((t) => agentWorking(t) && inT(sc)(t)) },
    waiting: { label: 'Waiting on others', kind: 'task', shapes: ['compact', 'list'], rule: 'tasks with someone else, or in review with someone else', empty: 'Nobody else has the ball.', run: (sc) => S.tasks.filter((t) => waitingOthers(t) && inT(sc)(t)) },
    todos: { label: 'Todos', kind: 'task', shapes: ['todos', 'compact', 'list'], rule: 'tasks with no work item that aren\'t done', empty: 'No todos.', run: (sc) => S.tasks.filter((t) => !t.wi && t.status !== 'done' && inT(sc)(t)) },
    left: { label: 'What\'s left', kind: 'task', shapes: ['groups', 'board', 'list'], rule: 'every task that isn\'t done, grouped by whose move it is', empty: 'Nothing left.', run: (sc) => S.tasks.filter((t) => t.status !== 'done' && liveOrScoped(sc)(t) && inT(sc)(t)) },
    all: { label: 'All tasks', kind: 'task', shapes: ['board', 'list'], rule: 'every task in work items that are in progress, plus todos', empty: 'No tasks.', run: (sc) => S.tasks.filter((t) => liveOrScoped(sc)(t) && inT(sc)(t)) },
    items: { label: 'Work items in progress', kind: 'wi', shapes: ['cards', 'wlist'], rule: 'work items you brought into scope', empty: 'No work items in progress.', run: (sc) => S.items.filter((w) => w.inProgress && inW(sc)(w)) },
    upnext: { label: 'Not in progress', kind: 'wi', shapes: ['wlist', 'cards'], rule: 'parked work items, plus the backlog from your trackers (Nexus reads them; it doesn\'t own them)', empty: 'Nothing waiting to start.', run: (sc) => S.items.filter((w) => !w.inProgress && !w.closed).concat(S.backlog).filter(inW(sc)) },
    since: { label: 'Since you were here', kind: 'event', shapes: ['feed'], rule: 'every card that moved since you last looked, and why', empty: 'Nothing moved.', run: (sc) => S.feed.filter(inF(sc)) },
  };
  // App-provided widgets: content rather than a query. They still take a scope (a work item).
  const X = {
    donewhen: { label: 'Done when', rule: 'the work item\'s done-when list, ticked as its tasks finish' },
    thread: { label: 'Thread', rule: 'comments from the source, next to the tasks the watcher made from them' },
    doc: { label: 'Working doc', rule: 'the temporary wiki the agent keeps for this work item' },
    mr: { label: 'Merge request', rule: 'the MR in this work item, with its checks and diff' },
    build: { label: 'Build progress', rule: 'the steps of the agent\'s build, live' },
    design: { label: 'Design doc', rule: 'the design doc and the open question on it' },
    chat: { label: 'Agent', any: true, shapes: ['minimal', 'conversation'], rule: 'a conversation with the agent: the home agent on Home, the project agent in a project, the session in a workspace' },
  };
  const SHAPE_NAME = { list: 'List', compact: 'Compact', board: 'Board', agenda: 'Agenda', groups: 'Grouped by whose move', todos: 'Todo list', cards: 'Cards', wlist: 'List', feed: 'Feed', minimal: 'Minimal', conversation: 'Conversation' };

  // ---------- layouts: every space is a list of widgets ----------
  function mkW(o) { return Object.assign({ id: 'w' + (++S.seq), scope: 'inherit', size: 1, h: 'auto', by: 'you', when: clockShort() }, o); }
  function template(sp) {
    const L = (arr) => arr.map((o) => mkW(Object.assign({ by: 'default' }, o)));
    const chatTop = { q: 'chat', shape: 'minimal', size: 3 };
    if (sp === 'home') return L([chatTop]);
    if (sp === 'p:personal') return L([chatTop, { q: 'needs', shape: 'list', size: 2 }, { q: 'todos', shape: 'todos', size: 1 }]);
    if (sp.startsWith('p:')) return L([chatTop, { q: 'needs', shape: 'list', size: 2 }, { q: 'since', shape: 'feed', size: 1 }, { q: 'items', shape: 'cards', size: 2 }, { q: 'upnext', shape: 'wlist', size: 1 }, { q: 'waiting', shape: 'compact', size: 1 }, { q: 'agents', shape: 'compact', size: 1 }]);
    const id = sp.slice(3);
    const ag = (o, why) => Object.assign(o, { by: 'agent', why });
    const extra = {
      pantry14: [ag({ q: 'mr', size: 2 }, 'The agent opened it: MR !23 is what\'s waiting on you.'), { q: 'donewhen', size: 1 }],
      nex12: [ag({ q: 'design', size: 1 }, 'The agent opened it: the platform lead asked a question on section 3.'), ag({ q: 'build', size: 1 }, 'The agent opened it: the plugin build is running.'), { q: 'donewhen', size: 1 }],
      jobeval: [ag({ q: 'doc', size: 2 }, 'The agent opened it: 3 sections changed since you last read it.'), { q: 'donewhen', size: 1 }],
      sec431: [ag({ q: 'thread', size: 2 }, 'The agent opened it: security replied on Friday.'), { q: 'donewhen', size: 1 }, ag({ q: 'mr', size: 2 }, 'The agent opened it: MR !41 answers round 2, ask 1.'), { q: 'since', shape: 'feed', size: 1 }],
    }[id] || [{ q: 'donewhen', size: 1 }, { q: 'since', shape: 'feed', size: 2 }];
    return L([{ q: 'left', shape: 'groups', size: 2 }, { q: 'chat', shape: 'conversation', size: 1 }].concat(extra));
  }
  function cfg(sp) { return S.spaceCfg[sp] || (S.spaceCfg[sp] = { cols: 3, tiling: 'flow' }); }
  function layoutFor(sp) { if (!S.layouts[sp]) S.layouts[sp] = template(sp); return S.layouts[sp]; }
  function findW(id) {
    for (const sp in S.layouts) { const i = S.layouts[sp].findIndex((w) => w.id === id); if (i >= 0) return { sp, arr: S.layouts[sp], i, wd: S.layouts[sp][i] }; }
    return null;
  }
  function snapshot(sp) { S.undo = { sp, layout: JSON.parse(JSON.stringify(layoutFor(sp))) }; }

  // ---------- the agent arranges with the same parts ----------
  function say(sp, who, html) { (S.chat[sp] = S.chat[sp] || []).push({ who, html, when: clockShort() }); }
  function shortLeft(id, sp) {
    const w = W(id); const ts = wiTasks(id).filter((t) => t.status !== 'done');
    const metN = w.doneWhen.filter(met).length;
    const line = (lab, arr, f) => (arr.length ? `<div class="sl"><span class="sl-k">${lab}</span><span>${arr.map(f).join(' · ')}</span></div>` : '');
    return `<strong>${esc(w.key)}: ${metN} of ${w.doneWhen.length} done-when met.</strong>
      ${line('You', ts.filter(needsMe), (t) => `<button class="link" data-act="open-task" data-id="${t.id}">${esc(t.title)}</button>${t.est ? ` <span class="muted">${mins(t.est)}</span>` : ''}`)}
      ${line('Agent', ts.filter(agentWorking), (t) => `${esc(t.title)}${t.progress ? ` <span class="muted">(${esc(t.progress)})</span>` : ''}`)}
      ${line('Waiting', ts.filter(waitingOthers), (t) => `${esc(waitWho(t))}${t.waitDays != null ? ` <span class="muted">${t.waitDays}d</span>` : ''}`)}
      ${line('After', ts.filter((t) => t.status === 'blocked'), (t) => esc(t.title))}
      ${sp.startsWith('wi:') ? '' : `<div class="chips"><button class="pill" data-act="open-wi" data-id="${id}">Open the workspace</button><button class="pill" data-act="pin-left" data-id="${id}" data-a="${sp}">Pin this as a widget</button></div>`}`;
  }
  const CLASSIC = [{ q: 'needs', shape: 'list', size: 2 }, { q: 'since', shape: 'feed', size: 1 }, { q: 'items', shape: 'cards', size: 2 }, { q: 'upnext', shape: 'wlist', size: 1 }, { q: 'todos', shape: 'todos', size: 1 }, { q: 'agents', shape: 'compact', size: 1 }, { q: 'waiting', shape: 'compact', size: 1 }];
  function place(sp, wd) { const lay = layoutFor(sp); lay.splice(lay[0] && lay[0].q === 'chat' ? 1 : 0, 0, wd); return wd; }
  const hasQ = (sp, q, scope) => layoutFor(sp).some((w) => w.q === q && (!scope || w.scope === scope));
  const ASKS = [
    { key: 'needs', where: (sp) => !sp.startsWith('wi:'), label: () => 'What needs me?', run(sp) {
      const l = needsList().filter(inT(ctxOf(sp))); const total = l.reduce((a, t) => a + (t.est || 0), 0);
      say(sp, 'agent', `<strong>${l.length} things, about ${mins(total)}.</strong>${l.slice(0, 6).map((t) => `<div class="sl"><span class="sl-k">${esc(keyOf(t))}</span><span><button class="link" data-act="open-task" data-id="${t.id}">${esc(t.title)}</button></span></div>`).join('')}${hasQ(sp, 'needs') ? '' : `<div class="chips"><button class="pill" data-act="pin-q" data-id="needs" data-a="${sp}">Keep this on screen</button></div>`}`);
    } },
    { key: 'plan', where: (sp) => sp === 'home', label: () => 'Plan my day', run(sp) {
      if (hasQ(sp, 'today')) return say(sp, 'agent', 'Today is already on your home.');
      snapshot(sp); const l = planList();
      place(sp, mkW({ q: 'today', shape: 'agenda', size: 2, by: 'agent', why: 'You asked: "Plan my day."' }));
      say(sp, 'agent', `I put <strong>Today</strong> under this box: ${l.length} things, about ${mins(l.reduce((a, t) => a + (t.est || 0), 0))}. Security and the platform lead are waiting on you, so theirs come first. Reorder it or press Not today; pick any one to start.`);
      toast('The agent added Today', true);
    } },
    { key: 'left', where: (sp) => sp === 'home' || sp === 'p:nexus', label: () => 'What\'s left to close NEX-12?', run(sp) { say(sp, 'agent', shortLeft('nex12', sp)); } },
    { key: 'usual', where: (sp) => sp === 'home' && CLASSIC.some((o) => !hasQ(sp, o.q)), label: () => 'Set up my usual dashboard', run(sp) {
      snapshot(sp); const add = CLASSIC.filter((o) => !hasQ(sp, o.q));
      S.layouts[sp] = layoutFor(sp).concat(add.map((o) => mkW(Object.assign({}, o, { by: 'agent', why: 'You asked for your usual dashboard.' }))));
      say(sp, 'agent', `Added ${add.length} widgets under what you already had: ${add.map((o) => Q[o.q].label).join(', ')}. Customize manually to move things around.`);
      toast('The agent set up your dashboard', true);
    } },
    { key: 'sec', where: (sp) => sp === 'home', label: () => 'Keep SEC-431\'s back-and-forth on my home', run(sp) {
      if (hasQ(sp, 'thread', 'wi:sec431')) return say(sp, 'agent', 'It\'s already on your home.');
      snapshot(sp); place(sp, mkW({ q: 'thread', scope: 'wi:sec431', size: 2, by: 'agent', why: 'You asked to keep SEC-431\'s back-and-forth on your home.' }));
      say(sp, 'agent', 'Added the SEC-431 thread under this box. It\'s the same widget as in the workspace, scoped to SEC-431.');
      toast('The agent added the SEC-431 thread', true);
    } },
    { key: 'trim', where: (sp) => sp === 'home' && layoutFor(sp).length > 2, label: () => 'Clear my home back to this box', run(sp) {
      snapshot(sp); const chat = layoutFor(sp).find((w) => w.q === 'chat') || mkW({ q: 'chat', shape: 'minimal', size: 3, by: 'agent' });
      S.layouts[sp] = [chat];
      say(sp, 'agent', 'Your home is back to just this box. Everything else is in the Tasks app, or one ask away.');
      toast('The agent cleared your home', true);
    } },
    { key: 'wsleft', where: (sp) => sp.startsWith('wi:'), label: () => 'What\'s left?', run(sp) { say(sp, 'agent', shortLeft(sp.slice(3), sp)); } },
    { key: 'wsboard', where: (sp) => sp.startsWith('wi:'), label: () => 'Show the tasks as a board', run(sp) {
      const l = layoutFor(sp).find((w) => w.q === 'left');
      if (!l || l.shape === 'board') return say(sp, 'agent', 'They\'re already a board.');
      snapshot(sp); l.shape = 'board'; l.size = 3; l.by = 'agent'; l.why = 'You asked to see the tasks as a board.';
      say(sp, 'agent', 'Switched What\'s left to a board. Same tasks, same rules. Undo, or ⋯ → Grouped, turns it back.');
      toast('The agent changed What\'s left to a board', true);
    } },
  ];

  // ---------- actions ----------
  const act = {
    // navigation
    go: (sp) => { ui.focusTask = null; ui.chats = null; ui.space = sp; ui.page = null; ui.drawer = null; ui.after = null; ui.pop = null; ui.edit = false; try { window.scrollTo(0, 0); } catch (e) { /* no scroll */ } },
    'open-wi': (id) => { if (!S.items.find((w) => w.id === id)) return act['start-wi'](id); act.go('wi:' + id); },
    'open-ws': (id) => act['open-wi'](id),
    'open-app': (v) => { ui.page = { kind: 'app', view: v || 'needs' }; ui.drawer = null; ui.after = null; ui.pop = null; ui.edit = false; },
    'open-doc': (id) => { ui.page = { kind: 'full', wd: { id: 'full-doc', q: 'doc', scope: 'wi:' + id, size: 3, by: 'app' }, ctx: 'wi:' + id }; ui.drawer = null; },
    'close-page': () => { ui.page = null; },
    back: () => { ui.page = null; },
    pop: (which) => { ui.pop = ui.pop === which ? null : which; ui.menu = null; },
    sim: () => { ui.pop = ui.pop === 'sim' ? null : 'sim'; },
    // task panel
    'open-task': (id, from) => { ui.chats = null; ui.drawer = id; ui.after = null; ui.confirm = null; ui.from = from && RENDERED[from] ? RENDERED[from] : null; },
    'close-drawer': () => { ui.drawer = null; ui.after = null; ui.confirm = null; },
    approve: (id) => {
      const t = T(id);
      (t.packet && t.packet.followups || []).forEach((f) => {
        if (f.choice === 'start') addTask({ wi: t.wi, title: f.text, assignee: 'agent', status: 'doing', type: 'agent', progress: 'just started', why: 'Follow-up you started' });
        else if (f.choice !== 'drop') addTask({ wi: t.wi, title: f.text, assignee: 'agent', status: 'todo', type: 'agent', progress: 'queued', why: 'Follow-up you queued' });
      });
      setStatus(t, 'done', 'you', t.reviewer === 'you' ? 'You approved it.' : null);
      ui.drawer = null; settle(); toast(`Approved: ${t.title}`);
    },
    keep: (id) => {
      const t = T(id); const el = document.getElementById('note-' + id); const note = el && el.value.trim();
      t.status = 'doing'; t.reviewer = t.reviewer === 'you' ? null : t.reviewer; t.assignee = 'agent'; t.progress = 'working on your note'; flash(id);
      feed('you', `Sent "${t.title}" back to the agent`, note ? `Your note: ${note}` : 'Keep working, same session and branch.', t.wi);
      ui.drawer = null; toast('Sent back to the same session with your note');
    },
    take: (id) => {
      const t = T(id); const w = t.wi && W(t.wi);
      t.took = true; t.assignee = 'you'; t.status = t.status === 'blocked' ? 'blocked' : 'doing'; t.reviewer = null; flash(id);
      feed('you', `You took "${t.title}"`, `The agent stopped at a clean point${w && w.ws ? ` and left branch ${w.ws.branch} and its notes` : ''}.`, t.wi);
      toast('It\'s yours. The agent stopped and left its branch and notes.');
    },
    handback: (id) => { const t = T(id); t.took = false; t.assignee = 'agent'; t.status = 'doing'; t.progress = 'picked up from your changes'; flash(id); feed('you', `Handed "${t.title}" back to the agent`, 'It picks up from your commits.', t.wi); ui.drawer = null; toast('Handed back'); },
    confirm: (id, a) => { ui.confirm = { act: a, id }; },
    'cancel-confirm': () => { ui.confirm = null; },
    'do-post-answer': (id) => { const t = T(id); ui.confirm = null; setStatus(t, 'done', 'you', `You posted your answer to ${t.postTo}.`); settle(); ui.drawer = null; toast(`Posted to ${t.postTo}, as you`); },
    'save-answer': (id) => { const t = T(id); setStatus(t, 'done', 'you', 'Answer saved. It goes out with the SEC-431 reply.'); settle(); ui.drawer = null; toast('Answer saved for the SEC-431 reply'); },
    'do-post': (id) => { const t = T(id); ui.confirm = null; setStatus(t, 'done', 'you', 'You posted both replies to SEC-431.'); settle(); const w = W(t.wi); w.rounds[w.rounds.length - 1].closed = true; ui.drawer = null; toast('Posted to SEC-431. Their move now.'); },
    'do-nudge': (id) => { const t = T(id); ui.confirm = null; t.nudged = true; t.nudge = null; feed('you', 'Sent the nudge on IT-882', null, t.wi); toast('Nudge sent, as you'); },
    'mark-done': (id) => { const t = T(id); setStatus(t, 'done', 'you', t.assignee === 'you' ? 'You marked it done.' : `You marked it done for ${t.assignee}.`); settle(); ui.drawer = null; },
    'start-agent': (id) => { const t = T(id); t.progress = 'just started'; setStatus(t, 'doing', 'you', 'You started it.'); ui.drawer = null; },
    'start-live': (id) => { const t = T(id); setStatus(t, 'doing', 'you', 'You started it. It uses your Jira credentials.'); t.progress = 'running against live Jira'; ui.drawer = null; toast('Started with your credentials'); },
    'do-land': (id) => { const t = T(id); ui.confirm = null; setStatus(t, 'done', 'you', 'You landed it: merged and deployed.'); settle(); ui.drawer = null; toast('Landed: MR !23 merged and deployed'); },
    'do-close': (id) => { const t = T(id); const w = W(t.wi); ui.confirm = null; setStatus(t, 'done', 'you', `You closed ${w.key} in ${w.source}.`); w.closed = true; w.inProgress = false; if (w.ws) w.ws.state = 'closed'; ui.drawer = null; toast(`${w.key} closed. One fewer thread.`); },
    'mark-reviewed': (id) => { act.approve(id); },
    comment: (id) => {
      const t = T(id); const el = document.getElementById('note-' + id); const note = el && el.value.trim();
      if (!note) { toast('Write the comment first'); return; }
      addTask({ wi: t.wi, title: `Address your comment: "${note.slice(0, 48)}${note.length > 48 ? '…' : ''}"`, assignee: 'agent', status: 'doing', type: 'agent', progress: 'just started' });
      feed('you', 'Commented on the working doc', `The agent took it as a task: ${note}`, t.wi); el.value = ''; toast('The agent is on it');
    },
    'found-problem': (id) => {
      const t = T(id); const el = document.getElementById('note-' + id); const note = (el && el.value.trim()) || 'Export button overlaps the menu on phones';
      const fix = addTask({ wi: t.wi, title: `Fix: ${note}`, assignee: 'agent', status: 'doing', type: 'agent', progress: 'just started' });
      t.blockedBy = [fix.id]; t.status = 'blocked';
      const land = S.tasks.find((x) => x.wi === t.wi && x.type === 'land'); if (land && land.status !== 'done') { land.status = 'blocked'; land.blockedBy = [fix.id]; }
      feed('you', 'Found a problem on the phone', `New agent task: ${note}. The check and the merge wait for it.`, t.wi); ui.drawer = null; toast('New task for the agent; the merge waits for the fix');
    },
    decide: (id) => {
      const t = T(id); const el = document.getElementById('lean-' + id); const lean = el && el.value;
      if (!lean) { toast('Pick a lean first'); return; }
      t.lean = lean; setStatus(t, 'done', 'you', `Your call: ${lean}.`); settle(); ui.drawer = null; toast('Decided. The agent drafts the write-up from your call.');
    },
    'todo-done': (id) => { const t = T(id); setStatus(t, 'done', 'you', null); ui.drawer = null; },
    'todo-help': (id) => {
      const t = T(id); t.helped = true;
      addTask({ id: 'hotel', wi: null, project: 'personal', title: t.help, assignee: 'agent', status: 'doing', type: 'agent', progress: 'searching', why: 'Helping with: ' + t.title, forTask: id });
      feed('you', `Asked the agent: ${t.help}`, 'It searches and reports options; booking stays with you.', null); toast('The agent is searching. Booking stays with you.');
    },
    snooze: (id) => { const t = T(id); t.dueIn = 3; t.due = 'Thu'; flash(id); toast('Moved to Thursday'); ui.drawer = null; },
    'add-todo': (wid) => {
      const inp = document.getElementById('todo-text-' + wid); const due = document.getElementById('todo-due-' + wid);
      const text = inp && inp.value.trim(); if (!text) return;
      const d = due ? due.value : '';
      const map = { today: [0, 'Today'], tomorrow: [1, 'Tue'], week: [4, 'Fri'] };
      addTask({ wi: null, project: 'personal', title: text, assignee: 'you', status: 'todo', est: 10, type: 'todo', dueIn: map[d] ? map[d][0] : null, due: map[d] ? map[d][1] : null });
      feed('you', `Added a todo: ${text}`, 'No work item. It shows in Needs you when it is due soon.', null);
      inp.value = ''; toast(map[d] && map[d][0] <= 2 ? 'Added. It\'s in Needs you.' : 'Added to your todos');
    },
    // work items: into and out of scope, from anywhere they appear
    park: (id) => { const w = W(id); w.inProgress = false; if (w.ws && w.ws.state !== 'closed') w.ws.state = 'suspended'; const wt = wiTasks(id).find(waitingOthersRaw); w.wakeOn = wt ? wt.id : null; w.parkNote = wt ? `wakes when "${wt.title}" is done` : 'parked until you bring it back'; feed('you', `Parked ${w.key}`, w.parkNote, id); ui.drawer = null; toast(`${w.key} parked. Its tasks are hidden from your lists, not deleted.`); },
    unpark: (id) => { const w = W(id); w.inProgress = true; w.wakeOn = null; w.parkNote = null; feed('you', `Brought ${w.key} back`, null, id); toast(`${w.key} is back in progress`); },
    'resume-ws': (id) => { const w = W(id); w.ws.state = 'resumed'; feed('you', `Resumed the ${w.key} session`, 'Same branch and transcript.', id); toast(`${w.key} session resumed where it stopped`); },
    'suspend-ws': (id) => { const w = W(id); w.ws.state = 'suspended'; w.ws.since = clockShort(); w.ws.whileAway = []; feed('you', `Suspended the ${w.key} session`, 'Branch, notes and transcript kept; it resumes where it stopped.', id); toast(`${w.key} session suspended. Nothing lost.`); },
    'start-wi': (id) => { ui.modal = { kind: 'seed', id }; ui.pop = null; },
    'close-modal': () => { ui.modal = null; },
    'seed-start': (id, parkFirst) => {
      if (parkFirst) act.park(parkFirst);
      const b = S.backlog.find((x) => x.id === id);
      const boxes = Array.from(document.querySelectorAll('[data-seed]')).filter((c) => c.checked).map((c) => +c.getAttribute('data-seed'));
      S.backlog = S.backlog.filter((x) => x.id !== id);
      const w = { id: b.id, project: b.project, key: b.key, title: b.title, source: b.source, sourceRef: b.key, inProgress: true, scope: b.scope, doneWhen: [], ws: { state: 'running', branch: 'fm/' + b.id, since: 'just now', whileAway: [] } };
      S.items.push(w);
      let prev = null; const made = [];
      b.seed.forEach((s, i) => {
        if (!boxes.includes(i)) return;
        const t = addTask({ wi: w.id, title: s[0], assignee: s[1], status: prev ? 'blocked' : (s[1] === 'agent' ? 'doing' : 'todo'), blockedBy: prev ? [prev] : undefined, est: 15, type: s[1] === 'agent' ? 'agent' : 'check', progress: 'just started' });
        prev = t.id; made.push(t.id);
      });
      w.doneWhen = b.doneWhen.map((text, i) => ({ text, by: [made[Math.min(i + 1, made.length - 1)] || made[0]] }));
      feed('you', `Started ${w.key}: ${w.title}`, `Workspace created with the scope, done-when and ${made.length} tasks.`, w.id);
      ui.modal = null; toast(`${w.key} started. Its first task is underway.`);
    },
    'toggle-watcher': (id) => { const w = W(id); w.watcher.on = !w.watcher.on; w.watcher.log.unshift(`${clockShort()}  ${w.watcher.on ? 'resumed' : 'paused'} by you`); toast(w.watcher.on ? 'Watcher resumed' : 'Watcher paused. Nothing checks SEC-431 until you resume it.'); },
    'fix-split': () => { toast('In the real thing: tell the agent what it missed, and it redoes the split'); },
    fu: (id, idx, choice) => { T(id).packet.followups[+idx].choice = choice; },
    'plan-up': (id) => { const o = S.plan.order; ensureInOrder(id); const i = o.indexOf(id); if (i > 0) { o.splice(i, 1); o.splice(i - 1, 0, id); } },
    'plan-down': (id) => { const o = S.plan.order; ensureInOrder(id); const i = o.indexOf(id); if (i < o.length - 1) { o.splice(i, 1); o.splice(i + 1, 0, id); } },
    'plan-defer': (id) => { S.plan.deferred.push(id); toast('Not today. It stays on the board.'); },
    'plan-undefer': (id) => { S.plan.deferred = S.plan.deferred.filter((x) => x !== id); },
    more: (wid) => { ui.more[wid] = !ui.more[wid]; },
    // widgets
    'w-menu': (id) => { ui.menu = ui.menu === id ? null : id; },
    'w-full': (id) => { const r = RENDERED[id]; ui.menu = null; ui.page = { kind: 'full', wd: Object.assign({}, r.wd, { id: 'full-' + r.wd.id, size: 3 }), ctx: r.ctx, origin: r.wd.id }; },
    'w-shape': (id, shape) => { const r = RENDERED[id]; r.wd.shape = shape; ui.menu = null; },
    'w-why': (id) => { ui.menu = null; ui.modal = { kind: 'why', id }; },
    'w-remove': (id) => { const f = findW(id); if (!f) return; snapshot(f.sp); f.arr.splice(f.i, 1); ui.menu = null; ui.modal = null; toast('Removed from this space. Its data is untouched.', true); },
    'w-left': (id) => { const f = findW(id); if (f && f.i > 0) { f.arr.splice(f.i, 1); f.arr.splice(f.i - 1, 0, f.wd); } },
    'w-right': (id) => { const f = findW(id); if (f && f.i < f.arr.length - 1) { f.arr.splice(f.i, 1); f.arr.splice(f.i + 1, 0, f.wd); } },
    'w-wider': (id) => { const f = findW(id); if (f) f.wd.size = Math.min(cfg(f.sp).cols, (f.wd.size || 1) + 1); },
    'w-narrower': (id) => { const f = findW(id); if (f) f.wd.size = Math.max(1, Math.min(cfg(f.sp).cols, f.wd.size || 1) - 1); },
    'w-taller': (id) => { const f = findW(id); if (f) f.wd.h = f.wd.h === 'auto' || !f.wd.h ? 1 : Math.min(4, f.wd.h + 1); },
    'w-shorter': (id) => { const f = findW(id); if (f) f.wd.h = !f.wd.h || f.wd.h === 'auto' || f.wd.h <= 1 ? 'auto' : f.wd.h - 1; },
    'cfg-cols': (n) => { cfg(ui.space).cols = +n; },
    'cfg-tiling': (m) => { cfg(ui.space).tiling = m; },
    'w-pin': (id) => {
      const r = RENDERED[id]; const sc = scopeOf(r.wd, r.ctx); ui.menu = null;
      layoutFor('home').push(mkW({ q: r.wd.q, shape: r.wd.shape, size: r.wd.size === 3 ? 2 : r.wd.size, scope: sc === 'all' ? 'inherit' : sc, by: 'you' }));
      toast(`Pinned ${(Q[r.wd.q] || X[r.wd.q]).label}${sc !== 'all' ? ' (' + scopeName(sc) + ')' : ''} to Home`);
    },
    edit: () => { ui.edit = !ui.edit; ui.menu = null; },
    'reset-layout': () => { snapshot(ui.space); S.layouts[ui.space] = template(ui.space); toast('This space is back to its default layout', true); },
    'add-widget': () => { const ctx = ctxOf(ui.space); ui.modal = { kind: 'add', sp: ui.space, pick: null, shape: null, scope: 'inherit', ctx }; },
    'add-pick': (key) => {
      const m = ui.modal; m.pick = key; m.shape = (Q[key] || X[key]).shapes ? (Q[key] || X[key]).shapes[0] : null;
      if (X[key] && !X[key].any) m.scope = m.ctx.startsWith('wi:') ? 'inherit' : 'wi:' + (S.items.find((w) => w.inProgress) || S.items[0]).id;
      else if (m.scope !== 'inherit' && !m.scope.startsWith('p:') && m.scope !== 'all' && !m.ctx.startsWith('wi:')) m.scope = 'inherit';
    },
    'add-confirm': () => { const m = ui.modal; if (!m.pick) return; layoutFor(m.sp).push(mkW({ q: m.pick, shape: m.shape, scope: m.scope, size: m.pick === 'thread' || m.pick === 'doc' || m.shape === 'board' ? 2 : 1, by: 'you' })); ui.modal = null; toast('Added. ⋯ on it changes its shape, or removes it.'); },
    ask: (key, sp) => { const a = ASKS.find((x) => x.key === key); sp = sp || ui.space; say(sp, 'you', esc(a.label(sp))); a.run(sp); },
    'ask-text': (sp, form) => {
      const el = form ? form.querySelector('input') : document.getElementById(askId(sp)); const text = el && el.value.trim(); if (!text) return;
      say(sp, 'you', esc(text)); el.value = '';
      const hit = ASKS.find((a) => a.where(sp) && a.label(sp).toLowerCase().split(/\W+/).filter((x) => x.length > 3).some((w) => text.toLowerCase().includes(w)));
      if (hit) hit.run(sp); else say(sp, 'agent', 'In this mock I only know the suggestions. In Nexus I\'d answer here and arrange the space with the same widgets you can.');
    },
    'pin-left': (id, sp) => { snapshot(sp); layoutFor(sp).push(mkW({ q: 'left', scope: 'wi:' + id, shape: 'groups', size: 1, by: 'agent', why: `Pinned from "What's left to close ${W(id).key}?"` })); toast(`Pinned What's left (${W(id).key})`, true); },
    undo: () => { if (!S.undo) return; S.layouts[S.undo.sp] = S.undo.layout; S.undo = null; S.toasts = S.toasts.filter((x) => !x.undo); toast('Undone'); },
    'app-scope': () => {},
    'pin-q': (q, sp) => { if (hasQ(sp, q)) return; snapshot(sp); place(sp, mkW({ q, shape: Q[q].shapes[0], size: 2, by: 'agent', why: 'You asked to keep it on screen.' })); toast(`Added ${Q[q].label}`, true); },
    'open-in-ws': (id) => { const t = T(id); act.go('wi:' + t.wi); ui.focusTask = id; ui.from = null; },
    unfocus: () => { ui.focusTask = null; },
    chats: (id) => { ui.drawer = null; ui.after = null; ui.chats = id === 'list' ? { thread: null } : ui.chats ? null : { thread: null }; },
    'chats-thread': (tk) => { ui.drawer = null; ui.after = null; ui.chats = { thread: tk }; },
    'chats-close': () => { ui.chats = null; },
    'pin-chat': (tk) => { const sp = ui.space; place(sp, mkW({ q: 'chat', scope: tk === 'home' ? 'all' : tk, shape: 'conversation', size: 1, by: 'you' })); ui.chats = null; toast(`Put the conversation on ${spaceName(sp)}`); },
    reset: () => { S = fresh(); ui.space = 'home'; ui.page = null; ui.drawer = null; ui.after = null; ui.modal = null; ui.confirm = null; ui.edit = false; ui.pop = null; toast('Reset to Monday 08:30'); },
    run: (name) => { SIMS[name].run(); ui.pop = null; },
  };
  const askId = (sp) => 'ask-in-' + sp.replace(/[^a-z0-9]/gi, '-');
  // Actions that finish with a task. If you opened it from a list, the panel offers what's next in that list.
  const FINISH = new Set(['approve', 'keep', 'handback', 'do-post-answer', 'save-answer', 'do-post', 'mark-done', 'start-agent', 'start-live', 'do-land', 'do-close', 'mark-reviewed', 'decide', 'todo-done', 'snooze', 'found-problem']);

  // ---------- simulated events: the world moves, the cards follow ----------
  const SIMS = {
    spike: { label: 'The agent finishes the Temporal spike', ok: () => T('j6').status === 'doing' && T('j6').assignee === 'agent',
      run() { S.clock = 'Mon Nov 16, 10:40'; const t = T('j6'); t.reviewer = 'you'; setStatus(t, 'review', 'agent', 'Spike finished: output matched on 7 of 7 nights.'); t.est = 15; t.why = 'Agent finished at 10:40'; toast('Spike finished. It\'s in Needs you as a review.'); } },
    build: { label: 'The agent finishes the Jira plugin build', ok: () => T('n7').status === 'doing' && T('n7').assignee === 'agent',
      run() { S.clock = 'Mon Nov 16, 11:15'; const t = T('n7'); t.reviewer = 'you'; t.est = 25; t.stepsDone = 9; t.why = 'Agent finished at 11:15. You review before it goes to the team.'; t.approveLabel = 'Approve and open the MR for team review'; setStatus(t, 'review', 'agent', 'All 9 steps done, tests pass.'); toast('Plugin build finished. You review it before the team sees it.'); } },
    it: { label: 'IT grants Jira API access (IT-882 resolved)', ok: () => T('n2').status !== 'done',
      run() { S.clock = 'Mon Nov 16, 13:05'; setStatus(T('n2'), 'done', 'source', 'IT-882 was resolved in IT\'s tracker.'); settle(); toast('IT granted access. "Test on live Jira" is unblocked and waits for your start.'); } },
    lead: { label: 'The platform lead approves the design', ok: () => T('n5').status === 'review',
      run() { S.clock = 'Mon Nov 16, 14:20'; setStatus(T('n5'), 'done', 'source', 'Approved on the design doc.'); settle(); toast('Design approved'); } },
    sec3: { label: 'Security replies on SEC-431 (round 3)', ok: () => T('s7').status === 'doing', hint: 'Post your round 2 replies first',
      run() {
        S.clock = 'Mon Nov 16, 15:30'; const w = W('sec431');
        setStatus(T('s7'), 'done', 'watcher', 'Security replied on SEC-431.');
        const a = addTask({ wi: 'sec431', title: 'Add audit logging for admin actions', assignee: 'agent', status: 'doing', type: 'agent', progress: 'just started', why: 'From round 3, ask 1' });
        const b = addTask({ wi: 'sec431', title: 'Send security a sample of the audit log', assignee: 'you', status: 'blocked', blockedBy: [a.id], est: 5, type: 'check', why: 'From round 3, ask 2' });
        const c = addTask({ wi: 'sec431', title: 'Post the round 3 reply to SEC-431', assignee: 'you', status: 'blocked', blockedBy: [b.id], est: 3, type: 'post', why: 'Posting is visible to the security team, so you send it' });
        const d = addTask({ wi: 'sec431', title: 'Round 3 review', assignee: 'Security reviewer', status: 'blocked', blockedBy: [c.id], unblockTo: 'doing', type: 'waiting' });
        w.rounds.push({ n: 3, when: 'Mon Nov 16, 15:30', by: 'Security reviewer', closed: false, raw: 'Thanks, the rotation answer works and the pins look good. Last item: admin actions (user role changes, report deletion) need an audit log. Please enable it and send us a sample entry.', asks: [{ text: 'Enable audit logging for admin actions', task: a.id }, { text: 'Send a sample audit entry', task: b.id }] });
        w.doneWhen[2].by = [d.id];
        w.watcher.log.unshift('Mon 15:30  new comment from security, read as 2 asks');
        feed('watcher', 'New comment on SEC-431, read as 2 asks', 'One agent task (audit logging) started in its environment; one for you after it.', 'sec431');
        toast('Security replied. The watcher split it into 2 asks; the agent started the code change.');
      } },
    any: { label: 'The agent finishes another task', ok: () => !!anyAgentTask(), hint: 'No other agent task is running',
      run() {
        const t = anyAgentTask(); t.reviewer = 'you'; t.est = 10; t.why = 'Agent finished just now';
        t.packet = { summary: `Finished: ${t.title}. Changes are on the workspace branch with notes.`, checks: ['Tests pass', 'Rebased on main'], files: [['(see the workspace)', '']], diff: ['+ …'], followups: [] };
        setStatus(t, 'review', 'agent', 'Finished; waiting for your review.'); toast(`Finished: ${t.title}. It's in Needs you.`);
      } },
    hotel: { label: 'The agent finishes the hotel search', ok: () => T('hotel') && T('hotel').status === 'doing', hint: 'Ask the agent to help with the hotel todo first',
      run() {
        const t = T('hotel'); t.reviewer = 'you'; t.est = 5; t.type = 'review-options';
        t.options = [['Harbor Inn', '$219/night', '6 min walk', 'free cancellation'], ['Hotel Meridian', '$245/night', 'across the street', 'breakfast included'], ['The Foundry', '$189/night', '15 min by tram', 'no cancellation']];
        setStatus(t, 'review', 'agent', 'Found 3 options under $250.'); toast('3 hotel options are ready. Booking stays with you.');
      } },
  };

  // ---------- small render helpers ----------
  const who = (a) => (a === 'you' ? '<span class="who you">you</span>' : a === 'agent' ? '<span class="who agent">agent</span>' : `<span class="who other">${esc(a)}</span>`);
  function chip(t) {
    if (t.status === 'done') return '<span class="st done">Done</span>';
    if (needsMe(t)) return '<span class="st you">Needs you</span>';
    if (agentWorking(t)) return '<span class="st agent">Agent working</span>';
    if (waitingOthers(t)) return `<span class="st wait">Waiting: ${esc(waitWho(t))}</span>`;
    if (t.status === 'blocked') return '<span class="st blocked">Blocked</span>';
    return `<span class="st ready">${statusLabel[t.status]}</span>`;
  }
  const keyChip = (t) => `<span class="key" data-p="${esc(projOf(t))}">${esc(keyOf(t))}</span>`;
  const flashCls = (id) => (S.flash.includes(id) ? ' flash' : '');
  const blockedText = (t) => (t.blockedBy || []).map((id) => T(id)).filter((x) => x.status !== 'done').map((x) => `${esc(x.title)} (${x.assignee === 'you' ? 'you' : esc(x.assignee)})`).join(', ');
  function actionVerb(t) {
    if (t.status === 'review') return t.type === 'review-doc' ? 'Read' : t.type === 'review-options' ? 'Choose' : 'Review';
    return { answer: 'Answer', post: 'Post', land: 'Land', 'start-live': 'Start', close: 'Close', check: 'Do', todo: 'Do' }[t.type] || 'Open';
  }
  const from = (wid) => (wid ? ` data-from="${esc(wid)}"` : '');
  function row(t, wid, sc) {
    const sub = t.status === 'blocked' ? `Blocked on ${blockedText(t)}` : (t.why || '');
    const showKey = !(sc && sc.startsWith('wi:'));
    return `<button class="row${flashCls(t.id)}" data-act="open-task" data-id="${t.id}"${from(wid)}>
      <span class="row-main">${showKey ? keyChip(t) : ''}<span class="row-title">${esc(t.title)}</span><span class="row-sub">${esc(sub)}</span></span>
      <span class="row-side">${t.dueIn != null && t.dueIn <= 2 ? `<span class="due">due ${esc(t.due)}</span>` : ''}${t.est ? `<span class="est">${mins(t.est)}</span>` : ''}<span class="verb">${actionVerb(t)}</span></span>
    </button>`;
  }
  function miniRow(t, noteHtml, wid, lead) {
    return `<button class="mini${flashCls(t.id)}" data-act="open-task" data-id="${t.id}"${from(wid)}>${lead || keyChip(t)}<span class="mini-title">${esc(t.title)}</span><span class="mini-note">${noteHtml || ''}</span></button>`;
  }
  const empty = (txt) => `<p class="empty-s">${esc(txt)}</p>`;

  // ---------- shapes: how a query's rows are drawn ----------
  function noteFor(q, t) {
    if (q === 'agents') return esc(t.progress || '');
    if (q === 'waiting') return `${esc(waitWho(t))}${t.waitDays != null ? ' · ' + t.waitDays + 'd' : ''}`;
    if (q === 'todos') return t.assignee === 'agent' ? `agent: ${esc(t.progress || '')}` : t.due ? 'due ' + esc(t.due) : '';
    return chip(t);
  }
  const SHAPES = {
    list: (ts, wd, sc) => (ts.length ? `<div class="list">${ts.map((t) => row(t, wd.id, sc)).join('')}</div>` : empty(Q[wd.q].empty)),
    compact: (ts, wd, sc) => ts.map((t) => miniRow(t, noteFor(wd.q, t), wd.id, sc.startsWith('wi:') ? who(t.status === 'review' && t.reviewer ? t.reviewer : t.assignee) : null)).join('') || empty(Q[wd.q].empty),
    board: (ts, wd, sc) => {
      const cols = ['todo', 'doing', 'blocked', 'review', 'done'];
      return `<div class="board">${cols.map((c) => { const cs = ts.filter((t) => t.status === c); return `<div class="col"><h4>${statusLabel[c]} <span class="count">${cs.length}</span></h4>${cs.map((t) => `<button class="bcard${flashCls(t.id)}" data-act="open-task" data-id="${t.id}"${from(wd.id)}>${sc.startsWith('wi:') ? '' : keyChip(t)}<span>${esc(t.title)}</span><span class="bmeta">${who(t.reviewer && t.status === 'review' ? t.reviewer : t.assignee)}${t.status === 'review' ? ' reviews' : ''}</span></button>`).join('')}</div>`; }).join('')}</div>`;
    },
    agenda: (ts, wd, sc) => {
      const deferred = S.plan.deferred.map(T).filter((t) => t && t.status !== 'done' && inT(sc)(t));
      const total = ts.reduce((s, t) => s + (t.est || 0), 0);
      return `<p class="muted small">${ts.length} things, about ${mins(total)}. Pick any one; the order is only a suggestion you can change.</p>
        <ol class="agenda">${ts.map((t, i) => `<li class="agenda-row${flashCls(t.id)}"><span class="rank">${i + 1}</span>
          <div class="agenda-main">${keyChip(t)}<button class="link strong" data-act="open-task" data-id="${t.id}"${from(wd.id)}>${esc(t.title)}</button><div class="muted small">${esc(t.planReason || t.why || 'New since you planned')}</div></div>
          <span class="est">${t.est ? mins(t.est) : ''}</span>
          <span class="agenda-ctl"><button class="icon" aria-label="Move up" data-act="plan-up" data-id="${t.id}">↑</button><button class="icon" aria-label="Move down" data-act="plan-down" data-id="${t.id}">↓</button><button class="btn ghost small" data-act="plan-defer" data-id="${t.id}">Not today</button></span></li>`).join('')}</ol>
        ${deferred.length ? `<div class="deferred"><span class="muted small">Not today:</span> ${deferred.map((t) => `<button class="pill" data-act="plan-undefer" data-id="${t.id}">${esc(t.title)} ↺</button>`).join(' ')}</div>` : ''}`;
    },
    groups: (ts, wd, sc) => {
      const g = [['Needs you', ts.filter(needsMe)], ['Agent working', ts.filter(agentWorking)], ['Waiting on others', ts.filter(waitingOthers)], ['Blocked', ts.filter((t) => t.status === 'blocked')], ['Not started', ts.filter((t) => !needsMe(t) && !agentWorking(t) && !waitingOthers(t) && t.status !== 'blocked')]].filter((x) => x[1].length);
      return g.map(([h, arr]) => `<h4 class="grp">${h} <span class="count">${arr.length}</span></h4>${arr.map((t) => miniRow(t, t.status === 'blocked' ? 'after ' + blockedText(t) : esc(t.progress || (t.waitDays != null ? t.waitDays + 'd' : '')), wd.id, (sc.startsWith('wi:') ? '' : keyChip(t)) + who(t.status === 'review' && t.reviewer ? t.reviewer : t.assignee))).join('')}`).join('') || empty(Q[wd.q].empty);
    },
    todos: (ts, wd) => `${ts.map((t) => `<button class="todo-line${flashCls(t.id)}" data-act="open-task" data-id="${t.id}"${from(wd.id)}><span>${esc(t.title)}</span><span class="muted small">${t.due ? 'due ' + esc(t.due) : ''}${t.assignee === 'agent' ? 'agent: ' + esc(t.progress || '') : ''}</span></button>`).join('') || empty('No todos.')}
      <form class="addtodo" data-form="add-todo" data-id="${wd.id}"><label class="sr" for="todo-text-${wd.id}">New todo</label><input id="todo-text-${wd.id}" placeholder="Add a todo, no project needed" autocomplete="off"><label class="sr" for="todo-due-${wd.id}">Due</label><select id="todo-due-${wd.id}"><option value="">no date</option><option value="today">today</option><option value="tomorrow">tomorrow</option><option value="week">this week</option></select><button class="btn" type="submit">Add</button></form>`,
    cards: (ws, wd) => (ws.length ? `<div class="cards">${ws.map((w) => card(w)).join('')}</div>` : empty(Q[wd.q].empty)),
    wlist: (ws, wd) => ws.map((w) => wiRow(w)).join('') || empty(Q[wd.q].empty),
    feed: (fs, wd) => {
      const n = ui.more[wd.id] ? fs.length : 6;
      return `${fs.slice(0, n).map((f) => `<div class="feed${f.fresh ? ' fresh' : ''}"><span class="when">${esc(f.when)}</span><span class="by by-${esc(f.who)}">${esc(f.who)}</span><div class="feed-body"><div>${f.wi && W(f.wi) ? `<button class="key linkkey" data-p="${esc(W(f.wi).project)}" data-act="open-wi" data-id="${f.wi}">${esc(W(f.wi).key)}</button> ` : ''}${esc(f.text)}</div>${f.why ? `<div class="muted small">${esc(f.why)}</div>` : ''}</div></div>`).join('') || empty('Nothing moved.')}
        ${fs.length > 6 ? `<button class="link small" data-act="more" data-id="${wd.id}">${ui.more[wd.id] ? 'Show fewer' : `Show all ${fs.length}`}</button>` : ''}`;
    },
  };
  function card(w) {
    const m = move(w.id); const n = nextAction(w.id); const metN = w.doneWhen.filter(met).length;
    return `<article class="card${n && S.flash.includes(n.id) ? ' flash' : ''}">
      <header><span class="key" data-p="${esc(w.project)}">${esc(w.key)}</span><span class="src">${esc(w.source)}</span><span class="move ${m.kind}">${esc(m.label)}${m.n ? ' · ' + m.n : ''}${m.note && m.kind !== 'agent' ? ' · ' + esc(m.note) : ''}</span></header>
      <h3><button class="link strong" data-act="open-wi" data-id="${w.id}">${esc(w.title)}</button></h3>
      ${w.doneWhen.length ? `<div class="dw"><div class="progress"><span style="width:${Math.round((metN / w.doneWhen.length) * 100)}%"></span></div><span class="small muted">${metN} of ${w.doneWhen.length} done-when</span></div>` : ''}
      ${n ? `<div class="next"><span class="muted small">Next</span><button class="next-btn" data-act="open-task" data-id="${n.id}">${chip(n)} ${esc(n.title)}</button></div>` : ''}
      <footer><button class="link" data-act="open-wi" data-id="${w.id}">Open${w.ws && w.ws.state === 'suspended' ? ' (session suspended)' : ''}</button>${w.inProgress ? `<button class="link" data-act="park" data-id="${w.id}">Park</button>` : `<button class="link" data-act="unpark" data-id="${w.id}">Bring back</button>`}</footer>
    </article>`;
  }
  function wiRow(w) {
    const started = S.items.includes(w);
    let status; let btn;
    if (started && w.inProgress) { const m = move(w.id); status = `<span class="move ${m.kind}">${esc(m.label)}${m.n ? ' · ' + m.n : ''}</span>`; btn = `<button class="btn small" data-act="park" data-id="${w.id}">Park</button>`; }
    else if (started) { status = `<span class="muted small">parked, ${esc(w.parkNote || '')}</span>`; btn = `<button class="btn small" data-act="unpark" data-id="${w.id}">Bring back</button>`; }
    else { status = `<span class="muted small">${esc(w.source)} backlog</span>`; btn = `<button class="btn small" data-act="start-wi" data-id="${w.id}">Start</button>`; }
    return `<div class="backlog-row"><span class="key" data-p="${esc(w.project)}">${esc(w.key)}</span><span class="grow">${started ? `<button class="link strong" data-act="open-wi" data-id="${w.id}">${esc(w.title)}</button>` : esc(w.title)} ${status}</span>${btn}</div>`;
  }

  // ---------- app-provided widgets ----------
  const needWi = (sc) => (sc.startsWith('wi:') ? W(sc.slice(3)) : null);
  const XR = {
    donewhen: (sc) => {
      const w = needWi(sc); if (!w) return empty('Scope this to a work item.');
      const metN = w.doneWhen.filter(met).length; const close = T('close-' + w.id);
      return `<div class="dw"><div class="progress"><span style="width:${Math.round((metN / Math.max(1, w.doneWhen.length)) * 100)}%"></span></div><span class="small muted">${metN} of ${w.doneWhen.length}</span></div>
        <ul class="dwl">${w.doneWhen.map((c) => `<li class="${met(c) ? 'met' : ''}"><span class="tick">${met(c) ? '✓' : '○'}</span>${esc(c.text)}</li>`).join('')}</ul>
        <p class="muted small">Scope: ${esc(w.scope)}</p>
        ${close && close.status !== 'done' ? `<button class="btn primary small" data-act="open-task" data-id="${close.id}">Close ${esc(w.key)} in ${esc(w.source)}…</button>` : ''}`;
    },
    thread: (sc) => {
      const w = needWi(sc); if (!w || !w.rounds) return empty('No thread for this work item.');
      const mv = move(w.id);
      return `<div class="thread-h"><span class="move ${mv.kind}">${mv.kind === 'you' ? 'Your move' : mv.kind === 'wait' ? 'Their move' : esc(mv.label)}</span>${w.watcher ? `<span class="small muted">Watcher ${w.watcher.on ? 'on, ' + esc(w.watcher.cadence) : 'paused'} · reads and proposes, never posts</span><button class="btn small" data-act="toggle-watcher" data-id="${w.id}">${w.watcher.on ? 'Pause' : 'Resume'}</button>` : ''}</div>
        ${w.rounds.slice().reverse().map((r) => `<div class="round${r.closed ? ' closed' : ''}"><div class="round-h"><strong>Round ${r.n}</strong><span class="muted small">${esc(r.by)} · ${esc(r.when)}${r.closed ? ' · answered' : ''}</span></div>
          <div class="round-body"><blockquote>${esc(r.raw)}</blockquote><div class="asks"><span class="muted small">The watcher read this as ${r.asks.length} asks</span>${r.asks.map((a) => `<button class="mini" data-act="open-task" data-id="${a.task}">${chip(T(a.task))}<span class="mini-title">${esc(a.text)}</span></button>`).join('')}${r.closed ? '' : '<button class="link small" data-act="fix-split">Correct the split</button>'}</div></div></div>`).join('')}
        ${w.watcher ? `<details class="small"><summary>Watcher log</summary><ul class="log">${w.watcher.log.slice(0, 8).map((l) => `<li>${esc(l)}</li>`).join('')}</ul></details>` : ''}`;
    },
    mr: (sc) => {
      const w = needWi(sc); const t = w && S.tasks.find((x) => x.wi === w.id && x.type === 'review-mr');
      if (!t) return empty('No merge request in this work item yet.');
      const p = t.packet; const title = t.title.replace(/^Review /, '');
      return `<div class="mr-h"><strong>${esc(title)}</strong>${chip(t)}</div><p class="small">${esc(p.summary)}</p>
        <ul class="checks">${p.checks.map((x) => `<li>✓ ${esc(x)}</li>`).join('')}</ul>
        <pre class="diff">${p.diff.map((l) => `<span class="${l[0] === '+' ? 'add' : l[0] === '-' ? 'del' : ''}">${esc(l)}</span>`).join('\n')}</pre>
        ${t.status === 'review' && t.reviewer === 'you' ? `<div class="tp-actions"><button class="btn primary small" data-act="open-task" data-id="${t.id}">Review it</button><span class="muted small">Approve, Keep working or Take it</span></div>` : `<p class="muted small">${t.status === 'done' ? 'You approved it.' : 'Not waiting on you.'}</p>`}`;
    },
    build: (sc) => {
      const t = T('n7'); if (!needWi(sc) || needWi(sc).id !== 'nex12') return empty('Only the NEX-12 build is in this mock.');
      return `<div class="mr-h"><strong>Jira plugin, against a sandbox</strong>${chip(t)}</div>
        <ul class="steps">${t.steps.map((s, i) => `<li class="${i < t.stepsDone ? 'ok' : i === t.stepsDone && t.status === 'doing' ? 'now' : ''}">${i < t.stepsDone ? '✓' : i === t.stepsDone && t.status === 'doing' ? '◐' : '○'} ${esc(s)}</li>`).join('')}</ul>
        <div class="tp-actions">${t.status === 'review' && t.reviewer === 'you' ? `<button class="btn primary small" data-act="open-task" data-id="n7">Review it</button>` : ''}${t.assignee === 'agent' && t.status === 'doing' ? `<button class="btn small" data-act="take" data-id="n7">Take it</button>` : ''}${t.took && t.status !== 'done' ? `<button class="btn small" data-act="handback" data-id="n7">Hand back</button>` : ''}<span class="muted small">branch <code>fm/jira-source</code> · agent's environment</span></div>`;
    },
    design: (sc) => {
      if (!needWi(sc) || needWi(sc).id !== 'nex12') return empty('Only the NEX-12 design doc is in this mock.');
      const q = T('n6');
      return `<p class="small"><strong>Section 3: How Nexus reads Jira.</strong> Nexus polls the Jira search API every 5 minutes for tickets in tracked projects, and reads a ticket in full when it changes…</p>
        <blockquote class="small"><strong>Platform lead, Fri 15:10:</strong> ${esc(q.origin.raw)}</blockquote>
        ${q.status !== 'done' ? `<button class="btn primary small" data-act="open-task" data-id="n6">Answer (drafted)</button>` : '<p class="small muted">You answered. Waiting on the platform lead\'s approval.</p>'}`;
    },
    doc: (sc) => {
      if (!needWi(sc) || needWi(sc).id !== 'jobeval') return empty('Only the ARCH-7 working doc is in this mock.');
      const src = (s) => `<span class="srcchip">${esc(s)}</span>`;
      const j5 = T('j5');
      return `<article class="docbody">
        ${j5.status === 'review' ? `<div class="banner"><span>3 sections changed since you last read it.</span><button class="btn small" data-act="mark-reviewed" data-id="j5">Mark reviewed</button></div>` : ''}
        <section><h3>What you said matters <span class="tag">from you</span></h3><ul><li>Less on-call time on stuck jobs</li><li>Migration must not take more than one quarter</li><li>Hosting cost under $400/month</li></ul></section>
        <section class="changed"><h3>What our runner does today <span class="tag">facts</span><span class="tag new">changed</span></h3><ul>
          <li>23 jobs; 19 are simple schedules, 4 are multi-step ${src('runner/jobs/*.yaml')}</li>
          <li>Retries are hand-written in 11 jobs ${src('grep "retry" runner/jobs')}</li>
          <li>Stuck jobs paged on-call 9 times in the last 90 days ${src('pager export, Aug–Nov')}</li></ul></section>
        <section><h3>Feature by feature <span class="tag">facts</span></h3><div class="tablewrap"><table><thead><tr><th>Need</th><th>Our runner</th><th>Temporal</th></tr></thead><tbody>
          <tr><td>Schedules</td><td>cron strings ${src('runner/schedule.py')}</td><td>Schedules API ${src('docs.temporal.io/workflows#schedule')}</td></tr>
          <tr><td>Retries</td><td>per job, by hand</td><td>built in, per activity ${src('docs.temporal.io/retry-policies')}</td></tr>
          <tr><td>See a stuck job</td><td>logs only</td><td>web UI with history ${src('docs.temporal.io/web-ui')}</td></tr>
          <tr><td>Skip if previous run still going</td><td>yes</td><td>via workflow ID policy ${src('spike notes')}</td></tr></tbody></table></div></section>
        <section class="changed"><h3>Cost and operations <span class="tag">facts</span><span class="tag new">changed</span></h3><p>Managed cloud: about $200–350/month at our volume ${src('temporal.io/pricing, read Nov 16')}. Self-hosting adds a database and four services to operate ${src('docs.temporal.io/self-hosted-guide')}.</p></section>
        <section class="changed"><h3>Spike results <span class="tag">facts</span><span class="tag new">changed</span></h3><p>${T('j6').status === 'doing' ? 'Running now: porting the nightly report job.' : 'Output matched our runner on 7 of 7 nights; 11m 40s vs 12m 05s.'} ${src('branch fm/temporal-spike')}</p></section>
        <section class="opinion"><h3>Options <span class="tag op">agent's read, not a fact</span></h3><p>Three options, no recommendation until you state a lean: <strong>adopt managed Temporal</strong> (biggest on-call win, new vendor), <strong>self-host</strong> (no vendor, more to operate), or <strong>keep ours and add a UI</strong> (cheapest now, keeps the hand-written retries).</p></section>
        <section><h3>Research log</h3><p class="small muted">Read 14 pages, skipped 3 (two blog posts older than 2023, one vendor comparison with no sources).</p></section>
      </article>
      <div class="doc-comment"><label for="note-j5"><strong>Comment</strong> <span class="muted small">The agent picks it up as a task</span></label><textarea id="note-j5" rows="2" placeholder="e.g. Check what the 4 multi-step jobs would look like"></textarea><button class="btn small" data-act="comment" data-id="j5">Comment</button></div>`;
    },
    chat: (sc, wd) => chatBody(sc === 'all' ? 'home' : sc, wd.shape || 'conversation', {}),
  };
  const threadTitle = (tk) => (tk === 'home' ? 'Home agent' : tk.startsWith('p:') ? `${P(tk.slice(2)).name} · project agent` : `${W(tk.slice(3)).key} · session`);
  const strip = (h) => h.replace(/<[^>]+>/g, '');
  function chatBody(tk, shape, o) {
    const w = tk.startsWith('wi:') ? W(tk.slice(3)) : null;
    const msgs = S.chat[tk] || [];
    const chips = ASKS.filter((a) => a.where(tk));
    const inputId = askId(tk) + (o.panel ? '-p' : '');
    const homeish = !w;
    const form = `<form class="askform" data-form="ask-text" data-id="${tk}"><label class="sr" for="${inputId}">${homeish ? 'Ask Nexus' : 'Message the agent'}</label><input id="${inputId}" placeholder="${homeish ? 'Ask Nexus anything, or tell it how to set up this screen' : 'Message the agent'}" autocomplete="off"><button class="btn${shape === 'minimal' ? ' primary' : ' small'}" type="submit">${homeish ? 'Ask' : 'Send'}</button></form>`;
    const chipRow = chips.length ? `<div class="chips">${chips.map((a) => `<button class="pill" data-act="ask" data-id="${a.key}" data-a="${tk}">${esc(a.label(tk))}</button>`).join('')}</div>` : '';
    const bubble = (m) => `<div class="msg ${m.who}">${m.when ? `<span class="mwhen">${esc(m.when)}</span>` : ''}<div>${m.html}</div></div>`;
    const status = w && w.ws ? `<div class="small muted">Session ${esc(w.ws.state === 'running' ? 'running' : w.ws.state === 'resumed' ? 'open' : w.ws.state === 'closed' ? 'closed' : 'suspended ' + w.ws.since)} · branch <code>${esc(w.ws.branch)}</code> · agent's environment</div>` : '';
    if (shape === 'minimal') {
      const last = msgs.slice(-2);
      return `<div class="chat chat-min">${status}${form}${chipRow}${last.map(bubble).join('')}${msgs.length > 2 ? `<button class="link small" data-act="chats-thread" data-id="${tk}">Conversation history (${msgs.length})</button>` : ''}</div>`;
    }
    let session = status;
    if (w && w.ws) {
      const susp = w.ws.state === 'suspended'; const away = w.ws.whileAway || [];
      session += `${susp ? `<div class="msg sys">Suspended. The branch, notes and transcript are kept. <button class="link" data-act="resume-ws" data-id="${w.id}">Resume where it stopped</button></div>` : ''}
        ${away.length ? `<div class="msg agent"><strong>${susp ? 'Before I stopped:' : 'While you were away:'}</strong><ul>${away.map((a) => `<li>${esc(a)}</li>`).join('')}</ul></div>` : ''}
        ${!susp && w.ws.state !== 'closed' ? `<div class="msg agent">Anything you want to take over? Everything I've done is on <code>${esc(w.ws.branch)}</code> and in the task notes.</div>` : ''}`;
    }
    return `<div class="chat chat-conv"><div class="msgs">${msgs.map(bubble).join('')}${session}</div>${chipRow}${form}</div>`;
  }
  function chatsPanel() {
    const c = ui.chats;
    if (c.thread) {
      const tk = c.thread; const here = ui.page ? null : ui.space;
      const onScreen = here && layoutFor(here).some((wd) => wd.q === 'chat' && scopeOf(wd, ctxOf(here)) === (tk === 'home' ? 'all' : tk));
      return `<div class="tp"><header class="tp-h"><button class="icon close" aria-label="Close" data-act="chats-close">×</button><div><button class="link small" data-act="chats" data-id="list">← All conversations</button></div><h2>${esc(threadTitle(tk))}</h2></header>
        ${chatBody(tk, 'conversation', { panel: true })}
        <div class="tp-foot small muted">${onScreen ? 'This conversation is also on this screen as a widget.' : here ? `<button class="link" data-act="pin-chat" data-id="${tk}">Put this conversation on ${esc(spaceName(here))}</button>` : ''}${tk.startsWith('wi:') ? ` · <button class="link" data-act="open-wi" data-id="${tk.slice(3)}">Open the workspace</button>` : ''}</div></div>`;
    }
    const threads = ['home'].concat(S.projects.map((p) => 'p:' + p.id).filter((k) => (S.chat[k] || []).length), S.items.filter((w) => w.ws).map((w) => 'wi:' + w.id));
    return `<div class="tp"><header class="tp-h"><button class="icon close" aria-label="Close" data-act="chats-close">×</button><h2>Conversations</h2><p class="muted small">Every agent conversation is kept here, whether or not it's on a screen.</p></header>
      <div class="threads">${threads.map((tk) => {
        const ms = S.chat[tk] || []; const last = ms[ms.length - 1]; const w = tk.startsWith('wi:') ? W(tk.slice(3)) : null;
        const preview = last ? strip(last.html) : w && w.ws.whileAway && w.ws.whileAway[0] ? w.ws.whileAway[0] : 'No messages yet';
        return `<button class="threadrow" data-act="chats-thread" data-id="${tk}"><span class="tr-h"><strong>${esc(threadTitle(tk))}</strong><span class="muted small">${last ? esc(last.when || '') : w ? esc(w.ws.state) : ''}</span></span><span class="muted small">${esc(preview.slice(0, 96))}</span></button>`;
      }).join('')}</div></div>`;
  }

  // ---------- a widget: frame + body ----------
  function widget(wd, ctx, opts = {}) {
    const sc = scopeOf(wd, ctx); RENDERED[wd.id] = { wd, ctx };
    const def = Q[wd.q] || X[wd.q];
    let body; let count = '';
    if (Q[wd.q]) { const data = Q[wd.q].run(sc); if (Q[wd.q].kind !== 'event') count = data.length; body = SHAPES[wd.shape](data, wd, sc); }
    else body = XR[wd.q](sc, wd);
    const scChip = sc !== ctx && sc !== 'all' ? `<span class="scope">${esc(scopeName(sc))}</span>` : '';
    const tools = ui.edit && !opts.full && !opts.app
      ? `<span class="etools"><button class="icon" aria-label="Move earlier" data-act="w-left" data-id="${wd.id}">←</button><button class="icon" aria-label="Move later" data-act="w-right" data-id="${wd.id}">→</button><span class="dim"><button class="icon" aria-label="Narrower" data-act="w-narrower" data-id="${wd.id}">−</button><span>w${Math.min(wd.size || 1, opts.cols || 3)}</span><button class="icon" aria-label="Wider" data-act="w-wider" data-id="${wd.id}">+</button></span><span class="dim"><button class="icon" aria-label="Shorter" data-act="w-shorter" data-id="${wd.id}">−</button><span>h${wd.h && wd.h !== 'auto' ? wd.h : '·'}</span><button class="icon" aria-label="Taller" data-act="w-taller" data-id="${wd.id}">+</button></span><button class="icon" aria-label="Remove" data-act="w-remove" data-id="${wd.id}">×</button></span>`
      : `<button class="icon" aria-label="About this widget" aria-expanded="${ui.menu === wd.id}" data-act="w-menu" data-id="${wd.id}">⋯</button>`;
    const span = Math.min(wd.size || 1, opts.cols || 3); const h = wd.h && wd.h !== 'auto' ? +wd.h : 0;
    const style = `grid-column: span ${span};${h ? (opts.tile ? ` grid-row: span ${h};` : ` height: ${h * 220}px;`) : ''}`;
    return `<section class="widget${wd.by === 'agent' ? ' by-agent' : ''}${h || opts.tile ? ' fixed-h' : ''}${wd.q === 'chat' && wd.shape === 'minimal' ? ' w-prompt' : ''}" style="${style}" data-w="${wd.id}">
      <header class="wh"><h3>${esc(def.label)}${count !== '' ? ` <span class="count">${count}</span>` : ''}</h3>${scChip}${wd.by === 'agent' ? `<button class="badge" data-act="w-why" data-id="${wd.id}" title="Why is this here?">agent</button>` : ''}<span class="wh-r">${tools}</span>
        ${ui.menu === wd.id ? menu(wd, def, opts) : ''}</header>
      <div class="wb">${body}</div>
    </section>`;
  }
  function menu(wd, def, opts) {
    const shapes = (Q[wd.q] || X[wd.q]).shapes || [];
    return `<div class="wmenu" role="menu">
      ${opts.full ? '' : `<button class="mi" data-act="w-full" data-id="${wd.id}">Open full view</button>`}
      ${shapes.length > 1 ? `<div class="mi-h">Show as</div><div class="seg">${shapes.map((s) => `<button class="${wd.shape === s ? 'on' : ''}" data-act="w-shape" data-id="${wd.id}" data-a="${s}">${SHAPE_NAME[s]}</button>`).join('')}</div>` : ''}
      ${ui.space !== 'home' || ui.page ? `<button class="mi" data-act="w-pin" data-id="${wd.id}">Pin to Home</button>` : ''}
      <button class="mi" data-act="w-why" data-id="${wd.id}">What is this?</button>
      ${!opts.full && !opts.app ? `<button class="mi danger" data-act="w-remove" data-id="${wd.id}">Remove from this space</button>` : ''}
    </div>`;
  }

  // ---------- spaces ----------
  function homeHead() {
    const n = needsList().length; const ag = S.tasks.filter(agentWorking).length; const wt = S.tasks.filter(waitingOthers).length;
    return `<div class="space-h"><div class="grow"><h2>Good morning</h2><p class="muted">${esc(S.clock)} · <strong class="fg">${n} things need you</strong> · ${ag} agents working · waiting on ${wt}</p></div></div>`;
  }
  function projHead(sp) {
    const p = P(sp.slice(2)); const n = needsList().filter(inT(sp)).length;
    return `<div class="crumbs"><button class="link" data-act="go" data-id="home">Home</button> ›</div>
      <div class="space-h"><div class="grow"><h2>${esc(p.name)}</h2><p class="muted">Project space: the same widgets as Home, narrowed to this project · ${n} need you</p></div></div>`;
  }
  function wsHead(sp) {
    const w = W(sp.slice(3)); const m = move(w.id); const p = P(w.project);
    const sess = w.ws ? (w.ws.state === 'suspended' ? `<button class="btn small" data-act="resume-ws" data-id="${w.id}">Resume session</button>` : w.ws.state === 'closed' ? '' : `<button class="btn ghost small" data-act="suspend-ws" data-id="${w.id}">Suspend session</button>`) : '';
    return `<div class="crumbs"><button class="link" data-act="go" data-id="home">Home</button> › <button class="link" data-act="go" data-id="p:${esc(w.project)}">${esc(p.name)}</button> ›</div>
      <div class="space-h"><span class="key big" data-p="${esc(w.project)}">${esc(w.key)}</span><div class="grow"><h2>${esc(w.title)}</h2><p class="muted small">${esc(w.source)}: ${esc(w.sourceRef)} · Nexus reads it, ${esc(w.source)} owns it · the tasks below live only in your Nexus</p></div><span class="move ${m.kind}">${esc(m.label)}${m.n ? ' · ' + m.n : ''}</span></div>
      <div class="ws-bar">${sess}${w.closed ? '' : w.inProgress ? `<button class="btn ghost small" data-act="park" data-id="${w.id}">Park</button>` : `<button class="btn small" data-act="unpark" data-id="${w.id}">Bring back</button>`}</div>
      ${!w.inProgress && !w.closed ? `<div class="banner park">Parked: ${esc(w.parkNote || '')}. Its tasks are out of your lists until then.</div>` : ''}
      ${w.closed ? `<div class="banner done">Closed in ${esc(w.source)}. The workspace is kept for reference.</div>` : ''}`;
  }
  function editBar(sp) {
    const c = cfg(sp);
    return `<div class="editbar" role="toolbar" aria-label="Customize this space">
      <strong>Customizing ${esc(spaceName(sp))}</strong>
      <span class="eb-g"><span class="muted small">Columns</span><span class="seg">${[1, 2, 3, 4].map((n) => `<button class="${c.cols === n ? 'on' : ''}" data-act="cfg-cols" data-id="${n}">${n}</button>`).join('')}</span></span>
      <span class="eb-g"><span class="muted small">Tiling</span><span class="seg"><button class="${c.tiling === 'flow' ? 'on' : ''}" data-act="cfg-tiling" data-id="flow" title="Widgets take the height they need; the page scrolls">Flow</button><button class="${c.tiling === 'tile' ? 'on' : ''}" data-act="cfg-tiling" data-id="tile" title="Widgets fill the screen like a tiling window manager; each scrolls inside">Tile</button></span></span>
      <span class="eb-g"><button class="btn small" data-act="add-widget">+ Add a widget</button><button class="btn ghost small" data-act="reset-layout">Reset to default</button><button class="btn primary small" data-act="edit">Done</button></span>
    </div>`;
  }
  function spaceView(sp) {
    const lay = layoutFor(sp); const ctx = ctxOf(sp); const c = cfg(sp); const tile = c.tiling === 'tile';
    const head = sp === 'home' ? homeHead() : sp.startsWith('p:') ? projHead(sp) : wsHead(sp);
    const solo = !ui.edit && lay.length === 1 && lay[0].q === 'chat' && lay[0].shape === 'minimal';
    const ft = ui.focusTask && T(ui.focusTask);
    const docked = ft && sp === 'wi:' + ft.wi ? `<section class="docked" aria-label="Task you're digging into">${panel(ft, { docked: true })}</section>` : '';
    const grid = `<div class="grid${ui.edit ? ' editing' : ''}${tile && !solo ? ' tile' : ''}" style="--cols:${solo ? 1 : c.cols}">${lay.map((wd) => widget(wd, ctx, { cols: solo ? 1 : c.cols, tile: tile && !solo })).join('')}${ui.edit ? '<button class="addtile" data-act="add-widget">+ Add a widget</button>' : ''}</div>`;
    const emptyNote = !lay.length && !ui.edit ? `<p class="empty">This space is empty. Press <strong>Customize manually</strong>, or open <strong>Chats</strong> to talk to the agent.</p>` : '';
    const corner = ui.edit ? '' : `<button class="corner btn" data-act="edit">Customize manually</button>`;
    return `${ui.edit ? editBar(sp) : ''}${solo ? `<div class="solo">${head}${grid}</div>` : head + docked + grid}${emptyNote}${corner}`;
  }

  // ---------- the Tasks app: default views, made of the same widgets ----------
  const APPVIEWS = [
    ['needs', 'Needs you', [{ q: 'needs', shape: 'list' }]],
    ['today', 'Today', [{ q: 'today', shape: 'agenda' }]],
    ['items', 'Work items', [{ q: 'items', shape: 'cards' }, { q: 'upnext', shape: 'wlist' }]],
    ['board', 'Board', [{ q: 'all', shape: 'board' }]],
    ['todos', 'Todos', [{ q: 'todos', shape: 'todos' }]],
    ['since', 'Since you were here', [{ q: 'since', shape: 'feed' }]],
  ];
  const appWidgets = {};
  function appView() {
    const v = APPVIEWS.find((x) => x[0] === ui.page.view) || APPVIEWS[0];
    const wds = v[2].map((o, i) => { const id = `app-${v[0]}-${i}`; appWidgets[id] = appWidgets[id] || Object.assign({ id, size: 3, scope: 'inherit', by: 'app' }, o); return appWidgets[id]; });
    return `<div class="app">
      <nav class="subnav" aria-label="Task views">${APPVIEWS.map(([k, l]) => `<button class="${k === v[0] ? 'on' : ''}" data-act="open-app" data-id="${k}">${l}</button>`).join('')}</nav>
      <div class="app-main">
        <div class="space-h"><div class="grow"><h2>Tasks · ${esc(v[1])}</h2><p class="muted small">The default views for tasks. You never have to come here: each one is also a widget you can put on any space (⋯ → Pin to Home).</p></div>
          <label class="small" for="app-scope">Show</label><select id="app-scope" data-change="app-scope"><option value="all"${ui.appScope === 'all' ? ' selected' : ''}>everything</option>${S.projects.map((p) => `<option value="p:${p.id}"${ui.appScope === 'p:' + p.id ? ' selected' : ''}>${esc(p.name)}</option>`).join('')}</select>
          <button class="btn ghost small" data-act="close-page">Close</button></div>
        <div class="grid" style="--cols:1">${wds.map((wd) => widget(wd, ui.appScope, { app: true, cols: 1 })).join('')}</div>
      </div>
    </div>`;
  }
  function fullView() {
    const p = ui.page;
    return `<div class="page"><div><button class="link" data-act="close-page">← Back</button></div><div class="grid" style="--cols:1">${widget(p.wd, p.ctx, { full: true, cols: 1 })}</div></div>`;
  }

  // ---------- the task panel: one place to act, wherever the task was shown ----------
  function panel(t, o = {}) {
    const w = t.wi && W(t.wi);
    const c = ui.confirm && ui.confirm.id === t.id ? ui.confirm.act : null;
    let body = ''; let actions = '';
    const take = (t.assignee === 'agent' && t.status !== 'done') ? `<button class="btn ghost" data-act="take" data-id="${t.id}">Take it</button>` : '';
    if (t.took && t.status !== 'done') actions += `<button class="btn ghost" data-act="handback" data-id="${t.id}">Hand back to the agent</button>`;

    if (t.status === 'done') {
      body = '<p class="muted">Done.</p>';
    } else if (t.status === 'blocked') {
      body = `<p>Blocked on <strong>${blockedText(t)}</strong>.</p><p class="muted small">${esc(t.why || '')} It moves on its own when that's done.</p>`;
      if (t.assignee === 'agent') actions += take;
    } else if (t.status === 'review' && t.reviewer === 'you' && t.packet) {
      const p = t.packet;
      body = `<p>${esc(p.summary)}</p>
        <ul class="checks">${p.checks.map((x) => `<li>✓ ${esc(x)}</li>`).join('')}</ul>
        <div class="files">${p.files.map((f) => `<div><code>${esc(f[0])}</code><span class="muted small">${esc(f[1])}</span></div>`).join('')}</div>
        <pre class="diff">${p.diff.map((l) => `<span class="${l[0] === '+' ? 'add' : l[0] === '-' ? 'del' : ''}">${esc(l)}</span>`).join('\n')}</pre>
        ${p.followups.length ? `<div class="fu"><strong class="small">Follow-ups it proposes</strong>${p.followups.map((f, i) => `<div class="fu-row"><span>${esc(f.text)}</span><span class="seg">${['start', 'queue', 'drop'].map((ch) => `<button class="${(f.choice || 'queue') === ch ? 'on' : ''}" data-act="fu" data-id="${t.id}" data-a="${i}" data-b="${ch}">${ch === 'start' ? 'Start now' : ch === 'queue' ? 'Queue' : 'Drop'}</button>`).join('')}</span></div>`).join('')}</div>` : ''}
        <label for="note-${t.id}" class="small">Note for the agent (for Keep working)</label><textarea id="note-${t.id}" rows="2" placeholder="e.g. Use semicolons for European locales"></textarea>`;
      actions += `<button class="btn primary" data-act="approve" data-id="${t.id}">${esc(t.approveLabel || 'Approve')}</button><button class="btn" data-act="keep" data-id="${t.id}">Keep working</button>${take}`;
    } else if (t.status === 'review' && t.type === 'review-options') {
      body = `<p>Options for: ${esc(T(t.forTask).title)}</p><div class="tablewrap"><table><thead><tr><th>Hotel</th><th>Price</th><th>Distance</th><th>Note</th></tr></thead><tbody>${t.options.map((o) => `<tr>${o.map((x) => `<td>${esc(x)}</td>`).join('')}</tr>`).join('')}</tbody></table></div><p class="muted small">Booking needs your login, so it stays with you. The links open in your browser.</p>`;
      actions += `<button class="btn primary" data-act="approve" data-id="${t.id}">Thanks, I'll book one</button>`;
    } else if (t.type === 'review-doc' && t.status === 'review') {
      body = `<p>3 sections changed since you last read it: what the runner does today, cost and operations, spike results.</p><p class="muted small">Facts carry a source; the options section is labelled as the agent's read.</p>`;
      actions += `<button class="btn primary" data-act="open-doc" data-id="${t.wi}">Open the working doc</button><button class="btn" data-act="mark-reviewed" data-id="${t.id}">Mark reviewed</button>`;
    } else if (t.type === 'answer') {
      const o = t.origin;
      body = `<div class="origin"><span class="muted small">${esc(o.by)} · ${esc(o.when)} · ${esc(o.where)}</span><blockquote>${esc(o.raw)}</blockquote></div>
        <label for="draft-${t.id}" class="small"><strong>Your answer</strong> <span class="muted">drafted by the agent, edit freely</span></label>
        <textarea id="draft-${t.id}" data-input="draft" data-id="${t.id}" rows="5">${esc(t.draft)}</textarea>
        <div class="claims"><strong class="small">Claims in the draft</strong>${t.claims.map((cl) => `<div class="claim ${cl.src ? '' : 'unverified'}"><span>${esc(cl.text)}</span><span class="small">${cl.src ? '<span class="srcchip">' + esc(cl.src) + '</span>' : '⚠ not verified: check this or cut it'}</span></div>`).join('')}</div>`;
      if (t.postTo) actions += c === 'post-answer' ? confirmBox(t, `Posts your answer to ${t.postTo} as you.`, 'do-post-answer', 'Post as me') : `<button class="btn primary" data-act="confirm" data-a="post-answer" data-id="${t.id}">Post reply…</button>`;
      else actions += `<button class="btn primary" data-act="save-answer" data-id="${t.id}">Save answer</button><span class="muted small">It goes out with the SEC-431 reply, which you post.</span>`;
    } else if (t.type === 'post') {
      const r = w.rounds[w.rounds.length - 1];
      const ans = S.tasks.filter((x) => x.wi === t.wi && x.type === 'answer' && x.status === 'done').map((x) => x.draft);
      body = `<p>Preview of the comment on ${esc(w.key)}:</p><div class="preview">${r.n === 2 ? `<p>1. Dependencies are pinned and urllib3 is upgraded: MR !41 (pip-audit now reports 0 known vulnerabilities).</p><p>2. ${esc(ans[0] || '')}</p>` : '<p>Audit logging for admin actions is enabled. A sample entry is attached.</p>'}</div>`;
      actions += c === 'post' ? confirmBox(t, `Posts this to ${w.key} as you. The security team will see it.`, 'do-post', 'Post as me') : `<button class="btn primary" data-act="confirm" data-a="post" data-id="${t.id}">Post…</button>`;
    } else if (t.type === 'waiting' || waitingOthers(t)) {
      body = `<p>Waiting on <strong>${esc(waitWho(t))}</strong>${t.waitDays != null ? ` for ${t.waitDays} days` : ''}.</p><p class="muted small">${esc(t.why || '')} This card is your private note; ${esc(waitWho(t))} never sees it.</p>
        ${t.nudge ? `<label class="small" for="nudge-${t.id}"><strong>Nudge</strong> <span class="muted">drafted because it's been idle 5+ days</span></label><textarea id="nudge-${t.id}" rows="3">${esc(t.nudge)}</textarea>` : ''}${t.nudged ? '<p class="small">Nudge sent.</p>' : ''}`;
      if (t.nudge) actions += c === 'nudge' ? confirmBox(t, 'Sends this comment on IT-882 as you.', 'do-nudge', 'Send as me') : `<button class="btn primary" data-act="confirm" data-a="nudge" data-id="${t.id}">Send nudge…</button>`;
      actions += `<button class="btn ghost" data-act="mark-done" data-id="${t.id}">They did it</button>`;
    } else if (t.type === 'start-live' && t.status === 'todo') {
      body = `<p>IT granted access. This test runs against <strong>live Jira</strong> with <strong>your credentials</strong>, so it waits for your start instead of starting itself.</p><ul class="small"><li>Reads 20 tickets from NEX and PLAT</li><li>Posts nothing (comment test uses the sandbox)</li></ul>`;
      actions += `<button class="btn primary" data-act="start-live" data-id="${t.id}">Start</button>${take}`;
    } else if (t.type === 'land') {
      body = `<p>MR !23 is approved and its checks are green. Landing merges it on GitHub and runs the usual deploy (staging, then production).</p>`;
      actions += c === 'land' ? confirmBox(t, 'Merges MR !23 on GitHub as you and starts the deploy.', 'do-land', 'Land') : `<button class="btn primary" data-act="confirm" data-a="land" data-id="${t.id}">Land…</button>`;
    } else if (t.type === 'close') {
      body = `<p>Every done-when item is met. Closing it in ${esc(w.source)} posts a short summary and closes ${esc(w.key)}.</p><div class="preview"><p>Done: ${esc(w.title)}. ${w.doneWhen.map((d) => esc(d.text)).join('; ')}.</p></div>`;
      actions += c === 'close' ? confirmBox(t, `Closes ${w.key} in ${w.source} as you.`, 'do-close', 'Close it') : `<button class="btn primary" data-act="confirm" data-a="close" data-id="${t.id}">Close ${esc(w.key)}…</button>`;
    } else if (t.type === 'todo') {
      body = `<p>${t.due ? 'Due ' + esc(t.due) + '.' : 'No due date.'} No work item: this lives only in your Nexus.</p>${t.help && !t.helped ? `<p class="small">The agent can help: <em>${esc(t.help)}</em>.</p>` : ''}`;
      actions += `<button class="btn primary" data-act="todo-done" data-id="${t.id}">Done</button>${t.help && !t.helped ? `<button class="btn" data-act="todo-help" data-id="${t.id}">Ask the agent</button>` : ''}<button class="btn ghost" data-act="snooze" data-id="${t.id}">Thursday</button>`;
    } else if (t.id === 'j7') {
      body = `<p>Spike and working doc are reviewed. Your lean decides what the write-up argues.</p><label class="small" for="lean-${t.id}">Your call</label><select id="lean-${t.id}"><option value="">Pick one</option><option>Adopt managed Temporal</option><option>Self-host Temporal</option><option>Keep ours and add a UI</option></select>`;
      actions += `<button class="btn primary" data-act="decide" data-id="${t.id}">Decide</button>`;
    } else if (t.type === 'check' && t.assignee === 'you') {
      body = `<p>${esc(t.why || 'Your step.')}</p><label for="note-${t.id}" class="small">What's wrong? (only if you found a problem)</label><textarea id="note-${t.id}" rows="2"></textarea>`;
      actions += `<button class="btn primary" data-act="mark-done" data-id="${t.id}">Done</button>${t.id === 'p5' ? `<button class="btn" data-act="found-problem" data-id="${t.id}">Found a problem</button>` : ''}`;
    } else if (t.assignee === 'agent' || t.took) {
      body = `<p>${t.took ? 'You took this from the agent. Its branch and notes are where it left them.' : t.status === 'todo' ? 'Queued for the agent. It starts when you say.' : 'The agent is on it' + (t.progress ? ': ' + esc(t.progress) : '') + '.'}</p>
        ${t.steps ? `<ul class="steps">${t.steps.map((s, i) => `<li class="${i < t.stepsDone ? 'ok' : i === t.stepsDone ? 'now' : ''}">${i < t.stepsDone ? '✓' : i === t.stepsDone ? '◐' : '○'} ${esc(s)}</li>`).join('')}</ul>` : ''}
        <p class="muted small">${esc(t.why || '')}</p>`;
      if (t.took) actions += `<button class="btn primary" data-act="mark-done" data-id="${t.id}">Done</button>`;
      else if (t.status === 'todo') actions += `<button class="btn primary" data-act="start-agent" data-id="${t.id}">Start now</button>`;
      if (!t.took) actions += take;
      if (w && w.ws) actions += `<button class="btn ghost" data-act="open-wi" data-id="${w.id}">Watch it work</button>`;
    } else {
      body = `<p>${esc(t.why || '')}</p>`;
      actions += `<button class="btn primary" data-act="mark-done" data-id="${t.id}">Done</button>`;
    }
    const nxt = ui.from && !o.docked ? nextIn(ui.from, t.id)[0] : null;
    return `<div class="tp">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="${o.docked ? 'unfocus' : 'close-drawer'}">×</button>
        <div class="tp-meta">${keyChip(t)}${chip(t)}${t.est && t.status !== 'done' ? `<span class="est">${mins(t.est)}</span>` : ''}${w && !o.docked ? `<button class="btn small dig" data-act="open-in-ws" data-id="${t.id}">Open in workspace ↗</button>` : ''}</div>
        <h2>${esc(t.title)}</h2>
        <div class="small muted">${t.status === 'review' && t.reviewer ? `${esc(t.assignee === 'agent' ? 'Done by the agent' : t.assignee)} · reviewer: ${esc(t.reviewer)}` : `Assigned to ${esc(t.assignee)}`}</div>
      </header>
      <div class="tp-body">${body}</div>
      <div class="tp-actions">${actions}</div>
      <div class="tp-foot small muted">${o.docked ? 'Docked here while you dig in: the agent session, doc and tasks are around it. × puts it away; the task stays on the board.' : w ? `<button class="link" data-act="open-wi" data-id="${w.id}">Open the ${esc(w.key)} workspace</button>${w.inProgress && !w.closed ? ` · <button class="link" data-act="park" data-id="${w.id}">Park ${esc(w.key)}</button>` : ''}` : 'A todo: no work item, no workspace.'}
        ${nxt ? `<div class="nextin">Next in ${esc(label(ui.from.wd))}: <button class="link" data-act="open-task" data-id="${nxt.id}" data-from="${esc(ui.from.wd.id)}">${esc(nxt.title)} →</button></div>` : ''}</div>
    </div>`;
  }
  const label = (wd) => (Q[wd.q] || X[wd.q]).label;
  function nextIn(fr, exclude) {
    if (!Q[fr.wd.q] || Q[fr.wd.q].kind !== 'task') return [];
    return Q[fr.wd.q].run(scopeOf(fr.wd, fr.ctx)).filter((t) => t.id !== exclude && t.status !== 'done');
  }
  function afterPanel() {
    const a = ui.after; const t = T(a.done); const list = nextIn(a.from, a.done).slice(0, 4);
    return `<div class="tp">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="close-drawer">×</button>
        <div class="tp-meta">${keyChip(t)}${chip(t)}</div><h2>Handled: ${esc(t.title)}</h2></header>
      <div class="tp-body"><p class="muted small">Pick what's next from ${esc(label(a.from.wd))}, or close this and choose anywhere.</p>
        ${list.map((x) => `<button class="row" data-act="open-task" data-id="${x.id}" data-from="${esc(a.from.wd.id)}"><span class="row-main">${keyChip(x)}<span class="row-title">${esc(x.title)}</span></span><span class="row-side">${x.est ? `<span class="est">${mins(x.est)}</span>` : ''}<span class="verb">${actionVerb(x)}</span></span></button>`).join('') || '<p>Nothing else there.</p>'}</div>
      <div class="tp-actions"><button class="btn" data-act="close-drawer">Close</button></div>
    </div>`;
  }
  function confirmBox(t, text, doAct, lbl) {
    return `<div class="confirm" role="group" aria-label="Confirm"><p><strong>${esc(text)}</strong></p><button class="btn primary" data-act="${doAct}" data-id="${t.id}">${esc(lbl)}</button><button class="btn ghost" data-act="cancel-confirm">Cancel</button></div>`;
  }

  // ---------- modals ----------
  function seedModal(id) {
    const b = S.backlog.find((x) => x.id === id);
    const inProg = S.items.filter((w) => w.inProgress);
    const stuck = inProg.map((w) => ({ w, t: wiTasks(w.id).find(waitingOthers) })).filter((x) => x.t && !wiTasks(x.w.id).some(needsMe) && !wiTasks(x.w.id).some(agentWorking))[0]
      || inProg.map((w) => ({ w, t: wiTasks(w.id).find((t) => waitingOthers(t) && (t.waitDays || 0) >= 5) })).filter((x) => x.t)[0];
    const nudge = inProg.length >= 4 && stuck;
    return `<div class="modal-bg"><div class="modal" role="dialog" aria-label="Start a work item">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="close-modal">×</button><span class="key" data-p="${esc(b.project)}">${esc(b.key)}</span><h2>Start: ${esc(b.title)}</h2><p class="muted small">From ${esc(b.source)}. Nexus creates a workspace and seeds it with what's below.</p></header>
      ${nudge ? `<div class="nudge"><strong>You have ${inProg.length} work items in progress.</strong> ${esc(stuck.w.key)} has been waiting on ${esc(waitWho(stuck.t))} for ${stuck.t.waitDays || 0} days. Park it until that's done?<div class="nudge-actions"><button class="btn primary" data-act="seed-start" data-id="${b.id}" data-a="${stuck.w.id}">Park ${esc(stuck.w.key)} and start</button><button class="btn" data-act="seed-start" data-id="${b.id}">Start anyway</button></div></div>` : ''}
      <div class="seed">
        <h3>Scope</h3><p>${esc(b.scope)}</p>
        <h3>Done when</h3><ul>${b.doneWhen.map((d) => `<li>${esc(d)}</li>`).join('')}</ul>
        <h3>First tasks <span class="muted small">proposed by the agent; untick any you don't want</span></h3>
        ${b.seed.map((s, i) => `<label class="seedrow"><input type="checkbox" data-seed="${i}" checked> ${esc(s[0])} ${who(s[1])}</label>`).join('')}
      </div>
      ${nudge ? '' : `<div class="tp-actions"><button class="btn primary" data-act="seed-start" data-id="${b.id}">Start</button><button class="btn ghost" data-act="close-modal">Cancel</button></div>`}
    </div></div>`;
  }
  function whyModal(id) {
    const r = RENDERED[id] || (findW(id) && { wd: findW(id).wd, ctx: ctxOf(findW(id).sp) });
    if (!r) return '';
    const wd = r.wd; const def = Q[wd.q] || X[wd.q]; const sc = scopeOf(wd, r.ctx);
    const by = wd.by === 'agent' ? `The agent, ${esc(wd.when || '')}. ${esc(wd.why || '')}` : wd.by === 'default' ? 'Part of the default layout for this space.' : wd.by === 'app' ? 'A default view of the Tasks app.' : `You, ${esc(wd.when || '')}.`;
    const spec = { data: wd.q, scope: wd.scope === 'inherit' ? 'follows the space' : wd.scope, shape: wd.shape || '(app widget)', width: wd.size, height: wd.h || 'auto' };
    return `<div class="modal-bg"><div class="modal" role="dialog" aria-label="What is this widget">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="close-modal">×</button><h2>${esc(def.label)}</h2></header>
      <dl class="spec">
        <dt>Shows</dt><dd>${esc(def.rule)}</dd>
        <dt>Scope</dt><dd>${esc(scopeName(sc))}${wd.scope === 'inherit' ? ' <span class="muted small">(follows the space it sits in: on Home everything, in a project that project, in a workspace that work item)</span>' : ''}</dd>
        <dt>Shape</dt><dd>${esc(wd.shape ? SHAPE_NAME[wd.shape] : 'provided by the app')}</dd>
        <dt>Added by</dt><dd>${by}</dd>
      </dl>
      <pre class="diff">${esc(JSON.stringify(spec, null, 2))}</pre>
      <p class="muted small">Every widget is this: data, a scope and a shape. The agent builds with exactly the same parts you do, so anything it adds can be read, changed or removed here.</p>
      <div class="tp-actions">${findW(wd.id) ? `<button class="btn" data-act="w-remove" data-id="${wd.id}">Remove it</button>` : ''}<button class="btn primary" data-act="close-modal">OK</button></div>
    </div></div>`;
  }
  function addModal() {
    const m = ui.modal;
    const scopes = [['inherit', `Follows this space (${scopeName(m.ctx)})`], ['all', 'Everything']].concat(S.projects.map((p) => ['p:' + p.id, p.name]), S.items.filter((w) => !w.closed).map((w) => ['wi:' + w.id, `${w.key} · ${w.title}`]));
    const okScopes = m.pick && X[m.pick] && !X[m.pick].any ? scopes.filter(([k]) => k.startsWith('wi:') || (k === 'inherit' && m.ctx.startsWith('wi:'))) : scopes;
    const preview = m.pick ? `<div class="grid" style="--cols:1">${widget({ id: 'preview', q: m.pick, shape: m.shape, scope: m.scope, size: 1, by: 'you' }, m.ctx, { full: true, app: true, cols: 1 })}</div>` : '';
    return `<div class="modal-bg"><div class="modal wide" role="dialog" aria-label="Add a widget">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="close-modal">×</button><h2>Add a widget to ${esc(spaceName(m.sp))}</h2><p class="muted small">Pick what it shows, then how. Or ask the agent to do it for you.</p></header>
      <div class="catalog">${Object.keys(Q).concat(Object.keys(X)).map((k) => `<button class="cat${m.pick === k ? ' on' : ''}" data-act="add-pick" data-id="${k}"><strong>${esc((Q[k] || X[k]).label)}</strong><span class="muted small">${esc((Q[k] || X[k]).rule)}</span></button>`).join('')}</div>
      ${m.pick ? `<div class="addopts">
        ${(Q[m.pick] || X[m.pick]).shapes && (Q[m.pick] || X[m.pick]).shapes.length > 1 ? `<label class="small" for="add-shape">Shape</label><select id="add-shape" data-change="add-shape">${(Q[m.pick] || X[m.pick]).shapes.map((s) => `<option value="${s}"${m.shape === s ? ' selected' : ''}>${SHAPE_NAME[s]}</option>`).join('')}</select>` : ''}
        <label class="small" for="add-scope">Scope</label><select id="add-scope" data-change="add-scope">${okScopes.map(([k, l]) => `<option value="${esc(k)}"${m.scope === k ? ' selected' : ''}>${esc(l)}</option>`).join('')}</select></div>
        <div class="previewbox"><span class="muted small">Preview</span>${preview}</div>` : ''}
      <div class="tp-actions"><button class="btn primary" data-act="add-confirm"${m.pick ? '' : ' disabled'}>Add</button><button class="btn ghost" data-act="close-modal">Cancel</button></div>
    </div></div>`;
  }

  // ---------- popovers ----------
  function spacesPop() {
    const ws = S.items.filter((w) => !w.closed);
    const st = (w) => (!w.inProgress ? 'parked' : w.ws ? (w.ws.state === 'suspended' ? 'session suspended' : w.ws.state === 'closed' ? 'closed' : 'session open') : '');
    return `<div class="pop pop-l" role="menu">
      <button class="mi${ui.space === 'home' && !ui.page ? ' on' : ''}" data-act="go" data-id="home">Home</button>
      <div class="mi-h">Projects</div>${S.projects.map((p) => `<button class="mi${ui.space === 'p:' + p.id && !ui.page ? ' on' : ''}" data-act="go" data-id="p:${p.id}">${esc(p.name)} <span class="muted small">${needsList().filter(inT('p:' + p.id)).length || ''}</span></button>`).join('')}
      <div class="mi-h">Workspaces</div>${ws.map((w) => `<button class="mi${ui.space === 'wi:' + w.id && !ui.page ? ' on' : ''}" data-act="go" data-id="wi:${w.id}"><span class="key" data-p="${esc(w.project)}">${esc(w.key)}</span> ${esc(w.title)} <span class="muted small">${st(w)}</span></button>`).join('')}
    </div>`;
  }
  function appsPop() {
    return `<div class="pop" role="menu"><div class="mi-h">Tasks</div>${APPVIEWS.map(([k, l]) => `<button class="mi" data-act="open-app" data-id="${k}">${l}</button>`).join('')}
      <p class="muted small pop-note">Terminal, browser, review and the rest of Nexus are apps too; they're not in this mock.</p></div>`;
  }
  function simPop() {
    return `<div class="pop" role="menu"><p class="small muted">Things that happen in the world. Watch the widgets update on their own.</p>${Object.keys(SIMS).map((k) => { const s = SIMS[k]; const ok = s.ok(); return `<button class="simbtn" data-act="run" data-id="${k}"${ok ? '' : ' disabled'}><span>${esc(s.label)}</span>${!ok ? `<span class="muted small">${esc(s.hint || 'already happened')}</span>` : ''}</button>`; }).join('')}</div>`;
  }

  // ---------- frame ----------
  function render() {
    const app = document.getElementById('app'); if (!app) return;
    const focusEl = document.activeElement && document.activeElement.id; const pos = focusEl && document.activeElement.selectionStart;
    RENDERED = {};
    const main = ui.page && ui.page.kind === 'app' ? appView() : ui.page && ui.page.kind === 'full' ? fullView() : spaceView(ui.space);
    const drawer = ui.after && T(ui.after.done) ? afterPanel() : ui.drawer && T(ui.drawer) ? panel(T(ui.drawer)) : '';
    const modal = !ui.modal ? '' : ui.modal.kind === 'seed' ? seedModal(ui.modal.id) : ui.modal.kind === 'why' ? whyModal(ui.modal.id) : addModal();
    const n = needsList().length;
    const where = ui.page && ui.page.kind === 'app' ? 'Tasks app' : ui.page ? 'Full view' : spaceName(ui.space);
    app.innerHTML = `
      <header class="top">
        <div class="brand">Nexus <span class="muted small">mock v2</span></div>
        <button class="spacebtn" data-act="pop" data-id="spaces" aria-expanded="${ui.pop === 'spaces'}">${esc(where.length > 34 ? where.slice(0, 33) + '…' : where)} ▾</button>
        <div class="top-r">
          <button class="needs" data-act="open-app" data-id="needs" title="Always here, whatever your layout">${n} need you</button>
          <span class="clock">${esc(S.clock)}</span>
          <button class="btn small" data-act="chats" aria-expanded="${!!ui.chats}">Chats</button>
          <button class="btn small" data-act="pop" data-id="apps" aria-expanded="${ui.pop === 'apps'}">Apps</button>
          <button class="btn small" data-act="pop" data-id="sim" aria-expanded="${ui.pop === 'sim'}">Make something happen</button>
          <button class="btn ghost small" data-act="reset">Reset</button>
        </div>
        ${ui.pop === 'spaces' ? spacesPop() : ui.pop === 'apps' ? appsPop() : ui.pop === 'sim' ? simPop() : ''}
      </header>
      <main class="main">${main}</main>
      ${drawer ? `<div class="drawer-bg" data-act="close-drawer"></div><aside class="drawer" aria-label="Task">${drawer}</aside>` : ui.chats ? `<div class="drawer-bg" data-act="chats-close"></div><aside class="drawer chats" aria-label="Conversations">${chatsPanel()}</aside>` : ''}
      ${modal}
      <div class="toasts" aria-live="polite">${S.toasts.map((x) => `<div class="toast">${esc(x.text)}${x.undo && S.undo ? ' <button class="link undo" data-act="undo">Undo</button>' : ''}</div>`).join('')}</div>`;
    if (focusEl) { const el = document.getElementById(focusEl); if (el && el.tagName !== 'BUTTON') { el.focus(); try { if (pos != null) el.setSelectionRange(pos, pos); } catch (e) { /* select */ } } }
  }

  document.addEventListener('click', (e) => {
    const el = e.target.closest('[data-act]');
    if (!el) { if ((ui.pop || ui.menu) && !e.target.closest('.pop, .wmenu')) { ui.pop = null; ui.menu = null; render(); } return; }
    if (el.classList.contains('drawer-bg') && e.target !== el) return;
    const a = el.getAttribute('data-act'); const id = el.getAttribute('data-id');
    if (!act[a]) return;
    e.preventDefault();
    if (a !== 'w-menu' && !el.closest('.wmenu')) ui.menu = null;
    if (a !== 'pop' && a !== 'sim' && !el.closest('.pop')) ui.pop = null;
    const before = ui.drawer; const fromW = ui.from;
    if (a === 'fu') act.fu(id, el.getAttribute('data-a'), el.getAttribute('data-b'));
    else if (a === 'open-task') act['open-task'](id, el.getAttribute('data-from'));
    else act[a](id, el.getAttribute('data-a'));
    if (FINISH.has(a) && before && !ui.drawer && fromW && nextIn(fromW, before).length) ui.after = { from: fromW, done: before };
    render();
  });
  document.addEventListener('submit', (e) => { const f = e.target.closest('[data-form]'); if (!f) return; e.preventDefault(); act[f.getAttribute('data-form')](f.getAttribute('data-id'), f); render(); });
  document.addEventListener('input', (e) => { const el = e.target; if (el.getAttribute('data-input') === 'draft') T(el.getAttribute('data-id')).draft = el.value; });
  document.addEventListener('change', (e) => {
    const k = e.target.getAttribute('data-change'); if (!k) return;
    if (k === 'app-scope') ui.appScope = e.target.value;
    if (k === 'add-shape') ui.modal.shape = e.target.value;
    if (k === 'add-scope') ui.modal.scope = e.target.value;
    render();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    if (ui.modal) ui.modal = null; else if (ui.drawer || ui.after || ui.chats) { ui.drawer = null; ui.after = null; ui.chats = null; } else if (ui.pop || ui.menu) { ui.pop = null; ui.menu = null; } else if (ui.edit) ui.edit = false; else if (ui.page) ui.page = null; else return;
    render();
  });

  function fresh() { const d = window.NexusMock.makeData(); d.layouts = {}; d.undo = null; d.spaceCfg = {}; d.chat = seedChat(); return d; }
  function seedChat() {
    return {
      home: [
        { who: 'you', when: 'Fri 17:41', html: 'Remind me on Monday that conference hotel rates go up this week' },
        { who: 'agent', when: 'Fri 17:41', html: 'Added a todo: <em>Book a hotel for the conference (Dec 2–4)</em>, due Tuesday. It will be in Needs you on Monday.' },
        { who: 'you', when: 'Fri 17:58', html: 'Anything still waiting on me before the weekend?' },
        { who: 'agent', when: 'Fri 17:58', html: 'Two reviews (MR !23 and MR !41) are ready, but nobody is blocked on them until Monday. IT-882 has been idle 4 days; I\'ll draft a nudge if it reaches 5.' },
      ],
      'wi:nex12': [
        { who: 'you', when: 'Thu 16:20', html: 'Draft the design doc from our notes. Polling, not webhooks.' },
        { who: 'agent', when: 'Thu 16:52', html: 'Design doc v1 is ready. I sent it to the platform lead for approval, as you asked.' },
      ],
    };
  }
  S = fresh();
  render();
  window.NexusMock.debug = { get state() { return S; }, ui, needsList, act, render, SIMS, Q, layoutFor, findW };
})();

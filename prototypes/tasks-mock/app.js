// Nexus tasks mock: three home-screen variants over one task set.
// Throwaway. Vanilla JS, no build, mock data only.
(function () {
  'use strict';

  let S;
  const ui = { view: 'inbox', page: null, drawer: null, modal: null, confirm: null, sim: false, boardScope: 'all', sinceOpen: true, wiBoard: false };
  const VIEWS = ['inbox', 'plan', 'items', 'board'];

  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const T = (id) => S.tasks.find((t) => t.id === id);
  const W = (id) => S.items.find((w) => w.id === id) || S.backlog.find((w) => w.id === id);
  const P = (id) => S.projects.find((p) => p.id === id);
  const projOf = (t) => (t.wi ? W(t.wi).project : t.project);
  const keyOf = (t) => (t.wi ? W(t.wi).key : 'Personal');
  const mins = (n) => (n >= 60 ? `${Math.floor(n / 60)}h ${n % 60 ? (n % 60) + 'm' : ''}`.trim() : `${n}m`);

  // ---------- derived state: the board runs underneath ----------
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
    const ts = wiTasks(wi);
    const mine = ts.filter(needsMe);
    if (W(wi).closed) return { kind: 'done', label: 'Done' };
    if (W(wi).complete) return { kind: 'you', label: 'Your move', n: mine.length, note: 'ready to close' };
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
  function feed(who, text, why, wi) { S.feed.unshift({ when: S.clock.replace('Mon Nov 16, ', 'Mon '), who, text, why, wi, fresh: true }); }
  function toast(text) {
    const id = ++S.seq; S.toasts.push({ id, text });
    setTimeout(() => { S.toasts = S.toasts.filter((x) => x.id !== id); render(); }, 4500);
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
        else if (t.assignee === 'agent') { setStatus(t, 'doing', 'agent', 'Unblocked and stays in its own workspace, so it started on its own.'); }
        else setStatus(t, 'todo', 'board', 'What it waited on is done.');
      }
    }
    for (const w of S.items) {
      if (!w.inProgress && w.wakeOn && T(w.wakeOn).status === 'done') {
        w.inProgress = true; w.wakeOn = null; w.parkNote = null;
        feed('rule', `${w.key} is back in progress`, 'The thing it was parked on happened.', w.id);
        toast(`${w.key} is back: what it was waiting on happened`);
      }
      if (!w.complete && !w.closed && w.doneWhen.every(met)) {
        w.complete = true;
        const id = 'close-' + w.id;
        if (!T(id)) S.tasks.push({ id, wi: w.id, title: `Close ${w.key} in ${w.source}`, assignee: 'you', status: 'todo', est: 1, type: 'close', why: 'Every done-when item is met' });
        feed('board', `${w.key}: every done-when item is met`, 'Added a task to close it in the source.', w.id);
        toast(`${w.key} is done. Closing it in ${w.source} is one press.`);
      }
    }
  }
  function addTask(t) { t.id = t.id || 'x' + (++S.seq); S.tasks.push(t); flash(t.id); return t; }

  const act = {
    'set-view': (v) => { ui.view = v; ui.page = null; ui.drawer = null; try { history.replaceState(null, '', '#' + v); } catch (e) { location.hash = v; } },
    'open-task': (id) => { ui.drawer = id; ui.confirm = null; },
    'close-drawer': () => { ui.drawer = null; ui.confirm = null; },
    'open-wi': (id) => { ui.page = { kind: 'wi', id }; ui.drawer = null; ui.wiBoard = false; },
    'open-ws': (id) => { const w = W(id); if (w.ws && w.ws.state === 'suspended') { w.ws.state = 'resumed'; feed('you', `Resumed the ${w.key} workstream`, null, id); } ui.page = { kind: 'ws', id }; ui.drawer = null; },
    'open-doc': (id) => { ui.page = { kind: 'doc', id }; ui.drawer = null; },
    back: () => { ui.page = null; },
    'suspend-ws': (id) => { const w = W(id); w.ws.state = 'suspended'; w.ws.since = S.clock.replace('Mon Nov 16, ', 'Mon '); w.ws.whileAway = []; feed('you', `Suspended the ${w.key} workstream`, 'Context saved; it resumes where it stopped.', id); ui.page = null; toast(`${w.key} workstream suspended. Nothing lost.`); },
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
      t.reviewerBack = 'you'; ui.drawer = null; toast('Sent back to the same session with your note');
    },
    take: (id) => {
      const t = T(id); const w = t.wi && W(t.wi);
      t.took = true; t.assignee = 'you'; t.status = t.status === 'review' ? 'doing' : (t.status === 'blocked' ? 'blocked' : 'doing'); t.reviewer = null; flash(id);
      feed('you', `You took "${t.title}"`, `The agent stopped at a clean point${w && w.ws ? ` and left branch ${w.ws.branch} and its notes` : ''}.`, t.wi);
      toast('It\'s yours. The agent stopped and left its branch and notes.');
    },
    handback: (id) => { const t = T(id); t.took = false; t.assignee = 'agent'; t.status = 'doing'; t.progress = 'picked up from your changes'; flash(id); feed('you', `Handed "${t.title}" back to the agent`, 'It picks up from your commits.', t.wi); ui.drawer = null; toast('Handed back'); },
    confirm: (a, id) => { ui.confirm = { act: a, id }; },
    'cancel-confirm': () => { ui.confirm = null; },
    'do-post-answer': (id) => { const t = T(id); ui.confirm = null; setStatus(t, 'done', 'you', `You posted your answer to ${t.postTo}.`); settle(); ui.drawer = null; toast(`Posted to ${t.postTo}, as you`); },
    'save-answer': (id) => { const t = T(id); setStatus(t, 'done', 'you', 'Answer saved. It goes out with the SEC-431 reply.'); settle(); ui.drawer = null; toast('Answer saved for the SEC-431 reply'); },
    'do-post': (id) => { const t = T(id); ui.confirm = null; setStatus(t, 'done', 'you', 'You posted both replies to SEC-431.'); settle(); const w = W(t.wi); w.rounds[w.rounds.length - 1].closed = true; ui.drawer = null; toast('Posted to SEC-431. Their move now.'); },
    'do-nudge': (id) => { const t = T(id); ui.confirm = null; t.nudged = true; t.nudge = null; feed('you', `Sent the nudge on ${t.title.match(/\(([^)]+)\)/) ? t.title.match(/\(([^)]+)\)/)[1] : 'the request'}`, null, t.wi); toast('Nudge sent, as you'); },
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
      addTask({ wi: t.wi, title: `Fix: ${note}`, assignee: 'agent', status: 'doing', type: 'agent', progress: 'just started' });
      t.blockedBy = [S.tasks[S.tasks.length - 1].id]; t.status = 'blocked';
      const land = S.tasks.find((x) => x.wi === t.wi && x.type === 'land'); if (land && land.status !== 'done') { land.status = 'blocked'; land.blockedBy = [S.tasks[S.tasks.length - 1].id]; }
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
    'add-todo': () => {
      const inp = document.getElementById('todo-text'); const due = document.getElementById('todo-due');
      const text = inp && inp.value.trim(); if (!text) return;
      const d = due ? due.value : '';
      const map = { today: [0, 'Today'], tomorrow: [1, 'Tue'], week: [4, 'Fri'] };
      addTask({ wi: null, project: 'personal', title: text, assignee: 'you', status: 'todo', est: 10, type: 'todo', dueIn: map[d] ? map[d][0] : null, due: map[d] ? map[d][1] : null });
      feed('you', `Added a todo: ${text}`, 'No work item. It shows in Needs you when it is due soon.', null);
      inp.value = ''; toast(map[d] && map[d][0] <= 2 ? 'Added. It\'s in Needs you.' : 'Added to Personal');
    },
    park: (id) => { const w = W(id); w.inProgress = false; if (w.ws && w.ws.state !== 'closed') w.ws.state = 'suspended'; const wt = wiTasks(id).find(waitingOthersRaw); w.wakeOn = wt ? wt.id : null; w.parkNote = wt ? `wakes when ${wt.title.replace(/^Grant /, '').replace(/^Approve /, '')} is done` : 'parked'; feed('you', `Parked ${w.key}`, w.parkNote, id); ui.page = null; toast(`${w.key} parked. Its tasks are hidden, not deleted.`); },
    unpark: (id) => { const w = W(id); w.inProgress = true; w.wakeOn = null; w.parkNote = null; feed('you', `Brought ${w.key} back`, null, id); toast(`${w.key} is back in progress`); },
    'start-wi': (id) => { ui.modal = { kind: 'seed', id }; },
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
      feed('you', `Started ${w.key}: ${w.title}`, `Workstream created with the scope, done-when and ${made.length} tasks.`, w.id);
      ui.modal = null; toast(`${w.key} started. Its first task is underway.`);
    },
    'toggle-watcher': (id) => { const w = W(id); w.watcher.on = !w.watcher.on; w.watcher.log.unshift(`${S.clock.replace('Mon Nov 16, ', 'Mon ')}  ${w.watcher.on ? 'resumed' : 'paused'} by you`); toast(w.watcher.on ? 'Watcher resumed' : 'Watcher paused. Nothing checks SEC-431 until you resume it.'); },
    'fix-split': () => { toast('In the real thing: tell the agent what it missed, and it redoes the split'); },
    fu: (id, idx, choice) => { T(id).packet.followups[+idx].choice = choice; },
    'plan-up': (id) => { const o = S.plan.order; ensureInOrder(id); const i = o.indexOf(id); if (i > 0) { o.splice(i, 1); o.splice(i - 1, 0, id); } },
    'plan-down': (id) => { const o = S.plan.order; ensureInOrder(id); const i = o.indexOf(id); if (i < o.length - 1) { o.splice(i, 1); o.splice(i + 1, 0, id); } },
    'plan-defer': (id) => { S.plan.deferred.push(id); toast('Not today. It stays on the board.'); },
    'plan-undefer': (id) => { S.plan.deferred = S.plan.deferred.filter((x) => x !== id); },
    'plan-start': () => { S.plan.started = true; S.plan.doneAtStart = S.tasks.filter((t) => t.status === 'done').length; },
    'plan-skip': (id) => { ensureInOrder(id); S.plan.order = S.plan.order.filter((x) => x !== id).concat([id]); },
    'plan-edit': () => { S.plan.started = false; },
    sim: () => { ui.sim = !ui.sim; },
    'since-toggle': () => { ui.sinceOpen = !ui.sinceOpen; },
    'wi-board': () => { ui.wiBoard = !ui.wiBoard; },
    reset: () => { S = window.NexusMock.makeData(); ui.drawer = null; ui.page = null; ui.modal = null; ui.confirm = null; toast('Reset to Monday 08:30'); },
    run: (name) => { SIMS[name].run(); ui.sim = false; },
  };
  const anyAgentTask = () => S.tasks.find((t) => agentWorking(t) && !['j6', 'n7', 'hotel'].includes(t.id) && !t.took);
  const waitingOthersRaw = (t) => t.status !== 'done' && t.status !== 'blocked' && ((t.status === 'review' && t.reviewer && t.reviewer !== 'you') || (t.assignee !== 'you' && t.assignee !== 'agent'));
  function ensureInOrder(id) { if (!S.plan.order.includes(id)) S.plan.order.push(id); }

  // ---------- simulated events: the world moves, the cards follow ----------
  const SIMS = {
    spike: {
      label: 'The agent finishes the Temporal spike', ok: () => T('j6').status === 'doing' && T('j6').assignee === 'agent',
      run() { S.clock = 'Mon Nov 16, 10:40'; const t = T('j6'); t.reviewer = 'you'; setStatus(t, 'review', 'agent', 'Spike finished: output matched on 7 of 7 nights.'); t.est = 15; t.why = 'Agent finished at 10:40'; toast('Spike finished. It\'s in Needs you as a review.'); },
    },
    build: {
      label: 'The agent finishes the Jira plugin build', ok: () => T('n7').status === 'doing' && T('n7').assignee === 'agent',
      run() { S.clock = 'Mon Nov 16, 11:15'; const t = T('n7'); t.reviewer = 'you'; t.est = 25; t.why = 'Agent finished at 11:15. You review before it goes to the team.'; t.approveLabel = 'Approve and open the MR for team review'; setStatus(t, 'review', 'agent', 'All 9 steps done, tests pass.'); toast('Plugin build finished. You review it before the team sees it.'); },
    },
    it: {
      label: 'IT grants Jira API access (IT-882 resolved)', ok: () => T('n2').status !== 'done',
      run() { S.clock = 'Mon Nov 16, 13:05'; setStatus(T('n2'), 'done', 'source', 'IT-882 was resolved in IT\'s tracker.'); settle(); toast('IT granted access. "Test on live Jira" is unblocked and waits for your start.'); },
    },
    lead: {
      label: 'The platform lead approves the design', ok: () => T('n5').status === 'review',
      run() { S.clock = 'Mon Nov 16, 14:20'; setStatus(T('n5'), 'done', 'source', 'Approved on the design doc.'); settle(); toast('Design approved'); },
    },
    sec3: {
      label: 'Security replies on SEC-431 (round 3)', ok: () => T('s7').status === 'doing',
      hint: 'Post your round 2 replies first',
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
        feed('watcher', 'New comment on SEC-431, read as 2 asks', 'One agent task (audit logging) started in its workspace; one for you after it.', 'sec431');
        toast('Security replied. The watcher split it into 2 asks; the agent started the code change.');
      },
    },
    any: {
      label: 'The agent finishes another task', ok: () => !!anyAgentTask(),
      hint: 'No other agent task is running',
      run() {
        const t = anyAgentTask(); t.reviewer = 'you'; t.est = 10; t.why = 'Agent finished just now';
        t.packet = { summary: `Finished: ${t.title}. Changes are on the workstream branch with notes.`, checks: ['Tests pass', 'Rebased on main'], files: [['(see the workstream)', '']], diff: ['+ …'], followups: [] };
        setStatus(t, 'review', 'agent', 'Finished; waiting for your review.'); toast(`Finished: ${t.title}. It's in Needs you.`);
      },
    },
    hotel: {
      label: 'The agent finishes the hotel search', ok: () => T('hotel') && T('hotel').status === 'doing',
      hint: 'Ask the agent to help with the hotel todo first',
      run() {
        const t = T('hotel'); t.reviewer = 'you'; t.est = 5; t.type = 'review-options';
        t.options = [['Harbor Inn', '$219/night', '6 min walk', 'free cancellation'], ['Hotel Meridian', '$245/night', 'across the street', 'breakfast included'], ['The Foundry', '$189/night', '15 min by tram', 'no cancellation']];
        setStatus(t, 'review', 'agent', 'Found 3 options under $250.'); toast('3 hotel options are ready. Booking stays with you.');
      },
    },
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
  const wiChip = (t) => `<span class="key" data-p="${esc(projOf(t))}">${esc(keyOf(t))}</span>`;
  const flashCls = (id) => (S.flash.includes(id) ? ' flash' : '');
  const blockedText = (t) => (t.blockedBy || []).map((id) => T(id)).filter((x) => x.status !== 'done').map((x) => `${esc(x.title)} (${x.assignee === 'you' ? 'you' : esc(x.assignee)})`).join(', ');
  function actionVerb(t) {
    if (t.status === 'review') return t.type === 'review-doc' ? 'Read' : t.type === 'review-options' ? 'Choose' : 'Review';
    return { answer: 'Answer', post: 'Post', land: 'Land', 'start-live': 'Start', close: 'Close', check: 'Do', todo: 'Do' }[t.type] || 'Open';
  }
  function row(t, opts = {}) {
    const sub = t.status === 'blocked' ? `Blocked on ${blockedText(t)}` : (t.why || '');
    return `<button class="row${flashCls(t.id)}" data-act="open-task" data-id="${t.id}">
      ${opts.rank ? `<span class="rank">${opts.rank}</span>` : ''}
      <span class="row-main">${wiChip(t)}<span class="row-title">${esc(t.title)}</span><span class="row-sub">${esc(sub)}</span></span>
      <span class="row-side">${t.dueIn != null && t.dueIn <= 2 ? `<span class="due">due ${esc(t.due)}</span>` : ''}${t.est ? `<span class="est">${mins(t.est)}</span>` : ''}<span class="verb">${actionVerb(t)}</span></span>
    </button>`;
  }
  function miniRow(t, note) {
    return `<button class="mini${flashCls(t.id)}" data-act="open-task" data-id="${t.id}">${wiChip(t)}<span class="mini-title">${esc(t.title)}</span><span class="mini-note">${esc(note || '')}</span></button>`;
  }
  function since() {
    const list = S.feed.slice(0, ui.sinceOpen ? 9 : 0);
    const nNew = S.feed.length;
    return `<section class="panel since">
      <header class="panel-h"><h3>Since you were here <span class="muted">(${esc(S.lastSeen)})</span></h3><button class="link" data-act="since-toggle">${ui.sinceOpen ? 'Hide' : `Show ${nNew}`}</button></header>
      ${list.map((f) => `<div class="feed${f.fresh ? ' fresh' : ''}"><span class="when">${esc(f.when)}</span><span class="by by-${esc(f.who)}">${esc(f.who)}</span><div class="feed-body"><div>${esc(f.text)}</div>${f.why ? `<div class="muted small">${esc(f.why)}</div>` : ''}</div></div>`).join('')}
    </section>`;
  }
  function addTodo() {
    return `<form class="addtodo" data-form="add-todo"><label class="sr" for="todo-text">New todo</label><input id="todo-text" placeholder="Add a todo, no project needed" autocomplete="off"><label class="sr" for="todo-due">Due</label><select id="todo-due"><option value="">no date</option><option value="today">today</option><option value="tomorrow">tomorrow</option><option value="week">this week</option></select><button class="btn" type="submit">Add</button></form>`;
  }
  function contextColumn() {
    const ag = S.tasks.filter(agentWorking); const wt = S.tasks.filter(waitingOthers);
    const todos = S.tasks.filter((t) => !t.wi && t.assignee === 'you' && t.status !== 'done' && !needsMe(t));
    return `${since()}
      <section class="panel"><header class="panel-h"><h3>Agents working <span class="count">${ag.length}</span></h3></header>${ag.map((t) => miniRow(t, t.progress)).join('') || '<p class="muted small">None running.</p>'}</section>
      <section class="panel"><header class="panel-h"><h3>Waiting on others <span class="count">${wt.length}</span></h3></header>${wt.map((t) => miniRow(t, `${waitWho(t)}${t.waitDays != null ? ' · ' + t.waitDays + 'd' : ''}`)).join('') || '<p class="muted small">Nobody.</p>'}</section>
      <section class="panel"><header class="panel-h"><h3>Personal</h3></header>${todos.map((t) => miniRow(t, t.due ? 'due ' + t.due : '')).join('')}${addTodo()}</section>`;
  }

  // ---------- variant A: one ranked list ----------
  function viewInbox() {
    const list = needsList(); const total = list.reduce((s, t) => s + (t.est || 0), 0);
    return `<div class="split">
      <section class="main-col">
        <div class="summary"><strong>${list.length} things need you</strong><span class="muted"> · about ${mins(total)} · ${S.tasks.filter(agentWorking).length} agents working · waiting on ${S.tasks.filter(waitingOthers).length}</span></div>
        <div class="list">${list.map((t) => row(t)).join('') || '<p class="empty">Nothing needs you. Agents and other people have the ball.</p>'}</div>
        <p class="muted small">Order: what someone else is waiting on, then due dates, then size. Open a line to act on it.</p>
      </section>
      <aside class="side-col">${contextColumn()}</aside>
    </div>`;
  }

  // ---------- variant B: plan the day, then one at a time ----------
  function viewPlan() {
    const list = planList(); const total = list.reduce((s, t) => s + (t.est || 0), 0);
    if (!S.plan.started) {
      const deferred = S.plan.deferred.map(T).filter((t) => t && t.status !== 'done');
      return `<div class="plan">
        <div class="plan-intro"><h2>Good morning. Here's a plan for today.</h2><p class="muted">${list.length} things, about ${mins(total)}. Reorder, push to tomorrow, then start. The rest of the board keeps moving without you.</p></div>
        ${since()}
        <ol class="agenda">${list.map((t, i) => `<li class="agenda-row${flashCls(t.id)}">
          <span class="rank">${i + 1}</span>
          <div class="agenda-main">${wiChip(t)}<button class="link strong" data-act="open-task" data-id="${t.id}">${esc(t.title)}</button><div class="muted small">${esc(t.planReason || t.why || 'New since you planned')}</div></div>
          <span class="est">${t.est ? mins(t.est) : ''}</span>
          <span class="agenda-ctl"><button class="icon" aria-label="Move up" data-act="plan-up" data-id="${t.id}">↑</button><button class="icon" aria-label="Move down" data-act="plan-down" data-id="${t.id}">↓</button><button class="btn ghost small" data-act="plan-defer" data-id="${t.id}">Not today</button></span>
        </li>`).join('')}</ol>
        ${deferred.length ? `<div class="deferred"><span class="muted small">Not today:</span> ${deferred.map((t) => `<button class="pill" data-act="plan-undefer" data-id="${t.id}">${esc(t.title)} ↺</button>`).join(' ')}</div>` : ''}
        <div class="plan-go"><button class="btn primary big" data-act="plan-start">Start my day</button><span class="muted small">Agents: ${S.tasks.filter(agentWorking).length} working · Waiting on: ${S.tasks.filter(waitingOthers).map(waitWho).join(', ') || 'nobody'}</span></div>
        <section class="panel">${addTodo()}</section>
      </div>`;
    }
    const cur = list[0];
    const doneToday = S.tasks.filter((t) => t.status === 'done').length - (S.plan.doneAtStart || 0);
    const pct = Math.round((doneToday / Math.max(1, doneToday + list.length)) * 100);
    return `<div class="focus">
      <div class="focus-bar"><div class="progress"><span style="width:${pct}%"></span></div><span class="small">${doneToday} done · ${list.length} left · about ${mins(total)}</span><button class="link" data-act="plan-edit">Edit plan</button></div>
      ${cur ? `<div class="focus-card${flashCls(cur.id)}">${panel(cur, true)}<div class="focus-nav"><button class="btn ghost" data-act="plan-skip" data-id="${cur.id}">Skip for now</button><span class="muted small">Up next: ${list[1] ? esc(list[1].title) : 'nothing'}</span></div></div>`
        : `<div class="clear"><h2>You're clear.</h2><p class="muted">Nothing needs you right now. ${S.tasks.filter(agentWorking).length} agent tasks are running and ${S.tasks.filter(waitingOthers).length} things wait on other people. New items will appear here as they arrive.</p></div>`}
      <div class="focus-under">${contextColumn()}</div>
    </div>`;
  }

  // ---------- variant C: work items as cards ----------
  function viewItems() {
    const groups = S.projects.filter((p) => p.id !== 'personal').map((p) => ({ p, items: S.items.filter((w) => w.project === p.id && w.inProgress) })).filter((g) => g.items.length);
    const todos = S.tasks.filter((t) => !t.wi && t.status !== 'done');
    const notIn = S.items.filter((w) => !w.inProgress && !w.closed).concat(S.backlog);
    const nIn = S.items.filter((w) => w.inProgress).length;
    return `<div class="items-view">
      <div class="summary"><strong>${nIn} work items in progress</strong><span class="muted"> · ${needsList().length} things need you · ${S.tasks.filter(waitingOthers).length} waiting on others</span></div>
      <div class="cards">${groups.map((g) => g.items.map((w) => card(w)).join('')).join('')}
        <article class="card personal"><header><span class="key" data-p="personal">Personal</span><h3>Todos</h3></header>
          ${todos.map((t) => `<button class="todo-line${flashCls(t.id)}" data-act="open-task" data-id="${t.id}"><span>${esc(t.title)}</span><span class="muted small">${t.due ? 'due ' + esc(t.due) : ''}${t.assignee === 'agent' ? 'agent: ' + esc(t.progress || '') : ''}</span></button>`).join('')}
          ${addTodo()}
        </article>
      </div>
      <section class="panel"><header class="panel-h"><h3>Not in progress</h3><span class="muted small">From your trackers. Nexus reads them; it doesn't own them.</span></header>
        ${notIn.map((w) => `<div class="backlog-row"><span class="key" data-p="${esc(w.project)}">${esc(w.key)}</span><span class="grow">${esc(w.title)} <span class="muted small">${esc(w.source)}${w.parkNote ? ' · parked, ' + esc(w.parkNote) : ''}</span></span>${w.parkNote ? `<button class="btn small" data-act="unpark" data-id="${w.id}">Bring back</button>` : `<button class="btn small" data-act="start-wi" data-id="${w.id}">Start</button>`}</div>`).join('')}
      </section>
      ${since()}
    </div>`;
  }
  function card(w) {
    const m = move(w.id); const n = nextAction(w.id); const metN = w.doneWhen.filter(met).length;
    return `<article class="card${n && S.flash.includes(n.id) ? ' flash' : ''}">
      <header><span class="key" data-p="${esc(w.project)}">${esc(w.key)}</span><span class="src">${esc(w.source)}</span><span class="move ${m.kind}">${esc(m.label)}${m.n ? ' · ' + m.n : ''}${m.note && m.kind !== 'agent' ? ' · ' + esc(m.note) : ''}</span></header>
      <h3><button class="link strong" data-act="open-wi" data-id="${w.id}">${esc(w.title)}</button></h3>
      <div class="dw"><div class="progress"><span style="width:${Math.round((metN / w.doneWhen.length) * 100)}%"></span></div><span class="small muted">${metN} of ${w.doneWhen.length} done-when</span></div>
      ${n ? `<div class="next"><span class="muted small">Next</span><button class="next-btn" data-act="open-task" data-id="${n.id}">${chip(n)} ${esc(n.title)}</button></div>` : ''}
      <footer><button class="link" data-act="open-wi" data-id="${w.id}">What's left</button><button class="link" data-act="open-ws" data-id="${w.id}">${w.ws && w.ws.state === 'suspended' ? 'Resume workstream' : 'Open workstream'}</button><button class="link" data-act="park" data-id="${w.id}">Park</button></footer>
    </article>`;
  }

  // ---------- the board, available but not the default ----------
  function viewBoard(scopeWi) {
    const cols = ['todo', 'doing', 'blocked', 'review', 'done'];
    let ts = S.tasks.filter(live);
    if (scopeWi) ts = ts.filter((t) => t.wi === scopeWi);
    else if (ui.boardScope !== 'all') ts = ts.filter((t) => projOf(t) === ui.boardScope);
    const head = scopeWi ? '' : `<div class="board-h"><p class="muted">This board runs underneath every view. Cards move when work happens; you never have to drag them.</p><label class="small" for="scope">Show</label><select id="scope" data-change="scope"><option value="all"${ui.boardScope === 'all' ? ' selected' : ''}>everything</option>${S.projects.map((p) => `<option value="${p.id}"${ui.boardScope === p.id ? ' selected' : ''}>${esc(p.name)}</option>`).join('')}</select></div>`;
    return `${head}<div class="board">${cols.map((c) => `<div class="col"><h4>${statusLabel[c]} <span class="count">${ts.filter((t) => t.status === c).length}</span></h4>${ts.filter((t) => t.status === c).map((t) => `<button class="bcard${flashCls(t.id)}" data-act="open-task" data-id="${t.id}">${wiChip(t)}<span>${esc(t.title)}</span><span class="bmeta">${who(t.reviewer && t.status === 'review' ? t.reviewer : t.assignee)}${t.status === 'review' ? ' reviews' : ''}</span></button>`).join('')}</div>`).join('')}</div>`;
  }

  // ---------- pages ----------
  function pageWi(w) {
    const ts = wiTasks(w.id); const m = move(w.id);
    const grp = [
      ['Needs you', ts.filter(needsMe)], ['Agent working', ts.filter(agentWorking)], ['Waiting on others', ts.filter(waitingOthers)],
      ['Blocked', ts.filter((t) => t.status === 'blocked')], ['Not started', ts.filter((t) => t.status === 'todo' && !needsMe(t))], ['Done', ts.filter((t) => t.status === 'done')],
    ].filter((g) => g[1].length);
    return `<div class="page">
      <button class="link" data-act="back">← Back</button>
      <header class="page-h"><span class="key big" data-p="${esc(w.project)}">${esc(w.key)}</span><div><h2>${esc(w.title)}</h2><p class="muted small">${esc(w.source)}: ${esc(w.sourceRef)} · read by Nexus, owned by ${esc(w.source)}</p></div><span class="move ${m.kind}">${esc(m.label)}</span></header>
      <div class="split">
        <section class="main-col">
          <section class="panel"><h3>Scope</h3><p>${esc(w.scope)}</p><h3>Done when</h3><ul class="dwl">${w.doneWhen.map((c) => `<li class="${met(c) ? 'met' : ''}"><span class="tick">${met(c) ? '✓' : '○'}</span>${esc(c.text)}<span class="muted small"> ← ${c.by.map((id) => esc(T(id).title)).join(', ')}</span></li>`).join('')}</ul></section>
          <section class="panel"><header class="panel-h"><h3>What's left</h3><button class="link" data-act="wi-board">${ui.wiBoard ? 'Show as list' : 'Show as board'}</button></header>
            ${ui.wiBoard ? viewBoard(w.id) : grp.map((g) => `<h4 class="grp">${g[0]} <span class="count">${g[1].length}</span></h4>${g[1].map((t) => `<button class="mini${flashCls(t.id)}" data-act="open-task" data-id="${t.id}">${who(t.status === 'review' && t.reviewer ? t.reviewer : t.assignee)}<span class="mini-title">${esc(t.title)}</span><span class="mini-note">${t.status === 'blocked' ? 'after ' + blockedText(t) : esc(t.progress || (t.waitDays != null ? t.waitDays + 'd' : ''))}</span></button>`).join('')}`).join('')}
          </section>
          ${w.rounds ? rounds(w) : ''}
        </section>
        <aside class="side-col">
          ${w.ws ? `<section class="panel"><h3>Workstream</h3><p class="small">${esc(w.ws.state === 'running' ? 'Running' : w.ws.state === 'resumed' ? 'Open' : w.ws.state === 'closed' ? 'Closed' : 'Suspended since ' + w.ws.since)} · branch <code>${esc(w.ws.branch)}</code></p><button class="btn" data-act="open-ws" data-id="${w.id}">${w.ws.state === 'suspended' ? 'Resume' : 'Open'} workstream</button></section>` : ''}
          ${w.id === 'jobeval' ? `<section class="panel"><h3>Working doc</h3><p class="small">A temporary wiki for this decision. Facts carry sources; the agent's opinion is labelled.</p><button class="btn" data-act="open-doc" data-id="jobeval">Open working doc</button></section>` : ''}
          ${w.watcher ? `<section class="panel"><header class="panel-h"><h3>Watcher</h3><span class="move ${w.watcher.on ? 'agent' : 'ready'}">${w.watcher.on ? 'On' : 'Paused'}</span></header><p class="small">Checks SEC-431 ${esc(w.watcher.cadence)}. Reads and proposes; never posts.</p><ul class="log">${w.watcher.log.slice(0, 6).map((l) => `<li>${esc(l)}</li>`).join('')}</ul><button class="btn small" data-act="toggle-watcher" data-id="${w.id}">${w.watcher.on ? 'Pause' : 'Resume'}</button></section>` : ''}
          <section class="panel"><h3>Focus</h3><p class="small muted">Parking hides its tasks from your lists until you bring it back or what it waits on happens.</p><button class="btn small" data-act="${w.inProgress ? 'park' : 'unpark'}" data-id="${w.id}">${w.inProgress ? 'Park this work item' : 'Bring back'}</button></section>
        </aside>
      </div>
    </div>`;
  }
  function rounds(w) {
    const mv = move(w.id);
    return `<section class="panel"><header class="panel-h"><h3>Rounds with security</h3><span class="move ${mv.kind}">${mv.kind === 'you' ? 'YOUR MOVE' : mv.kind === 'wait' ? 'THEIR MOVE' : esc(mv.label)}</span></header>
      ${w.rounds.slice().reverse().map((r) => `<div class="round${r.closed ? ' closed' : ''}"><div class="round-h"><strong>Round ${r.n}</strong><span class="muted small">${esc(r.by)} · ${esc(r.when)}${r.closed ? ' · closed' : ''}</span></div>
        <div class="round-body"><blockquote>${esc(r.raw)}</blockquote><div class="asks"><span class="muted small">The watcher read this as ${r.asks.length} asks</span>${r.asks.map((a) => `<button class="mini" data-act="open-task" data-id="${a.task}">${chip(T(a.task))}<span class="mini-title">${esc(a.text)}</span></button>`).join('')}${r.closed ? '' : '<button class="link small" data-act="fix-split">Correct the split</button>'}</div></div></div>`).join('')}
    </section>`;
  }
  function pageWs(w) {
    const ts = wiTasks(w.id).filter((t) => t.status !== 'done');
    let artifact = '';
    if (w.id === 'pantry14' || w.id === 'sec431') {
      const mr = T(w.id === 'pantry14' ? 'p4' : 's4');
      artifact = `<h4>${w.id === 'pantry14' ? 'MR !23: CSV export' : 'MR !41: pin dependencies'}</h4><pre class="diff">${mr.packet.diff.map((l) => `<span class="${l[0] === '+' ? 'add' : l[0] === '-' ? 'del' : ''}">${esc(l)}</span>`).join('\n')}</pre>${w.rounds ? `<h4>SEC-431 thread</h4>${w.rounds.map((r) => `<blockquote class="small">${esc(r.raw)}</blockquote>`).join('')}` : ''}`;
    } else if (w.id === 'nex12') {
      artifact = `<h4>Design doc, section 3: How Nexus reads Jira</h4><p class="small">Nexus polls the Jira search API every 5 minutes for tickets in tracked projects, and reads a ticket in full when it changes…</p><blockquote class="small"><strong>Platform lead:</strong> ${esc(T('n6').origin.raw)}</blockquote><h4>Plugin build</h4><ul class="small">${T('n7').steps.map((s, i) => `<li>${i < (T('n7').status === 'doing' ? T('n7').stepsDone : 9) ? '✓' : '○'} ${esc(s)}</li>`).join('')}</ul>`;
    } else if (w.id === 'jobeval') {
      artifact = `<h4>Working doc</h4><p class="small">Open it full-size to read and comment.</p><button class="btn" data-act="open-doc" data-id="jobeval">Open working doc</button><h4>Spike</h4><pre class="diff">$ temporal server start-dev\n$ python spike/run.py --day 2026-11-09\nworkflow NightlyReport-2026-11-09 completed in 11m 40s\noutput matches runner: yes</pre>`;
    } else artifact = '<p class="small muted">Nothing open yet.</p>';
    return `<div class="page">
      <button class="link" data-act="back">← Back</button>
      <header class="page-h"><span class="key big" data-p="${esc(w.project)}">${esc(w.key)}</span><div><h2>${esc(w.title)}</h2><p class="muted small">Workstream · branch <code>${esc(w.ws.branch)}</code> · agent's workspace (your machine is reached only through grants)</p></div><button class="btn" data-act="suspend-ws" data-id="${w.id}">Suspend</button></header>
      ${w.ws.whileAway && w.ws.whileAway.length ? `<div class="away"><strong>While you were away:</strong> ${w.ws.whileAway.map(esc).join(' · ')}</div>` : ''}
      <div class="ws">
        <section class="panel"><h3>Tasks</h3>${ts.map((t) => `<button class="mini${flashCls(t.id)}" data-act="open-task" data-id="${t.id}">${chip(t)}<span class="mini-title">${esc(t.title)}</span></button>`).join('')}</section>
        <section class="panel artifact">${artifact}</section>
        <section class="panel chat"><h3>Agent</h3>
          <div class="msg agent">${esc(w.ws.whileAway && w.ws.whileAway[0] ? 'Picking up where we left off. ' + w.ws.whileAway.join('. ') + '.' : 'Ready when you are.')}</div>
          <div class="msg agent">Anything you want to take over? Everything I've done is on <code>${esc(w.ws.branch)}</code> and in the task notes.</div>
          <label class="sr" for="chat-in">Message the agent</label><input id="chat-in" placeholder="Message the agent (mock)" disabled>
          <p class="muted small">Terminal, files and previews open as panes here. Every pane says which machine it's on.</p>
        </section>
      </div>
    </div>`;
  }
  function pageDoc() {
    const src = (s) => `<span class="srcchip">${esc(s)}</span>`;
    return `<div class="page doc">
      <button class="link" data-act="back">← Back</button>
      <header class="page-h"><span class="key big" data-p="platform">ARCH-7</span><div><h2>Working doc: Temporal vs our job runner</h2><p class="muted small">Temporary wiki for this work item · archived when ARCH-7 is done · 3 sections changed since you last read it</p></div></header>
      <article class="docbody">
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
        <section><h3>Research log</h3><p class="small muted">Read 14 pages, skipped 3 (two blog posts older than 2023, one vendor comparison with no sources). Full list in the workstream.</p></section>
      </article>
      <section class="panel"><label for="note-j5"><strong>Comment</strong> <span class="muted small">The agent picks it up as a task</span></label><textarea id="note-j5" rows="2" placeholder="e.g. Check what the 4 multi-step jobs would look like"></textarea><button class="btn" data-act="comment" data-id="j5">Comment</button></section>
    </div>`;
  }

  // ---------- the task panel: one place to act ----------
  function panel(t, inline) {
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
      if (t.postTo) {
        actions += c === 'post-answer' ? confirmBox(t, `Posts your answer to ${t.postTo} as you.`, 'do-post-answer', 'Post as me') : `<button class="btn primary" data-act="confirm" data-a="post-answer" data-id="${t.id}">Post reply…</button>`;
      } else actions += `<button class="btn primary" data-act="save-answer" data-id="${t.id}">Save answer</button><span class="muted small">It goes out with the SEC-431 reply, which you post.</span>`;
    } else if (t.type === 'post') {
      const w2 = W(t.wi); const r = w2.rounds[w2.rounds.length - 1];
      const ans = S.tasks.filter((x) => x.wi === t.wi && x.type === 'answer' && x.status === 'done').map((x) => x.draft);
      body = `<p>Preview of the comment on ${esc(w2.key)}:</p><div class="preview">${r.n === 2 ? `<p>1. Dependencies are pinned and urllib3 is upgraded: MR !41 (pip-audit now reports 0 known vulnerabilities).</p><p>2. ${esc(ans[0] || '')}</p>` : '<p>Audit logging for admin actions is enabled. A sample entry is attached.</p>'}</div>`;
      actions += c === 'post' ? confirmBox(t, `Posts this to ${w2.key} as you. The security team will see it.`, 'do-post', 'Post as me') : `<button class="btn primary" data-act="confirm" data-a="post" data-id="${t.id}">Post…</button>`;
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
      if (w && w.ws) actions += `<button class="btn ghost" data-act="open-ws" data-id="${w.id}">Watch it work</button>`;
    } else {
      body = `<p>${esc(t.why || '')}</p>`;
      actions += `<button class="btn primary" data-act="mark-done" data-id="${t.id}">Done</button>`;
    }
    return `<div class="tp">
      <header class="tp-h">${inline ? '' : '<button class="icon close" aria-label="Close" data-act="close-drawer">×</button>'}
        <div class="tp-meta">${wiChip(t)}${chip(t)}${t.est && t.status !== 'done' ? `<span class="est">${mins(t.est)}</span>` : ''}</div>
        <h2>${esc(t.title)}</h2>
        <div class="small muted">${t.status === 'review' && t.reviewer ? `${esc(t.assignee === 'agent' ? 'Done by the agent' : t.assignee)} · reviewer: ${esc(t.reviewer)}` : `Assigned to ${esc(t.assignee)}`}${w ? ` · <button class="link" data-act="open-wi" data-id="${w.id}">${esc(w.key)}: what's left</button>` : ''}</div>
      </header>
      <div class="tp-body">${body}</div>
      <div class="tp-actions">${actions}</div>
      ${w && w.ws ? `<div class="tp-foot small muted">Dig in: <button class="link" data-act="open-ws" data-id="${w.id}">workstream</button> · <button class="link" data-act="open-wi" data-id="${w.id}">work item</button> · transcript and card history live in the workstream</div>` : ''}
    </div>`;
  }
  function confirmBox(t, text, doAct, label) {
    return `<div class="confirm" role="group" aria-label="Confirm"><p><strong>${esc(text)}</strong></p><button class="btn primary" data-act="${doAct}" data-id="${t.id}">${esc(label)}</button><button class="btn ghost" data-act="cancel-confirm">Cancel</button></div>`;
  }

  function seedModal(id) {
    const b = S.backlog.find((x) => x.id === id);
    const inProg = S.items.filter((w) => w.inProgress);
    const stuck = inProg.map((w) => ({ w, t: wiTasks(w.id).find(waitingOthers) })).filter((x) => x.t && !wiTasks(x.w.id).some(needsMe) && !wiTasks(x.w.id).some(agentWorking))[0]
      || inProg.map((w) => ({ w, t: wiTasks(w.id).find((t) => waitingOthers(t) && (t.waitDays || 0) >= 5) })).filter((x) => x.t)[0];
    return `<div class="modal-bg"><div class="modal" role="dialog" aria-label="Start a work item">
      <header class="tp-h"><button class="icon close" aria-label="Close" data-act="close-modal">×</button><span class="key" data-p="${esc(b.project)}">${esc(b.key)}</span><h2>Start: ${esc(b.title)}</h2><p class="muted small">From ${esc(b.source)}. Nexus creates a workstream and seeds it with what's below.</p></header>
      ${inProg.length >= 4 && stuck ? `<div class="nudge"><strong>You have ${inProg.length} work items in progress.</strong> ${esc(stuck.w.key)} has been waiting on ${esc(waitWho(stuck.t))} for ${stuck.t.waitDays || 0} days. Park it until that's done?<div class="nudge-actions"><button class="btn primary" data-act="seed-start" data-id="${b.id}" data-a="${stuck.w.id}">Park ${esc(stuck.w.key)} and start</button><button class="btn" data-act="seed-start" data-id="${b.id}">Start anyway</button></div></div>` : ''}
      <div class="seed">
        <h3>Scope</h3><p>${esc(b.scope)}</p>
        <h3>Done when</h3><ul>${b.doneWhen.map((d) => `<li>${esc(d)}</li>`).join('')}</ul>
        <h3>First tasks <span class="muted small">proposed by the agent; untick any you don't want</span></h3>
        ${b.seed.map((s, i) => `<label class="seedrow"><input type="checkbox" data-seed="${i}" checked> ${esc(s[0])} ${who(s[1])}</label>`).join('')}
        <h3>The agent's seed</h3><ul class="small"><li>The ticket, its scope and done-when, and these tasks</li><li>The project wiki index</li><li>Read-only access to the repo; its own branch <code>fm/${esc(b.id)}</code></li><li>Hand back results as tasks: review, answer, check. No prose walls.</li></ul>
      </div>
      ${inProg.length >= 4 && stuck ? '' : `<div class="tp-actions"><button class="btn primary" data-act="seed-start" data-id="${b.id}">Start</button><button class="btn ghost" data-act="close-modal">Cancel</button></div>`}
    </div></div>`;
  }

  // ---------- frame ----------
  const BLURB = {
    inbox: 'Variant A · Inbox: one ranked list of what needs you. Context (what changed, agents, waiting) sits beside it. Open a line to act.',
    plan: 'Variant B · Day plan: Monday starts with a proposed plan you adjust, then one task at a time in focus.',
    items: 'Variant C · Work items: a card per work item with whose move it is, progress toward done and the next action.',
    board: 'The board: To do, In progress, Blocked, In review, Done. It runs under every variant; you don\'t have to look at it.',
  };
  function render() {
    const app = document.getElementById('app'); if (!app) return;
    const focusEl = document.activeElement && document.activeElement.id; const pos = focusEl && document.activeElement.selectionStart;
    let main;
    if (ui.page && ui.page.kind === 'wi') main = pageWi(W(ui.page.id));
    else if (ui.page && ui.page.kind === 'ws') main = pageWs(W(ui.page.id));
    else if (ui.page && ui.page.kind === 'doc') main = pageDoc();
    else main = { inbox: viewInbox, plan: viewPlan, items: viewItems, board: () => viewBoard() }[ui.view]();
    const n = needsList().length;
    app.innerHTML = `
      <header class="top">
        <div class="brand">Nexus <span class="muted small">mock</span></div>
        <nav class="tabs" aria-label="Variants">
          ${[['inbox', 'A · Inbox'], ['plan', 'B · Day plan'], ['items', 'C · Work items']].map(([v, l]) => `<button class="tab${ui.view === v && !ui.page ? ' on' : ''}" data-act="set-view" data-id="${v}">${l}</button>`).join('')}
          <button class="tab sub${ui.view === 'board' && !ui.page ? ' on' : ''}" data-act="set-view" data-id="board">Board</button>
        </nav>
        <div class="top-r"><span class="needs" title="Needs you">${n} need you</span><span class="clock">${esc(S.clock)}</span><button class="btn small" data-act="sim" aria-expanded="${ui.sim}">Make something happen</button><button class="btn ghost small" data-act="reset">Reset</button></div>
      </header>
      <div class="blurb">${esc(ui.page ? 'Drill-down: every line opens one level deeper. Back returns to the variant you were on.' : BLURB[ui.view])}</div>
      ${ui.sim ? `<div class="simpanel" role="menu"><p class="small muted">Things that happen in the world. Watch the cards move on their own.</p>${Object.keys(SIMS).map((k) => { const s = SIMS[k]; const ok = s.ok(); return `<button class="simbtn" data-act="run" data-id="${k}"${ok ? '' : ' disabled'}><span>${esc(s.label)}</span>${!ok && s.hint ? `<span class="muted small">${esc(s.hint)}</span>` : ''}${!ok && !s.hint ? '<span class="muted small">already happened</span>' : ''}</button>`; }).join('')}</div>` : ''}
      <main class="main">${main}</main>
      ${ui.drawer && T(ui.drawer) ? `<div class="drawer-bg" data-act="close-drawer"></div><aside class="drawer" aria-label="Task">${panel(T(ui.drawer), false)}</aside>` : ''}
      ${ui.modal ? seedModal(ui.modal.id) : ''}
      <div class="toasts" aria-live="polite">${S.toasts.map((x) => `<div class="toast">${esc(x.text)}</div>`).join('')}</div>`;
    if (focusEl) { const el = document.getElementById(focusEl); if (el && el.tagName !== 'BUTTON') { el.focus(); try { if (pos != null) el.setSelectionRange(pos, pos); } catch (e) { /* select */ } } }
  }

  document.addEventListener('click', (e) => {
    const el = e.target.closest('[data-act]'); if (!el) return;
    if (el.classList.contains('drawer-bg') && e.target !== el) return;
    const a = el.getAttribute('data-act'); const id = el.getAttribute('data-id');
    if (!act[a]) return;
    e.preventDefault();
    if (a === 'fu') act.fu(id, el.getAttribute('data-a'), el.getAttribute('data-b'));
    else if (a === 'confirm') act.confirm(el.getAttribute('data-a'), id);
    else if (a === 'seed-start') act['seed-start'](id, el.getAttribute('data-a'));
    else act[a](id);
    render();
  });
  document.addEventListener('submit', (e) => { const f = e.target.closest('[data-form]'); if (!f) return; e.preventDefault(); act[f.getAttribute('data-form')](); render(); });
  document.addEventListener('input', (e) => { const el = e.target; if (el.getAttribute('data-input') === 'draft') T(el.getAttribute('data-id')).draft = el.value; });
  document.addEventListener('change', (e) => { if (e.target.getAttribute('data-change') === 'scope') { ui.boardScope = e.target.value; render(); } });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') { if (ui.modal) ui.modal = null; else if (ui.drawer) ui.drawer = null; else if (ui.sim) ui.sim = false; else return; render(); } });
  window.addEventListener('hashchange', () => { const v = location.hash.slice(1); if (VIEWS.includes(v)) { ui.view = v; ui.page = null; render(); } });

  S = window.NexusMock.makeData();
  const h = location.hash.slice(1); if (VIEWS.includes(h)) ui.view = h;
  render();
  window.NexusMock.debug = { get state() { return S; }, ui, needsList, act, render, SIMS };
})();

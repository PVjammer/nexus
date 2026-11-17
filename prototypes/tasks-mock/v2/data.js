// Mock data only. Nothing here is real, and nothing is wired to Nexus or desk.
// Work items live in their sources (GitHub, Jira, a team tracker); Nexus only links to them.
// Tasks live only in your Nexus.
window.NexusMock = window.NexusMock || {};
window.NexusMock.makeData = function () {
  const projects = [
    { id: 'pantry', name: 'Pantry app' },
    { id: 'nexus', name: 'Nexus' },
    { id: 'platform', name: 'Platform team' },
    { id: 'reporting', name: 'Reporting service' },
    { id: 'personal', name: 'Personal' },
  ];

  const items = [
    {
      id: 'pantry14', project: 'pantry', key: '#14', title: 'Export my pantry as CSV',
      source: 'GitHub', sourceRef: 'pantry issue #14', inProgress: true,
      scope: 'Let people download their pantry as a CSV file they can open in a spreadsheet.',
      doneWhen: [
        { text: 'Export button on the pantry page', by: ['p2'] },
        { text: 'The file opens in a spreadsheet with every column', by: ['p2', 'p5'] },
        { text: 'Tests cover an empty pantry and names with commas', by: ['p3'] },
        { text: 'Merged and deployed (landed)', by: ['p6'] },
      ],
      ws: { state: 'suspended', branch: 'fm/csv-export', since: 'Fri 18:02',
        whileAway: ['Built the export endpoint and button', 'Added 6 tests; all 48 pass', 'Opened MR !23 with screenshots, then suspended'] },
    },
    {
      id: 'nex12', project: 'nexus', key: 'NEX-12', title: 'Jira integration for Nexus',
      source: 'Jira', sourceRef: 'NEX-12', inProgress: true,
      scope: "Nexus reads tickets from the team's Jira as work items. Comments go back only as writes you approve.",
      doneWhen: [
        { text: 'Design approved by the platform lead', by: ['n5'] },
        { text: 'Lists and reads tickets from the team Jira', by: ['n7'] },
        { text: 'Works against live Jira', by: ['n9'] },
        { text: 'MR approved and merged (landed)', by: ['n8'] },
      ],
      ws: { state: 'running', branch: 'fm/jira-source', since: 'Fri 09:10',
        whileAway: ['Built 6 of 9 plugin steps against the sandbox', 'Drafted an answer to the platform lead', 'Drafted a nudge for IT-882'] },
    },
    {
      id: 'jobeval', project: 'platform', key: 'ARCH-7', title: 'Replace our job runner with Temporal?',
      source: 'Team tracker', sourceRef: 'ARCH-7', inProgress: true,
      scope: 'Decide whether to adopt Temporal or keep maintaining our in-house job runner.',
      doneWhen: [
        { text: 'Covers the jobs we actually run', by: ['j2'] },
        { text: 'Covers operating cost and on-call load', by: ['j3'] },
        { text: 'Spike shows whether the hardest job ports cleanly', by: ['j6'] },
        { text: 'You sign off the recommendation', by: ['j7'] },
        { text: 'Team write-up shared', by: ['j8'] },
      ],
      ws: { state: 'running', branch: 'fm/temporal-spike', since: 'Mon 08:02',
        whileAway: ['Updated 3 sections of the working doc', 'Started the spike: porting the nightly report job'] },
    },
    {
      id: 'sec431', project: 'reporting', key: 'SEC-431', title: 'Security approval: reporting service',
      source: 'Jira', sourceRef: 'SEC-431 (security team)', inProgress: true,
      scope: 'Get the reporting service approved for production by the security team.',
      doneWhen: [
        { text: 'Every finding closed with evidence', by: ['s4', 's5', 's6'] },
        { text: 'Required code changes merged', by: ['s4'] },
        { text: 'Security marks SEC-431 approved', by: ['s7'] },
      ],
      ws: { state: 'suspended', branch: 'fm/sec-431', since: 'Fri 17:55',
        whileAway: ['Watcher read a new comment from security (2 asks)', 'Pinned 14 dependencies and upgraded urllib3 (MR !41)', 'Drafted the secrets answer with sources'] },
      watcher: { on: true, cadence: 'hourly on weekdays',
        log: ['Mon 08:00  checked SEC-431: nothing new', 'Fri 17:00  checked: nothing new', 'Fri 16:45  new comment from security, read as 2 asks', 'Fri 16:00  checked: nothing new'] },
      rounds: [
        { n: 1, when: 'Nov 3', by: 'Security reviewer', closed: true,
          raw: 'Please complete the standard questionnaire and attach a data-flow diagram showing where report data goes.',
          asks: [{ text: 'Complete the questionnaire', task: 's1' }, { text: 'Data-flow diagram', task: 's2' }] },
        { n: 2, when: 'Fri Nov 13, 16:40', by: 'Security reviewer', closed: false,
          raw: 'Thanks for the diagram. Two things before we can sign off: (1) requirements.txt has unpinned dependencies, and urllib3 is on a version with a known advisory. Please pin everything and upgrade it. (2) Where are the service\'s secrets stored, and how often are they rotated? Please be specific.',
          asks: [{ text: 'Pin all dependencies; upgrade urllib3', task: 's4' }, { text: 'Where secrets are stored and how they rotate', task: 's5' }] },
      ],
    },
  ];

  // Work items that exist in your trackers but are not in progress.
  const backlog = [
    { id: 'pantry17', project: 'pantry', key: '#17', title: 'Scan barcodes to add items', source: 'GitHub',
      scope: 'Add items by scanning a barcode with the phone camera.',
      doneWhen: ['Scanning adds the right item for 10 test barcodes', 'Works on iOS and Android browsers', 'Merged and deployed'],
      seed: [['Pick a barcode library (2 options drafted)', 'you'], ['Build the scanner view', 'agent'], ['Add tests', 'agent'], ['Review the MR', 'you'], ['Land: merge and deploy', 'you']] },
    { id: 'nex15', project: 'nexus', key: 'NEX-15', title: 'Linear as a source', source: 'Jira',
      scope: 'Read Linear issues as work items, like the Jira source.',
      doneWhen: ['Lists and reads issues', 'Comments only as approved writes', 'Merged'],
      seed: [['Confirm Linear workspace access', 'you'], ['Adapt the Jira source design', 'agent'], ['Review the design', 'you'], ['Build the source', 'agent'], ['Review the MR', 'you']] },
    { id: 'rep88', project: 'reporting', key: 'REP-88', title: 'Warm the report cache overnight', source: 'Jira',
      scope: 'Pre-compute the five slowest reports overnight.',
      doneWhen: ['The five reports load in under 2 s at 9am', 'Merged and deployed'],
      seed: [['Measure today\'s load times', 'agent'], ['Agree the target reports', 'you'], ['Build the warmup job', 'agent'], ['Review the MR', 'you']] },
  ];

  const tasks = [
    // 1. Pantry CSV export
    { id: 'p1', wi: 'pantry14', title: 'Confirm the columns and file name', assignee: 'you', status: 'done' },
    { id: 'p2', wi: 'pantry14', title: 'Build the export endpoint and button', assignee: 'agent', status: 'done' },
    { id: 'p3', wi: 'pantry14', title: 'Add tests', assignee: 'agent', status: 'done' },
    { id: 'p4', wi: 'pantry14', title: 'Review MR !23: CSV export', assignee: 'agent', status: 'review', reviewer: 'you',
      est: 20, type: 'review-mr', why: 'Agent finished Fri 18:02',
      planReason: 'Unblocks your phone check and the merge.',
      packet: {
        summary: 'Adds GET /pantry/export.csv and an Export button on the pantry page. Escapes commas and quotes; empty pantry exports a header row only.',
        checks: ['48 tests pass (6 new)', 'Rebased on main, no conflicts', 'Screenshots: desktop and phone'],
        files: [['api/export.py', '+64'], ['web/pantry.js', '+18 −2'], ['tests/test_export.py', '+71']],
        diff: ['+ @app.get("/pantry/export.csv")', '+ def export_csv(user):', '+     rows = pantry_rows(user)', '+     return csv_response(rows, filename="pantry.csv")'],
        followups: [{ text: 'Offer XLSX export too', choice: null }],
      } },
    { id: 'p5', wi: 'pantry14', title: 'Check the export on your phone', assignee: 'you', status: 'blocked', blockedBy: ['p4'], est: 5, type: 'check',
      why: 'Open the staging link on your phone and try Export' },
    { id: 'p6', wi: 'pantry14', title: 'Land: merge MR !23 and deploy', assignee: 'you', status: 'blocked', blockedBy: ['p4'], est: 2, type: 'land',
      why: 'Merging is visible to others, so it waits for your press' },

    // 2. NEX-12 Jira integration
    { id: 'n1', wi: 'nex12', title: 'Request Jira API access from IT', assignee: 'you', status: 'done' },
    { id: 'n2', wi: 'nex12', title: 'Grant API access (IT-882)', assignee: 'IT', status: 'doing', waitDays: 6, type: 'waiting',
      why: 'Requested Nov 10. No movement for 6 days.',
      nudge: 'Hi, checking on IT-882 (Jira API access for the Nexus integration). Is there anything else you need from me? We are building against a sandbox meanwhile, so no rush beyond this week.' },
    { id: 'n3', wi: 'nex12', title: 'Write the design doc', assignee: 'agent', status: 'done' },
    { id: 'n4', wi: 'nex12', title: 'Review and own the design', assignee: 'you', status: 'done' },
    { id: 'n5', wi: 'nex12', title: 'Approve the design', assignee: 'agent', status: 'review', reviewer: 'Platform lead', waitDays: 2, type: 'waiting',
      why: 'Sent Thu. They asked one question Fri (see your answer task).' },
    { id: 'n6', wi: 'nex12', title: 'Answer the platform lead: why polling, not webhooks?', assignee: 'you', status: 'todo',
      est: 10, type: 'answer', postTo: 'the design doc', why: 'Asked Fri 15:10 on the design doc. Answer drafted.',
      planReason: 'The platform lead is waiting on this before approving the design.',
      origin: { by: 'Platform lead', when: 'Fri 15:10', where: 'design doc, section 3', raw: 'Why polling every 5 minutes instead of Jira webhooks? Webhooks would be cheaper for the Jira side.' },
      draft: 'Webhooks need a public endpoint Jira can reach. Nexus runs on a laptop or the homelab with no inbound access, so polling is the only option that works everywhere. At 5 minutes for ~40 tracked tickets that is about 480 API calls a day, well under the 10k/day limit. If we later run a hosted core, webhooks become an option and the source interface already supports push.',
      claims: [
        { text: 'Nexus has no inbound endpoint', src: 'where-we-are.md: placement is configuration' },
        { text: '~480 calls/day at 5-minute polling for 40 tickets', src: 'calculated from the design doc, section 2' },
        { text: '10k/day API limit', src: null },
      ] },
    { id: 'n7', wi: 'nex12', title: 'Build the plugin against a sandbox', assignee: 'agent', status: 'doing', type: 'agent',
      progress: '6 of 9 steps', why: 'Running since Fri 09:10',
      steps: ['Source interface', 'Auth with an API token', 'List tickets', 'Read one ticket', 'Map fields to work items', 'Scope and done-when extraction', 'Comment as a brokered write', 'Rate limiting', 'Tests'],
      stepsDone: 6,
      packet: {
        summary: 'A Jira source for Nexus: lists and reads tickets, maps them to work items, posts comments only through the broker. Built against a sandbox Jira.',
        checks: ['31 tests pass', 'Leak check green: the token never reaches logs', 'Rate limit: 2 req/s with backoff'],
        files: [['sources/jira/source.go', '+412'], ['sources/jira/map.go', '+138'], ['sources/jira/source_test.go', '+290']],
        diff: ['+ func (s *Source) List(ctx context.Context, q Query) ([]Ref, error) {', '+     return s.search(ctx, q.JQL())', '+ }'],
        followups: [{ text: 'Linear source reusing the same mapping', choice: null }],
      } },
    { id: 'n8', wi: 'nex12', title: 'Review the MR', assignee: 'agent', status: 'blocked', blockedBy: ['n7'], unblockTo: 'review', reviewer: 'Team reviewer', type: 'waiting',
      why: 'Opens for the team reviewer after you approve the build' },
    { id: 'n9', wi: 'nex12', title: 'Test on live Jira', assignee: 'agent', status: 'blocked', blockedBy: ['n2'], live: true, est: 1, type: 'start-live',
      why: 'Uses your Jira credentials against the live instance, so it waits for your start' },

    // 3. Job runner evaluation
    { id: 'j1', wi: 'jobeval', title: 'Agree what matters for the decision', assignee: 'you', status: 'done' },
    { id: 'j2', wi: 'jobeval', title: 'Inventory what our runner actually does', assignee: 'agent', status: 'done' },
    { id: 'j3', wi: 'jobeval', title: 'Research Temporal against those criteria', assignee: 'agent', status: 'done' },
    { id: 'j4', wi: 'jobeval', title: 'Build the working doc', assignee: 'agent', status: 'done' },
    { id: 'j5', wi: 'jobeval', title: 'Review the working doc', assignee: 'agent', status: 'review', reviewer: 'you', est: 30, type: 'review-doc',
      why: '3 sections changed since you last read it', planReason: 'Nobody is waiting; the biggest block of thinking today.' },
    { id: 'j6', wi: 'jobeval', title: 'Spike: port the nightly report job', assignee: 'agent', status: 'doing', type: 'agent',
      progress: 'started 08:02', why: 'Runs in its own sandbox, so it started on its own',
      steps: ['Set up a local Temporal dev server', 'Port the job as a workflow', 'Run it against last week\'s data', 'Compare output and timing'], stepsDone: 1,
      packet: {
        summary: 'The nightly report ported to a Temporal workflow in about 140 lines. Output matched our runner on last week\'s data. Retries replaced 60 lines of our own retry code. One gap: our "skip if the previous run is still going" rule needs a workflow ID policy.',
        checks: ['Output identical for 7 of 7 nights', 'Run time 11m 40s vs 12m 05s today', 'Spike code on branch fm/temporal-spike'],
        files: [['spike/nightly_report_workflow.py', '+142'], ['spike/README.md', '+40']],
        diff: ['+ @workflow.defn', '+ class NightlyReport:', '+     @workflow.run', '+     async def run(self, day: str) -> str:'],
        followups: [{ text: 'Price a managed Temporal cluster vs self-hosting', choice: null }],
      } },
    { id: 'j7', wi: 'jobeval', title: 'State your lean, then decide', assignee: 'you', status: 'blocked', blockedBy: ['j5', 'j6'], est: 20, type: 'check',
      why: 'The agent lays out options until you state a lean, so the call stays yours' },
    { id: 'j8', wi: 'jobeval', title: 'Draft the write-up for the team', assignee: 'agent', status: 'blocked', blockedBy: ['j7'], type: 'agent', progress: 'not started' },

    // 4. SEC-431 Security approval
    { id: 's1', wi: 'sec431', title: 'Fill in the security questionnaire', assignee: 'you', status: 'done' },
    { id: 's2', wi: 'sec431', title: 'Provide a data-flow diagram', assignee: 'agent', status: 'done' },
    { id: 's3', wi: 'sec431', title: 'Round 1 review', assignee: 'Security reviewer', status: 'done' },
    { id: 's4', wi: 'sec431', title: 'Review MR !41: pin dependencies, upgrade urllib3', assignee: 'agent', status: 'review', reviewer: 'you', est: 15, type: 'review-mr',
      why: 'From round 2, ask 1. Agent finished Fri 17:55', planReason: 'Security is waiting on round 2. Do these two first.',
      packet: {
        summary: 'Pins all 14 direct dependencies to exact versions and upgrades urllib3 to the patched release named in the advisory. No code changes beyond requirements.txt.',
        checks: ['212 tests pass', 'pip-audit: 0 known vulnerabilities (was 1)', 'Rebased on main'],
        files: [['requirements.txt', '+14 −14']],
        diff: ['- requests>=2.0', '+ requests==2.32.3', '- urllib3', '+ urllib3==2.2.2'],
        followups: [{ text: 'Add pip-audit to CI so this stays fixed', choice: null }],
      } },
    { id: 's5', wi: 'sec431', title: 'Answer security: where are secrets stored and rotated?', assignee: 'you', status: 'todo', est: 10, type: 'answer',
      why: 'From round 2, ask 2. Answer drafted from the repo and wiki.', planReason: 'Security is waiting on round 2.',
      origin: { by: 'Security reviewer', when: 'Fri 16:40', where: 'SEC-431 comment', raw: 'Where are the service\'s secrets stored, and how often are they rotated? Please be specific.' },
      draft: 'The service reads its database password and API keys from the team Vault at startup (path secret/reporting/*); nothing is stored in the repo or the image. Database credentials are issued by Vault per deploy and expire after 24 hours. The two third-party API keys are rotated every 90 days.',
      claims: [
        { text: 'Secrets read from Vault at secret/reporting/*', src: 'deploy/vault.hcl, line 12' },
        { text: 'Nothing stored in the repo or image', src: 'secret scan of the repo and Dockerfile' },
        { text: 'DB credentials expire after 24 hours', src: 'deploy/vault.hcl, line 30 (ttl = "24h")' },
        { text: 'API keys rotated every 90 days', src: null },
      ] },
    { id: 's6', wi: 'sec431', title: 'Post both replies to SEC-431', assignee: 'you', status: 'blocked', blockedBy: ['s4', 's5'], est: 3, type: 'post',
      why: 'Posting is visible to the security team, so you send it' },
    { id: 's7', wi: 'sec431', title: 'Round 2 review', assignee: 'Security reviewer', status: 'blocked', blockedBy: ['s6'], unblockTo: 'doing', type: 'waiting',
      why: 'Security re-reviews after your replies' },

    // Plain todos: no work item
    { id: 't1', wi: null, project: 'personal', title: 'Book a hotel for the conference (Dec 2–4)', assignee: 'you', status: 'todo', est: 15, dueIn: 1, due: 'Tue', type: 'todo',
      help: 'Find 3 hotels near the venue under $250 a night', planReason: 'Due tomorrow; rates go up this week.' },
    { id: 't2', wi: null, project: 'personal', title: 'Schedule a dentist appointment', assignee: 'you', status: 'todo', est: 5, type: 'todo' },
    { id: 't3', wi: null, project: 'personal', title: 'Renew passport', assignee: 'you', status: 'todo', est: 20, dueIn: 15, due: 'Dec 1', type: 'todo' },
  ];

  const feed = [
    { when: 'Mon 08:02', who: 'agent', text: 'Started "Spike: port the nightly report job"', why: 'Its blockers cleared and it runs only in its sandbox, so it started on its own.', wi: 'jobeval' },
    { when: 'Mon 08:00', who: 'agent', text: 'Updated 3 sections of the ARCH-7 working doc', why: 'New pricing page and the runner inventory.', wi: 'jobeval' },
    { when: 'Sat 09:00', who: 'rule', text: 'IT-882 idle for 5 days: drafted a nudge', why: 'Rule: someone else\'s task, idle 5 days.', wi: 'nex12' },
    { when: 'Fri 18:02', who: 'agent', text: 'Moved "Review MR !23" to In review for you', why: 'The CSV export is built and tested.', wi: 'pantry14' },
    { when: 'Fri 17:55', who: 'agent', text: 'Moved "Review MR !41" to In review for you', why: 'Dependencies pinned, urllib3 upgraded, tests pass.', wi: 'sec431' },
    { when: 'Fri 16:45', who: 'watcher', text: 'New comment on SEC-431, read as 2 asks', why: 'Created one agent task (pin deps) and one for you (secrets answer).', wi: 'sec431' },
    { when: 'Fri 15:10', who: 'source', text: 'Platform lead asked a question on the NEX-12 design', why: 'Created a task for you with a drafted answer.', wi: 'nex12' },
  ];

  return {
    clock: 'Mon Nov 16, 08:30', lastSeen: 'Fri 18:00',
    projects, items, backlog, tasks, feed,
    plan: { order: ['s4', 's5', 'n6', 'p4', 'j5', 't1'], deferred: [], started: false, idx: 0 },
    flash: [], toasts: [], seq: 100,
  };
};

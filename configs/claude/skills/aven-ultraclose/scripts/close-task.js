export const meta = {
  name: 'close-task',
  description: 'Implement one aven task in its worktree, then loop adversarial review and fixes until a reviewer has no findings or the task reaches its cap',
  phases: [
    { title: 'Implement', detail: 'one implementer, in the task worktree' },
    { title: 'Sync', detail: 'merge the base branch into a branch that no longer lands' },
    { title: 'Review', detail: 'a fresh adversarial reviewer per round' },
    { title: 'Fix', detail: 'a fresh fixer answers each finding' },
    { title: 'Final', detail: 'at the cap: exact fixes, one bounded review, then narrow the claims' },
  ],
}

// Input: one object that `ultraclose.py next` or `ultraclose.py args` prints.
const { run, repo, base } = args
const T = args.task
const ROUNDS = args.rounds || 6
const RESUME = T.resume || null
const FIRST = RESUME ? RESUME.round : 1
const LAST = RESUME && RESUME.lastRound ? RESUME.lastRound : Math.max(ROUNDS, FIRST)
// Prompts must not state the round cap or say that a cycle is the last.
// Agents that know the cap rush or soften their work near it.
const MSG = `${T.dir}/msg.txt`
const HISTORY = `${T.dir}/history.md`

const IMPL_SCHEMA = {
  type: 'object',
  properties: {
    status: { type: 'string', enum: ['implemented', 'abandoned', 'obsolete'] },
    summary: { type: 'string', description: 'What changed, in a few sentences' },
    notes: { type: 'string', description: 'For abandoned or obsolete: the evidence, and what a later attempt must know. Otherwise caveats, or empty.' },
  },
  required: ['status', 'summary', 'notes'],
}

const REVIEW_SCHEMA = {
  type: 'object',
  properties: {
    verdict: { type: 'string', enum: ['approve', 'changes', 'reject'] },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          severity: { type: 'string', enum: ['blocker', 'major', 'minor'] },
          location: { type: 'string', description: 'file:line in the worktree' },
          problem: { type: 'string' },
          evidence: { type: 'string', description: 'Command output, a failing test, or the code reading that proves it' },
          fix: { type: 'string', description: 'The exact change the fixer makes' },
          regression: { type: 'boolean', description: `true when ${base} does not have this fault, so the change introduced it` },
        },
        required: ['severity', 'location', 'problem', 'evidence', 'fix', 'regression'],
      },
    },
    improves_base: { type: 'boolean', description: `true when the change as it stands leaves ${base} strictly better: it resolves part of the task and makes nothing worse` },
    reject_reason: { type: 'string', description: 'Only for reject: why no fix inside the task scope repairs the approach' },
  },
  required: ['verdict', 'findings', 'improves_base', 'reject_reason'],
}

const FIX_SCHEMA = {
  type: 'object',
  properties: {
    responses: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          finding: { type: 'integer', description: '1-based index of the finding' },
          action: { type: 'string', enum: ['fixed', 'rebutted'] },
          detail: { type: 'string', description: 'What changed, or the evidence that the finding is wrong' },
        },
        required: ['finding', 'action', 'detail'],
      },
    },
    gave_up: { type: 'boolean', description: 'true when the findings show that the task cannot land without massive scope creep or significant regressions' },
    notes: { type: 'string' },
  },
  required: ['responses', 'gave_up', 'notes'],
}

const SYNC_SCHEMA = {
  type: 'object',
  properties: {
    status: { type: 'string', enum: ['synced', 'abandoned'] },
    summary: { type: 'string', description: 'What the merge required: each conflict and each adaptation' },
    notes: { type: 'string' },
  },
  required: ['status', 'summary', 'notes'],
}

const NARROW_SCHEMA = {
  type: 'object',
  properties: {
    followups: {
      type: 'array',
      description: 'One task per open finding',
      items: {
        type: 'object',
        properties: {
          title: { type: 'string', description: 'Sentence case, names the symptom, not the fix' },
          description: { type: 'string', description: 'Markdown with the sections Observed, Expected, Reproduction, and Evidence' },
        },
        required: ['title', 'description'],
      },
    },
    notes: { type: 'string', description: 'Each claim you removed or reworded' },
  },
  required: ['followups', 'notes'],
}

const rules = `
Ground rules:
- The worktree is ${T.wt}, on branch task/${T.ref}, which was cut from ${base}. Work only there. Use absolute paths under it for every file you read or edit, and start every shell command with \`cd ${T.wt} && \`. Never change files or git state in the main checkout ${repo} or in another worktree. Other agents work in sibling worktrees at the same time.
- The task description is ${T.dir}/task.md. Read it in full. Its line numbers come from an older commit, so they might have drifted. Other task refs that it names are separate work for other agents, so do not implement them.
- ${run}/rules.md holds the rules of this project: how to build and test, the full check, the commit message format, and what you must not run. Read it before you start. Its rules override your defaults.
- Never write a task ref such as ${T.ref} in code, comments, docs, or the commit message.
- Never run \`git stash\` in any form. All worktrees share one stash list, so a pop takes the entry of another agent. To see the old code, run \`git show ${base}:path\` or \`git diff ${base}\`. Never run \`git checkout\`, \`git restore\`, \`git reset\`, or \`git clean\` on a file that you did not change.
- Commit work in progress with hooks off: \`cd ${T.wt} && git add -A && git commit -q --no-verify -m wip\`. The orchestrator writes the real commit on ${base} later, from the message in ${MSG}.
- Other builds share this machine. Iterate with targeted builds and tests, and run the full check only when the work is otherwise finished. Never clear a shared build cache.
- If the worktree holds uncommitted changes when you start, an earlier agent with your assignment stopped in the middle of its work. Read those changes first, keep what is right, and finish the assignment.
- Put probe files and other throwaway output in ${T.dir}/scratch/, never in the worktree. \`git status\` in the worktree must print nothing when you return.`

const implPrompt = `You implement one tracked task.

Task ${T.ref}: "${T.title}"
${rules}

Steps:
1. Read the task file, then the code that it points at, as the worktree has it.
2. Confirm that the problem still exists. If an earlier commit resolved it, change nothing and return status "obsolete" with the evidence.
3. Implement the task. Stay inside its scope.
4. Add or adjust tests. Each new behavior needs a test that fails without the change.
5. Update each doc comment, doc, and example that describes a behavior or an interface that you changed.
6. Make the full check pass in the worktree.
7. Write the commit message for the whole change to ${MSG}, in the format that rules.md gives.
8. Commit the work in progress.

Stop instead of forcing the task through. If the task requires massive scope beyond its description, or it causes significant regressions that you cannot reconcile, return status "abandoned". Put the evidence in notes: what you tried, what breaks, and what a later attempt must know. Leave the partial work committed in the worktree.`

const reviewPrompt = (round, summary, extra) => `You are an adversarial code reviewer. A different agent implemented task ${T.ref}: "${T.title}". Find what is wrong with the change. Assume that it has defects until you have checked.
${rules}
- You are read-only on the worktree. Do not edit or commit there. Run experiments in ${T.dir}/scratch/. If an experiment requires a file inside the worktree, delete that file before you return.
- Do not run the full check. Run targeted builds and tests.

${summary ? `The implementer's summary: ${summary}` : ''}
The proposed commit message is in ${MSG}.
${extra}
Review steps:
1. Read the task file. Then read \`cd ${T.wt} && git diff ${base}...HEAD\` and the changed code in its full context.
2. Check that the change resolves the task. Run the examples of the task as real code where you can. Probe the edge cases that the implementer might have skipped, such as empty input, zero values, error paths, and concurrent use.
3. Hunt regressions: callers in other packages, behavior that the tests stopped pinning, and performance cliffs.
4. Check the tests. Each test must fail without the change, and each new branch that a public call reaches needs a test.
5. Check docs and conventions: doc comments and docs that the change made false, the rules of rules.md and CLAUDE.md, leftover shims, changes outside the task scope, and task refs in the tree. Search the repository for the old names and the old behavior.
6. Check that the commit message describes the diff and claims nothing more.

Report only findings that require a change, each with a location, the proof, and the exact fix. Leave out praise, restatements, and style preferences that the linters accept. Severity: blocker for wrong behavior or a broken build, major for a missing part of the task, a regression, or a missing test, and minor for a doc or convention fault. Set regression on each finding, and set improves_base for the change as a whole.

Verdict:
- "approve" only when you have zero findings.
- "changes" when a fixer can repair the findings inside the task scope.
- "reject" only when the approach itself forces massive scope creep or significant regressions. Give the reason in reject_reason.`

const historyNote = rebutted => `
Earlier review rounds already happened, and ${HISTORY} records each finding with the fixer's answer. Read it. Verify each claimed fix in the code. Do not raise a rebutted finding again unless the rebuttal is wrong, and then say why. Then review the whole diff afresh, because fixes introduce new faults.
${rebutted.length ? 'Rebuttals so far:\n' + rebutted.join('\n') : ''}
`

const listFindings = findings => findings
  .map((f, i) => `${i + 1}. [${f.severity}] ${f.location}\n   Problem: ${f.problem}\n   Evidence: ${f.evidence}\n   Fix: ${f.fix}`)
  .join('\n')

const boundedNote = open => `
This review is narrow. A fixer applied only the exact fixes of the findings below. Do not review the whole diff again. Report a finding only when it is one of these findings and it is still open, or when one of these fixes introduced it.
${listFindings(open)}
`

const fixPrompt = (round, findings, exact) => `You repair a change after an adversarial review. A different agent implemented task ${T.ref}: "${T.title}", and a reviewer reported the findings below.
${rules}

The current commit message is in ${MSG}.

Findings:
${listFindings(findings)}

Steps:
1. Read the task file and \`cd ${T.wt} && git diff ${base}...HEAD\`, so that you know the change.
2. Check each finding against the code. Fix each finding that is right, with a test where the finding shows a behavior gap. Rebut a finding only when you can prove it wrong, and give that proof in the response detail.${exact ? `
   Keep this pass minimal. Apply only the exact fix of each finding. Do not refactor, and do not widen the change.` : ''}
3. Make the full check pass in the worktree.
4. Update ${MSG} so that it describes the whole diff against ${base}.
5. Append a section to ${HISTORY}, creating the file if it does not exist. Head it \`## Round ${round}\`. List each finding with its location and problem, followed by your answer: fixed or rebutted, and the detail.
6. Commit the work in progress.
7. Return one response per finding.

If the findings show that the task cannot land without massive scope creep or significant regressions, set gave_up to true and explain in notes instead of forcing the task through.`

const syncPrompt = reason => `You bring one finished change up to date with ${base}. A different agent implemented task ${T.ref}: "${T.title}" on a branch cut from an older ${base}, and adversarial review accepted it. ${base} has gained commits from other tasks, and the branch stopped landing.

Why the branch does not land:
${reason}
${rules}

The current commit message is in ${MSG}.

Steps:
1. Read the task file and \`cd ${T.wt} && git diff ${base}...HEAD\`, so that you know the change. Read \`git log --stat HEAD..${base}\`, so that you know what ${base} gained.
2. Merge ${base} into the branch: \`cd ${T.wt} && git merge --no-verify ${base}\`. Resolve each conflict so that both changes keep their intent. Before you resolve a conflicted file, read the commits of ${base} that changed it (\`git log -p HEAD..${base} -- path\`). Finish the merge with \`git commit --no-verify --no-edit\`.
3. Adapt the code, tests, and docs of this task to the interfaces and the behavior that ${base} has. Never revert or weaken what ${base} gained. Check that the docs of this task are still true beside the new commits.
4. Make the full check pass in the worktree.
5. Update ${MSG} only where the merge changed what the diff against ${base} does.
6. Commit the work in progress.

If the commits on ${base} make this task obsolete or force a redesign, return status "abandoned" with the evidence in notes.`

const narrowPrompt = findings => `You prepare one change to land with known gaps. A different agent implemented task ${T.ref}: "${T.title}". A review left the findings below open. A reviewer judged that the change is a strict improvement over ${base} and that no open finding is a regression, so the change lands as it stands, and a follow-up task tracks each open finding.
${rules}

Open findings:
${listFindings(findings)}

Steps:
1. Do not fix the findings, and do not change behavior.
2. Narrow the claims to what the code does. Edit each doc comment, doc, and example of this change, and the commit message in ${MSG}, so that none promises a behavior that an open finding contradicts. State a known limit where a reader would otherwise expect the full behavior.
3. Make the full check pass in the worktree, and commit the work in progress.
4. Return one follow-up task per open finding. Each task must stand alone for a reader who never saw this review: write what happens, what should happen, the minimal reproduction, and the evidence.`

// An agent that dies, or that ends without its structured result, returns
// null here, and each caller retries it one time. The retry finds the
// uncommitted work of the first attempt in the worktree.
const attempt = async (prompt, opts) => {
  for (const suffix of ['', ' retry']) {
    try {
      const result = await agent(prompt, { ...opts, label: opts.label + suffix })
      if (result) return result
    } catch (err) {
      log(`${opts.label}${suffix} failed: ${err && err.message ? err.message : err}`)
    }
  }
  return null
}

const out = { ref: T.ref, title: T.title, rounds: 0, summary: '', notes: '', findings: [], followups: [], boundedReview: false }
const done = (status, extra) => ({ ...out, status, ...extra })

let summary = ''
let extra = ''
if (RESUME && RESUME.sync) {
  const sync = await attempt(syncPrompt(RESUME.sync), { label: `sync ${T.ref}`, phase: 'Sync', schema: SYNC_SCHEMA })
  if (!sync) return done('error', { notes: 'the sync agent returned nothing' })
  if (sync.status !== 'synced') return done('abandoned', { summary: sync.summary, notes: sync.notes })
  out.notes = sync.notes
  extra = `
This change passed adversarial review on an older ${base}. ${base} then gained commits from other tasks, and the branch stopped landing: ${RESUME.sync}
A sync agent merged ${base} into the branch and adapted the change. Its report: ${sync.summary}
Review the whole diff against ${base} again. Give the merge the most attention: lost or duplicated hunks, code of this task that ignores what ${base} gained, and docs that are false beside the new commits.
`
} else if (!RESUME) {
  const impl = await attempt(implPrompt, { label: `impl ${T.ref}`, phase: 'Implement', schema: IMPL_SCHEMA })
  if (!impl) return done('error', { notes: 'the implementer returned nothing' })
  out.summary = impl.summary
  out.notes = impl.notes
  if (impl.status !== 'implemented') return done(impl.status)
  summary = impl.summary
}

const rebutted = []
for (let round = FIRST; ; round++) {
  out.rounds = round
  const history = round > 1 ? historyNote(rebutted) : ''
  const rev = await attempt(reviewPrompt(round, summary, extra + history), { label: `review ${T.ref} r${round}`, phase: 'Review', schema: REVIEW_SCHEMA })
  if (!rev) return done('error', { notes: `the reviewer of round ${round} returned nothing` })
  if (rev.verdict === 'reject') return done('rejected', { notes: rev.reject_reason, findings: rev.findings })
  if (rev.findings.length === 0) return done('approved')
  log(`${T.ref}: round ${round} has ${rev.findings.length} findings`)

  const capped = round >= LAST
  const fix = await attempt(fixPrompt(round, rev.findings, capped), { label: `fix ${T.ref} r${round}`, phase: capped ? 'Final' : 'Fix', schema: FIX_SCHEMA })
  if (!fix) return done('error', { notes: `the fixer of round ${round} returned nothing`, findings: rev.findings })
  if (fix.gave_up) return done('abandoned', { notes: fix.notes, findings: rev.findings })
  rev.findings.forEach((f, i) => {
    const r = fix.responses.find(x => x.finding === i + 1)
    if (r && r.action === 'rebutted') rebutted.push(`- ${f.location}: ${f.problem}\n  Rebuttal: ${r.detail}`)
  })
  if (!capped) continue

  // The cap: one bounded review of the exact fixes, then land or note.
  log(`${T.ref}: reached the cap at round ${round}`)
  out.rounds = round + 1
  out.boundedReview = true
  const fin = await attempt(reviewPrompt(round + 1, summary, boundedNote(rev.findings) + historyNote(rebutted)), { label: `review ${T.ref} final`, phase: 'Final', schema: REVIEW_SCHEMA })
  if (!fin) return done('unconverged', { notes: 'the final reviewer returned nothing, so the findings of the last round stand unverified', findings: rev.findings })
  if (fin.verdict !== 'reject' && fin.findings.length === 0) return done('approved')
  const landable = fin.verdict !== 'reject' && fin.improves_base && !fin.findings.some(f => f.regression)
  if (!landable) return done('unconverged', { notes: fin.reject_reason, findings: fin.findings })
  const narrow = await attempt(narrowPrompt(fin.findings), { label: `narrow ${T.ref}`, phase: 'Final', schema: NARROW_SCHEMA })
  if (!narrow) return done('unconverged', { notes: 'the change is a strict improvement, but the narrowing agent returned nothing', findings: fin.findings })
  return done('landable', { notes: narrow.notes, findings: fin.findings, followups: narrow.followups })
}

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

// `ultraclose.py` launches each task from a copy of this script. In the
// meta of the copy, the name holds the ref of the task, and the
// description holds its title. So the Workflow list shows which task each
// Workflow closes. `ultraclose.py` replaces the name line and the
// description line of the meta, so keep each of the two values on one
// line.

// Input: one object that `ultraclose.py next`, `ultraclose.py land --next`,
// or `ultraclose.py args` prints.
const { run, repo, base } = args
const T = args.task
const ROUNDS = args.rounds || 6
// The model that the agents of each role run on, by the name of the
// role. `ultraclose.py` fills the field from the run config. The roles
// are implementer, sync, reviewer, fixer, and narrow. The reviewer and
// the fixer of the final cycle take the models of the reviewer role and
// the fixer role. The agents of a role that the field leaves out follow
// the session model.
const MODELS = args.models || {}
const RESUME = T.resume || null
const FIRST = RESUME ? RESUME.round : 1
const LAST = RESUME && RESUME.lastRound ? RESUME.lastRound : Math.max(ROUNDS, FIRST)
// Prompts must not state the round cap or say that a cycle is the last.
// Agents that know the cap rush or soften their work near it.
const MSG = `${T.dir}/msg.txt`
const HISTORY = `${T.dir}/history.md`
// The files that keep the summary and the notes of the change from one
// launch to the next. This script cannot read or write a file. So each
// agent that returns a summary and notes also writes them to these
// files. For a relaunch, `ultraclose.py args` reads the files into the
// resume field of the input.
const SUMMARY = `${T.dir}/summary.txt`
const NOTES = `${T.dir}/notes.txt`
// The branch of the task. `ultraclose.py` names it in the input, because
// a landing that proposes a pull request renames the branch. An input
// without the name comes from a task that starts on its first branch.
const BRANCH = T.branch || `task/${T.ref}`
// What the orchestrator does with the finished change. The pr field of
// the input is true for a run whose landings propose pull requests. The
// landing of such a run records no commit on the base branch.
const LANDING = args.pr
  ? `The orchestrator opens a pull request against ${base} later. The first line of the message in ${MSG} becomes its title, and the rest of the message becomes its body.`
  : `The orchestrator writes the real commit on ${base} later, from the message in ${MSG}.`

// One task that the orchestrator files, in the format of the track-issue
// skill.
const NEW_TASK = {
  type: 'object',
  properties: {
    title: { type: 'string', description: 'Sentence case, names the symptom, not the fix' },
    description: { type: 'string', description: 'Markdown with the sections Observed, Expected, Reproduction, and Evidence' },
  },
  required: ['title', 'description'],
}

// A defect is a fault outside the task that an agent found and left
// unfixed. The implementer and the sync agent return each defect that
// they found. A fixer and the narrowing agent return only the defects
// that the result does not list yet.
const defectsField = which => ({
  type: 'array',
  description: `One task per defect outside the task that ${which}. Empty when you found none. Never put a caveat of the change here.`,
  items: NEW_TASK,
})

const IMPL_SCHEMA = {
  type: 'object',
  properties: {
    status: { type: 'string', enum: ['implemented', 'abandoned', 'obsolete'] },
    summary: { type: 'string', description: 'What changed, in a few sentences' },
    notes: { type: 'string', description: 'For abandoned or obsolete: the evidence, and what a later attempt must know. Otherwise caveats, or empty.' },
    defects: defectsField('you found and left unfixed'),
  },
  required: ['status', 'summary', 'notes', 'defects'],
}

// What a reviewer reports as a regression. At the cap, one open
// regression keeps the change from landing. The task then stays open, and
// so does each task that waits for it. So the rule covers only a fault in
// behavior. A missing test or a comment fault leaves the base branch no
// worse, even in code that the change added. So the change lands, and a
// follow-up task tracks the fault. An agent acts on the text of a skill or
// a prompt, so a wrong instruction there counts as a fault in behavior.
// The rule does not say what a regression decides, so the consequence
// does not sway the reviewer.
const REGRESSION = `A finding is a regression only when it reports a fault in behavior that the change introduced, such as a wrong result, a crash, a broken build, a broken caller, a failing test, or slower code. A fault that ${base} also has is not a regression. A missing or weak test leaves the behavior as it is, and so does a doc, comment, or convention fault. None of these faults is a regression, even in code that the change added. Text that an agent acts on is behavior, so a wrong instruction in a prompt, a skill, an agent definition, a CLAUDE.md file, or other such text is a fault in behavior.`

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
          regression: { type: 'boolean', description: REGRESSION },
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
    summary: { type: 'string', description: 'What the whole change does as you leave it, in a few sentences' },
    notes: { type: 'string', description: 'For gave_up: why the task cannot land, and what a later attempt must know. Otherwise the caveats of the whole change as you leave it, or empty.' },
    defects: defectsField('you found in this round, left unfixed, and do not see in the defects so far'),
    withdrawn_defects: {
      type: 'array',
      description: 'The number of each defect so far that you withdraw, because this change fixes it or because it is not a fault. Empty when every defect so far still holds.',
      items: { type: 'integer', description: '1-based index in the defects so far' },
    },
  },
  required: ['responses', 'gave_up', 'summary', 'notes', 'defects', 'withdrawn_defects'],
}

const SYNC_SCHEMA = {
  type: 'object',
  properties: {
    status: { type: 'string', enum: ['synced', 'abandoned'] },
    report: { type: 'string', description: 'What the merge required: each conflict and each adaptation' },
    summary: { type: 'string', description: 'What the whole change does as you leave it, in a few sentences' },
    notes: { type: 'string', description: 'For abandoned: the evidence, and what a later attempt must know. Otherwise the caveats of the whole change as you leave it, or empty.' },
    defects: defectsField('you found and left unfixed'),
  },
  required: ['status', 'report', 'summary', 'notes', 'defects'],
}

const NARROW_SCHEMA = {
  type: 'object',
  properties: {
    followups: {
      type: 'array',
      description: 'One task per open finding',
      items: NEW_TASK,
    },
    summary: { type: 'string', description: 'What the whole change does as it lands, in a few sentences' },
    notes: { type: 'string', description: 'Each claim that you removed or reworded, and each earlier caveat that still holds' },
    defects: defectsField('you found, left unfixed, and do not see in the defects so far'),
  },
  required: ['followups', 'summary', 'notes', 'defects'],
}

// The rules of every agent.
//
// The rule about commands that run at the same time keeps the agents of a
// run inside the memory of the machine that they share. A task about a
// test that fails under load invites its agent to create that load. One
// agent ran twelve copies of a race test at the same time, and the kernel
// killed the services of the user to free memory.
//
// The last rule covers a message that the user sends to the orchestrator
// during the run. Workflow relays that message to each agent of a
// Workflow that the orchestrator launches in the same turn. It presents
// the message as the user request behind the run. Without the rule, an
// agent that reads a message about other work can abandon its assignment.
const rules = `
Ground rules:
- The worktree is ${T.wt}, on branch ${BRANCH}, which was cut from ${base}. Work only there. Use absolute paths under it for every file you read or edit, and start every shell command with \`cd ${T.wt} && \`. Never change files or git state in the main checkout ${repo} or in another worktree. Other agents work in sibling worktrees at the same time.
- The task description is ${T.dir}/task.md. Read it in full. Its line numbers come from an older commit, so they might have drifted. Other task refs that it names are separate work for other agents, so do not implement them.
- ${run}/rules.md holds the rules of this project: how to build and test, the full check, the commit message format, and what you must not run. Read it before you start. Its rules override your defaults.
- Never write a task ref such as ${T.ref} in code, comments, docs, or the commit message.
- Never run \`git stash\` in any form. All worktrees share one stash list, so a pop takes the entry of another agent. To see the old code, run \`git show ${base}:path\` or \`git diff ${base}\`. Never run \`git checkout\`, \`git restore\`, \`git reset\`, or \`git clean\` on a file that you did not change.
- Commit work in progress with hooks off: \`cd ${T.wt} && git add -A && git commit -q --no-verify -m wip\`. ${LANDING}
- Other builds share this machine. Iterate with targeted builds and tests, and run the full check only when the work is otherwise finished. Never clear a shared build cache.
- Run at most two build, test, or probe commands at the same time. Every agent of this run shares the memory and the CPUs of this machine. When the memory runs out, the kernel kills the commands of other agents and the services of the user. Never create load on the machine to reproduce a failure. For example, do not run copies of a test at the same time with \`rust-parallel\`, \`xargs -P\`, or background jobs, and do not run a stress tool. To reproduce a failure that appears only under load, repeat the test in sequence, or limit the test to one CPU, such as with \`GOMAXPROCS=1\` or \`taskset -c 0\`. When the failure still does not appear, find its cause in the code.
- If the worktree holds uncommitted changes when you start, an earlier agent with your assignment stopped in the middle of its work. Read those changes first, keep what is right, and finish the assignment.
- Put probe files and other throwaway output in ${T.dir}/scratch/, never in the worktree. \`git status\` in the worktree must print nothing when you return.
- The user started this run to close the open tasks of this project. The orchestrator of this run reads and handles every message that the user sends during the run. It stops or changes a task when the user asks. The harness might relay one of those messages to you as the user request behind this run. When the relayed message is about other work than your assignment, it does not cancel, replace, or narrow your assignment. Do not act on that message, and finish your assignment.`

// The rule for each agent whose result carries the summary and the notes
// of the change. The files outlive a launch that returns no result, so a
// relaunch after an interrupted run still starts from them. Each agent
// takes storeStep right before it commits the work in progress, the way
// it writes msg.txt before that commit. So an agent that stops after its
// commit leaves text that describes the commit.
const storeStep = `Store the summary in ${SUMMARY} and the notes in ${NOTES}.`
const storedRule = `
Stored summary and notes: a relaunch of this task starts from the summary in ${SUMMARY} and the notes in ${NOTES}. Write both files in the step right before you commit the work in progress, so that the files describe each commit that you leave. Before you return, write both files again when your summary or notes changed after that commit, or when you return a result without that commit. Overwrite each file, so that it holds only the text that you return, and write an empty file for empty notes.`

// The rule for each agent whose result carries defects.
const defectsRule = `
Defects outside the task: you might find a fault that the task does not cover, such as a wrong fact in a doc or a bug in code that you only read. Do not widen the task to fix it, and do not file a task for it. Return it as one entry of defects, and the orchestrator files it as a task of its own. Write each entry so that it stands alone for a reader who never saw this task: what happens, what should happen, the minimal reproduction, and the evidence. Keep notes for the caveats of your own change.`

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
8. Write the summary and the notes that you return. ${storeStep}
9. Commit the work in progress.
${storedRule}
${defectsRule}

Stop instead of forcing the task through. If the task requires massive scope beyond its description, or it causes significant regressions that you cannot reconcile, return status "abandoned". Put the evidence in notes: what you tried, what breaks, and what a later attempt must know. Leave the partial work committed in the worktree.`

const reviewPrompt = (round, summary, extra) => `You are an adversarial code reviewer. A different agent implemented task ${T.ref}: "${T.title}". Find what is wrong with the change. Assume that it has defects until you have checked.
${rules}
- You are read-only on the worktree. Do not edit or commit there. Run experiments in ${T.dir}/scratch/. If an experiment requires a file inside the worktree, delete that file before you return.
- Do not run the full check. Run targeted builds and tests.

${summary ? `The summary from the agent that last changed the work: ${summary}` : ''}
The proposed commit message is in ${MSG}.
${extra}
Review steps:
1. Read the task file. Then read \`cd ${T.wt} && git diff ${base}...HEAD\` and the changed code in its full context.
2. Check that the change resolves the task. Run the examples of the task as real code where you can. Probe the edge cases that the implementer might have skipped, such as empty input, zero values, error paths, and concurrent use.
3. Hunt regressions: callers in other packages, behavior that the tests stopped pinning, and performance cliffs.
4. Check the tests. Each test must fail without the change, and each new branch that a public call reaches needs a test.
5. Check docs and conventions: doc comments and docs that the change made false, the rules of rules.md and CLAUDE.md, leftover shims, changes outside the task scope, and task refs in the tree. Search the repository for the old names and the old behavior.
6. Check that the commit message describes the diff and claims nothing more.

Report only findings that require a change, each with a location, the proof, and the exact fix. Leave out praise, restatements, and style preferences that the linters accept. Severity: blocker for wrong behavior or a broken build, major for a missing part of the task, a regression, or a missing test, and minor for a doc or convention fault. Set regression on each finding. ${REGRESSION} Set improves_base for the change as a whole.

Verdict:
- "approve" only when you have zero findings.
- "changes" when a fixer can repair the findings inside the task scope.
- "reject" only when the approach itself forces massive scope creep or significant regressions. Give the reason in reject_reason.`

// What a reviewer after the first round learns about the earlier rounds.
// The afresh flag adds the sentence that asks for a new review of the
// whole diff. Each loop round after the first one sets the flag.
// boundedNote limits the final review to the last fixes, so that review
// leaves the flag unset.
const historyNote = (rebutted, afresh) => `
Earlier review rounds already happened, and ${HISTORY} records each finding with the fixer's answer. Read it. Verify each claimed fix in the code. Do not raise a rebutted finding again unless the rebuttal is wrong, and then say why.${afresh ? ' Then review the whole diff afresh, because fixes introduce new faults.' : ''}
${rebutted.length ? 'Rebuttals so far:\n' + rebutted.join('\n') : ''}
`

const listFindings = findings => findings
  .map((f, i) => `${i + 1}. [${f.severity}] ${f.location}\n   Problem: ${f.problem}\n   Evidence: ${f.evidence}\n   Fix: ${f.fix}`)
  .join('\n')

// The scope of the final review: the findings of the last round and
// their exact fixes. The review steps of reviewPrompt follow this note
// in the prompt, and they describe a review of the whole change. So the
// note limits what the reviewer checks in those steps, and what the
// reviewer reports.
const boundedNote = open => `
This review is narrow. A fixer applied only the exact fixes of the findings below. Do not review the whole diff again. In the review steps, check only these findings and what their fixes changed. Report a finding only when it is one of these findings and it is still open, or when one of these fixes introduced it.
${listFindings(open)}
`

// The summary and the notes that the result carries so far, for the
// prompt of an agent that changes the work and returns both revised.
const summaryNote = ({ summary, notes }) => `
The orchestrator of this run acts on the summary and the notes of the change, and it does not read the diff.

The summary of the change so far:
${summary || 'none'}

The notes of the change so far:
${notes || 'none'}
`

// The defects that the result carries so far, for the prompt of a fixer
// or the narrowing agent. A fixer withdraws a defect by its number in this
// list.
const defectsNote = defects => `
The defects outside the task that the change reports so far:
${defects.length ? defects.map((d, i) => `${i + 1}. ${d.title}\n${d.description.replace(/^/gm, '   ')}`).join('\n') : 'none'}
`

const fixPrompt = (round, findings, exact, current) => `You repair a change after an adversarial review. A different agent implemented task ${T.ref}: "${T.title}", and a reviewer reported the findings below.
${rules}

The current commit message is in ${MSG}.
${summaryNote(current)}${defectsNote(current.defects)}
Findings:
${listFindings(findings)}

Steps:
1. Read the task file and \`cd ${T.wt} && git diff ${base}...HEAD\`, so that you know the change.
2. Check each finding against the code. Fix each finding that is right, with a test where the finding shows a behavior gap. Rebut a finding only when you can prove it wrong, and give that proof in the response detail.${exact ? `
   Keep this pass minimal. Apply only the exact fix of each finding. Do not refactor, and do not widen the change.` : ''}
3. Make the full check pass in the worktree.
4. Update ${MSG} so that it describes the whole diff against ${base}.
5. Append a section to ${HISTORY}, creating the file if it does not exist. Head it \`## Round ${round}\`. List each finding with its location and problem, followed by your answer: fixed or rebutted, and the detail.
6. Write summary and notes that describe the whole change as you leave it, not this round alone. Start from the summary and the notes so far, and check each of their statements against the worktree and the findings. Keep each statement that still holds. Correct each statement that a finding or a fix made false, and remove each caveat that a fix resolved. Add each caveat that this round found. When the summary so far is "none", write it from the diff. ${storeStep}
7. Commit the work in progress.
8. Return one response per finding.
9. Return the summary and the notes that you stored.
10. Check each defect so far against the worktree and the findings. Return in withdrawn_defects the number of each defect that this change fixes, and of each defect that you can prove is not a fault. The result keeps every defect that you do not withdraw, so do not return it again in defects.
${storedRule}
${defectsRule}

If the findings show that the task cannot land without massive scope creep or significant regressions, set gave_up to true and explain in notes instead of forcing the task through.`

const syncPrompt = (reason, current) => `You bring one finished change up to date with ${base}. A different agent implemented task ${T.ref}: "${T.title}" on a branch cut from an older ${base}, and adversarial review accepted it. ${base} has gained commits from other tasks, and the branch stopped landing.

Why the branch does not land:
${reason}
${rules}

The current commit message is in ${MSG}.
${summaryNote(current)}
Steps:
1. Read the task file and \`cd ${T.wt} && git diff ${base}...HEAD\`, so that you know the change. Read \`git log --stat HEAD..${base}\`, so that you know what ${base} gained.
2. Merge ${base} into the branch: \`cd ${T.wt} && git merge --no-verify -m "Merge ${base}" ${base}\`. Keep the \`-m\` option, because the default message of a merge names the branch of this task. Resolve each conflict so that both changes keep their intent. Before you resolve a conflicted file, read the commits of ${base} that changed it (\`git log -p HEAD..${base} -- path\`). Finish the merge with \`git commit --no-verify --no-edit\`.
3. Adapt the code, tests, and docs of this task to the interfaces and the behavior that ${base} has. Never revert or weaken what ${base} gained. Check that the docs of this task are still true beside the new commits.
4. Make the full check pass in the worktree.
5. Update ${MSG} only where the merge changed what the diff against ${base} does.
6. Write summary and notes that describe the whole change as you leave it, not the merge alone. Start from the summary and the notes so far, and check each of their statements against the worktree. Keep each statement that still holds. Correct each statement that the merge made false, and remove each caveat that the merge resolved. Add each caveat that the merge brought. When the summary so far is "none", write it from the diff. ${storeStep}
7. Commit the work in progress.
8. Return in report what the merge required: each conflict and each adaptation.
9. Return the summary and the notes that you stored.
${storedRule}
${defectsRule}

If the commits on ${base} make this task obsolete or force a redesign, return status "abandoned" with the evidence in notes.`

const narrowPrompt = (findings, current) => `You prepare one change to land with known gaps. A different agent implemented task ${T.ref}: "${T.title}". A review left the findings below open. A reviewer judged that the change is a strict improvement over ${base} and that no open finding is a regression, so the change lands as it stands, and a follow-up task tracks each open finding.
${rules}

Open findings:
${listFindings(findings)}
${summaryNote(current)}${defectsNote(current.defects)}
Steps:
1. Do not fix the findings, and do not change behavior.
2. Narrow the claims to what the code does. Edit each doc comment, doc, and example of this change, and the commit message in ${MSG}, so that none promises a behavior that an open finding contradicts. State a known limit where a reader would otherwise expect the full behavior.
3. Make the full check pass in the worktree.
4. Write summary and notes that describe the whole change as it lands. Start from the summary and the notes so far. Remove or reword each statement that an open finding contradicts. In notes, list each claim that you removed or reworded, and keep each caveat that still holds. When the summary so far is "none", write it from the diff. ${storeStep}
5. Commit the work in progress.
6. Return the summary and the notes that you stored.
7. Return one follow-up task per open finding. Each task must stand alone for a reader who never saw this review: write what happens, what should happen, the minimal reproduction, and the evidence.
8. Return in defects only the faults that the defects so far do not list. The result keeps every defect so far.
${storedRule}
${defectsRule}`

// An agent that dies, or that ends without its structured result, returns
// null here, and each caller retries it one time. The retry finds the
// uncommitted work of the first attempt in the worktree.
//
// The role option names the role of the agent. The agent and its retry
// run on the model that MODELS holds for that role. An agent call
// without a model option follows the session model, so the call of a
// role without a model carries no such option.
const attempt = async (prompt, { role, ...opts }) => {
  const model = MODELS[role] ? { model: MODELS[role] } : {}
  for (const suffix of ['', ' retry']) {
    try {
      const result = await agent(prompt, { ...opts, ...model, label: opts.label + suffix })
      if (result) return result
    } catch (err) {
      log(`${opts.label}${suffix} failed: ${err && err.message ? err.message : err}`)
    }
  }
  return null
}

// The result. The implementer writes its summary and notes. Each fixer,
// the sync agent, and the narrowing agent receive both in the prompt and
// return them revised for the whole change. So a fix that overturns a
// statement also corrects it. Each of these agents also writes both to
// the task directory before its commit, the way it writes msg.txt, as
// storedRule says. A task that resumes starts with the summary and the
// notes that `ultraclose.py args` read from those files. `ultraclose.py`
// removes both files when it starts a task at the implementer, so each
// one is empty when no agent wrote its file after that start. The error,
// rejected, and unconverged results replace the notes with their reason.
//
// The defects of the result start as the list of the implementer. A task
// that resumes at a review round starts with an empty list, or with the
// list of its sync agent. Each fixer receives the list in the prompt, adds
// the defects that it found, and withdraws entries by number. On a
// landable result, the narrowing agent receives the list and adds its
// defects last. No agent copies the list, so no entry changes on the way
// to the orchestrator. Every result carries the list, whatever its status.
const out = { ref: T.ref, title: T.title, rounds: 0, summary: RESUME ? RESUME.summary : '', notes: RESUME ? RESUME.notes : '', defects: [], findings: [], followups: [], boundedReview: false }

// What the orchestrator does with a result. Each result carries its
// action in the action field, with the `ultraclose.py` command that the
// orchestrator runs. So the orchestrator does not map a status to its
// commands from the text of the skill. A long run can drop that text
// from its context.
//
// The set command of `ultraclose.py` runs the aven commands of a noted
// task and of a closed task. It adds the note, and it sets the status of
// the aven task. For a noted task, it also adds the branch and the pull
// request of the task to the note.
//
// The pr field of the task holds the address of the pull request that
// the task records. Only the relaunch of a proposed task has one. The
// script contacts no remote, so the orchestrator closes that pull
// request or leaves it open.
const LAND = `land the task: run \`ultraclose.py land ${T.ref} --next\` in a background Bash call.`
const NOTE_COMMAND = `ultraclose.py set ${T.ref} noted --note '<the reason and the open findings>'`
// The text that notes the task, after a lead that says when to note it.
const noteAfter = lead => `${lead}: run \`${NOTE_COMMAND}\`. The command adds the note and the branch of the task to the aven task, sets the aven task to todo, and keeps the branch. Describe the findings in the note, and do not count the review rounds there. Do not force the task through.${T.pr ? ` Leave the pull request ${T.pr} open for the user, and record it in notes.md.` : ''}`
const NOTE = noteAfter('Note the task')
const CLOSE = `close the task: run \`ultraclose.py set ${T.ref} closed --note '<the evidence>'\` with the evidence that the notes give. The command adds the note to the aven task and sets the aven task to done.`
// A review that covered only the final fixes. The orchestrator reports
// each task that landed after such a review.
const BOUNDED = ' The last review covered only the final fixes, so record the task in notes.md.'
const sentence = text => text[0].toUpperCase() + text.slice(1)
const ACTIONS = {
  approved: sentence(LAND),
  landable: `File each entry of followups as an aven task with \`aven add\`, and link each new task with \`aven related add <new-ref> ${T.ref}\`. Then ${LAND}`,
  obsolete: T.pr ? `Close the pull request ${T.pr} with mcp__github__update_pull_request when it is open, and record it in notes.md. Then ${CLOSE}` : sentence(CLOSE),
  abandoned: NOTE,
  rejected: NOTE,
  unconverged: NOTE,
}
const RELAUNCH = `ultraclose.py args ${T.ref}`

// What the orchestrator checks first when an agent of a role returned
// nothing. Workflow starts no agent on a model that it cannot run, so
// such an agent returns nothing. MODELS names a model for the role only
// when the run names one, of its own or as the default model.
const modelCheck = role => MODELS[role]
  ? `The run names the model ${MODELS[role]} for the ${role} role. Check that Workflow can run that model, and correct a wrong model with the --model flag of \`ultraclose.py config\`. `
  : ''

// The action of an error result. The agent of the role returned nothing,
// and so did its retry. The relaunch argument holds the command that
// relaunches the task at that agent.
const errorAction = (role, relaunch) => `${modelCheck(role)}${MODELS[role] ? 'Then relaunch' : 'Relaunch'} the task one time: run \`${relaunch}\`, and launch the object that it prints from the path in its script field. If that relaunch ends in error as well, note the task with \`${NOTE_COMMAND}\`.`

// The action of an unconverged result whose narrowing agent returned
// nothing. The change is a strict improvement, so a relaunch at the
// final cycle can still land it after the orchestrator corrects a wrong
// model. A relaunched task runs in a new Workflow, so the orchestrator
// notes the task only when it does not relaunch it.
const narrowAction = MODELS.narrow
  ? `${modelCheck('narrow')}When the model was wrong, relaunch the task after you correct it: run \`${RELAUNCH} --final\`, and do not note the task. ${noteAfter('When the model is right, do not relaunch the task, and note it instead')}`
  : NOTE

const done = (status, extra) => {
  const result = { ...out, status, ...extra }
  const bounded = result.boundedReview && (status === 'approved' || status === 'landable') ? BOUNDED : ''
  return { ...result, action: result.action || ACTIONS[status] + bounded }
}

let extra = ''
if (RESUME && RESUME.sync) {
  const sync = await attempt(syncPrompt(RESUME.sync, out), { role: 'sync', label: `sync ${T.ref}`, phase: 'Sync', schema: SYNC_SCHEMA })
  if (!sync) return done('error', { notes: 'the sync agent returned nothing', action: errorAction('sync', `${RELAUNCH} --sync '<the reason that started the sync agent>'`) })
  out.summary = sync.summary
  out.notes = sync.notes
  out.defects = sync.defects
  if (sync.status !== 'synced') return done('abandoned')
  extra = `
This change passed adversarial review on an older ${base}. ${base} then gained commits from other tasks, and the branch stopped landing: ${RESUME.sync}
A sync agent merged ${base} into the branch and adapted the change. Its report: ${sync.report}
Review the whole diff against ${base} again. Give the merge the most attention: lost or duplicated hunks, code of this task that ignores what ${base} gained, and docs that are false beside the new commits.
`
} else if (!RESUME) {
  const impl = await attempt(implPrompt, { role: 'implementer', label: `impl ${T.ref}`, phase: 'Implement', schema: IMPL_SCHEMA })
  if (!impl) return done('error', { notes: 'the implementer returned nothing', action: errorAction('implementer', RELAUNCH) })
  out.summary = impl.summary
  out.notes = impl.notes
  out.defects = impl.defects
  if (impl.status !== 'implemented') return done(impl.status)
}

const rebutted = []
for (let round = FIRST; ; round++) {
  out.rounds = round
  const history = round > 1 ? historyNote(rebutted, true) : ''
  const rev = await attempt(reviewPrompt(round, out.summary, extra + history), { role: 'reviewer', label: `review ${T.ref} r${round}`, phase: 'Review', schema: REVIEW_SCHEMA })
  if (!rev) return done('error', { notes: `the reviewer of round ${round} returned nothing`, action: errorAction('reviewer', RELAUNCH) })
  if (rev.verdict === 'reject') return done('rejected', { notes: rev.reject_reason, findings: rev.findings })
  if (rev.findings.length === 0) return done('approved')
  log(`${T.ref}: round ${round} has ${rev.findings.length} findings`)

  const capped = round >= LAST
  const fix = await attempt(fixPrompt(round, rev.findings, capped, out), { role: 'fixer', label: `fix ${T.ref} r${round}`, phase: capped ? 'Final' : 'Fix', schema: FIX_SCHEMA })
  if (!fix) return done('error', { notes: `the fixer of round ${round} returned nothing`, findings: rev.findings, action: errorAction('fixer', RELAUNCH) })
  out.summary = fix.summary
  out.notes = fix.notes
  out.defects = out.defects.filter((_, i) => !fix.withdrawn_defects.includes(i + 1)).concat(fix.defects)
  if (fix.gave_up) return done('abandoned', { findings: rev.findings })
  rev.findings.forEach((f, i) => {
    const r = fix.responses.find(x => x.finding === i + 1)
    if (r && r.action === 'rebutted') rebutted.push(`- ${f.location}: ${f.problem}\n  Rebuttal: ${r.detail}`)
  })
  if (!capped) continue

  // The cap: one bounded review of the exact fixes, then land or note.
  log(`${T.ref}: reached the cap at round ${round}`)
  out.rounds = round + 1
  out.boundedReview = true
  const fin = await attempt(reviewPrompt(round + 1, out.summary, boundedNote(rev.findings) + historyNote(rebutted, false)), { role: 'reviewer', label: `review ${T.ref} final`, phase: 'Final', schema: REVIEW_SCHEMA })
  if (!fin) return done('unconverged', { notes: 'the final reviewer returned nothing, so the findings of the last round stand unverified', findings: rev.findings })
  if (fin.verdict !== 'reject' && fin.findings.length === 0) return done('approved')
  const landable = fin.verdict !== 'reject' && fin.improves_base && !fin.findings.some(f => f.regression)
  if (!landable) return done('unconverged', { notes: fin.reject_reason, findings: fin.findings })
  const narrow = await attempt(narrowPrompt(fin.findings, out), { role: 'narrow', label: `narrow ${T.ref}`, phase: 'Final', schema: NARROW_SCHEMA })
  if (!narrow) return done('unconverged', { notes: 'the change is a strict improvement, but the narrowing agent returned nothing', findings: fin.findings, action: narrowAction })
  out.summary = narrow.summary
  out.notes = narrow.notes
  out.defects = out.defects.concat(narrow.defects)
  return done('landable', { findings: fin.findings, followups: narrow.followups })
}

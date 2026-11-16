---
name: skill-creator
description: Create new skills, modify and improve existing skills, and measure skill performance. Use when users want to create a skill from scratch, edit, or optimize an existing skill, run evals to test a skill, benchmark skill performance with variance analysis, or optimize a skill's description for better triggering accuracy.
---

# Skill Creator

Use this skill to create a new skill and to improve it over several iterations.

Creating a skill runs through these stages:

1. Decide what the skill should do and roughly how it should do it.
2. Write a draft of the skill.
3. Write a few test prompts, and run Claude with access to the skill on each of them.
4. Help the user evaluate the results, both qualitatively and quantitatively. While the runs happen in the background, draft quantitative evals when none exist. When some exist, you can use them as they are, or change the ones that need a change. Explain the evals to the user, then show the user the results and the quantitative metrics with the `eval-viewer/generate_review.py` script.
5. Rewrite the skill based on the user's feedback on the results. Fix any glaring flaws that the quantitative benchmarks reveal as well.
6. Repeat the runs, the evaluation, and the rewrite until you are satisfied.
7. Expand the test set and try again at a larger scale.

When you use this skill, find where the user is in this process, and help them through the stages that remain. For example, the user might say "I want to make a skill for X". You can help clarify what they mean and write a draft. You can then write the test cases, decide with them how to evaluate the results, run all the prompts, and repeat.

If the user already has a draft of the skill, you can go straight to the evaluation and iteration stages of the loop.

Stay flexible. If the user wants to skip the formal evaluations and iterate by feel, you can work that way instead.

After the skill is done, or at another point that suits the user, you can run the description optimizer. The optimizer is a separate script that tunes the description so that the skill triggers on the right prompts.

## Communicating with the user

People who use the skill creator range widely in their familiarity with coding jargon. Claude has attracted people who never opened a terminal before, such as plumbers, and parents and grandparents who search for "how to install npm". But most users are comfortable with computers.

So read the context cues in the conversation to decide how to phrase what you say. Without such cues, these defaults give a rough guide:

- "Evaluation" and "benchmark" are borderline terms, but you can use them.
- Use "JSON" and "assertion" without an explanation only after the user shows clear signs that they know the terms.

When you are unsure whether the user knows a term, you can explain it with a short definition.

---

## Creating a skill

### Capture Intent

Start by understanding the user's intent. The current conversation might already hold a workflow that the user wants to capture, such as when they say "turn this into a skill". In that case, extract answers from the conversation history first. Look for the tools used, the sequence of steps, the corrections that the user made, and the input and output formats. Ask the user to fill any gaps and to confirm the answers before you move to the next step.

Answer these questions with the user:

1. What should this skill enable Claude to do?
2. When should this skill trigger, that is, on which user phrases and in which contexts?
3. What is the expected output format?
4. Should we write test cases to verify that the skill works? Skills with objectively verifiable outputs benefit from test cases, such as skills for file transforms, data extraction, code generation, and fixed workflow steps. Skills with subjective outputs, such as writing style or art, often do not need them. Suggest the default that fits the type of skill, but let the user decide.

### Interview and Research

Ask about edge cases, input and output formats, example files, success criteria, and dependencies without waiting for the user to raise them. Write the test prompts only after you settle these points.

Check which MCP servers are available. When a server can help the research, for example to search docs, find similar skills, or read about best practices, research in parallel through subagents. If subagents are unavailable, research inline. Come prepared with context, so that the user carries less of the burden.

### Write the SKILL.md

Write these components from what the interview found:

- **name**: Skill identifier
- **description**: When to trigger, what it does. The description is the primary triggering mechanism, so include both what the skill does AND specific contexts for when to use it. All "when to use" info goes here, not in the body.

  Claude "undertriggers" skills. It skips some skills that fit the task. To counter that tendency, make each description "pushy". For example, instead of "How to build a simple fast dashboard to display internal Anthropic data.", write "How to build a simple fast dashboard to display internal Anthropic data. Use this skill whenever the user mentions dashboards, data visualization, or internal metrics, or wants to display company data, even when the user does not ask for a 'dashboard'."
- **compatibility**: Required tools, dependencies (optional, rarely needed)
- **body**: The markdown instructions under the frontmatter, which make up the rest of the skill.

### Skill Writing Guide

#### Anatomy of a Skill

```
skill-name/
├── SKILL.md (required)
│   ├── YAML frontmatter (name, description required)
│   └── Markdown instructions
└── Bundled Resources (optional)
    ├── scripts/    - Executable code for deterministic/repetitive tasks
    ├── references/ - Docs loaded into context as needed
    └── assets/     - Files used in output (templates, icons, fonts)
```

#### Progressive Disclosure

Skills load in three levels:

1. **Metadata**: the name and the description. They are always in context, at about 100 words.
2. **SKILL.md body**: in context whenever the skill triggers. Fewer than 500 lines is ideal.
3. **Bundled resources**: loaded when the task calls for them. They have no size limit, and scripts can run without loading into context.

These sizes are approximate, so a skill can go past them when its content requires the space.

Follow these patterns:

- Keep SKILL.md to fewer than 500 lines. When a skill approaches that limit, add another layer of hierarchy, and point clearly to the file that the model should read next.
- Reference each bundled file clearly from SKILL.md, and say when to read it.
- Give each reference file of more than 300 lines a table of contents.

**Domain organization**: When a skill supports several domains or frameworks, organize its references by variant:

```
cloud-deploy/
├── SKILL.md (workflow + selection)
└── references/
    ├── aws.md
    ├── gcp.md
    └── azure.md
```

Claude reads only the relevant reference file.

#### Principle of Lack of Surprise

Skills must not contain malware, exploit code, or any content that might compromise system security. The contents of a skill should not surprise a user who has read a description of its intent. Refuse requests for misleading skills, or for skills built to help with unauthorized access, data exfiltration, or other malicious activities. Requests such as "roleplay as an XYZ" are fine.

#### Writing Patterns

Prefer the imperative form in instructions.

**Output formats**: You can define an output format with an exact template, as in this example:

```markdown
## Report structure
ALWAYS use this exact template:
# [Title]
## Executive summary
## Key findings
## Recommendations
```

**Examples**: Examples are useful to include, and you can format them as in this one. When the examples themselves contain the words "Input" and "Output", consider adjusting the labels so that they stand apart from the content.

```markdown
## Commit message format
**Example 1:**
Input: Added user authentication with JWT tokens
Output: feat(auth): implement JWT-based authentication
```

### Writing Style

Explain to the model why each instruction matters, instead of stacking heavy-handed MUSTs. Use theory of mind, and keep the skill general rather than narrowed to specific examples. Write a draft first, then reread it as a first-time reader, and improve it.

### Test Cases

After you write the draft, write two or three realistic test prompts, the kind of request that a real user types. Share them with the user, in words such as "Here are a few test cases I'd like to try. Do these look right, or do you want to add more?" Then run them.

Save the test cases to `evals/evals.json` with only the prompts. You draft the assertions later, during the runs.

```json
{
  "skill_name": "example-skill",
  "evals": [
    {
      "id": 1,
      "prompt": "User's task prompt",
      "expected_output": "Description of expected result",
      "files": []
    }
  ]
}
```

See `references/schemas.md` for the full schema, which includes the `assertions` field that you add later.

## Running and evaluating test cases

Run this section as one continuous sequence, and do not stop partway through. Do NOT use `/skill-test` or any other testing skill.

Put the results in `<skill-name>-workspace/`, next to the skill directory. Inside the workspace, give each iteration a directory, such as `iteration-1/` and `iteration-2/`. Inside each iteration, give each test case a directory, such as `eval-0/` and `eval-1/`. Create each directory when you first write to it.

### Step 1: Spawn all runs (with-skill AND baseline) in the same turn

For each test case, spawn two subagents in the same turn, one with the skill and one without it. Do not spawn the with-skill runs first and spawn the baselines in a later turn. Launch every run at once, so that all of them finish at about the same time.

**With-skill run:**

```
Execute this task:
- Skill path: <path-to-skill>
- Task: <eval prompt>
- Input files: <eval files if any, or "none">
- Save outputs to: <workspace>/iteration-<N>/eval-<ID>/with_skill/outputs/
- Outputs to save: <what the user cares about — e.g., "the .docx file", "the final CSV">
```

**Baseline run**: The baseline gets the same prompt, and the context decides what it runs with:

- **Creating a new skill**: The baseline runs with no skill at all. Give it the same prompt and no skill path, and save its outputs to `without_skill/outputs/`.
- **Improving an existing skill**: The baseline runs the old version. Before you edit, snapshot the skill with `cp -r <skill-path> <workspace>/skill-snapshot/`, then point the baseline subagent at the snapshot. Save its outputs to `old_skill/outputs/`.

Write an `eval_metadata.json` for each test case. Its assertions can stay empty at this stage. Give each eval a descriptive name that says what it tests, rather than a bare "eval-0", and use that name for its directory too. When this iteration uses new or changed eval prompts, create these files in each new eval directory. Do not assume that they carry over from earlier iterations.

```json
{
  "eval_id": 0,
  "eval_name": "descriptive-name-here",
  "prompt": "The user's task prompt",
  "assertions": []
}
```

### Step 2: While runs are in progress, draft assertions

Use the time while the runs are in progress. Draft quantitative assertions for each test case, and explain them to the user. When `evals/evals.json` already holds assertions, review them and explain what they check.

Good assertions are objectively verifiable, and each has a descriptive name. The names should read clearly in the benchmark viewer, so that someone who glances at the results understands what each assertion checks. Prefer a qualitative evaluation for subjective skills, such as skills for writing style or design quality. Do not force assertions onto qualities that need human judgment.

After you draft the assertions, add them to the `eval_metadata.json` files and to `evals/evals.json`. Explain to the user what the viewer shows, both the qualitative outputs and the quantitative benchmark.

### Step 3: As runs complete, capture timing data

When each subagent task completes, you receive a notification that holds `total_tokens` and `duration_ms`. Save those values immediately to `timing.json` in the run directory:

```json
{
  "total_tokens": 84852,
  "duration_ms": 23332,
  "total_duration_seconds": 23.3
}
```

The values reach you only through the task notification, and nothing saves them anywhere else. So process each notification when it arrives, rather than in a batch.

### Step 4: Grade, aggregate, and launch the viewer

After all runs finish:

1. **Grade each run**: Spawn a grader subagent that reads `agents/grader.md` and evaluates each assertion against the outputs, or grade inline. Save the results to `grading.json` in each run directory. The `expectations` array of `grading.json` must use the fields `text`, `passed`, and `evidence`, never a variant such as `name`, `met`, or `details`. The viewer reads exactly these field names. When a script can check an assertion, write and run the script rather than judging the output by eye. Scripts run faster, give more reliable results, and serve every later iteration.

2. **Aggregate into a benchmark**: Run the aggregation script from the skill-creator directory:

   ```bash
   python -m scripts.aggregate_benchmark <workspace>/iteration-N --skill-name <name>
   ```

   The script writes `benchmark.json` and `benchmark.md`. They give the pass rate, the time, and the tokens of each configuration as a mean ± standard deviation, with the delta between the configurations. To write `benchmark.json` by hand, follow the exact schema in `references/schemas.md`, because the viewer expects that schema. Put each with_skill version before its baseline counterpart.

3. **Do an analyst pass**: Read the benchmark data, and find patterns that the aggregate statistics might hide. The "Analyzing Benchmark Results" section of `agents/analyzer.md` lists the patterns to find. They include non-discriminating assertions that pass with or without the skill, high-variance evals that might be flaky, and trade-offs between time and tokens.

4. **Launch the viewer** with both the qualitative outputs and the quantitative data:

   ```bash
   nohup python <skill-creator-path>/eval-viewer/generate_review.py \
     <workspace>/iteration-N \
     --skill-name "my-skill" \
     --benchmark <workspace>/iteration-N/benchmark.json \
     > /dev/null 2>&1 &
   VIEWER_PID=$!
   ```

   From iteration 2 on, also pass `--previous-workspace <workspace>/iteration-<N-1>`.

   **Cowork and headless environments**: When `webbrowser.open()` is not available, or the environment has no display, pass `--static <output_path>` to write a standalone HTML file instead of starting a server. When the user clicks "Submit All Reviews", the browser downloads the feedback as a `feedback.json` file. Copy that file into the workspace directory, where the next iteration reads it.

   Create the viewer with `generate_review.py`. You do not need to write custom HTML.

5. **Tell the user** where the results are, in words such as: "I've opened the results in your browser. The 'Outputs' tab shows each test case in turn and takes your feedback on it. The 'Benchmark' tab shows the quantitative comparison. When you finish, tell me here."

### Viewer Contents

The "Outputs" tab shows one test case at a time:

- **Prompt**: the task that the run received.
- **Output**: the files that the skill produced, rendered inline when the viewer can render their format.
- **Previous Output**: from iteration 2 on, a collapsed section that shows the output of the last iteration.
- **Formal Grades**: when a grader ran, a collapsed section that shows whether each assertion passed or failed.
- **Feedback**: a text box that saves as the user types.
- **Previous Feedback**: from iteration 2 on, the user's comments from the last iteration, below the text box.

The "Benchmark" tab shows the summary statistics, which give the pass rate, the timing, and the token usage of each configuration. It adds a breakdown for each eval and the analyst's observations.

The user moves between test cases with the previous and next buttons or with the arrow keys. When done, the user clicks "Submit All Reviews", which saves all the feedback to `feedback.json`.

### Step 5: Read the feedback

When the user tells you they're done, read `feedback.json`:

```json
{
  "reviews": [
    {"run_id": "eval-0-with_skill", "feedback": "the chart is missing axis labels", "timestamp": "..."},
    {"run_id": "eval-1-with_skill", "feedback": "", "timestamp": "..."},
    {"run_id": "eval-2-with_skill", "feedback": "perfect, love this", "timestamp": "..."}
  ],
  "status": "complete"
}
```

Empty feedback means the user thought the output was fine. Focus your improvements on the test cases where the user had specific complaints.

Kill the viewer server when you are done with it:

```bash
kill $VIEWER_PID 2>/dev/null
```

---

## Improving the skill

After you run the test cases and the user reviews the results, improve the skill based on the user's feedback. This stage is the core of the loop.

### Improvement Principles

1. **Generalize from the feedback.** The goal is a skill that runs a million times or more, across many different prompts. You and the user iterate repeatedly on a few examples, because the user knows them well and judges new outputs quickly. But a skill that works only for those examples is useless. When an issue persists, do not add fiddly changes that overfit the examples, or oppressively rigid MUSTs. You can try different metaphors instead, or recommend different ways of working, because such attempts cost little and one of them might work well.

2. **Keep the prompt lean.** Remove the parts of the skill that cost more than they add to the results. Read the transcripts as well as the final outputs. When the skill makes the model spend time on unproductive work, you can try removing the parts that cause that work, and see what happens.

3. **Explain the why.** Explain the reason behind everything that you ask the model to do. LLMs are *smart* and have good theory of mind, so with a good harness they go beyond rote instructions and get real results. Even when the user's feedback is terse or frustrated, work to understand the task, what the user wrote, and why they wrote it. Then carry that understanding into the instructions. Treat ALWAYS or NEVER in all caps, or a rigid structure, as a warning sign, and try to reframe the instruction around its reasoning. The model then understands why the instruction matters, and that approach is more humane and more effective.

4. **Find repeated work across test cases.** Read the transcripts of the test runs. Check whether the subagents each wrote similar helper scripts or took the same multi-step approach to a problem. When every test case led its subagent to write a `create_docx.py` or a `build_chart.py`, the skill should bundle that script. Write the script once, put it in `scripts/`, and tell the skill to use it. Every later run of the skill then starts with the script instead of writing it again.

This task matters, because the skills that this process creates can produce billions a year in economic value. Your thinking time is not the bottleneck, so take your time and consider the revision carefully. We suggest that you write a draft revision, then reread it as a first-time reader, and improve it. Do your best to understand what the user wants and needs.

### The iteration loop

Each round of the loop runs these steps:

1. Apply your improvements to the skill.
2. Rerun all test cases, including the baseline runs, into a new `iteration-<N+1>/` directory. For a new skill, the baseline is always `without_skill`, which runs with no skill, and it stays the same across iterations. For an existing skill, choose the baseline that makes the most sense, either the original version that the user started with or the previous iteration.
3. Launch the reviewer with `--previous-workspace` set to the previous iteration.
4. Wait for the user to review the results and tell you that they are done.
5. Read the new feedback, improve the skill again, and repeat.

Continue until one of these conditions holds:

- The user says they are happy.
- Every piece of feedback is empty, so everything looks good.
- You stop making meaningful progress.

---

## Advanced: Blind comparison

The blind comparison system gives a more rigorous comparison between two versions of a skill, for example when the user asks "is the new version better?" Read `agents/comparator.md` and `agents/analyzer.md` for the details. The system gives the two outputs to an independent agent without saying which is which, and the agent judges their quality. Then you analyze why the winner won.

The blind comparison is optional and requires subagents, and most users do not need it. The human review loop is usually enough.

---

## Description Optimization

The description field in SKILL.md frontmatter is the primary mechanism that determines whether Claude invokes a skill. After creating or improving a skill, offer to optimize the description for better triggering accuracy.

### Step 1: Generate trigger eval queries

Create 20 eval queries, a mix of queries that should trigger the skill and queries that should not. Save them as JSON:

```json
[
  {"query": "the user prompt", "should_trigger": true},
  {"query": "another prompt", "should_trigger": false}
]
```

Each query must be realistic, the kind of request that a Claude Code or Claude.ai user types. Write concrete, specific requests with a good amount of detail, not abstract ones. Include details such as file paths, personal context about the user's job or situation, column names and values, company names, URLs, and a little backstory. You may write some queries in lowercase, or with abbreviations, typos, or casual speech. Vary the lengths, and focus on edge cases rather than clear-cut ones. The user approves the queries in Step 2.

Bad: `"Format this data"`, `"Extract text from PDF"`, `"Create a chart"`

Good: `"ok so my boss just sent me this xlsx file (its in my downloads, called something like 'Q4 sales final FINAL v2.xlsx') and she wants me to add a column that shows the profit margin as a percentage. The revenue is in column C and costs are in column D i think"`

For the 8 to 10 **should-trigger** queries, think about coverage. Write different phrasings of the same intent, some formal and some casual. Include cases where the user needs the skill but does not name the skill or the file type. Add some uncommon use cases, and cases where this skill competes with another skill and should win.

For the 8 to 10 **should-not-trigger** queries, the near-misses are the most valuable. Near-misses share keywords or concepts with the skill but need something different. Think of adjacent domains, and of ambiguous phrasing that a naive keyword match triggers on but that should not trigger the skill. Include queries that involve something the skill does, but in a context where another tool fits better.

Do not make the should-not-trigger queries obviously irrelevant. "Write a fibonacci function" as a negative test for a PDF skill is too easy to test anything. Make each negative case tricky.

### Step 2: Review with user

Present the eval set to the user in the HTML review template:

1. Read the template from `assets/eval_review.html`.
2. Replace each placeholder with its value:
   - `__EVAL_DATA_PLACEHOLDER__`: the JSON array of eval items, without quotes around it, because the template assigns it to a JS variable.
   - `__SKILL_NAME_PLACEHOLDER__`: the skill's name.
   - `__SKILL_DESCRIPTION_PLACEHOLDER__`: the skill's current description.
3. Write the result to a temporary file, such as `/tmp/eval_review_<skill-name>.html`, and open it with `open /tmp/eval_review_<skill-name>.html`.
4. The user can edit the queries, toggle should-trigger, and add or remove entries. Then they click "Export Eval Set".
5. The browser downloads the file to `~/Downloads/eval_set.json`. When the Downloads folder holds several versions, such as `eval_set (1).json`, read the most recent one.

This step matters, because bad eval queries lead to bad descriptions.

### Step 3: Run the optimization loop

Tell the user: "The optimization takes some time. I'll run the loop in the background and report its progress periodically."

Save the eval set to the workspace, then run the loop in the background:

```bash
python -m scripts.run_loop \
  --eval-set <path-to-trigger-eval.json> \
  --skill-path <path-to-skill> \
  --model <model-id-powering-this-session> \
  --max-iterations 5 \
  --verbose
```

Pass the model ID from your system prompt, the model that powers the current session, so that the triggering test matches what the user experiences.

While the loop runs, tail its output periodically, and tell the user which iteration it is on and what the scores are.

The `run_loop` script runs the whole optimization loop:

1. It splits the eval set into a 60% train set and a 40% held-out test set.
2. It evaluates the current description, and it runs each query 3 times to get a reliable trigger rate.
3. It calls Claude to propose improvements based on the queries that failed.
4. It evaluates each new description on both the train set and the test set. It repeats the proposal and the evaluation for up to 5 iterations.
5. It opens an HTML report in the browser with the results of each iteration.
6. It returns JSON with `best_description`. It selects that description by test score rather than train score to avoid overfitting.

### Triggering

Design the eval queries around how triggering works. Skills appear in Claude's `available_skills` list with their name and description, and Claude decides from the description whether to consult a skill. Claude consults skills only for tasks that it cannot easily handle on its own. Simple one-step queries such as "read this PDF" might not trigger a skill, even when the description matches perfectly. Claude handles such queries directly with basic tools. Complex, multi-step, or specialized queries reliably trigger skills when the description matches.

So write eval queries substantive enough that Claude benefits from consulting a skill. Simple queries such as "read file X" make poor test cases, because they do not trigger skills however good the description is.

### Step 4: Apply the result

Copy `best_description` from the JSON output into the frontmatter of the skill's SKILL.md. Show the user the description before and after the change, and report the scores.

---

### Package and Present (only if `present_files` tool is available)

Check whether you have the `present_files` tool. If you do not have it, skip this step. If you have it, package the skill and present the `.skill` file to the user:

```bash
python -m scripts.package_skill <path/to/skill-folder>
```

After packaging, direct the user to the resulting `.skill` file path so they can install it.

---

## Claude.ai-specific instructions

In Claude.ai, the core workflow stays the same: draft, test, review, improve, and repeat. But Claude.ai does not have subagents, so you adapt these parts of the workflow:

**Running test cases**: Without subagents, the test cases cannot run in parallel. For each test case, read the skill's SKILL.md, then follow its instructions to complete the test prompt yourself, one test case at a time. This method is less rigorous than independent subagents. You wrote the skill, and you run it with the full context of writing it. It still works as a sanity check, and the human review step compensates for the gap. Skip the baseline runs, and use the skill to complete each task as the prompt asks.

**Reviewing results**: When you cannot open a browser, skip the browser reviewer. You cannot open one when Claude.ai's VM has no display, or when you work on a remote server. Present the results in the conversation instead, with the prompt and the output of each test case. Ask for feedback inline: "How does this look? Anything you'd change?"

When the output is a file for the user to look at, such as a .docx or an .xlsx file, save it to the filesystem. Tell the user where it is, so that they can download and inspect it.

**Benchmarking**: Skip the quantitative benchmarking. It relies on baseline comparisons, which mean little without subagents. Focus on qualitative feedback from the user.

**The iteration loop**: Run the same loop, without the browser reviewer. Improve the skill, rerun the test cases, and ask for feedback. If you have a filesystem, you can still organize the results into iteration directories.

**Description optimization**: The Description Optimization section requires `claude -p` from the `claude` CLI, which is available only in Claude Code. Skip it on Claude.ai.

**Blind comparison**: The blind comparison requires subagents, so skip it.

**Packaging**: The `package_skill.py` script works anywhere with Python and a filesystem. On Claude.ai, you can run it and the user can download the resulting `.skill` file.

**Updating an existing skill**: The user might ask you to update an existing skill rather than create a new one. In that case, follow these rules:

- **Preserve the original name.** Note the skill's directory name and its `name` frontmatter field, and use both unchanged. For example, if the installed skill is `research-helper`, output `research-helper.skill`, not `research-helper-v2`.
- **Copy the skill to a writable location before you edit it.** The installed skill path might be read-only. Copy the skill to `/tmp/skill-name/`, edit it there, and package it from the copy.
- **When you package manually, stage the package in `/tmp/` first**, then copy it to the output directory. Direct writes might fail because of permissions.

---

## Cowork-Specific Instructions

In Cowork, keep these points in mind:

- You have subagents, so the main workflow works, including parallel test cases, baseline runs, and grading. If you hit severe problems with timeouts, you may run the test prompts in series rather than in parallel.
- You do not have a browser or a display. So when you generate the eval viewer, pass `--static <output_path>` to write a standalone HTML file instead of starting a server. Then give the user a link that opens the HTML file in their browser.
- In Cowork, Claude often skips the eval viewer after it runs the tests. So in Cowork and in Claude Code alike, generate the eval viewer with `generate_review.py` after every test run, and do not write your own HTML. Let the human review the examples before you revise the skill yourself. GENERATE THE EVAL VIEWER *BEFORE* you evaluate the outputs yourself, because the human should see them as soon as possible.
- Feedback reaches you another way. No server is running, so the viewer's "Submit All Reviews" button downloads `feedback.json` as a file. You can then read the file from the download location. You might have to request access to it first.
- Packaging works, because `package_skill.py` requires only Python and a filesystem.
- Description optimization with `run_loop.py` and `run_eval.py` works in Cowork, because it calls `claude -p` in a subprocess and does not need a browser. Save it until you finish the skill and the user agrees that the skill is ready.
- **Updating an existing skill**: The user might ask you to update an existing skill rather than create a new one. Follow the guidance under **Updating an existing skill** in the Claude.ai-specific instructions.

---

## Reference files

The `agents/` directory holds instructions for specialized subagents. Read the matching file before you spawn one of these subagents:

- `agents/grader.md`: how to evaluate assertions against outputs.
- `agents/comparator.md`: how to run a blind A/B comparison between two outputs.
- `agents/analyzer.md`: how to analyze why one version beat another.

The `references/` directory holds more documentation. `references/schemas.md` gives the JSON structures of the files that the workflow writes, such as `evals.json` and `grading.json`.

---

This summary repeats the core loop for emphasis:

1. Learn what the skill is for.
2. Draft or edit the skill.
3. Run Claude with access to the skill on the test prompts.
4. Evaluate the outputs with the user. Create `benchmark.json`, run `eval-viewer/generate_review.py` to help the user review the outputs, and run the quantitative evals.
5. Repeat until you and the user are satisfied.
6. Package the final skill and return it to the user.

If you have a TodoList, add these steps to it so that you do not forget one. In Cowork, add the item "Create evals JSON and run `eval-viewer/generate_review.py` so human can review test cases" to the TodoList, so that the step happens.

---
name: review-implementation
description: Review code and prose changes against the plan they implemented.
---

# Review Implementation: Code and Docs Review

Review all changes made during plan implementation against the plan itself.
Address any issues found.

## Phase 1: Identify Changes

The caller provides the plan file path and the base SHA (commit hash from
before implementation began). Run `git diff <base-sha>` to see the full diff
(committed and uncommitted) since implementation began.

## Phase 2: Launch Two Review Agents in Parallel

Use the Agent tool to launch both agents concurrently in a single message.
Both agents read the full diff; they split by concern, not by file. Pass each
agent the plan file path, the base SHA, and any specific concerns the user
mentioned.

### Agent 1: Code Review

Launch `implementation-reviewer-code` to review correctness across all
changed files: logic, completeness against the plan, deviations, tests,
simplicity, security, and behavioral correctness.

### Agent 2: Docs Review

Launch `implementation-reviewer-docs` to review prose quality across all
changed files: self-containment, timeless framing, unnecessary comments,
docs completeness against the plan, and compliance with project conventions.
Applied to in-code comments, docstrings, and standalone docs alike.

## Phase 3: Address Issues

Wait for both agents to complete. Aggregate their findings and fix each
issue directly. If a finding is a false positive or not worth addressing,
note it and move on -- do not argue with the finding, just skip it.

After fixing, re-run both agents in parallel. Continue until both return
`LGTM!`. When done, briefly summarize what was fixed.

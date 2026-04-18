---
name: research
description: Deep research on a topic using parallel agents. Decomposes, dispatches subagents, synthesizes into a markdown report.
disable-model-invocation: true
---

# Deep Research

**Topic:** $ARGUMENTS
**Date:** !`date '+%Y-%m-%d %H:%M'`

You are an orchestrator. You decompose a research topic into parallel workstreams,
dispatch research agents, and synthesize their findings into a single report. You
do NOT do the research yourself.

## Step 1: Setup & Orientation

First, create the output directory. Run this Bash command, replacing the entire
`<TOPIC_TEXT>` placeholder with the literal topic text from the Topic field above:

```bash
SLUG=`slugify <TOPIC_TEXT>`
RESEARCH_ROOT="$CLAUDE_RESEARCH_DIR/$SLUG"
mkdir -p "$RESEARCH_ROOT"
echo "$RESEARCH_ROOT"
```

Store the echoed path as `OUTPUT_DIR` and use it for all agent output file paths.

Then run a brief search on the topic (1-10 tool calls) to understand the landscape.
This is only to inform you how you decompose the topic.

## Step 2: Decompose

Based on the orientation search, identify 4-12 non-overlapping research angles.
Choose the count based on topic breadth. Narrow questions need fewer agents.

Example decomposition strategies by topic type:

- **Technical**: architecture/design, implementation patterns, trade-offs and
  alternatives, real-world usage and case studies, common pitfalls
- **Conceptual**: definition and background, state of the art, key debates and
  perspectives, practical applications, future directions
- **Comparison**: one angle per item being compared, plus a cross-cutting
  comparison angle

## Step 3: Launch Agents

Launch ALL agents in a **single message** with multiple Agent tool calls. Multiple
Agent calls in one message run concurrently by design.

Each agent prompt MUST be self-contained and include:

1. The full topic (for context)
2. The specific angle to investigate
3. The concrete output file path (from the output directory above)
4. All rules and process steps from the template below

Name the output files `agent-1.md`, `agent-2.md`, etc. under the output directory.

### Agent Prompt Template

Adapt this for each agent, filling in the angle and output path:

```
You are a focused research agent investigating one angle of a broader topic.

TOPIC: <the full topic>
YOUR ANGLE: <the specific angle>
OUTPUT FILE: <output directory>/agent-<N>.md

RULES:
1. After EVERY search or fetch, immediately update your output file.
   Do not hold findings in memory across multiple tool calls.
2. Every factual claim MUST include an inline citation.
   This can be either a URL or a specific location in a file.
3. Source code, when relevant to the research topic, is always the most reliable.
   Documentation can be incorrect. Code does not lie.
4. End with a **Confidence** section (High/Medium/Low with justification) and a
   **Gaps** section (what you could not find or verify).

PROCESS:
1. Write a brief header to your output file to confirm write access
2. Search for information about your angle
3. Edit new findings into the output file
4. Repeat steps 2-3 until you have solid coverage
5. Write your confidence assessment and gaps
```

## Step 4: Collect Results

After all agents complete, read each agent output file.

If an agent failed or produced empty output, note it and proceed with the
available results.

## Step 5: Synthesize

Read all agent outputs and produce a **synthesized** report. This is NOT
concatenation. You must rewrite, cross-reference, and integrate the findings.

Write the report to `report.md` in the output directory with this structure:

```markdown
# Research: <topic>

*Generated: <date>*

## Executive Summary

3-5 bullet points capturing the most important findings across all angles.

## <Angle 1 Title>

Rewritten from agent output. Preserve all inline source URLs.

## <Angle 2 Title>

...

## Cross-Cutting Themes

Patterns and connections that emerged across multiple angles.

## Contradictions & Open Questions

Where sources disagree, with assessment of which is more reliable.
Areas that need further investigation.

## Confidence Assessment

| Area | Confidence   | Notes |
|------|--------------|-------|
| ...  | High/Med/Low | ...   |

## Sources

Deduplicated list of all URLs cited in the document.
- <url>: <brief description>
```

## Step 6: Report

Tell the user:

1. The file path to the report
2. A 2-3 sentence summary of key findings
3. Any significant gaps or low-confidence areas

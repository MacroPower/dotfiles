INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
STOP_ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')

# Already continuing from a prior block -- clean up marker and allow through
if [ "$STOP_ACTIVE" = "true" ]; then
  MARKER="${TMPDIR:-/tmp}/claude-plan-active-${SESSION_ID}"
  rm -f "$MARKER"
  exit 0
fi

# Check for plan marker (written by block-exit-plan.sh on plan approval)
MARKER="${TMPDIR:-/tmp}/claude-plan-active-${SESSION_ID}"
if [ ! -f "$MARKER" ]; then
  # Not a plan-originated task -- allow through
  exit 0
fi

# Read marker: line 1 = plan path, line 2 = base SHA
PLAN_PATH=$(sed -n '1p' "$MARKER")
BASE_SHA=$(sed -n '2p' "$MARKER")

# Block and request review, passing plan path and base SHA
jq -n --arg plan "$PLAN_PATH" --arg base "$BASE_SHA" '{
  decision: "block",
  reason: (
    "Before finishing, run the implementation-reviewer agent to review"
    + " your code changes against the plan at " + $plan + "."
    + " The pre-implementation baseline commit is " + $base + "."
    + " Pass it both the plan file path and the base SHA."
    + " If your implementation deviated from the original plan,"
    + " explain your reasoning to the reviewer."
    + " If the reviewer finds issues, fix them and re-run the reviewer."
    + " Repeat until you get LGTM, then you may stop."
  )
}'

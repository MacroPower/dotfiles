INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
PLAN_PATH=$(echo "$INPUT" | jq -r '.tool_input.planFilePath // ""')

# No session ID or plan path -- allow through
if [ -z "$SESSION_ID" ] || [ -z "$PLAN_PATH" ]; then
  exit 0
fi

COUNTER="${TMPDIR:-/tmp}/claude-exit-plan-${SESSION_ID}"

if [ -d "$COUNTER" ]; then
  # Second call -- allow through, clean up
  rmdir "$COUNTER"
  exit 0
fi

# First call -- create counter (mkdir is atomic), deny with reminder
if mkdir "$COUNTER" 2>/dev/null; then
  jq -n --arg plan "$PLAN_PATH" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: (
        "Before exiting plan mode, consider running the"
        + " plan-reviewer agent to review the plan at "
        + $plan + ". Pass it the plan file path. After"
        + " review is complete and any feedback has been"
        + " addressed, call ExitPlanMode again."
      )
    }
  }'
fi

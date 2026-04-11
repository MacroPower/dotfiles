INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
if [ -n "$SESSION_ID" ]; then
  rm -f "${TMPDIR:-/tmp}/claude-plan-active-${SESSION_ID}"
  rmdir "${TMPDIR:-/tmp}/claude-exit-plan-${SESSION_ID}" 2>/dev/null || true
fi
exit 0

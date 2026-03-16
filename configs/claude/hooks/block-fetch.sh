INPUT=$(cat)
URL=$(echo "$INPUT" | jq -r '.tool_input.url // ""')
case "$URL" in
*raw.githubusercontent.com*)
  case "$URL" in
  *.md) ;;
  *)
    jq -n '{
          hookSpecificOutput: {
            hookEventName: "PreToolUse",
            permissionDecision: "deny",
            permissionDecisionReason: "Fetching code from raw.githubusercontent.com is blocked. Clone the repo to /tmp/git/<owner>/<repo> and read files locally instead."
          }
        }'
    exit 0
    ;;
  esac
  ;;
esac

#!/bin/bash
# Engram — Stop hook for Claude Code (async)
#
# Marks the session as ended via the HTTP API.
# Runs async so it doesn't block Claude's response.

INTUIT_ENGRAM_PORT="${INTUIT_ENGRAM_PORT:-7438}"
ENGRAM_URL="http://127.0.0.1:${INTUIT_ENGRAM_PORT}"

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')

if [ -z "$SESSION_ID" ]; then
  exit 0
fi

curl -sf "${ENGRAM_URL}/sessions/${SESSION_ID}/end" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{}' \
  > /dev/null 2>&1

exit 0


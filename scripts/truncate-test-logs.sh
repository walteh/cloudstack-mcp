#!/usr/bin/env bash

# 📝 This script truncates test logs to prevent overwhelming output
# Usage: ./truncate-test-logs.sh [max_lines] -- command [args...]
#
# Example: ./truncate-test-logs.sh 1000 -- go test ./...

set -euo pipefail

# Default to 1000 lines if not specified
MAX_LINES=${1:-1000}
shift # Remove max_lines argument

# Check for -- separator
if [ "$1" != "--" ]; then
	echo "Error: Missing -- separator after max_lines"
	exit 1
fi
shift # Remove -- separator

# Run the command and pipe through head
"$@" | {
	# Use head to limit output, but always show test summary at the end
	head -n "$MAX_LINES"

	# If there was more output, show a message
	if [ -n "$(cat)" ]; then
		echo "... [Output truncated after $MAX_LINES lines. Set MAX_LINES=all to see full output] ..."
	fi
}

# Preserve the exit code of the original command
exit ${PIPESTATUS[0]}

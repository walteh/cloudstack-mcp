#!/bin/bash
set -e

VM_NAME="test-vm"

echo "Looking for QEMU processes for VM ${VM_NAME}"

# Get processes that look like QEMU for our VM
PS_OUTPUT=$(ps aux | grep qemu | grep ${VM_NAME} | grep -v grep || echo "No matching processes found")

echo "PS Output:"
echo "$PS_OUTPUT"

# Try to extract the PID
if [[ "$PS_OUTPUT" =~ [0-9]+ ]]; then
    PID=$(echo "$PS_OUTPUT" | awk '{print $2}')
    echo "Found PID: $PID"

    # Check if the process responds to signals
    if kill -0 $PID 2>/dev/null; then
        echo "Process is running and responds to signals"
        echo "Updating VM state file..."
        STATE_FILE=~/.cloudstack-mcp/vms/${VM_NAME}/vm-state.json
        TMP_FILE=$(mktemp)
        cat $STATE_FILE | jq ".pid = $PID | .status = \"running\"" >$TMP_FILE
        mv $TMP_FILE $STATE_FILE
        echo "Updated state file"
    else
        echo "Process does not respond to signals"
    fi
else
    echo "No valid PID found"
fi

#!/bin/bash
set -e

VM_NAME="test-vm"
STATE_FILE=~/.cloudstack-mcp/vms/${VM_NAME}/vm-state.json

# Get the PID of the running QEMU process
PID=$(ps aux | grep qemu | grep ${VM_NAME} | grep -v grep | awk '{print $2}')

if [ -z "$PID" ]; then
    echo "No QEMU process found for VM $VM_NAME"
    exit 1
fi

echo "Found QEMU process for $VM_NAME with PID $PID"

# Update the state file with the correct PID and status
TMP_FILE=$(mktemp)
cat $STATE_FILE | jq ".pid = $PID | .status = \"running\"" >$TMP_FILE
mv $TMP_FILE $STATE_FILE

echo "Updated state file $STATE_FILE"
echo "New state:"
cat $STATE_FILE | jq

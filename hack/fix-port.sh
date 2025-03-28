#!/bin/bash
set -e

VM_NAME="test-vm"
STATE_FILE=~/.cloudstack-mcp/vms/${VM_NAME}/vm-state.json

echo "Updating SSH port for VM $VM_NAME"
cat $STATE_FILE | jq '.ssh_info.port = 10022' >/tmp/vm-state.json
mv /tmp/vm-state.json $STATE_FILE

echo "Updated state file:"
cat $STATE_FILE | jq '.ssh_info'

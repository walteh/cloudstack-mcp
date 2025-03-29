#!/bin/bash
set -e

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# Start the VM if it's not already running
echo "Checking VM status..."
./vmctl list-vms | grep test-vm | grep running || {
	echo "Starting VM test-vm..."
	./vmctl start-vm test-vm
	echo "Waiting 30 seconds for VM to boot..."
	sleep 30
}

# Execute a command on the VM
echo "Executing command on VM..."
./vmctl --debug exec test-vm "uname -a && whoami && hostname"

# Try opening a shell
echo "Press Enter to test interactive shell (press Ctrl+D to exit shell)"
read
echo "Opening shell to VM..."
./vmctl --debug shell test-vm

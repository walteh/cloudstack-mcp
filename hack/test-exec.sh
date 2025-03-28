#!/bin/bash
set -e

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# Execute a command on the VM
echo "Executing a command on the VM..."
./vmctl --debug exec test-vm "uname -a"

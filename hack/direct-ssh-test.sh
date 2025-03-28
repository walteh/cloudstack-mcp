#!/bin/bash
set -e

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# First check VM status
echo "Checking VM status..."
./vmctl list-vms

# Try direct SSH to verify connection
echo "Testing direct SSH connection..."
ssh -p 10022 ubuntu@localhost -o StrictHostKeyChecking=no "uname -a && whoami && hostname"

# Then try VM exec
echo "Testing VM exec with the Go SSH library..."
./vmctl exec test-vm "uname -a"

echo "All tests passed!"

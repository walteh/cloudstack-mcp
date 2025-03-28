#!/bin/bash
set -e

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# Delete existing VM if it exists
echo "Cleaning up any existing test-vm..."
./vmctl --debug delete-vm test-vm || true

# Create a new VM
echo "Creating a new test VM..."
./vmctl --debug create-vm test-vm noble-server-cloudimg-arm64.img

# Check the VM list
echo "Listing VMs..."
./vmctl --debug list-vms

# Wait for VM to boot and become accessible (30 seconds)
echo "Waiting 30 seconds for VM to boot..."
sleep 30

# Try to connect to the VM
echo "Trying to open a shell to the VM..."
./vmctl --debug shell test-vm

# Execute a command on the VM
echo "Executing a command on the VM..."
./vmctl --debug exec test-vm "uname -a"

# Stop the VM
echo "Stopping the VM..."
./vmctl --debug stop-vm test-vm

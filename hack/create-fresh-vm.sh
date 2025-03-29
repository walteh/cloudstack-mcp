#!/bin/bash
set -e

VM_NAME="fresh-vm"

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# Delete existing VM if it exists
echo "Cleaning up any existing $VM_NAME..."
./vmctl delete-vm $VM_NAME || true

# Create a new VM
echo "Creating a new VM..."
./vmctl create-vm $VM_NAME noble-server-cloudimg-arm64.img

# Start the VM
echo "Starting VM..."
./vmctl start-vm $VM_NAME

# Wait for VM to boot
echo "Waiting for VM to boot (30 seconds)..."
sleep 30

# Check VM status
echo "Checking VM status..."
./vmctl list-vms

# Run a command on the VM
echo "Running command on VM..."
./vmctl exec $VM_NAME "uname -a && hostname && whoami"

# Try SSH shell (optional - will be interactive)
echo "Press ENTER to try interactive SSH shell, or Ctrl+C to exit"
read
echo "Opening shell to VM..."
./vmctl shell $VM_NAME

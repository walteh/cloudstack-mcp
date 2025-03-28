#!/bin/bash
set -e

# Build the CLI tool
echo "Building vmctl..."
go build -o vmctl ./cmd/vmctl

# Delete existing VM if it exists
echo "Cleaning up any existing test-vm..."
./vmctl --debug delete-vm test-vm || true

# Create a new VM (without starting)
echo "Creating a new test VM..."
./vmctl --debug create-vm test-vm noble-server-cloudimg-arm64.img

# Run the VM in foreground mode
echo "Starting VM in foreground mode..."
echo "Press Ctrl+A, X to exit QEMU"
./vmctl --debug foreground-start-vm test-vm

#!/bin/bash
set -e

echo "Setting up tmux for VM management..."

# Check if tmux is installed
if ! command -v tmux &>/dev/null; then
    echo "tmux is not installed. Installing..."

    # Check the OS and install accordingly
    if [[ "$(uname)" == "Darwin" ]]; then
        # macOS
        if command -v brew &>/dev/null; then
            brew install tmux
        else
            echo "Homebrew is not installed. Please install Homebrew first:"
            echo "https://brew.sh/"
            exit 1
        fi
    elif [[ "$(uname)" == "Linux" ]]; then
        # Linux - try common package managers
        if command -v apt-get &>/dev/null; then
            sudo apt-get update
            sudo apt-get install -y tmux
        elif command -v dnf &>/dev/null; then
            sudo dnf install -y tmux
        elif command -v yum &>/dev/null; then
            sudo yum install -y tmux
        elif command -v pacman &>/dev/null; then
            sudo pacman -S --noconfirm tmux
        else
            echo "Could not determine package manager. Please install tmux manually."
            exit 1
        fi
    else
        echo "Unsupported operating system. Please install tmux manually."
        exit 1
    fi
fi

echo "Ensuring go-tmux dependency is installed..."
go get github.com/jubnzv/go-tmux

echo "Setup complete. You can now use tmux-based VM management with vmctl."
echo "Try out these commands:"
echo "  vmctl start-vm <name>     - Start a VM with tmux session"
echo "  vmctl attach <name>       - Attach to a VM's tmux session"
echo "  vmctl shell <name>        - Open a shell in a VM's tmux session"
echo "  vmctl exec <name> <cmd>   - Run a command in a VM's tmux session"

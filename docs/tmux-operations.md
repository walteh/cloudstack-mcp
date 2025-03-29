# Tmux Operations for VM Management

This document outlines the essential tmux operations needed for effective VM management using tmux. These operations are implemented through a single master session with a dedicated window for each VM.

## Core Operations

1. **Session Management**

    - Create a master "kvmctl" session that contains all VM windows
    - Ensure the session exists before performing operations
    - Provide attach/detach functionality

2. **Window Management**

    - Create a dedicated window for each VM
    - Track VM windows by their tmux window IDs
    - Select and focus specific VM windows
    - Close VM windows when they're no longer needed

3. **Command Execution**

    - Run commands in a VM's tmux window
    - Execute both system commands and VM-specific commands
    - Support for running SSH commands within a VM's window

4. **Session Navigation**

    - Switch between VM windows using tmux's window switching shortcuts
    - Attach to the master session to access all VMs
    - Provide simple ways to find and select specific VM windows

5. **Window Cleanup**
    - Close individual VM windows cleanly
    - Close all VM windows when needed
    - Shut down the entire master session

## Features

1. **Single Session Organization**

    - All VMs are contained within a single "kvmctl" session
    - Each VM has its own dedicated window with its name
    - Simple navigation between VMs using tmux window commands

2. **Visual Separation**

    - Each VM is isolated in its own window
    - Clear visual indicators of which VM you're currently controlling
    - VM name displayed in window title and header

3. **SSH Integration**

    - Connect to VMs via SSH within the VM's window
    - Transfer commands to VMs via SSH
    - Maintain SSH connections within the window context

4. **Multiple VM Management**

    - Start multiple VMs in separate windows
    - Track all active VM windows
    - Switch between VM windows easily

5. **Error Handling**
    - Graceful recovery from failures
    - Automatic cleanup of tracking data when windows are closed
    - Clear error messaging

## Implementation Requirements

The tmux integration provides these features through a simple, consistent API that:

1. Takes a context for cancellation and logging
2. Uses descriptive error messages with proper wrapping
3. Provides thread safety via mutex locks
4. Follows consistent naming conventions
5. Includes appropriate validation of inputs
6. Logs operations at appropriate levels

## Usage Example

```go
// Create the session manager
manager, err := tmux.NewSessionManager(logger)
if err != nil {
    return err
}

// Create a window for a VM
err = manager.CreateVMWindow(ctx, "myvm1")
if err != nil {
    return err
}

// Run commands in the VM's window
err = manager.RunCommand(ctx, "myvm1", "echo 'Hello from myvm1'")
if err != nil {
    return err
}

// Select (focus) the VM's window
err = manager.SelectVMWindow(ctx, "myvm1")
if err != nil {
    return err
}

// Attach to the session to interact with all VM windows
err = manager.AttachSession(ctx)
if err != nil {
    return err
}

// Clean up when done
err = manager.CloseVMWindow(ctx, "myvm1")
if err != nil {
    return err
}
```

## Key Benefits

1. **Organized Workspace**: All VMs are in a single session for easy management
2. **Visual Separation**: Each VM has its own isolated window
3. **Navigation**: Easy switching between VMs using built-in tmux shortcuts
4. **Persistence**: Sessions remain active even when detached
5. **Clean Interface**: Simple API for all VM-related tmux operations

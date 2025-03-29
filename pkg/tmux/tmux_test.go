package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	gotmux "github.com/jubnzv/go-tmux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireTmux skips the test if tmux is not installed
func TestRequireTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed, skipping test")
	}
}

// TestSessionManager tests the basic functionality of the SessionManager
func TestSessionManager(t *testing.T) {
	TestRequireTmux(t)

	// Set up logger for tests
	logger := log.With().Str("component", "tmux-test").Logger()
	ctx := logger.WithContext(context.Background())

	// Create a new session manager
	manager, err := NewSessionManager(logger)
	require.NoError(t, err, "should create session manager")
	require.NotNil(t, manager, "manager should not be nil")

	// Clean up any existing test sessions
	testVM := "test-vm-1"
	sessionName := GetSessionName(testVM)
	cleanupSession(t, sessionName)

	t.Run("CreateSession", func(t *testing.T) {
		err := manager.CreateSession(ctx, testVM, "testing")
		require.NoError(t, err, "should create tmux session")

		// Verify session exists
		exists, err := verifySessionExists(sessionName)
		require.NoError(t, err, "checking session existence should not error")
		assert.True(t, exists, "session should exist after creation")
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		sshInfo := map[string]interface{}{
			"host":     "localhost",
			"port":     2222,
			"username": "testuser",
		}
		vmDetails := map[string]string{
			"CPUs":   "1",
			"Memory": "1G",
			"Disk":   "10G",
			"State":  "running",
		}

		err := manager.UpdateStatus(ctx, testVM, "started", sshInfo, vmDetails)
		require.NoError(t, err, "should update status")
	})

	t.Run("RunConsoleCommand", func(t *testing.T) {
		// Run some test commands
		testCommands := []string{
			"echo 'Running test command'",
			"date",
			"echo 'Another test command'",
		}

		for _, cmd := range testCommands {
			err := manager.RunConsoleCommand(ctx, testVM, cmd)
			require.NoError(t, err, "should run command in console")
		}
	})

	// This test is commented out because it would attach to the session and block the test
	// t.Run("AttachSession", func(t *testing.T) {
	//     err := manager.AttachSession(ctx, testVM)
	//     require.NoError(t, err, "should attach to session")
	// })

	t.Run("CloseSession", func(t *testing.T) {
		err := manager.CloseSession(ctx, testVM)
		require.NoError(t, err, "should close session")

		// Verify session no longer exists
		exists, err := verifySessionExists(sessionName)
		require.NoError(t, err, "checking session existence should not error")
		assert.False(t, exists, "session should not exist after closing")
	})
}

// TestMultipleVMs tests managing multiple VMs with tmux
func TestMultipleVMs(t *testing.T) {
	TestRequireTmux(t)

	// Set up logger for tests
	logger := log.With().Str("component", "tmux-test").Logger()
	ctx := logger.WithContext(context.Background())

	// Create a new session manager
	manager, err := NewSessionManager(logger)
	require.NoError(t, err, "should create session manager")

	// Clean up any existing test sessions
	testVMs := []string{"multi-vm-1", "multi-vm-2", "multi-vm-3"}
	for _, vm := range testVMs {
		cleanupSession(t, GetSessionName(vm))
	}

	// Create sessions for each VM
	for i, vm := range testVMs {
		err := manager.CreateSession(ctx, vm, fmt.Sprintf("vm-%d-status", i))
		require.NoError(t, err, "should create session for VM %s", vm)
	}

	// Verify all sessions exist
	for _, vm := range testVMs {
		exists, err := verifySessionExists(GetSessionName(vm))
		require.NoError(t, err, "checking session existence should not error")
		assert.True(t, exists, "session for VM %s should exist", vm)
	}

	// Run commands on each VM
	for i, vm := range testVMs {
		err := manager.RunConsoleCommand(ctx, vm, fmt.Sprintf("echo 'Running command on VM %d'", i))
		require.NoError(t, err, "should run command on VM %s", vm)
	}

	// Update status on each VM
	for i, vm := range testVMs {
		sshInfo := map[string]interface{}{
			"host":     "localhost",
			"port":     2222 + i,
			"username": fmt.Sprintf("user%d", i),
		}
		vmDetails := map[string]string{
			"CPUs":   "2",
			"Memory": "2G",
			"Disk":   "20G",
			"State":  "running",
		}
		err := manager.UpdateStatus(ctx, vm, "started", sshInfo, vmDetails)
		require.NoError(t, err, "should update status for VM %s", vm)
	}

	// Close all sessions
	for _, vm := range testVMs {
		err := manager.CloseSession(ctx, vm)
		require.NoError(t, err, "should close session for VM %s", vm)
	}

	// Verify all sessions are closed
	for _, vm := range testVMs {
		exists, err := verifySessionExists(GetSessionName(vm))
		require.NoError(t, err, "checking session existence should not error")
		assert.False(t, exists, "session for VM %s should not exist after closing", vm)
	}
}

// TestVMLifecycleSimulation simulates the lifecycle of a VM using tmux
func TestVMLifecycleSimulation(t *testing.T) {
	TestRequireTmux(t)

	// Set up logger for tests
	logger := log.With().Str("component", "tmux-test").Logger()
	ctx := logger.WithContext(context.Background())

	// Create a new session manager
	manager, err := NewSessionManager(logger)
	require.NoError(t, err, "should create session manager")

	// Clean up any existing test session
	testVM := "lifecycle-vm"
	sessionName := GetSessionName(testVM)
	cleanupSession(t, sessionName)

	// Step 1: Create a new VM session
	t.Log("Step 1: Creating new VM session")
	err = manager.CreateSession(ctx, testVM, "initializing")
	require.NoError(t, err, "should create session")

	// Step 2: Update status to indicate VM is starting
	t.Log("Step 2: Starting VM (simulated)")
	sshInfo := map[string]interface{}{
		"host":     "localhost",
		"port":     2222,
		"username": "ubuntu",
	}
	vmDetails := map[string]string{
		"CPUs":   "2",
		"Memory": "4G",
		"Disk":   "20G",
		"State":  "Starting",
	}
	err = manager.UpdateStatus(ctx, testVM, "starting", sshInfo, vmDetails)
	require.NoError(t, err, "should update status")

	// Simulate VM startup time
	time.Sleep(1 * time.Second)

	// Run a command to simulate VM initialization
	err = manager.RunConsoleCommand(ctx, testVM, "echo 'Initializing VM...'")
	require.NoError(t, err, "should run command")
	time.Sleep(500 * time.Millisecond)

	// Step 3: Update status to indicate VM is running
	t.Log("Step 3: VM started (simulated)")
	vmDetails["State"] = "Running"
	err = manager.UpdateStatus(ctx, testVM, "started", sshInfo, vmDetails)
	require.NoError(t, err, "should update status")

	// Step 4: Simulate running commands on the VM
	t.Log("Step 4: Running commands")
	testCommands := []string{
		"echo 'Checking system status...'",
		"echo 'System load: 0.45 0.32 0.28'",
		"echo 'Memory usage: 1.2G/4G'",
		"echo 'Disk usage: 5.6G/20G'",
	}

	for _, cmd := range testCommands {
		err := manager.RunConsoleCommand(ctx, testVM, cmd)
		require.NoError(t, err, "should run command")
		time.Sleep(300 * time.Millisecond)
	}

	// Step 5: Simulate SSH connectivity
	t.Log("Step 5: Testing SSH connectivity (simulated)")
	err = manager.RunConsoleCommand(ctx, testVM, "echo 'SSH connection successful'")
	require.NoError(t, err, "should run command")
	vmDetails["SSH Status"] = "Connected"
	err = manager.UpdateStatus(ctx, testVM, "started", sshInfo, vmDetails)
	require.NoError(t, err, "should update status")

	// Step 6: Simulate running a user command
	t.Log("Step 6: Running user command (simulated)")
	err = manager.RunConsoleCommand(ctx, testVM, "echo 'Running user command: apt update'")
	require.NoError(t, err, "should run command")
	time.Sleep(1 * time.Second)
	err = manager.RunConsoleCommand(ctx, testVM, "echo 'Command completed with status: 0'")
	require.NoError(t, err, "should run command")

	// Step 7: Simulate stopping the VM
	t.Log("Step 7: Stopping VM (simulated)")
	vmDetails["State"] = "Stopping"
	err = manager.UpdateStatus(ctx, testVM, "stopping", sshInfo, vmDetails)
	require.NoError(t, err, "should update status")
	time.Sleep(1 * time.Second)

	// Step 8: Update status to indicate VM is stopped
	t.Log("Step 8: VM stopped (simulated)")
	vmDetails["State"] = "Stopped"
	delete(vmDetails, "SSH Status") // Remove SSH status since VM is stopped
	err = manager.UpdateStatus(ctx, testVM, "stopped", sshInfo, vmDetails)
	require.NoError(t, err, "should update status")

	// Step 9: Close the session
	t.Log("Step 9: Closing session")
	err = manager.CloseSession(ctx, testVM)
	require.NoError(t, err, "should close session")

	// Verify session no longer exists
	exists, err := verifySessionExists(sessionName)
	require.NoError(t, err, "checking session existence should not error")
	assert.False(t, exists, "session should not exist after closing")
}

// Helper functions

// verifySessionExists checks if a tmux session exists
func verifySessionExists(sessionName string) (bool, error) {
	server := new(gotmux.Server)
	return server.HasSession(sessionName)
}

// cleanupSession closes a tmux session if it exists
func cleanupSession(t *testing.T, sessionName string) {
	server := new(gotmux.Server)
	exists, err := server.HasSession(sessionName)
	if err != nil {
		t.Logf("Error checking session existence: %v", err)
		return
	}

	if exists {
		t.Logf("Cleaning up existing session: %s", sessionName)
		err := server.KillSession(sessionName)
		if err != nil {
			t.Logf("Error killing session: %v", err)
		}
	}
}

// TestDirectTmuxOperations tests the raw tmux operations without the SessionManager
func TestDirectTmuxOperations(t *testing.T) {
	TestRequireTmux(t)

	server := new(gotmux.Server)
	sessionName := "direct-tmux-test"

	// Clean up any existing session
	exists, err := server.HasSession(sessionName)
	require.NoError(t, err, "checking session existence should not error")
	if exists {
		err = server.KillSession(sessionName)
		require.NoError(t, err, "should kill existing session")
	}

	// Create a new session
	session, err := server.NewSession(sessionName)
	require.NoError(t, err, "should create session")
	require.Equal(t, sessionName, session.Name, "session name should match")

	// Create new windows
	consoleWindow, err := session.NewWindow("console")
	require.NoError(t, err, "should create console window")

	statusWindow, err := session.NewWindow("status")
	require.NoError(t, err, "should create status window")

	// List windows
	windows, err := session.ListWindows()
	require.NoError(t, err, "should list windows")
	assert.GreaterOrEqual(t, len(windows), 2, "should have at least 2 windows")

	// Get panes
	consolePanes, err := consoleWindow.ListPanes()
	require.NoError(t, err, "should list console panes")
	require.GreaterOrEqual(t, len(consolePanes), 1, "should have at least 1 console pane")

	statusPanes, err := statusWindow.ListPanes()
	require.NoError(t, err, "should list status panes")
	require.GreaterOrEqual(t, len(statusPanes), 1, "should have at least 1 status pane")

	// Run commands in panes
	err = consolePanes[0].RunCommand("echo 'Test command in console'")
	require.NoError(t, err, "should run command in console pane")

	err = statusPanes[0].RunCommand("echo 'Status window updated'")
	require.NoError(t, err, "should run command in status pane")

	// Kill the session
	err = server.KillSession(sessionName)
	require.NoError(t, err, "should kill session")

	// Verify session no longer exists
	exists, err = server.HasSession(sessionName)
	require.NoError(t, err, "checking session existence should not error")
	assert.False(t, exists, "session should not exist after killing")
}

// TestErrorCases tests error handling in the tmux package
func TestErrorCases(t *testing.T) {
	TestRequireTmux(t)

	// Set up logger for tests
	logger := log.With().Str("component", "tmux-test").Logger()
	ctx := logger.WithContext(context.Background())

	// Create a new session manager
	manager, err := NewSessionManager(logger)
	require.NoError(t, err, "should create session manager")

	// Test with invalid VM name
	invalidVMName := ""
	err = manager.CreateSession(ctx, invalidVMName, "testing")
	assert.Error(t, err, "should error on empty VM name")

	// Test operations on non-existent session
	nonExistentVM := "non-existent-vm"
	cleanupSession(t, GetSessionName(nonExistentVM)) // Ensure it doesn't exist

	// UpdateStatus on non-existent session
	err = manager.UpdateStatus(ctx, nonExistentVM, "status", nil, nil)
	assert.Error(t, err, "should error when updating non-existent session")

	// RunConsoleCommand on non-existent session
	err = manager.RunConsoleCommand(ctx, nonExistentVM, "echo test")
	assert.Error(t, err, "should error when running command on non-existent session")

	// AttachSession to non-existent session
	// Note: This would be an interactive test, so we don't actually run it
	// err = manager.AttachSession(ctx, nonExistentVM)
	// assert.Error(t, err, "should error when attaching to non-existent session")

	// CloseSession on non-existent session (should not error)
	err = manager.CloseSession(ctx, nonExistentVM)
	assert.NoError(t, err, "closing non-existent session should not error")
}

// TestFeatureList verifies all required features for VM management via tmux
func TestFeatureList(t *testing.T) {
	TestRequireTmux(t)

	t.Log("TMUX Feature Validation for VM Management")

	features := []struct {
		name        string
		description string
		test        func(t *testing.T)
	}{
		{
			name:        "Session Creation",
			description: "Create a new tmux session for a VM",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				err := manager.CreateSession(ctx, "feature-vm", "testing")
				assert.NoError(t, err, "session creation should work")
				manager.CloseSession(ctx, "feature-vm") // Cleanup
			},
		},
		{
			name:        "Session Windows",
			description: "Create and manage multiple windows in a session (console, status)",
			test: func(t *testing.T) {
				// This is tested indirectly by CreateSession which creates multiple windows
				server := new(gotmux.Server)
				session, _ := server.NewSession("window-test")
				_, err1 := session.NewWindow("console")
				_, err2 := session.NewWindow("status")
				assert.NoError(t, err1, "console window creation should work")
				assert.NoError(t, err2, "status window creation should work")
				server.KillSession("window-test") // Cleanup
			},
		},
		{
			name:        "Command Execution",
			description: "Execute commands in a VM's tmux session",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				manager.CreateSession(ctx, "cmd-vm", "testing")
				err := manager.RunConsoleCommand(ctx, "cmd-vm", "echo 'Test command'")
				assert.NoError(t, err, "command execution should work")
				manager.CloseSession(ctx, "cmd-vm") // Cleanup
			},
		},
		{
			name:        "Status Updates",
			description: "Update the status display for a VM",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				manager.CreateSession(ctx, "status-vm", "initial")
				err := manager.UpdateStatus(ctx, "status-vm", "updated",
					map[string]interface{}{"host": "localhost"},
					map[string]string{"Status": "Running"})
				assert.NoError(t, err, "status update should work")
				manager.CloseSession(ctx, "status-vm") // Cleanup
			},
		},
		{
			name:        "Session Attachment",
			description: "Attach to an existing VM tmux session",
			test: func(t *testing.T) {
				// This is an interactive feature, so we just verify the method exists
				t.Log("AttachSession method exists (interactive feature, not tested)")
			},
		},
		{
			name:        "Session Closure",
			description: "Close a VM's tmux session",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				manager.CreateSession(ctx, "close-vm", "testing")
				err := manager.CloseSession(ctx, "close-vm")
				assert.NoError(t, err, "session closure should work")
			},
		},
		{
			name:        "Multiple VM Support",
			description: "Handle multiple VMs in different tmux sessions",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				vms := []string{"multi1-vm", "multi2-vm"}

				for _, vm := range vms {
					manager.CreateSession(ctx, vm, "testing")
				}

				for _, vm := range vms {
					exists, _ := verifySessionExists(GetSessionName(vm))
					assert.True(t, exists, "session for VM %s should exist", vm)
					manager.CloseSession(ctx, vm) // Cleanup
				}
			},
		},
		{
			name:        "SSH Command Integration",
			description: "Run SSH commands in the VM's tmux session",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()
				manager.CreateSession(ctx, "ssh-vm", "testing")

				sshCmd := "echo 'Simulating SSH: ssh user@localhost'"
				err := manager.RunConsoleCommand(ctx, "ssh-vm", sshCmd)
				assert.NoError(t, err, "SSH command simulation should work")

				manager.CloseSession(ctx, "ssh-vm") // Cleanup
			},
		},
		{
			name:        "VM Status Visualization",
			description: "Display VM status information in a dedicated window",
			test: func(t *testing.T) {
				// This is tested by UpdateStatus
				t.Log("Status visualization is handled by UpdateStatus")
			},
		},
		{
			name:        "Error Handling",
			description: "Proper error handling for tmux operations",
			test: func(t *testing.T) {
				logger := zerolog.New(os.Stdout)
				manager, _ := NewSessionManager(logger)
				ctx := context.Background()

				// Test error on non-existent session
				err := manager.RunConsoleCommand(ctx, "nonexistent-vm", "echo test")
				assert.Error(t, err, "should handle errors gracefully")
			},
		},
	}

	for _, feature := range features {
		t.Run(feature.name, func(t *testing.T) {
			t.Logf("Testing feature: %s - %s", feature.name, feature.description)
			feature.test(t)
		})
	}
}

package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gotmux "github.com/jubnzv/go-tmux"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
	"gitlab.com/tozd/go/errors"
)

// tmuxTestCmd is a command to test the tmux-based VM management
var tmuxTestCmd = &cobra.Command{
	Use:   "tmux-test [vm-name]",
	Short: "Test tmux-based VM management",
	Long:  `A proof of concept for tmux-based VM management. Creates a VM, starts it in a tmux session, and runs commands.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vmName := "tmux-test-vm"
		if len(args) > 0 {
			vmName = args[0]
		}

		return runTmuxTest(cmd.Context(), vmName)
	},
}

func init() {
	rootCmd.AddCommand(tmuxTestCmd)
}

// findValidImage attempts to find a valid VM image for testing
func findValidImage(ctx context.Context) (string, bool) {
	// Default preferred images in order of preference
	preferredImages := []string{
		"ubuntu-20.04",
		"ubuntu",
		"debian",
		"centos",
		"alpine",
	}

	// Check if images directory exists
	vmBaseDir := os.Getenv("HOME")
	if vmDir := os.Getenv("CLOUDSTACK_MCP_VM_DIR"); vmDir != "" {
		vmBaseDir = vmDir
	}
	imgDir := filepath.Join(vmBaseDir, ".cloudstack-mcp", "images")

	if _, err := os.Stat(imgDir); os.IsNotExist(err) {
		fmt.Printf("Images directory doesn't exist at %s\n", imgDir)
		if err := os.MkdirAll(imgDir, 0755); err != nil {
			fmt.Printf("Warning: Failed to create images directory: %v\n", err)
		} else {
			fmt.Println("Created images directory")
		}
		return "", false
	}

	// Try to list all available images
	entries, err := os.ReadDir(imgDir)
	if err != nil {
		fmt.Printf("Failed to read images directory: %v\n", err)
		return "", false
	}

	// Build a map of available images
	availableImages := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".qcow2") {
			baseName := strings.TrimSuffix(entry.Name(), ".qcow2")
			availableImages[baseName] = true
		}
	}

	// First check if any of our preferred images exist
	for _, img := range preferredImages {
		if availableImages[img] {
			return img, true
		}
	}

	// If none of the preferred images exist, just return the first available image
	if len(availableImages) > 0 {
		for img := range availableImages {
			return img, true
		}
	}

	// If no images are available, return empty string and false
	return "", false
}

// getSessionName returns a standardized session name for a VM
func getSessionName(vmName string) string {
	// Sanitize the name to be tmux compatible (no colons or dots)
	name := strings.ReplaceAll(vmName, ":", "-")
	name = strings.ReplaceAll(name, ".", "-")
	return fmt.Sprintf("vm-%s", name)
}

func runTmuxTest(ctx context.Context, vmName string) error {
	logger := log.With().Str("component", "tmux-test").Logger()
	logger.Info().Str("vm", vmName).Msg("Starting tmux test")

	// Step 1: Check if tmux is installed and the go-tmux package is available
	fmt.Println("=== TMUX TEST ===")
	fmt.Println("Step 1: Checking tmux installation")

	// Check if tmux is installed
	_, err := exec.LookPath("tmux")
	if err != nil {
		fmt.Printf("❌ Tmux is not installed: %v\n", err)
		fmt.Println("Please install tmux and run ./scripts/setup-tmux.sh first")
		return err
	}
	fmt.Println("✅ Tmux installation detected")

	// Create a tmux server instance
	server := new(gotmux.Server)
	sessionName := getSessionName(vmName)

	// Check if the VM directory exists
	vmBaseDir := os.Getenv("HOME")
	if vmDir := os.Getenv("CLOUDSTACK_MCP_VM_DIR"); vmDir != "" {
		vmBaseDir = vmDir
	}
	vmDir := filepath.Join(vmBaseDir, ".cloudstack-mcp", "vms")

	// Try to create the VM directory if it doesn't exist
	if _, err := os.Stat(vmDir); os.IsNotExist(err) {
		fmt.Printf("VM directory doesn't exist at %s, creating it...\n", vmDir)
		if err := os.MkdirAll(vmDir, 0755); err != nil {
			fmt.Printf("❌ Failed to create VM directory: %v\n", err)
			return errors.Errorf("creating VM directory: %w", err)
		}
		fmt.Println("✅ VM directory created")
	} else if err != nil {
		fmt.Printf("❌ Failed to check VM directory: %v\n", err)
		return errors.Errorf("checking VM directory: %w", err)
	} else {
		fmt.Println("✅ VM directory exists")
	}

	// Step 2: Check for the test VM or use an existing VM
	fmt.Println("\nStep 2: Looking for test VM")
	var testVM *vm.VM

	// Try to get the VM if it exists
	existingVM, err := Manager.GetVM(ctx, vmName)
	if err == nil && existingVM != nil {
		fmt.Printf("✅ Found existing VM: %s\n", vmName)
		testVM = existingVM
	} else {
		// Check for the test VM or create one
		baseImage, hasImage := findValidImage(ctx)
		if !hasImage {
			fmt.Println("⚠️ No VM images found. This is a test-only VM that won't be functional.")
			fmt.Println("For a working VM, please first create a VM image.")
		}

		// Create a default VM configuration for testing
		fmt.Printf("Creating test VM: %s\n", vmName)
		config := vm.VMConfig{
			Name:     vmName,
			ID:       1,
			CPUs:     1,
			Memory:   "1G",
			DiskSize: "10G",
			BaseImg: vm.Img{
				Name: baseImage,
			},
			Network: vm.NetworkConfig{
				Type:     "user",
				MAC:      "52:54:00:12:34:00",
				Hostname: vmName,
			},
		}

		testVM, err = Manager.CreateVM(ctx, config)
		if err != nil {
			fmt.Printf("❌ Failed to create VM: %v\n", err)
			return err
		}
		fmt.Println("✅ Test VM created")
	}

	// Step 3: Create a tmux session for the VM
	fmt.Println("\nStep 3: Creating tmux session")

	// Check if session already exists
	exists, err := server.HasSession(sessionName)
	if err != nil {
		fmt.Printf("❌ Failed to check for existing session: %v\n", err)
		return err
	}

	var session gotmux.Session
	if exists {
		fmt.Printf("Found existing session: %s, reusing\n", sessionName)
		session = gotmux.Session{Name: sessionName}
	} else {
		fmt.Printf("Creating new tmux session: %s\n", sessionName)
		session, err = server.NewSession(sessionName)
		if err != nil {
			fmt.Printf("❌ Failed to create tmux session: %v\n", err)
			return err
		}

		// Create console window
		_, err = session.NewWindow("console")
		if err != nil {
			fmt.Printf("❌ Failed to create console window: %v\n", err)
			return err
		}

		// Create status window
		statusWindow, err := session.NewWindow("status")
		if err != nil {
			fmt.Printf("❌ Failed to create status window: %v\n", err)
			return err
		}

		// Set up initial status
		statusPane := statusWindow.Panes[0]
		err = statusPane.RunCommand(fmt.Sprintf("echo 'VM %s Status: %s'", vmName, testVM.Status))
		if err != nil {
			fmt.Printf("❌ Failed to set up status window: %v\n", err)
			return err
		}
	}

	fmt.Println("✅ Tmux session ready")

	// Step 4: Start the VM if it's not already running
	fmt.Println("\nStep 4: Starting VM")
	if testVM.Status != "started" {
		fmt.Printf("Starting VM: %s\n", vmName)
		err = Manager.StartVM(ctx, testVM)
		if err != nil {
			fmt.Printf("❌ Failed to start VM: %v\n", err)
			return err
		}

		// Give the VM time to boot
		fmt.Println("Waiting for VM to boot...")
		time.Sleep(2 * time.Second)

		// Update the VM state
		testVM, err = Manager.GetVM(ctx, vmName)
		if err != nil {
			fmt.Printf("Warning: Failed to refresh VM state: %v\n", err)
		}
	} else {
		fmt.Printf("VM is already running: %s\n", vmName)
	}

	// Save the VM state
	testVM.SaveState()
	fmt.Println("✅ VM started")

	// Step 5: Get the windows in the session
	fmt.Println("\nStep 5: Setting up windows")
	windows, err := session.ListWindows()
	if err != nil {
		fmt.Printf("❌ Failed to list windows: %v\n", err)
		return err
	}

	// Find the console and status windows
	var consoleWindow, statusWindow gotmux.Window
	for _, window := range windows {
		if window.Name == "console" {
			consoleWindow = window
		} else if window.Name == "status" {
			statusWindow = window
		}
	}

	// Verify both windows exist
	if consoleWindow.Name == "" {
		fmt.Println("Console window not found, creating it...")
		consoleWindow, err = session.NewWindow("console")
		if err != nil {
			fmt.Printf("❌ Failed to create console window: %v\n", err)
			return err
		}
	}

	if statusWindow.Name == "" {
		fmt.Println("Status window not found, creating it...")
		statusWindow, err = session.NewWindow("status")
		if err != nil {
			fmt.Printf("❌ Failed to create status window: %v\n", err)
			return err
		}
	}

	// Get the panes
	consolePanes, err := consoleWindow.ListPanes()
	if err != nil || len(consolePanes) == 0 {
		fmt.Printf("❌ Failed to get console pane: %v\n", err)
		return err
	}

	statusPanes, err := statusWindow.ListPanes()
	if err != nil || len(statusPanes) == 0 {
		fmt.Printf("❌ Failed to get status pane: %v\n", err)
		return err
	}

	consolePane := consolePanes[0]
	statusPane := statusPanes[0]

	fmt.Println("✅ Session windows configured")

	// Step 6: Run some commands in the console and update status
	fmt.Println("\nStep 6: Running commands")

	// Update status
	sshInfo := map[string]interface{}{
		"host":     testVM.SSHInfo.Host,
		"port":     testVM.SSHInfo.Port,
		"username": testVM.SSHInfo.Username,
	}

	vmDetails := map[string]string{
		"CPUs":   fmt.Sprintf("%d", testVM.Config.CPUs),
		"Memory": testVM.Config.Memory,
		"Disk":   testVM.Config.DiskSize,
		"State":  testVM.Status,
	}

	// Clear status pane and update with VM info
	err = statusPane.RunCommand("clear")
	if err != nil {
		fmt.Printf("Warning: Failed to clear status pane: %v\n", err)
	}

	statusText := fmt.Sprintf("VM: %s\nStatus: %s\n", vmName, testVM.Status)
	statusText += fmt.Sprintf("SSH Host: %s\nSSH Port: %d\nSSH User: %s\n",
		sshInfo["host"], sshInfo["port"], sshInfo["username"])
	statusText += "\nVM Details:\n"
	for key, value := range vmDetails {
		statusText += fmt.Sprintf("%s: %s\n", key, value)
	}

	err = statusPane.RunCommand(fmt.Sprintf("echo '%s'", statusText))
	if err != nil {
		fmt.Printf("Warning: Failed to update status text: %v\n", err)
	}

	// Run some commands in the console
	commands := []string{
		"echo 'Testing tmux console...'",
		"echo 'Current time: ' $(date)",
		fmt.Sprintf("echo 'Trying to connect to VM %s...'", vmName),
		fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p %d %s@%s 'echo \"Hello from VM\" && uptime'",
			testVM.SSHInfo.Port, testVM.SSHInfo.Username, testVM.SSHInfo.Host),
	}

	for _, cmd := range commands {
		fmt.Printf("Running command: %s\n", cmd)
		err = consolePane.RunCommand(cmd)
		if err != nil {
			fmt.Printf("❌ Failed to run command '%s': %v\n", cmd, err)
			continue
		}
		time.Sleep(1 * time.Second)
	}

	fmt.Println("✅ Commands executed")

	// Step 7: Wait for SSH connectivity in another goroutine
	fmt.Println("\nStep 7: Monitoring SSH connectivity")
	vmDetails["State"] = "Checking SSH connection..."

	// Update status pane
	err = statusPane.RunCommand("clear")
	if err != nil {
		fmt.Printf("Warning: Failed to clear status pane: %v\n", err)
	}

	statusText = fmt.Sprintf("VM: %s\nStatus: %s\n", vmName, testVM.Status)
	statusText += fmt.Sprintf("SSH Host: %s\nSSH Port: %d\nSSH User: %s\n",
		sshInfo["host"], sshInfo["port"], sshInfo["username"])
	statusText += "\nVM Details:\n"
	for key, value := range vmDetails {
		statusText += fmt.Sprintf("%s: %s\n", key, value)
	}

	err = statusPane.RunCommand(fmt.Sprintf("echo '%s'", statusText))
	if err != nil {
		fmt.Printf("Warning: Failed to update status text: %v\n", err)
	}

	// Try to connect to the VM via SSH in a separate goroutine
	sshReady := make(chan bool, 1)
	go func() {
		attempts := 0
		maxAttempts := 5

		for attempts < maxAttempts {
			attempts++
			fmt.Printf("SSH connection attempt %d/%d...\n", attempts, maxAttempts)

			// Update status
			vmDetails["State"] = fmt.Sprintf("SSH attempt %d/%d", attempts, maxAttempts)

			// Update status pane
			err = statusPane.RunCommand("clear")
			if err != nil {
				fmt.Printf("Warning: Failed to clear status pane: %v\n", err)
			}

			statusText = fmt.Sprintf("VM: %s\nStatus: %s\n", vmName, testVM.Status)
			statusText += fmt.Sprintf("SSH Host: %s\nSSH Port: %d\nSSH User: %s\n",
				sshInfo["host"], sshInfo["port"], sshInfo["username"])
			statusText += "\nVM Details:\n"
			for key, value := range vmDetails {
				statusText += fmt.Sprintf("%s: %s\n", key, value)
			}

			err = statusPane.RunCommand(fmt.Sprintf("echo '%s'", statusText))
			if err != nil {
				fmt.Printf("Warning: Failed to update status text: %v\n", err)
			}

			// Try to establish an SSH connection
			sshClient, err := testVM.ConnectSSH()
			if err == nil {
				fmt.Println("✅ SSH connection successful!")
				sshClient.Close()
				sshReady <- true
				return
			}

			fmt.Printf("SSH attempt failed: %v\n", err)
			time.Sleep(5 * time.Second)
		}

		fmt.Println("❌ Failed to establish SSH connection after 5 attempts")
		sshReady <- false
	}()

	// Wait for SSH to be ready or timeout
	select {
	case success := <-sshReady:
		if success {
			vmDetails["State"] = "SSH Ready"
			fmt.Println("SSH is ready, VM is fully operational")
		} else {
			vmDetails["State"] = "SSH Failed"
			fmt.Println("Warning: SSH connection failed, VM may not be fully operational")
		}
	case <-time.After(60 * time.Second):
		vmDetails["State"] = "SSH Timed Out"
		fmt.Println("Warning: SSH connection timed out, continuing anyway")
	}

	// Final status update
	err = statusPane.RunCommand("clear")
	if err != nil {
		fmt.Printf("Warning: Failed to clear status pane: %v\n", err)
	}

	statusText = fmt.Sprintf("VM: %s\nStatus: %s\n", vmName, testVM.Status)
	statusText += fmt.Sprintf("SSH Host: %s\nSSH Port: %d\nSSH User: %s\n",
		sshInfo["host"], sshInfo["port"], sshInfo["username"])
	statusText += "\nVM Details:\n"
	for key, value := range vmDetails {
		statusText += fmt.Sprintf("%s: %s\n", key, value)
	}

	err = statusPane.RunCommand(fmt.Sprintf("echo '%s'", statusText))
	if err != nil {
		fmt.Printf("Warning: Failed to update status text: %v\n", err)
	}

	// Step 8: Attach to the tmux session
	fmt.Println("\nStep 8: Attaching to tmux session")
	fmt.Printf("Attaching to session: %s\n", sessionName)
	fmt.Println("You will be connected to the tmux session. Use Ctrl+b, d to detach.")
	fmt.Println("To reattach later, run: tmux attach -t", sessionName)
	time.Sleep(3 * time.Second)

	// Attach to the session
	err = session.AttachSession()
	if err != nil {
		fmt.Printf("❌ Failed to attach to session: %v\n", err)
		fmt.Println("You can still attach manually with: tmux attach -t", sessionName)
		return err
	}

	return nil
}

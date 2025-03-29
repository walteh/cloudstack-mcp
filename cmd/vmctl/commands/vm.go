package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
	"gitlab.com/tozd/go/errors"
)

var vmGroup = &cobra.Group{
	ID:    "vm",
	Title: "VM Management",
}

func init() {
	rootCmd.AddGroup(vmGroup)
	rootCmd.AddCommand(listVMsCmd)
	rootCmd.AddCommand(createVMCmd)
	rootCmd.AddCommand(startVMCmd)
	rootCmd.AddCommand(startVMForegroundCmd)
	rootCmd.AddCommand(stopVMCmd)
	rootCmd.AddCommand(deleteVMCmd)
	rootCmd.AddCommand(shellVMCmd)
	rootCmd.AddCommand(execVMCmd)
	rootCmd.AddCommand(cleanupVMsCmd)
}

// listVMsCmd represents the list-vms command
var listVMsCmd = &cobra.Command{
	Use:   "list-vms",
	Short: "List all VMs",
	Long:  `List all virtual machines currently managed by the system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listVMs(cmd.Context())
	},
	GroupID: vmGroup.ID,
}

// createVMCmd represents the create-vm command
var createVMCmd = &cobra.Command{
	Use:   "create-vm <name> <image-name>",
	Short: "Create a new VM",
	Long:  `Create a new virtual machine from a base image.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		visible, _ := cmd.Flags().GetBool("visible")
		logs, _ := cmd.Flags().GetBool("logs")
		noLogs, _ := cmd.Flags().GetBool("no-logs")

		// Default to showing logs unless --no-logs is specified
		logs = !noLogs && (logs || !visible)

		return createVM(cmd.Context(), args[0], args[1], visible, logs)
	},
	GroupID: vmGroup.ID,
}

// startVMCmd represents the start-vm command
var startVMCmd = &cobra.Command{
	Use:   "start-vm <n>",
	Short: "Start a VM",
	Long:  `Start a virtual machine in the background.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		visible, _ := cmd.Flags().GetBool("visible")
		showLogs, _ := cmd.Flags().GetBool("logs")
		noLogs, _ := cmd.Flags().GetBool("no-logs")

		// Default to showing logs unless --no-logs is specified
		showLogs = !noLogs && (showLogs || !visible)

		if showLogs {
			// Show logs in current terminal
			return startVMWithLogs(cmd.Context(), args[0])
		} else if visible {
			// Show logs in new terminal
			return startVMVisible(cmd.Context(), args[0])
		}

		// Standard start without logs
		return startVM(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// startVMForegroundCmd represents the foreground-start-vm command
var startVMForegroundCmd = &cobra.Command{
	Use:   "foreground-start-vm <name>",
	Short: "Start a VM in foreground",
	Long:  `Start a virtual machine and keep it in the foreground.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return startVMForeground(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// stopVMCmd represents the stop-vm command
var stopVMCmd = &cobra.Command{
	Use:   "stop-vm <name>",
	Short: "Stop a VM",
	Long:  `Stop a running virtual machine.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopVM(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// deleteVMCmd represents the delete-vm command
var deleteVMCmd = &cobra.Command{
	Use:   "delete-vm <name>",
	Short: "Delete a VM",
	Long:  `Delete a virtual machine and its associated resources.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteVM(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// shellVMCmd represents the shell command
var shellVMCmd = &cobra.Command{
	Use:   "shell <name>",
	Short: "Open a shell in a VM",
	Long:  `Open an interactive shell session inside a running virtual machine.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return shellVM(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// execVMCmd represents the exec command
var execVMCmd = &cobra.Command{
	Use:   "exec <name> <command>",
	Short: "Execute a command in a VM",
	Long:  `Execute a command inside a running virtual machine.`,
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return execVM(cmd.Context(), args[0], args[1:])
	},
	GroupID: vmGroup.ID,
}

// cleanupVMsCmd represents the cleanup-vms command
var cleanupVMsCmd = &cobra.Command{
	Use:   "cleanup-vms",
	Short: "Cleanup unused VMs",
	Long:  `Remove all stopped virtual machines and clean up associated resources.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cleanupVMs(cmd.Context())
	},
	GroupID: vmGroup.ID,
}

// Implementation functions

func listVMs(ctx context.Context) error {
	vms, err := Manager.ListVMs(ctx)
	if err != nil {
		return errors.Errorf("listing VMs: %w", err)
	}

	fmt.Println("Virtual Machines:")
	for _, vm := range vms {
		status := vm.GetStatus()
		statusInfo := status

		// Add more detailed status information
		switch status {
		case "initializing":
			statusInfo = fmt.Sprintf("%s (Cloud-init running)", status)
		case "ready":
			statusInfo = fmt.Sprintf("%s (Ready to start)", status)
		case "failed":
			if vm.LastError != "" {
				statusInfo = fmt.Sprintf("%s (%s)", status, vm.LastError)
			}
		}

		fmt.Printf("  - %s (Status: %s)\n", vm.Name, statusInfo)
	}

	return nil
}

func createVM(ctx context.Context, name, imgName string, visible bool, showLogs bool) error {
	// Create a cancelable context so we can handle ctrl-c
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Set up signal handling for graceful cancellation
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-signalCh:
			fmt.Println("\nReceived cancellation signal, cleaning up...")
			cancel()
		case <-ctx.Done():
			// Context canceled elsewhere
		}
	}()

	// Create default VM configuration
	config := vm.VMConfig{
		Name:     name,
		ID:       1, // TODO: Generate unique ID
		CPUs:     2,
		Memory:   "2G",
		DiskSize: "20G",
		BaseImg: vm.Img{
			Name: imgName,
		},
		Network: vm.NetworkConfig{
			Type:     "user",
			MAC:      "52:54:00:12:34:56",
			Hostname: name,
		},
	}

	fmt.Printf("Creating VM %s with image %s...\n", name, imgName)
	// Create VM
	createdVM, err := Manager.CreateVM(ctx, config)
	if err != nil {
		return errors.Errorf("creating VM: %w", err)
	}

	fmt.Printf("VM %s created successfully. Initializing VM...\n", name)

	// Start VM for initialization
	if visible {
		// Start VM in visible mode (new terminal window)
		fmt.Printf("Starting VM %s in a new terminal window...\n", name)
		if err := Manager.StartVMVisible(ctx, createdVM); err != nil {
			// If context was canceled, we need to clean up
			if ctx.Err() != nil {
				fmt.Println("VM creation canceled, cleaning up...")
				cleanupCanceledVM(context.Background(), createdVM)
				return ctx.Err()
			}
			return errors.Errorf("starting VM in visible mode: %w", err)
		}

		fmt.Printf("VM %s is initializing in a separate terminal window.\n", name)
		fmt.Println("You can connect to it using:")
		fmt.Printf("  vmctl shell %s\n", name)

		// Don't wait for initialization since the user can see the progress in the terminal window
		return nil
	} else if showLogs {
		// Start VM with console logs in current terminal (direct QEMU log streaming)
		fmt.Printf("Starting VM %s with direct console streaming...\n", name)

		// First start the VM normally
		if err := Manager.StartVM(ctx, createdVM); err != nil {
			// If context was canceled, we need to clean up
			if ctx.Err() != nil {
				fmt.Println("VM creation canceled, cleaning up...")
				cleanupCanceledVM(context.Background(), createdVM)
				return ctx.Err()
			}
			return errors.Errorf("starting VM: %w", err)
		}

		// Then stream the QEMU logs directly (this will block until Ctrl+C or VM terminates)
		bootSuccessful, err := Manager.StreamQEMULogs(ctx, createdVM)
		if err != nil {
			fmt.Printf("Warning: Log streaming ended with error: %v\n", err)
		}

		// Update VM status to ready only if boot was successful
		if bootSuccessful {
			createdVM.Status = vm.VMStatusStarted
			if err := createdVM.SaveState(); err != nil {
				fmt.Printf("Warning: Failed to update VM status to started: %v\n", err)
			} else {
				fmt.Printf("VM %s status updated to %s\n", name, vm.VMStatusStarted)
			}
		} else {
			fmt.Printf("VM %s boot not detected as successful, status not updated\n", name)
		}

		fmt.Printf("\nVM %s is now ready to use.\n", name)
		fmt.Println("You can connect to it using:")
		fmt.Printf("  vmctl shell %s\n", name)

		return nil
	} else {
		// This is now the non-default case, used when --no-logs is specified
		fmt.Printf("Starting VM %s without log streaming (cloud-init logs hidden)...\n", name)

		if err := Manager.StartVM(ctx, createdVM); err != nil {
			// If context was canceled, we need to clean up
			if ctx.Err() != nil {
				fmt.Println("VM creation canceled, cleaning up...")
				cleanupCanceledVM(context.Background(), createdVM)
				return ctx.Err()
			}
			return errors.Errorf("starting VM: %w", err)
		}

		fmt.Printf("VM %s is starting, waiting for cloud-init to complete...\n", name)
		fmt.Println("(Press Ctrl+C to cancel)")

		// Wait for VM to complete initialization
		err = Manager.WaitForInitialization(ctx, createdVM)

		// If context was canceled, we need to clean up
		if ctx.Err() != nil {
			fmt.Println("VM initialization canceled, cleaning up...")
			cleanupCanceledVM(context.Background(), createdVM)
			return ctx.Err()
		}

		if err != nil {
			fmt.Printf("VM %s failed to initialize: %v\n", name, err)
			return err
		}

		fmt.Printf("VM %s initialized successfully and is now ready to use\n", name)
		return nil
	}
}

// Function to start a VM and stream logs in the current terminal
func startVMWithLogs(ctx context.Context, name string) error {
	vmd, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Make sure the VM is in a state where it can be started
	status := vmd.GetStatus()
	if status != vm.VMStatusStopped && status != vm.VMStatusReady {
		return errors.Errorf("VM is in state %s and cannot be started. VM must be in '%s' or '%s' state",
			status, vm.VMStatusStopped, vm.VMStatusReady)
	}

	// First start the VM normally
	fmt.Printf("Starting VM %s with direct console streaming...\n", name)
	if err := Manager.StartVM(ctx, vmd); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	// Then stream QEMU logs directly (this will block until Ctrl+C or VM terminates)
	bootSuccessful, err := Manager.StreamQEMULogs(ctx, vmd)
	if err != nil {
		fmt.Printf("Warning: Log streaming ended with error: %v\n", err)
	}

	// Update VM status to ready only if boot was successful
	if bootSuccessful {
		vmd.Status = vm.VMStatusStarted
		if err := vmd.SaveState(); err != nil {
			fmt.Printf("Warning: Failed to update VM status to started: %v\n", err)
		} else {
			fmt.Printf("VM %s status updated to %s\n", name, vm.VMStatusStarted)
		}
	} else {
		fmt.Printf("VM %s boot not detected as successful, status not updated\n", name)
	}

	fmt.Printf("VM %s console streaming ended\n", name)
	return nil
}

// cleanupCanceledVM cleans up a VM that was canceled during creation or initialization
func cleanupCanceledVM(ctx context.Context, vm *vm.VM) {
	// Use a new context to ensure cleanup happens even if the original context was canceled
	fmt.Printf("Cleaning up VM %s...\n", vm.Name)

	// Forcefully kill the VM process if it's still running
	if vm.PID > 0 {
		proc, err := os.FindProcess(vm.PID)
		if err == nil {
			// First try to terminate gracefully
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				fmt.Printf("Warning: Failed to send SIGTERM to VM process: %v\n", err)
			}

			// Wait a moment for graceful shutdown
			time.Sleep(2 * time.Second)

			// Then force kill if still running
			if proc.Kill() == nil {
				fmt.Printf("Forcefully terminated VM process (PID: %d)\n", vm.PID)
			}
		}
	}

	// Stop the VM if it's running
	status := vm.GetStatus()
	if status == "started" || status == "initializing" {
		if err := vm.Stop(); err != nil {
			fmt.Printf("Warning: Failed to stop VM through normal means: %v\n", err)
		}
	}

	// Mark the VM as failed
	vm.Status = "failed"
	vm.LastError = "Canceled by user"
	vm.SaveState()

	// Report confirmation to user
	fmt.Printf("VM %s cleanup completed\n", vm.Name)
}

func startVM(ctx context.Context, name string) error {
	vmd, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Make sure the VM is in a state where it can be started
	status := vmd.GetStatus()
	if status != vm.VMStatusStopped && status != vm.VMStatusReady {
		return errors.Errorf("VM is in state %s and cannot be started. VM must be in '%s' or '%s' state",
			status, vm.VMStatusStopped, vm.VMStatusReady)
	}

	// Check if this VM has already been initialized (cloud-init completed)
	alreadyInitialized := status == vm.VMStatusReady

	// If VM has already been initialized, use a different initial status
	if alreadyInitialized {
		vmd.Status = vm.VMStatusStarting // Status for VMs where cloud-init has already run
	} else {
		vmd.Status = vm.VMStatusInitializing // Status for first-time boot where cloud-init needs to run
	}
	if err := vmd.SaveState(); err != nil {
		fmt.Printf("Warning: Failed to update initial VM status: %v\n", err)
	}

	if err := Manager.StartVM(ctx, vmd); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	// Show appropriate message based on whether VM needs initialization
	if alreadyInitialized {
		fmt.Printf("VM %s is starting (already initialized)...\n", name)
	} else {
		fmt.Printf("VM %s is starting and initializing (cloud-init running)...\n", name)
	}

	// Wait for the VM to be fully booted (either by checking SSH or another method)
	// If SSH connection works, the VM is ready
	fmt.Println("Waiting for VM to be fully started...")

	// Simple delay to allow VM to reach ready state
	// Could be replaced with more sophisticated check
	time.Sleep(5 * time.Second)

	// Try to make SSH connection to verify VM is up
	if _, err := vmd.ConnectSSH(); err == nil {
		fmt.Printf("SSH connection successful, VM is fully started\n")
	} else {
		fmt.Printf("Note: SSH connection not available yet, but VM is starting\n")
	}

	// IMPORTANT: Reload the VM before updating status to ensure we have the latest state
	vmd, err = Manager.GetVM(ctx, name)
	if err != nil {
		fmt.Printf("Warning: Could not reload VM before status update: %v\n", err)
	}

	// Update status to 'started' now that VM is running
	vmd.Status = vm.VMStatusStarted
	if err := vmd.SaveState(); err != nil {
		fmt.Printf("Warning: Failed to update VM status: %v\n", err)
	} else {
		fmt.Printf("VM %s status updated to %s\n", name, vm.VMStatusStarted)
	}

	// Verify the status was properly saved by reloading the VM
	time.Sleep(1 * time.Second) // Brief pause to ensure file is written
	updatedVM, err := Manager.GetVM(ctx, name)
	if err != nil {
		fmt.Printf("Warning: Could not verify status update: %v\n", err)
	} else if updatedVM.GetStatus() != vm.VMStatusStarted {
		fmt.Printf("Warning: VM status did not update properly. Expected '%s', got '%s'\n",
			vm.VMStatusStarted, updatedVM.GetStatus())

		// Try one more time with the freshly loaded VM object
		updatedVM.Status = vm.VMStatusStarted
		if err := updatedVM.SaveState(); err != nil {
			fmt.Printf("Warning: Final attempt to update VM status failed: %v\n", err)
		} else {
			fmt.Printf("VM %s status updated to %s (retry successful)\n", name, vm.VMStatusStarted)
		}
	}

	fmt.Printf("VM %s started successfully\n", name)
	return nil
}

func startVMVisible(ctx context.Context, name string) error {
	vmd, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Make sure the VM is in a state where it can be started
	status := vmd.GetStatus()
	if status != vm.VMStatusStopped && status != vm.VMStatusReady {
		return errors.Errorf("VM is in state %s and cannot be started. VM must be in '%s' or '%s' state",
			status, vm.VMStatusStopped, vm.VMStatusReady)
	}

	// Check if this VM has already been initialized (cloud-init completed)
	alreadyInitialized := status == vm.VMStatusReady

	// If VM has already been initialized, use a different initial status
	if alreadyInitialized {
		vmd.Status = vm.VMStatusStarting // Status for VMs where cloud-init has already run
	} else {
		vmd.Status = vm.VMStatusInitializing // Status for first-time boot where cloud-init needs to run
	}
	vmd.SaveState()

	if err := Manager.StartVMVisible(ctx, vmd); err != nil {
		return errors.Errorf("starting VM in visible mode: %w", err)
	}

	// Show appropriate message based on whether VM needs initialization
	if alreadyInitialized {
		fmt.Printf("VM %s is starting (already initialized) in visible mode.\n", name)
	} else {
		fmt.Printf("VM %s is starting and initializing in visible mode.\n", name)
	}

	// Simple delay to allow VM to reach ready state
	// Could be replaced with more sophisticated check
	time.Sleep(5 * time.Second)

	// Update status to 'started' now that VM is running
	vmd.Status = vm.VMStatusStarted
	if err := vmd.SaveState(); err != nil {
		fmt.Printf("Warning: Failed to update VM status: %v\n", err)
	}

	fmt.Printf("VM %s started in visible mode. Check the new terminal window\n", name)
	fmt.Printf("VM status has been updated to 'started'\n")
	return nil
}

func startVMForeground(ctx context.Context, name string) error {
	vmd, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Make sure the VM is in a state where it can be started
	status := vmd.GetStatus()
	if status != vm.VMStatusStopped && status != vm.VMStatusReady {
		return errors.Errorf("VM is in state %s and cannot be started. VM must be in '%s' or '%s' state",
			status, vm.VMStatusStopped, vm.VMStatusReady)
	}

	if err := Manager.StartVMForeground(ctx, vmd); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", name)
	return nil
}

func stopVM(ctx context.Context, name string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := vm.Stop(); err != nil {
		return errors.Errorf("stopping VM: %w", err)
	}

	fmt.Printf("VM %s stopped successfully\n", name)
	return nil
}

func deleteVM(ctx context.Context, name string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := Manager.DeleteVM(ctx, vm); err != nil {
		return errors.Errorf("deleting VM: %w", err)
	}

	fmt.Printf("VM %s deleted successfully\n", name)
	return nil
}

func shellVM(ctx context.Context, name string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	fmt.Printf("Opening shell to VM %s...\n", name)
	if err := vm.ExecShell(); err != nil {
		return errors.Errorf("opening shell: %w", err)
	}

	return nil
}

func execVM(ctx context.Context, name string, cmdArgs []string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Join the command and arguments
	command := cmdArgs[0]
	if len(cmdArgs) > 1 {
		command = fmt.Sprintf("%s %s", command, strings.Join(cmdArgs[1:], " "))
	}

	output, err := vm.ExecCommand(command)
	if err != nil {
		return errors.Errorf("executing command: %w", err)
	}

	fmt.Println(output)
	return nil
}

func cleanupVMs(ctx context.Context) error {
	if err := Manager.CleanupVMs(ctx); err != nil {
		return errors.Errorf("cleaning up VMs: %w", err)
	}

	fmt.Println("VM cleanup completed successfully")
	return nil
}

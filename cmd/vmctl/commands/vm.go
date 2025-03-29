package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	rootCmd.AddCommand(stopVMCmd)
	rootCmd.AddCommand(deleteVMCmd)
	rootCmd.AddCommand(shellVMCmd)
	rootCmd.AddCommand(execVMCmd)
	rootCmd.AddCommand(cleanupVMsCmd)
	rootCmd.AddCommand(attachVMCmd)
	rootCmd.AddCommand(tmuxCleanupCmd)
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

		// Standard start without logs
		return startVM(cmd.Context(), args[0])
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

// attachVMCmd represents the attach command
var attachVMCmd = &cobra.Command{
	Use:   "attach [n]",
	Short: "Attach to the tmux session",
	Long:  `Attach to the master tmux session to manage VMs. If a VM name is provided, selects that VM's window.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return attachVM(cmd.Context(), "")
		}
		return attachVM(cmd.Context(), args[0])
	},
	GroupID: vmGroup.ID,
}

// tmuxCleanupCmd represents the tmux-cleanup command
var tmuxCleanupCmd = &cobra.Command{
	Use:   "tmux-cleanup",
	Short: "Clean up all tmux windows and sessions",
	Long:  `Shut down all VM tmux windows and the master tmux session.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cleanupTmux(cmd.Context())
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
	for _, vmd := range vms {
		isRunning, err := vm.VMIsRunning(ctx, Manager.TmuxManager, vmd)
		if err != nil {
			return errors.Errorf("checking if VM is running: %w", err)
		}
		statusInfo := "stopped"
		if isRunning {
			statusInfo = "running"
		}

		fmt.Printf("  - %s (Status: %s)\n", vmd.Name, statusInfo)
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

	fmt.Printf("VM %s created successfully. Initializing VM... at MAC %s\n", createdVM.Name, createdVM.Config.Network.MAC)

	return nil

	// // Start VM for initialization
	// if visible {
	// 	// Start VM in visible mode (new terminal window)
	// 	fmt.Printf("Starting VM %s in a new terminal window...\n", name)
	// 	if err := Manager.StartVMVisible(ctx, createdVM); err != nil {
	// 		// If context was canceled, we need to clean up
	// 		if ctx.Err() != nil {
	// 			fmt.Println("VM creation canceled, cleaning up...")
	// 			cleanupCanceledVM(context.Background(), createdVM)
	// 			return ctx.Err()
	// 		}
	// 		return errors.Errorf("starting VM in visible mode: %w", err)
	// 	}

	// 	fmt.Printf("VM %s is initializing in a separate terminal window.\n", name)
	// 	fmt.Println("You can connect to it using:")
	// 	fmt.Printf("  vmctl shell %s\n", name)

	// 	// Don't wait for initialization since the user can see the progress in the terminal window
	// 	return nil
	// } else if showLogs {
	// 	// Start VM with console logs in current terminal (direct QEMU log streaming)
	// 	fmt.Printf("Starting VM %s with direct console streaming...\n", name)

	// 	// First start the VM normally
	// 	if err := Manager.StartVM(ctx, createdVM); err != nil {
	// 		// If context was canceled, we need to clean up
	// 		if ctx.Err() != nil {
	// 			fmt.Println("VM creation canceled, cleaning up...")
	// 			cleanupCanceledVM(context.Background(), createdVM)
	// 			return ctx.Err()
	// 		}
	// 		return errors.Errorf("starting VM: %w", err)
	// 	}

	// 	// Then stream the QEMU logs directly (this will block until Ctrl+C or VM terminates)
	// 	bootSuccessful, err := Manager.StreamQEMULogs(ctx, createdVM)
	// 	if err != nil {
	// 		fmt.Printf("Warning: Log streaming ended with error: %v\n", err)
	// 	}

	// 	// Update VM status to ready only if boot was successful
	// 	if bootSuccessful {
	// 		createdVM.Status = "started"
	// 		if err := createdVM.SaveState(); err != nil {
	// 			fmt.Printf("Warning: Failed to update VM status to started: %v\n", err)
	// 		} else {
	// 			fmt.Printf("VM %s status updated to %s\n", name, "started")
	// 		}
	// 	} else {
	// 		fmt.Printf("VM %s boot not detected as successful, status not updated\n", name)
	// 	}

	// 	fmt.Printf("\nVM %s is now ready to use.\n", name)
	// 	fmt.Println("You can connect to it using:")
	// 	fmt.Printf("  vmctl shell %s\n", name)

	// 	return nil
	// } else {
	// 	// This is now the non-default case, used when --no-logs is specified
	// 	fmt.Printf("Starting VM %s without log streaming (cloud-init logs hidden)...\n", name)

	// 	if err := Manager.StartVM(ctx, createdVM); err != nil {
	// 		// If context was canceled, we need to clean up
	// 		if ctx.Err() != nil {
	// 			fmt.Println("VM creation canceled, cleaning up...")
	// 			cleanupCanceledVM(context.Background(), createdVM)
	// 			return ctx.Err()
	// 		}
	// 		return errors.Errorf("starting VM: %w", err)
	// 	}

	// 	fmt.Printf("VM %s is starting, waiting for cloud-init to complete...\n", name)
	// 	fmt.Println("(Press Ctrl+C to cancel)")

	// 	// Wait for VM to complete initialization
	// 	err = Manager.WaitForInitialization(ctx, createdVM)

	// 	// If context was canceled, we need to clean up
	// 	if ctx.Err() != nil {
	// 		fmt.Println("VM initialization canceled, cleaning up...")
	// 		cleanupCanceledVM(context.Background(), createdVM)
	// 		return ctx.Err()
	// 	}

	// 	if err != nil {
	// 		fmt.Printf("VM %s failed to initialize: %v\n", name, err)
	// 		return err
	// 	}

	// 	fmt.Printf("VM %s initialized successfully and is now ready to use\n", name)
	// 	return nil
	// }
}

// cleanupCanceledVM cleans up a VM that was canceled during creation or initialization
func cleanupCanceledVM(ctx context.Context, vm *vm.VM) {
	if err := vm.Stop(ctx, Manager.TmuxManager); err != nil {
		fmt.Printf("Warning: Failed to stop VM through normal means: %v\n", err)
	}
}

func startVM(ctx context.Context, name string) error {

	vmd, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := vmd.Start(ctx, Manager.TmuxManager); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	if err := vmd.Wait(ctx, Manager.TmuxManager); err != nil {
		return errors.Errorf("waiting for VM: %w", err)
	}

	return nil
}

func stopVM(ctx context.Context, name string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := vm.Stop(ctx, Manager.TmuxManager); err != nil {
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

	// Use the tmux-enabled ShellVM method
	return vm.ExecShell(ctx, Manager.TmuxManager)
}

func execVM(ctx context.Context, name string, args []string) error {
	vm, err := Manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	// Use the tmux-enabled ExecVM method
	output, err := vm.Exec(ctx, Manager.TmuxManager, args)
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

// Implementation of the attachVM function
func attachVM(ctx context.Context, name string) error {
	return errors.Errorf("not implemented")
	// // Check if the tmux manager is available
	// if Manager.TmuxManager == nil {
	// 	return errors.Errorf("tmux session management is not available")
	// }

	// // If no VM name is provided, just attach to the master session
	// if name == "" {
	// 	fmt.Println("Attaching to the master tmux session. Use Ctrl+B then D to detach.")

	// 	// Attach to the master session
	// 	err := Manager.TmuxManager.AttachSession(ctx)
	// 	if err != nil {
	// 		return errors.Errorf("attaching to master tmux session: %w", err)
	// 	}

	// 	return nil
	// }

	// // If a VM name is provided, make sure it exists and is running
	// vm, err := Manager.GetVM(ctx, name)
	// if err != nil {
	// 	return errors.Errorf("getting VM: %w", err)
	// }

	// // Check if the VM has a window in the master session
	// hasWindow, err := Manager.TmuxManager.HasVM(ctx, name)
	// if err != nil {
	// 	return errors.Errorf("checking for VM window: %w", err)
	// }

	// if !hasWindow {
	// 	// Create a window for the VM if it doesn't have one
	// 	err = Manager.TmuxManager.CreateVMWindow(ctx, name)
	// 	if err != nil {
	// 		return errors.Errorf("creating window for VM: %w", err)
	// 	}
	// }

	// // Select the VM's window
	// err = Manager.TmuxManager.SelectVMWindow(ctx, name)
	// if err != nil {
	// 	return errors.Errorf("selecting VM window: %w", err)
	// }

	// fmt.Printf("Attaching to VM %s in the master tmux session. Use Ctrl+B then D to detach.\n", name)

	// // Attach to the master session
	// err = Manager.TmuxManager.AttachSession(ctx)
	// if err != nil {
	// 	return errors.Errorf("attaching to master tmux session: %w", err)
	// }

	return nil
}

// cleanupTmux handles shutting down all tmux-related resources
func cleanupTmux(ctx context.Context) error {
	// Check if the tmux manager is available
	if Manager.TmuxManager == nil {
		return errors.Errorf("tmux session management is not available")
	}

	fmt.Println("Shutting down all tmux windows and the master session...")

	// First try to close all VM windows
	err := Manager.TmuxManager.CloseAllVMWindows(ctx)
	if err != nil {
		fmt.Printf("Warning: Error closing VM windows: %v\n", err)
	}

	// Then shut down the entire session
	err = Manager.TmuxManager.ShutdownSession(ctx)
	if err != nil {
		return errors.Errorf("shutting down tmux session: %w", err)
	}

	fmt.Println("Tmux cleanup completed successfully")
	return nil
}

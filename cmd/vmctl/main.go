package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
	"gitlab.com/tozd/go/errors"
)

func main() {
	// Set up command line flags
	var (
		debug bool
	)

	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.Parse()

	// Set up logger
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// Process command
	if err := run(ctx); err != nil {
		log.Fatal().Err(err).Msg("Error running command")
	}
}

func run(ctx context.Context) error {
	// Create VM manager
	manager, err := vm.NewLocalManager()
	if err != nil {
		return errors.Errorf("creating VM manager: %w", err)
	}

	// Parse command
	args := flag.Args()
	if len(args) == 0 {
		return errors.Errorf("no command specified")
	}

	command := args[0]
	subArgs := args[1:]

	// Process command
	switch command {
	case "help":
		return printHelp()
	case "list-images":
		return listImages(ctx, manager)
	case "download-image":
		if len(subArgs) < 2 {
			return errors.Errorf("usage: download-image <name> <url>")
		}
		return downloadImage(ctx, manager, subArgs[0], subArgs[1])
	case "create-vm":
		if len(subArgs) < 2 {
			return errors.Errorf("usage: create-vm <name> <image-name>")
		}
		return createVM(ctx, manager, subArgs[0], subArgs[1])
	case "create-template":
		if len(subArgs) < 3 {
			return errors.Errorf("usage: create-template <name> <description> <base-image> [packages...]")
		}
		return createTemplate(ctx, manager, subArgs[0], subArgs[1], subArgs[2], subArgs[3:])
	case "list-templates":
		return listTemplates(ctx, manager)
	case "delete-template":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: delete-template <name>")
		}
		return deleteTemplate(ctx, manager, subArgs[0])
	case "create-vm-from-template":
		if len(subArgs) < 2 {
			return errors.Errorf("usage: create-vm-from-template <vm-name> <template-name>")
		}
		return createVMFromTemplate(ctx, manager, subArgs[0], subArgs[1])
	case "cleanup-vms":
		return cleanupVMs(ctx, manager)
	case "start-vm":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: start-vm <name>")
		}
		return startVM(ctx, manager, subArgs[0])
	case "foreground-start-vm":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: foreground-start-vm <name>")
		}
		return startVMForeground(ctx, manager, subArgs[0])
	case "stop-vm":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: stop-vm <name>")
		}
		return stopVM(ctx, manager, subArgs[0])
	case "delete-vm":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: delete-vm <name>")
		}
		return deleteVM(ctx, manager, subArgs[0])
	case "list-vms":
		return listVMs(ctx, manager)
	case "shell":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: shell <name>")
		}
		return shellVM(ctx, manager, subArgs[0])
	case "exec":
		if len(subArgs) < 2 {
			return errors.Errorf("usage: exec <name> <command>")
		}
		return execVM(ctx, manager, subArgs[0], strings.Join(subArgs[1:], " "))
	default:
		return errors.Errorf("unknown command: %s", command)
	}
}

func listImages(ctx context.Context, manager *vm.LocalManager) error {
	images, err := vm.ListImages(ctx)
	if err != nil {
		return errors.Errorf("listing images: %w", err)
	}

	fmt.Println("Available Images:")
	for _, img := range images {
		fmt.Printf("  - %s\n", img.Name)
	}

	return nil
}

func downloadImage(ctx context.Context, manager *vm.LocalManager, name, url string) error {
	img := vm.Img{
		Name: name,
		Url:  url,
	}

	if err := vm.DownloadImage(ctx, img, false); err != nil {
		return errors.Errorf("downloading image: %w", err)
	}

	fmt.Printf("Image %s downloaded successfully\n", name)
	return nil
}

func createVM(ctx context.Context, manager *vm.LocalManager, name, imgName string) error {
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

	// Create VM
	createdVM, err := manager.CreateVM(ctx, config)
	if err != nil {
		return errors.Errorf("creating VM: %w", err)
	}

	fmt.Printf("VM %s created successfully\n", name)

	// Start VM automatically
	if err := manager.StartVM(ctx, createdVM); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", name)
	return nil
}

func startVM(ctx context.Context, manager *vm.LocalManager, name string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := manager.StartVM(ctx, vm); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", name)
	return nil
}

func startVMForeground(ctx context.Context, manager *vm.LocalManager, name string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := manager.StartVMForeground(ctx, vm); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", name)
	return nil
}

func stopVM(ctx context.Context, manager *vm.LocalManager, name string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := vm.Stop(); err != nil {
		return errors.Errorf("stopping VM: %w", err)
	}

	fmt.Printf("VM %s stopped successfully\n", name)
	return nil
}

func deleteVM(ctx context.Context, manager *vm.LocalManager, name string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	if err := manager.DeleteVM(ctx, vm); err != nil {
		return errors.Errorf("deleting VM: %w", err)
	}

	fmt.Printf("VM %s deleted successfully\n", name)
	return nil
}

func listVMs(ctx context.Context, manager *vm.LocalManager) error {
	vms, err := manager.ListVMs(ctx)
	if err != nil {
		return errors.Errorf("listing VMs: %w", err)
	}

	fmt.Println("Available VMs:")
	for _, vm := range vms {
		status := vm.GetStatus()
		fmt.Printf("  - %s (Status: %s)\n", vm.Name, status)
	}

	return nil
}

func shellVM(ctx context.Context, manager *vm.LocalManager, name string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	fmt.Printf("Opening shell to VM %s...\n", name)
	if err := vm.ExecShell(); err != nil {
		return errors.Errorf("opening shell: %w", err)
	}

	return nil
}

func execVM(ctx context.Context, manager *vm.LocalManager, name, command string) error {
	vm, err := manager.GetVM(ctx, name)
	if err != nil {
		return errors.Errorf("getting VM: %w", err)
	}

	fmt.Printf("Executing command on VM %s: %s\n", name, command)
	output, err := vm.ExecCommand(command)
	if err != nil {
		return errors.Errorf("executing command: %w", err)
	}

	fmt.Println("Output:")
	fmt.Println(output)
	return nil
}

// Template management commands

func createTemplate(ctx context.Context, manager *vm.LocalManager, name, description, baseImage string, packages []string) error {
	fmt.Printf("Creating template %s from base image %s...\n", name, baseImage)

	// Create setupScript to install KVM
	setupScript := `#!/bin/bash
echo "Setting up KVM environment..."
# Additional setup commands can be added here
echo "Setup completed!"
`

	// Create the template
	template, err := manager.CreateTemplate(ctx, name, description, baseImage, packages, setupScript)
	if err != nil {
		return errors.Errorf("creating template: %w", err)
	}

	fmt.Printf("Template %s created with status: %s\n", template.Name, template.Status)

	// Prepare the template (creating temporary VM, installing packages, etc.)
	fmt.Printf("Preparing template %s (this may take several minutes)...\n", name)
	if err := manager.PrepareTemplate(ctx, template); err != nil {
		return errors.Errorf("preparing template: %w", err)
	}

	fmt.Printf("Template %s successfully prepared and ready to use\n", name)
	return nil
}

func listTemplates(ctx context.Context, manager *vm.LocalManager) error {
	templates, err := manager.ListTemplates(ctx)
	if err != nil {
		return errors.Errorf("listing templates: %w", err)
	}

	fmt.Println("Available Templates:")
	for _, template := range templates {
		fmt.Printf("  - %s (%s)\n    Status: %s\n    Created: %s\n    Description: %s\n",
			template.Name,
			template.BasePath,
			template.Status,
			template.CreatedAt.Format("2006-01-02 15:04:05"),
			template.Description)
	}

	return nil
}

func deleteTemplate(ctx context.Context, manager *vm.LocalManager, name string) error {
	template, err := manager.GetTemplate(ctx, name)
	if err != nil {
		return errors.Errorf("getting template: %w", err)
	}

	if err := manager.DeleteTemplate(ctx, template); err != nil {
		return errors.Errorf("deleting template: %w", err)
	}

	fmt.Printf("Template %s deleted successfully\n", name)
	return nil
}

func createVMFromTemplate(ctx context.Context, manager *vm.LocalManager, vmName, templateName string) error {
	template, err := manager.GetTemplate(ctx, templateName)
	if err != nil {
		return errors.Errorf("getting template: %w", err)
	}

	if template.Status != "ready" {
		return errors.Errorf("template %s is not ready (status: %s)", templateName, template.Status)
	}

	// Generate user data for the VM
	userData := fmt.Sprintf(`#cloud-config
hostname: %s
`, vmName)

	// Create VM from template
	createdVM, err := manager.CreateVMFromTemplate(ctx, vmName, template, userData)
	if err != nil {
		return errors.Errorf("creating VM from template: %w", err)
	}

	fmt.Printf("VM %s created successfully from template %s\n", vmName, templateName)

	// Start VM automatically
	if err := manager.StartVM(ctx, createdVM); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", vmName)
	return nil
}

// Add cleanup function
func cleanupVMs(ctx context.Context, manager *vm.LocalManager) error {
	fmt.Println("Cleaning up all VMs - stopping processes and moving to vms-deleted directory...")

	if err := manager.CleanupVMs(ctx); err != nil {
		return errors.Errorf("cleaning up VMs: %w", err)
	}

	fmt.Println("VM cleanup completed successfully")
	return nil
}

// printHelp displays usage information for the vmctl command
func printHelp() error {
	helpText := `CloudStack MCP VM Controller

Usage: vmctl <command> [args...]

Commands:
  Image Management:
    list-images                                        - List available VM images
    download-image <name> <url>                        - Download a VM image

  VM Management:
    create-vm <name> <image-name>                      - Create and start a new VM
    start-vm <name>                                    - Start a VM
    foreground-start-vm <name>                         - Start a VM in foreground mode
    stop-vm <name>                                     - Stop a VM
    delete-vm <name>                                   - Delete a VM
    list-vms                                           - List all VMs
    shell <name>                                       - Open a shell to a VM
    exec <name> <command>                              - Execute a command on a VM
    cleanup-vms                                        - Stop all VMs and move to vms-deleted dir

  Template Management:
    create-template <name> <desc> <base-img> [pkgs...] - Create a VM template
    list-templates                                     - List available templates
    delete-template <name>                             - Delete a template
    create-vm-from-template <vm> <template>            - Create a VM from template

  Other:
    help                                               - Show this help message
`
	fmt.Print(helpText)
	return nil
}

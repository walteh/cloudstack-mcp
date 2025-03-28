package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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

	ctx := context.Background()
	ctx = log.With().Str("command", "vmctl").Logger().WithContext(ctx)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(ctx)
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
	case "start-vm":
		if len(subArgs) < 1 {
			return errors.Errorf("usage: start-vm <name>")
		}
		return startVM(ctx, manager, subArgs[0])
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

	if err := vm.DownloadUnknownImage(ctx, url, false); err != nil {
		return errors.Errorf("downloading image: %w", err)
	}

	fmt.Printf("Image %s downloaded successfully\n", name)
	return nil
}

func createVM(ctx context.Context, manager *vm.LocalManager, name, imgName string) error {

	logger := zerolog.Ctx(ctx)

	logger.Info().Str("name", imgName).Msg("Downloading image")
	// try to get the image
	if err := vm.DownloadUnknownImage(ctx, imgName, false); err != nil {
		return errors.Errorf("downloading image: %w", err)
	}

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

	// Create VM (cloud-init will be generated inside the CreateVM method)
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
		fmt.Printf("  - %s (State: %s)\n", vm.Config.Name, vm.Status)
	}

	return nil
}

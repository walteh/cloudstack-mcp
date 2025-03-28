package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack/agent"
	"github.com/walteh/cloudstack-mcp/pkg/host/qemu"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	logger.Info().Msg("CloudStack KVM Setup Tool")

	go func() {
		<-signalChan
		logger.Info().Msg("Received signal, initiating cleanup")
		cancel()
	}()
	// Parse command line flags
	workDir := flag.String("work-dir", filepath.Join(os.TempDir(), "cloudstack-kvm"), "Working directory for KVM setup")
	flag.Parse()

	// Ensure working directory exists
	if err := os.MkdirAll(*workDir, 0755); err != nil {
		logger.Fatal().Err(err).Str("workDir", *workDir).Msg("Failed to create working directory")
	}

	logger.Info().Str("workDir", *workDir).Msg("Working directory initialized")

	host := qemu.NewManager(*workDir, logger)

	setup := agent.NewSetup(*workDir, logger, host)
	// Create CloudStack agent
	cloudstackAgent := agent.NewAgent(*workDir, logger, setup)

	// Start the agent
	if err := cloudstackAgent.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start CloudStack agent")
	}

	hypervisorVM, hypervisorDisk, err := cloudstackAgent.NewHypervisorVM(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create hypervisor VM")
	}

	fmt.Printf("Hypervisor VM: %s\n", hypervisorVM.Name())
	fmt.Printf("Hypervisor Disk: %s\n", hypervisorDisk.Path())
	// fmt.Printf("Hypervisor IP: %s\n", hypervisorVM.IP)
	// fmt.Printf("Hypervisor Port: %d\n", hypervisorVM.Port)
	// fmt.Printf("Hypervisor User: %s\n", hypervisorVM.User)
	// fmt.Printf("Hypervisor Password: %s\n", hypervisorVM.Password)

	if err := cloudstackAgent.MonitorVMs(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Failed to monitor VMs")
	}

}

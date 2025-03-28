package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack/agent"
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

	setup := agent.NewSetup(*workDir, logger)
	// Create CloudStack agent
	cloudstackAgent := agent.NewAgent(*workDir, logger, setup)

	// Start the agent
	if err := cloudstackAgent.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start CloudStack agent")
	}

	if err := cloudstackAgent.MonitorVMs(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Failed to monitor VMs")
	}

}

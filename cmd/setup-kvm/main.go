package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/walteh/cloudstack-mcp/pkg/cloudstack/agent"
)

func main() {
	ctx := context.Background()
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	logger.Info().Msg("CloudStack KVM Setup Tool")

	// Parse command line flags
	workDir := flag.String("work-dir", filepath.Join(os.TempDir(), "cloudstack-kvm"), "Working directory for KVM setup")
	skipTemplateDownload := flag.Bool("skip-template", false, "Skip template download")
	flag.Parse()

	// Ensure working directory exists
	if err := os.MkdirAll(*workDir, 0755); err != nil {
		logger.Fatal().Err(err).Str("workDir", *workDir).Msg("Failed to create working directory")
	}

	logger.Info().Str("workDir", *workDir).Msg("Working directory initialized")

	// Create CloudStack setup manager
	setupMgr := agent.NewSetup(*workDir, logger)

	// Setup steps
	steps := []struct {
		name string
		fn   func(context.Context) error
		skip bool
	}{
		{"Initialize Environment", setupMgr.InitializeEnvironment, false},
		{"Download Templates", setupMgr.DownloadTemplates, *skipTemplateDownload},
		{"Generate CloudStack Agent Config", setupMgr.GenerateCloudStackAgentConfig, false},
		{"Setup NFS Server", setupMgr.SetupNFSServer, false},
		{"Create Management Server VM", setupMgr.CreateManagementServer, false},
	}

	// Execute each step
	for _, step := range steps {
		if step.skip {
			logger.Info().Str("step", step.name).Msg("Skipping step")
			continue
		}

		logger.Info().Str("step", step.name).Msg("Executing step")
		if err := step.fn(ctx); err != nil {
			logger.Fatal().Err(err).Str("step", step.name).Msg("Step failed")
		}
		logger.Info().Str("step", step.name).Msg("Step completed successfully")
	}

	// Display information about the VMs
	logger.Info().Msg("Displaying VM information")
	if err := setupMgr.DisplayVMInfo(ctx); err != nil {
		logger.Warn().Err(err).Msg("Failed to display VM information")
	}

	logger.Info().Msg("CloudStack KVM setup completed successfully")
	logger.Info().Str("workDir", *workDir).Msg("All configuration and files are in this directory")
	logger.Info().Msg("For more information on CloudStack management, visit: https://cloudstack.apache.org/docs/")
}

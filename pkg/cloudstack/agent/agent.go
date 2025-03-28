package agent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/qemu"
)

// Agent represents the CloudStack agent
type Agent struct {
	workDir   string
	logger    zerolog.Logger
	cpuSpeed  int64
	setup     *Setup
	vmMonitor sync.Once
}

// Template represents a CloudStack template
type Template struct {
	Name     string
	URL      string
	Checksum string
}

// DefaultTemplates returns the default system VM templates
func DefaultTemplates() []Template {
	return []Template{
		{
			Name:     "systemvm-4.18-arm64",
			URL:      "https://download.cloudstack.org/arm64/systemvmtemplate/4.18/systemvmtemplate-4.18.0-kvm-arm64.qcow2",
			Checksum: "sha256:12c0f747a9b374c64922eced6fcaee712c87d9fdbf27f4556c4b63467c73da3d",
		},
	}
}

// NewAgent creates a new CloudStack agent
func NewAgent(workDir string, logger zerolog.Logger, setup *Setup) *Agent {
	return &Agent{
		workDir:  workDir,
		logger:   logger,
		setup:    setup,
		cpuSpeed: detectCPUSpeed(),
	}
}

// detectCPUSpeed returns the CPU speed in MHz
func detectCPUSpeed() int64 {
	// On Apple Silicon, we need to handle this differently
	if runtime.GOARCH == "arm64" && runtime.GOOS == "darwin" {
		// Apple Silicon M1/M2 base frequencies are typically 3200MHz
		// This is a conservative estimate
		return 3200
	}

	// For other platforms, try to read from sysfs
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq")
		if err == nil {
			var freq int64
			if _, err := fmt.Sscanf(string(data), "%d", &freq); err == nil {
				// Convert from KHz to MHz
				return freq / 1000
			}
		}
	}

	// Default fallback - this should be adjusted based on your needs
	return 2000
}

// Start starts the CloudStack agent
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info().Msg("Starting CloudStack agent")

	a.logger.Info().
		Int64("cpuSpeed", a.cpuSpeed).
		Str("arch", runtime.GOARCH).
		Str("os", runtime.GOOS).
		Msg("Agent started successfully")

	// Initialize setup

	// Initialize environment
	if err := a.setup.InitializeEnvironment(ctx); err != nil {
		a.logger.Fatal().Err(err).Msg("Failed to initialize environment")
	}

	// Download templates
	if err := a.setup.DownloadTemplates(ctx); err != nil {
		a.logger.Fatal().Err(err).Msg("Failed to download templates")
	}

	// Generate CloudStack agent configuration
	if err := a.setup.GenerateCloudStackAgentConfig(ctx); err != nil {
		a.logger.Fatal().Err(err).Msg("Failed to generate agent configuration")
	}

	// Setup NFS server
	if err := a.setup.SetupNFSServer(ctx); err != nil {
		a.logger.Fatal().Err(err).Msg("Failed to setup NFS server")
	}

	// Create management server VM
	if err := a.setup.CreateManagementServer(ctx); err != nil {
		a.logger.Fatal().Err(err).Msg("Failed to create management server")
	}

	return nil
}

// monitorVMs monitors the status of VMs
func (a *Agent) MonitorVMs(ctx context.Context) error {
	a.logger.Info().Msg("Starting VM monitoring")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Info().Msg("Stopping VM monitoring")
			if ctx.Err() == context.Canceled {
				return nil
			}
			return ctx.Err()

		case <-ticker.C:
			vms, err := a.GetQEMUManager().ListRunningVMs()
			if err != nil {
				a.logger.Error().Err(err).Msg("Failed to list VMs")
				continue
			}

			if len(vms) == 0 {
				a.logger.Info().Msg("No VMs running")
				continue
			}

			for _, vm := range vms {
				status, err := a.GetQEMUManager().GetVMStatus(ctx, vm)
				if err != nil {
					a.logger.Error().Err(err).Str("vm", vm).Msg("Failed to get VM status")
					continue
				}

				a.logger.Debug().
					Str("vm", vm).
					Str("status", status).
					Msg("VM Status")
			}
		}
	}

}

// GetHostInfo returns information about the host
func (a *Agent) GetHostInfo() map[string]interface{} {
	return map[string]interface{}{
		"cpuSpeed": a.cpuSpeed,
		"cpuArch":  runtime.GOARCH,
		"os":       runtime.GOOS,
		"workDir":  a.workDir,
	}
}

func (a *Agent) GetQEMUManager() *qemu.Manager {
	return a.setup.GetQEMUManager()
}

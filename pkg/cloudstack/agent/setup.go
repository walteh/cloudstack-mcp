package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/qemu"
)

const (
	// Default CloudStack system VM template URL for ARM64
	DefaultSystemVMTemplateURL = "https://download.cloudstack.org/arm64/systemvmtemplate/4.18/systemvmtemplate-4.18.0-arm64.qcow2"

	// Default size for CloudStack management server disk
	DefaultManagementDiskSizeGB = 20

	// Default memory for CloudStack management server
	DefaultManagementMemoryMB = 4096

	// Default CPU count for CloudStack management server
	DefaultManagementCPU = 4
)

// Setup contains all CloudStack setup configuration
type Setup struct {
	workDir    string
	logger     zerolog.Logger
	qemuMgr    *qemu.Manager
	configPath string
}

// NewSetup creates a new CloudStack setup instance
func NewSetup(workDir string, logger zerolog.Logger) *Setup {
	configPath := filepath.Join(workDir, "config")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		logger.Fatal().Err(err).Str("configPath", configPath).Msg("Failed to create config directory")
	}

	qemuMgr := qemu.NewManager(workDir, logger)

	return &Setup{
		workDir:    workDir,
		logger:     logger,
		qemuMgr:    qemuMgr,
		configPath: configPath,
	}
}

// InitializeEnvironment prepares the environment for CloudStack
func (s *Setup) InitializeEnvironment(ctx context.Context) error {
	s.logger.Info().Msg("Initializing CloudStack environment")

	// Check if QEMU is installed
	if err := s.qemuMgr.CheckQEMUInstalled(ctx); err != nil {
		return fmt.Errorf("QEMU check failed: %w", err)
	}

	// Create necessary directories
	for _, dir := range []string{"templates", "disks", "storage"} {
		path := filepath.Join(s.workDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	s.logger.Info().Msg("Environment initialized")
	return nil
}

// DownloadTemplates downloads required CloudStack templates
func (s *Setup) DownloadTemplates(ctx context.Context) error {
	s.logger.Info().Msg("Downloading CloudStack templates")

	templatePath := filepath.Join(s.workDir, "templates", "systemvm.qcow2")

	// Skip if template already exists
	if _, err := os.Stat(templatePath); err == nil {
		s.logger.Info().Str("path", templatePath).Msg("Template already exists, skipping download")
		return nil
	}

	// Download the system VM template
	if err := s.qemuMgr.DownloadCloudStackTemplate(ctx, DefaultSystemVMTemplateURL, templatePath); err != nil {
		return fmt.Errorf("failed to download system VM template: %w", err)
	}

	s.logger.Info().Msg("Templates downloaded")
	return nil
}

// CreateManagementServer creates a VM for CloudStack Management Server
func (s *Setup) CreateManagementServer(ctx context.Context) error {
	s.logger.Info().Msg("Creating CloudStack Management Server VM")

	vmName := "cloudstack-management"
	diskPath := filepath.Join(s.workDir, "disks", "management.qcow2")

	// Create disk if it doesn't exist
	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		if err := s.qemuMgr.CreateDisk(ctx, diskPath, DefaultManagementDiskSizeGB); err != nil {
			return fmt.Errorf("failed to create management server disk: %w", err)
		}
	}

	// Create VM configuration
	config := qemu.NewVMConfig(vmName, diskPath)
	config.CPU = DefaultManagementCPU
	config.MemoryMB = DefaultManagementMemoryMB
	config.KVM = true

	// For CloudStack management, we want a graphical console
	config.Headless = false

	// Start the VM
	if err := s.qemuMgr.CreateVMWithConfig(ctx, config); err != nil {
		return fmt.Errorf("failed to create management server VM: %w", err)
	}

	s.logger.Info().Msg("CloudStack Management Server VM created")
	return nil
}

// GenerateCloudStackAgentConfig generates CloudStack agent configuration for KVM
func (s *Setup) GenerateCloudStackAgentConfig(ctx context.Context) error {
	s.logger.Info().Msg("Generating CloudStack agent configuration")

	agentConfig := `
# CloudStack Agent Configuration
agent.storage.template.cleanup=true
agent.vm.prefix=s-
host=localhost
cluster=Cluster1
pod=Pod1
zone=Zone1
# Fixed CPU speed to work around detection issue on Apple Silicon
host.cpu.speed=2400
# Storage configuration
primary.storage.url=nfs://{{.LocalIP}}:/export/primary
primary.storage.path=/export/primary
secondary.storage.url=nfs://{{.LocalIP}}:/export/secondary
secondary.storage.path=/export/secondary
`

	// Get local IP address for NFS configuration
	localIP, err := getLocalIP()
	if err != nil {
		return fmt.Errorf("failed to get local IP: %w", err)
	}

	// Parse template
	tmpl, err := template.New("agent").Parse(agentConfig)
	if err != nil {
		return fmt.Errorf("failed to parse agent config template: %w", err)
	}

	// Create config file
	configFile := filepath.Join(s.configPath, "agent.properties")
	file, err := os.Create(configFile)
	if err != nil {
		return fmt.Errorf("failed to create agent config file: %w", err)
	}
	defer file.Close()

	// Execute template
	if err := tmpl.Execute(file, map[string]string{"LocalIP": localIP}); err != nil {
		return fmt.Errorf("failed to write agent config: %w", err)
	}

	s.logger.Info().Str("path", configFile).Msg("CloudStack agent configuration generated")
	return nil
}

// SetupNFSServer sets up an NFS server for CloudStack storage
func (s *Setup) SetupNFSServer(ctx context.Context) error {
	s.logger.Info().Msg("Setting up NFS server for CloudStack storage")

	// Create NFS export directories
	for _, dir := range []string{"primary", "secondary"} {
		path := filepath.Join(s.workDir, "storage", dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create NFS export directory %s: %w", dir, err)
		}
	}

	// On macOS, we'll use the built-in NFS server
	// Generate exports file
	exportsFile := filepath.Join(s.configPath, "nfs.exports")
	content := fmt.Sprintf(`
%s/storage/primary -network 192.168.0.0/16 -mask 255.255.0.0 -maproot=root
%s/storage/secondary -network 192.168.0.0/16 -mask 255.255.0.0 -maproot=root
`, s.workDir, s.workDir)

	if err := os.WriteFile(exportsFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write exports file: %w", err)
	}

	s.logger.Info().Msg("NFS configuration generated")
	s.logger.Info().Msg("NOTE: You'll need to manually configure your NFS server with this configuration")
	s.logger.Info().Str("exportsFile", exportsFile).Msg("See this file for NFS export configuration")

	return nil
}

// getLocalIP gets the local IP address
func getLocalIP() (string, error) {
	cmd := exec.Command("ipconfig", "getifaddr", "en0")
	output, err := cmd.Output()
	if err != nil {
		return "127.0.0.1", nil // Fallback to localhost
	}

	return string(output), nil
}

// DisplayVMInfo displays information about the running VMs
func (s *Setup) DisplayVMInfo(ctx context.Context) error {
	s.logger.Info().Msg("CloudStack VM Information")

	// List running VMs
	vms, err := s.qemuMgr.ListRunningVMs()
	if err != nil {
		return fmt.Errorf("failed to list running VMs: %w", err)
	}

	if len(vms) == 0 {
		s.logger.Info().Msg("No VMs are currently running")
		return nil
	}

	s.logger.Info().Int("count", len(vms)).Msg("Running VMs")

	for _, vm := range vms {
		status, err := s.qemuMgr.GetVMStatus(ctx, vm)
		if err != nil {
			s.logger.Warn().Err(err).Str("vm", vm).Msg("Failed to get VM status")
			continue
		}

		// Get VM info
		info, err := s.qemuMgr.GetVMInfo(ctx, vm)
		if err != nil {
			s.logger.Warn().Err(err).Str("vm", vm).Msg("Failed to get VM info")
			continue
		}

		s.logger.Info().
			Str("name", vm).
			Str("status", status).
			Int("cpus", info.CPUs).
			Int("memory_mb", info.MemoryMB).
			Str("vnc_port", info.VNCPort).
			Str("qmp_port", info.QMPPort).
			Msg("VM Details")

		// Display connection information
		s.logger.Info().
			Str("vm", vm).
			Msgf("VNC connection available at: localhost:%s", info.VNCPort)
		s.logger.Info().
			Str("vm", vm).
			Msgf("QMP connection available at: localhost:%s", info.QMPPort)
	}

	// Display working directory information
	s.logger.Info().
		Str("workDir", s.workDir).
		Msg("VM files and configurations are located in this directory")

	return nil
}

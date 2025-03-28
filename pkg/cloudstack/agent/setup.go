package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/host"

	"gitlab.com/tozd/go/errors"
)

const (
	// Default CloudStack system VM template URL for ARM64
	DefaultSystemVMTemplateURL = "https://download.cloudstack.org/arm64/systemvmtemplate/4.18/systemvmtemplate-4.18.0-kvm-arm64.qcow2"
	DefaultSystemVMChecksum    = "sha256:12c0f747a9b374c64922eced6fcaee712c87d9fdbf27f4556c4b63467c73da3d"

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
	host       host.Host
	configPath string
}

// NewSetup creates a new CloudStack setup instance
func NewSetup(workDir string, logger zerolog.Logger, hypervisor host.Host) *Setup {
	configPath := filepath.Join(workDir, "config")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		logger.Fatal().Err(err).Str("configPath", configPath).Msg("Failed to create config directory")
	}

	return &Setup{
		workDir:    workDir,
		logger:     logger,
		host:       hypervisor,
		configPath: configPath,
	}
}

// getTemplateCachePath returns the path where a template should be stored in cache
func (s *Setup) getTemplateCachePath(name string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Errorf("getting user cache directory: %w", err)
	}

	// Create a cache directory specific to our application
	templateCacheDir := filepath.Join(cacheDir, "cloudstack-kvm", "templates")
	if err := os.MkdirAll(templateCacheDir, 0755); err != nil {
		return "", errors.Errorf("creating template cache directory: %w", err)
	}

	return filepath.Join(templateCacheDir, name+".qcow2"), nil
}

// InitializeEnvironment prepares the environment for CloudStack
func (s *Setup) InitializeEnvironment(ctx context.Context) error {
	s.logger.Info().Msg("Initializing CloudStack environment")

	// Check if QEMU is installed
	if err := s.host.InstallDependencies(ctx); err != nil {
		return errors.Errorf("QEMU check failed: %w", err)
	}

	// Create necessary directories
	for _, dir := range []string{"templates", "disks", "storage"} {
		path := filepath.Join(s.workDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return errors.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	s.logger.Info().Msg("Environment initialized")
	return nil
}

// verifyChecksum verifies the SHA256 checksum of a file
func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.Errorf("opening file: %w", err)
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return errors.Errorf("reading file: %w", err)
	}

	computed := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if computed != expected {
		return errors.Errorf("checksum mismatch")
	}

	return nil
}

// DownloadTemplates downloads required CloudStack templates
func (s *Setup) DownloadTemplates(ctx context.Context) error {
	s.logger.Info().Msg("Downloading CloudStack templates")

	// Get the cache path for the template
	templateName := "systemvm-4.18-arm64"
	templateCachePath, err := s.getTemplateCachePath(templateName)
	if err != nil {
		return errors.Errorf("getting template cache path: %w", err)
	}

	// Check if template exists in cache and verify checksum
	if _, err := os.Stat(templateCachePath); err == nil {
		if err := verifyChecksum(templateCachePath, DefaultSystemVMChecksum); err == nil {
			s.logger.Info().
				Str("template", templateName).
				Str("path", templateCachePath).
				Msg("Template found in cache with valid checksum")

			// Create symlink in workdir
			workdirPath := filepath.Join(s.workDir, "templates", templateName+".qcow2")
			if err := os.MkdirAll(filepath.Dir(workdirPath), 0755); err != nil {
				return errors.Errorf("creating template directory in workdir: %w", err)
			}

			// Remove existing symlink or file
			_ = os.Remove(workdirPath)

			// Create relative symlink
			if err := os.Symlink(templateCachePath, workdirPath); err != nil {
				return errors.Errorf("creating symlink to cached template: %w", err)
			}

			return nil
		}
		// Checksum failed, remove the cached file
		os.Remove(templateCachePath)
	}

	s.logger.Info().
		Str("template", templateName).
		Str("url", DefaultSystemVMTemplateURL).
		Msg("Downloading template")

	// Create a temporary file for downloading
	tmpPath := templateCachePath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return errors.Errorf("creating temporary file: %w", err)
	}
	defer out.Close()

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", DefaultSystemVMTemplateURL, nil)
	if err != nil {
		return errors.Errorf("creating request: %w", err)
	}

	// Send the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Errorf("downloading template: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("downloading template: HTTP %d", resp.StatusCode)
	}

	// Create a hash writer to verify the checksum
	hash := sha256.New()
	writer := io.MultiWriter(out, hash)

	// Copy the response body to the file and hash writer
	if _, err := io.Copy(writer, resp.Body); err != nil {
		return errors.Errorf("writing template file: %w", err)
	}

	// Close the file before moving it
	out.Close()

	// Verify the checksum
	computed := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if computed != DefaultSystemVMChecksum {
		os.Remove(tmpPath)
		return errors.Errorf("checksum mismatch: got %s, want %s", computed, DefaultSystemVMChecksum)
	}

	// Move the temporary file to the final location
	if err := os.Rename(tmpPath, templateCachePath); err != nil {
		return errors.Errorf("moving template file: %w", err)
	}

	// Create symlink in workdir
	workdirPath := filepath.Join(s.workDir, "templates", templateName+".qcow2")
	if err := os.MkdirAll(filepath.Dir(workdirPath), 0755); err != nil {
		return errors.Errorf("creating template directory in workdir: %w", err)
	}

	// Remove existing symlink or file
	_ = os.Remove(workdirPath)

	// Create relative symlink
	if err := os.Symlink(templateCachePath, workdirPath); err != nil {
		return errors.Errorf("creating symlink to cached template: %w", err)
	}

	s.logger.Info().
		Str("template", templateName).
		Str("path", templateCachePath).
		Msg("Template downloaded and cached successfully")

	return nil
}

// CreateManagementServer creates a VM for CloudStack Management Server
func (s *Setup) CreateManagementServer(ctx context.Context) error {
	s.logger.Info().Msg("Creating CloudStack Management Server VM")

	vmName := "cloudstack-management"
	diskPath := filepath.Join(s.workDir, "disks", "management.qcow2")

	// Create disk if it doesn't exist
	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		if err := s.host.CreateDisk(ctx, diskPath, DefaultManagementDiskSizeGB); err != nil {
			return errors.Errorf("creating management server disk: %w", err)
		}
	}

	// Create VM configuration
	config := host.NewVMConfig(vmName, diskPath)
	config.CPU = DefaultManagementCPU
	config.MemoryMB = DefaultManagementMemoryMB
	config.KVM = true

	// For CloudStack management, we want a graphical console
	config.Headless = false

	// Start the VM
	if err := s.host.CreateVMWithConfig(ctx, config); err != nil {
		return errors.Errorf("creating management server VM: %w", err)
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
	vms, err := s.host.ListRunningVMs()
	if err != nil {
		return fmt.Errorf("failed to list running VMs: %w", err)
	}

	if len(vms) == 0 {
		s.logger.Info().Msg("No VMs are currently running")
		return nil
	}

	s.logger.Info().Int("count", len(vms)).Msg("Running VMs")

	for _, vm := range vms {
		status, err := s.host.GetVMStatus(ctx, vm)
		if err != nil {
			s.logger.Warn().Err(err).Str("vm", vm).Msg("Failed to get VM status")
			continue
		}

		// Get VM info
		info, err := s.host.GetVMInfo(ctx, vm)
		if err != nil {
			s.logger.Warn().Err(err).Str("vm", vm).Msg("Failed to get VM info")
			continue
		}

		s.logger.Info().
			Str("name", vm).
			Str("status", string(status)).
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

// GetQEMUManager returns the QEMU manager instance
func (s *Setup) GetHost() host.Host {
	return s.host
}

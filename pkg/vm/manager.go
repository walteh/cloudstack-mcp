package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

// Manager defines the interface for managing VMs
type Manager interface {
	// CreateVM creates a new VM with the given configuration
	CreateVM(ctx context.Context, config VMConfig) (*VM, error)

	// StartVM starts a VM
	StartVM(ctx context.Context, vm *VM) error

	// StopVM stops a running VM
	StopVM(ctx context.Context, vm *VM) error

	// DeleteVM deletes a VM and its associated resources
	DeleteVM(ctx context.Context, vm *VM) error

	// GetVM retrieves a VM by name
	GetVM(ctx context.Context, name string) (*VM, error)

	// ListVMs lists all available VMs
	ListVMs(ctx context.Context) ([]*VM, error)

	// WaitForInitialization waits for VM initialization to complete
	WaitForInitialization(ctx context.Context, vm *VM) error
}

// LocalManager implements Manager for local QEMU VMs
type LocalManager struct {
	QemuPath    string
	MkisofsPath string
	QemuImgPath string
}

// NewLocalManager creates a new LocalManager
func NewLocalManager() (*LocalManager, error) {

	// Find required executables
	qemuPath, err := exec.LookPath("qemu-system-aarch64")
	if err != nil {
		// Try ARM architecture
		qemuPath, err = exec.LookPath("qemu-system-x86_64")
		if err != nil {
			return nil, errors.Errorf("finding qemu executable: %w", err)
		}
	}

	mkisofsPath, err := exec.LookPath("mkisofs")
	if err != nil {
		return nil, errors.Errorf("finding mkisofs executable: %w", err)
	}

	qemuImgPath, err := exec.LookPath("qemu-img")
	if err != nil {
		return nil, errors.Errorf("finding qemu-img executable: %w", err)
	}

	return &LocalManager{
		QemuPath:    qemuPath,
		MkisofsPath: mkisofsPath,
		QemuImgPath: qemuImgPath,
	}, nil
}

// CreateVM creates a new VM with the given configuration
func (m *LocalManager) CreateVM(ctx context.Context, config VMConfig) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", config.Name).Msg("Creating VM")

	vm := &VM{
		Config: config,
		SSHInfo: SSHInfo{
			Username:   "ubuntu", // Default for Ubuntu cloud images
			PrivateKey: "",       // Will be set when VM starts
			Host:       "",       // Will be set when VM starts
			Port:       22,
		},
		MetaData: map[string]string{},
		Status:   VMStatusCreated,
	}

	if err := os.MkdirAll(vm.Dir(), 0755); err != nil {
		return nil, errors.Errorf("creating VM directory: %w", err)
	}

	// Create VM disk based on base image

	imgPath := config.BaseImg.Path()

	// Download base image if it doesn't exist
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		// TODO: implement image download
		return nil, errors.Errorf("base image %s does not exist", imgPath)
	}

	// Create disk using qemu-img
	cmd := exec.CommandContext(
		ctx,
		m.QemuImgPath,
		"create",
		"-F", "qcow2",
		"-b", imgPath,
		"-f", "qcow2",
		vm.DiskPath(),
		config.DiskSize,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Errorf("creating VM disk: %s: %w", output, err)
	}

	vm.MetaData["disk_output"] = string(output)

	metaData, err := vm.BuildMetaData()
	if err != nil {
		return nil, errors.Errorf("generating meta-data: %w", err)
	}

	userData, err := vm.UserData(true)
	if err != nil {
		return nil, errors.Errorf("generating user-data: %w", err)
	}

	networkConfig, err := vm.NetworkConfig()
	if err != nil {
		return nil, errors.Errorf("generating network-config: %w", err)
	}

	// Create cloud-init files
	metaDataPath := filepath.Join(vm.Dir(), "meta-data")
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0644); err != nil {
		return nil, errors.Errorf("writing meta-data: %w", err)
	}

	userDataPath := filepath.Join(vm.Dir(), "user-data")
	if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
		return nil, errors.Errorf("writing user-data: %w", err)
	}

	networkConfigPath := filepath.Join(vm.Dir(), "network-config")
	if err := os.WriteFile(networkConfigPath, []byte(networkConfig), 0644); err != nil {
		return nil, errors.Errorf("writing network-config: %w", err)
	}

	// Create cloud-init ISO
	mkisofsCmdArgs := []string{
		"-output", vm.CIDataPath(),
		"-volid", "cidata",
		"-joliet",
		"-rock",
		metaDataPath,
		userDataPath,
		networkConfigPath,
	}

	cmd = exec.CommandContext(ctx, m.MkisofsPath, mkisofsCmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.Errorf("creating cloud-init ISO: %s: %w", output, err)
	}

	logger.Info().Str("name", config.Name).Msg("VM created successfully")

	// Save the state
	if err := vm.SaveState(); err != nil {
		return nil, errors.Errorf("saving VM state: %w", err)
	}

	return vm, nil
}

// StartVM starts a VM
func (m *LocalManager) StartVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM")

	// Make sure the VM name is set
	vm.Name = vm.Config.Name

	// Determine architecture and platform-specific settings
	imageName := vm.Config.BaseImg.Name
	arch := DetectArchitecture(imageName)
	isAppleSilicon := IsAppleSilicon()

	// Set the appropriate QEMU path based on architecture
	m.QemuPath = GetQEMUPath(arch)

	// Get machine settings and EFI path
	machine := GetMachineSetting(arch, isAppleSilicon)
	efi := GetEFIPath(arch)

	logger.Debug().
		Str("arch", arch).
		Str("qemu", m.QemuPath).
		Str("machine", machine).
		Str("efi", efi).
		Msg("VM hardware configuration")

	// Check if disk and cidata ISO exist
	diskPath := vm.DiskPath()
	ciDataPath := vm.CIDataPath()

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return errors.Errorf("VM disk image does not exist: %s", diskPath)
	}

	if _, err := os.Stat(ciDataPath); os.IsNotExist(err) {
		return errors.Errorf("VM cloud-init ISO does not exist: %s", ciDataPath)
	}

	logger.Debug().
		Str("disk", diskPath).
		Str("cidata", ciDataPath).
		Msg("VM disk configuration")

	// Set up networking based on platform
	var nic string
	switch vm.Config.Network.Type {
	case "vmnet-shared":
		nic = fmt.Sprintf("vmnet-shared,start-address=%s,subnet-mask=%s",
			vm.Config.Network.IPRange,
			vm.Config.Network.Subnet)
	case "tap":
		// TODO: Implement tap script handling
		nic = "tap"
	default:
		// Default to user mode networking with port forwarding
		// Use a dynamic port to avoid conflicts
		port, err := findAvailablePort()
		if err != nil {
			return errors.Errorf("finding available port: %w", err)
		}
		nic = fmt.Sprintf("user,hostfwd=tcp::%d-:22", port)
		vm.SSHInfo.Host = "localhost"
		vm.SSHInfo.Port = port
	}

	logger.Debug().
		Str("nic", nic).
		Str("mac", vm.Config.Network.MAC).
		Msg("VM network configuration")

	// Ensure SSH info is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
	}

	// Prepare QEMU command
	qemuArgs := []string{
		"-nographic",
		"-machine", machine,
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", vm.Config.CPUs),
		"-m", vm.Config.Memory,
		"-bios", efi,
		"-nic", fmt.Sprintf("%s,mac=%s", nic, vm.Config.Network.MAC),
		"-hda", diskPath,
		"-drive", fmt.Sprintf("file=%s,driver=raw,if=virtio", ciDataPath),
	}

	logger.Debug().
		Strs("args", qemuArgs).
		Msg("QEMU command arguments")

	// Create a log file for QEMU output
	logFile, err := os.Create(vm.QEMULogPath())
	if err != nil {
		return errors.Errorf("creating QEMU log file: %w", err)
	}

	// Try running QEMU and capturing output
	cmd := exec.CommandContext(ctx, m.QemuPath, qemuArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	logger.Info().
		Str("command", m.QemuPath).
		Strs("args", qemuArgs).
		Str("log_file", vm.QEMULogPath()).
		Msg("Starting QEMU process")

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return errors.Errorf("starting VM: %w", err)
	}

	// Update VM state
	vm.Status = VMStatusInitializing
	vm.PID = cmd.Process.Pid

	// Save the state
	if err := vm.SaveState(); err != nil {
		logger.Error().Err(err).Msg("Failed to save VM state")
		// Continue anyway, as the VM is running
	}

	logger.Info().
		Str("name", vm.Name).
		Int("pid", vm.PID).
		Str("ssh_host", vm.SSHInfo.Host).
		Int("ssh_port", vm.SSHInfo.Port).
		Msg("VM started successfully, initializing...")

	return nil
}

// StartVMForeground starts a VM in foreground mode for testing
func (m *LocalManager) StartVMForeground(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM in foreground")

	// Make sure the VM name is set
	vm.Name = vm.Config.Name

	// Determine architecture and platform-specific settings
	imageName := vm.Config.BaseImg.Name
	arch := DetectArchitecture(imageName)
	isAppleSilicon := IsAppleSilicon()

	// Set the appropriate QEMU path based on architecture
	m.QemuPath = GetQEMUPath(arch)

	// Get machine settings and EFI path
	machine := GetMachineSetting(arch, isAppleSilicon)
	efi := GetEFIPath(arch)

	logger.Debug().
		Str("arch", arch).
		Str("qemu", m.QemuPath).
		Str("machine", machine).
		Str("efi", efi).
		Msg("VM hardware configuration")

	// Check if disk and cidata ISO exist
	diskPath := vm.DiskPath()
	ciDataPath := vm.CIDataPath()

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return errors.Errorf("VM disk image does not exist: %s", diskPath)
	}

	if _, err := os.Stat(ciDataPath); os.IsNotExist(err) {
		return errors.Errorf("VM cloud-init ISO does not exist: %s", ciDataPath)
	}

	logger.Debug().
		Str("disk", diskPath).
		Str("cidata", ciDataPath).
		Msg("VM disk configuration")

	// Set up networking based on platform
	var nic string
	switch vm.Config.Network.Type {
	case "vmnet-shared":
		nic = fmt.Sprintf("vmnet-shared,start-address=%s,subnet-mask=%s",
			vm.Config.Network.IPRange,
			vm.Config.Network.Subnet)
	case "tap":
		// TODO: Implement tap script handling
		nic = "tap"
	default:
		// Default to user mode networking with port forwarding
		// Use a dynamic port to avoid conflicts
		port, err := findAvailablePort()
		if err != nil {
			return errors.Errorf("finding available port: %w", err)
		}
		nic = fmt.Sprintf("user,hostfwd=tcp::%d-:22", port)
		vm.SSHInfo.Host = "localhost"
		vm.SSHInfo.Port = port
	}

	logger.Debug().
		Str("nic", nic).
		Str("mac", vm.Config.Network.MAC).
		Msg("VM network configuration")

	// Ensure SSH info is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
	}

	// Prepare QEMU command
	qemuArgs := []string{
		"-nographic",
		"-machine", machine,
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", vm.Config.CPUs),
		"-m", vm.Config.Memory,
		"-bios", efi,
		"-nic", fmt.Sprintf("%s,mac=%s", nic, vm.Config.Network.MAC),
		"-hda", diskPath,
		"-drive", fmt.Sprintf("file=%s,driver=raw,if=virtio", ciDataPath),
		// Add debugging options
		"-d", "guest_errors,unimp",
		"-D", vm.QEMULogPath(),
	}

	logger.Debug().
		Strs("args", qemuArgs).
		Msg("QEMU command arguments")

	// Update VM state before running (in case we want to kill it)
	vm.Status = VMStatusInitializing
	vm.SaveState()

	// Run QEMU in foreground
	cmd := exec.CommandContext(ctx, m.QemuPath, qemuArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Info().
		Str("command", m.QemuPath).
		Strs("args", qemuArgs).
		Msg("Starting QEMU process in foreground")

	// This will block until QEMU exits
	err := cmd.Run()
	if err != nil {
		logger.Error().Err(err).Msg("QEMU exited with error")
		vm.Status = VMStatusFailed
		vm.LastError = err.Error()
		vm.SaveState()
		return err
	}

	// QEMU has exited
	vm.Status = VMStatusStopped
	vm.SaveState()

	logger.Info().
		Str("name", vm.Name).
		Msg("VM stopped")

	return nil
}

// WaitForInitialization waits for VM initialization to complete
func (m *LocalManager) WaitForInitialization(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Waiting for VM initialization")

	// Check if the VM is running
	if vm.GetStatus() != VMStatusInitializing && vm.GetStatus() != VMStatusStarted {
		return errors.Errorf("VM is not initializing or running")
	}

	// Create a ticker for checking cloud-init logs
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Create a channel to signal cancellation
	doneCh := make(chan struct{})

	// Keep track of the last read position in the QEMU log file
	var lastReadPos int64 = 0

	// Start a goroutine to stream QEMU logs
	go func() {
		defer close(doneCh)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Open the QEMU log file
				logFile, err := os.Open(vm.QEMULogPath())
				if err != nil {
					logger.Warn().Err(err).Msg("Failed to open QEMU log file")
					continue
				}

				// Get current file size
				stat, err := logFile.Stat()
				if err != nil {
					logFile.Close()
					logger.Warn().Err(err).Msg("Failed to get QEMU log file size")
					continue
				}

				// If there's new content, read it
				if stat.Size() > lastReadPos {
					_, err = logFile.Seek(lastReadPos, 0)
					if err != nil {
						logFile.Close()
						logger.Warn().Err(err).Msg("Failed to seek in QEMU log file")
						continue
					}

					buffer := make([]byte, stat.Size()-lastReadPos)
					n, err := logFile.Read(buffer)
					logFile.Close()

					if err != nil && err != io.EOF {
						logger.Warn().Err(err).Msg("Failed to read QEMU log file")
						continue
					}

					if n > 0 {
						fmt.Print(string(buffer[:n]))
						lastReadPos += int64(n)
					}
				} else {
					logFile.Close()
				}
			}
		}
	}()

	// Wait for SSH to become available
	sshClient, err := vm.WaitForSSH(ctx, 300) // 5 minute timeout
	if err != nil {
		// Wait for log streaming to finish
		<-doneCh

		logger.Error().Err(err).Msg("Failed to connect to VM via SSH")
		vm.Status = VMStatusFailed
		vm.LastError = fmt.Sprintf("Failed to connect via SSH: %v", err)
		vm.SaveState()
		return errors.Errorf("waiting for SSH: %w", err)
	}
	defer sshClient.Close()

	// Check if cloud-init has completed
	fmt.Println("\nVM is up, checking cloud-init status...")
	for i := 0; i < 60; i++ { // Try for up to 5 minutes (5 seconds * 60)
		if ctx.Err() != nil {
			// Wait for log streaming to finish
			<-doneCh

			// Context was canceled
			vm.Status = VMStatusFailed
			vm.LastError = "Initialization canceled by user"
			vm.SaveState()
			return errors.Errorf("initialization canceled: %w", ctx.Err())
		}

		logger.Debug().Str("name", vm.Config.Name).Int("attempt", i+1).Msg("Checking cloud-init status")

		cmd := "cloud-init status --wait || echo 'cloud-init-failed'"
		output, err := vm.RunCommand(ctx, cmd)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to check cloud-init status, retrying...")
			time.Sleep(5 * time.Second)
			continue
		}

		// Show the cloud-init status output to the user
		fmt.Printf("Cloud-init status: %s\n", strings.TrimSpace(output))

		// Check cloud-init status output
		if strings.Contains(output, "done") {
			logger.Info().Str("name", vm.Config.Name).Msg("Cloud-init completed successfully")

			// Fetch and display the cloud-init logs
			logs, err := vm.RunCommand(ctx, "cat /var/log/cloud-init.log | tail -n 50")
			if err == nil {
				fmt.Println("\nCloud Init Logs (last 50 lines):")
				fmt.Println(logs)
			}

			// Stop the VM since we're now fully initialized
			if err := vm.Stop(); err != nil {
				logger.Warn().Err(err).Msg("Failed to stop VM after initialization")
			}

			// Set status to ready
			vm.Status = VMStatusReady
			vm.SaveState()

			// Wait for log streaming to finish
			<-doneCh

			return nil
		} else if strings.Contains(output, "error") || strings.Contains(output, "cloud-init-failed") {
			// Fetch and display the cloud-init logs
			logs, err := vm.RunCommand(ctx, "cat /var/log/cloud-init.log | tail -n 100")
			if err == nil {
				fmt.Println("\nCloud Init Logs (last 100 lines):")
				fmt.Println(logs)
			}

			vm.Status = VMStatusFailed
			vm.LastError = fmt.Sprintf("Cloud-init failed: %s", output)
			vm.SaveState()
			logger.Error().Str("name", vm.Config.Name).Str("output", output).Msg("Cloud-init failed")

			// Wait for log streaming to finish
			<-doneCh

			return errors.Errorf("cloud-init failed: %s", output)
		}

		logger.Debug().Str("name", vm.Config.Name).Str("output", output).Msg("Cloud-init still running")
		time.Sleep(5 * time.Second)
	}

	// If we get here, cloud-init didn't complete within the timeout
	// Fetch and display the cloud-init logs
	logs, err := vm.RunCommand(ctx, "cat /var/log/cloud-init.log | tail -n 100")
	if err == nil {
		fmt.Println("\nCloud Init Logs (last 100 lines):")
		fmt.Println(logs)
	}

	vm.Status = VMStatusFailed
	vm.LastError = "Cloud-init initialization timed out"
	vm.SaveState()

	// Wait for log streaming to finish
	<-doneCh

	return errors.Errorf("cloud-init initialization timed out")
}

// StopVM stops a running VM
func (m *LocalManager) StopVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Stopping VM")

	if vm.GetStatus() != VMStatusStarted && vm.GetStatus() != VMStatusInitializing {
		return errors.Errorf("VM is not running or initializing")
	}

	if err := vm.Stop(); err != nil {
		return errors.Errorf("stopping VM: %w", err)
	}

	// If the VM was in initialized state before, keep it as "ready"
	// rather than "stopped"
	if vm.Status == VMStatusInitializing {
		vm.Status = VMStatusFailed
	} else {
		vm.Status = VMStatusStopped
	}

	vm.SaveState()

	return nil
}

// DeleteVM deletes a VM and its associated resources
func (m *LocalManager) DeleteVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Deleting VM")

	if vm.GetStatus() == VMStatusStarted || vm.GetStatus() == VMStatusInitializing {
		if err := vm.Stop(); err != nil {
			return errors.Errorf("stopping VM: %w", err)
		}
	}

	// Mark as deleted first, in case anything fails during cleanup
	vm.Status = VMStatusDeleted
	vm.SaveState()

	if err := os.RemoveAll(vm.Dir()); err != nil {
		return errors.Errorf("removing VM directory: %w", err)
	}

	return nil
}

// GetVM retrieves a VM by name
func (m *LocalManager) GetVM(ctx context.Context, name string) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", name).Msg("Getting VM")

	vmDir := filepath.Join(vmsDir(), name)
	if _, err := os.Stat(vmDir); os.IsNotExist(err) {
		return nil, errors.Errorf("VM not found: %s", name)
	}

	// Create a basic VM object
	vm := &VM{
		Name: name,
		Config: VMConfig{
			Name: name,
		},
	}

	// Load VM state from disk
	stateFile := filepath.Join(vmDir, "vm-state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// No state file, assume VM is stopped
		return vm, nil
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, errors.Errorf("reading VM state file: %w", err)
	}

	if err := json.Unmarshal(data, vm); err != nil {
		return nil, errors.Errorf("unmarshaling VM state: %w", err)
	}

	// Also check for QEMU processes that match this VM's name if we have no PID or
	// the process with our saved PID is not running
	if vm.GetStatus() != "running" {
		cmd := exec.Command("ps", "aux")
		output, err := cmd.Output()
		if err == nil {
			outputStr := string(output)
			// Look for QEMU processes with this VM's name in the command line
			if strings.Contains(outputStr, "qemu") && strings.Contains(outputStr, name) {
				// Parse PID from output
				lines := strings.Split(outputStr, "\n")
				for _, line := range lines {
					if strings.Contains(line, "qemu") && strings.Contains(line, name) {
						fields := strings.Fields(line)
						if len(fields) > 1 {
							pid, err := strconv.Atoi(fields[1])
							if err == nil && pid > 0 {
								logger.Info().Int("pid", pid).Msg("Found QEMU process for VM, updating PID")
								vm.PID = pid
								vm.SaveState()
								break
							}
						}
					}
				}
			}
		}
	}

	// Ensure the VM name is set
	if vm.Name == "" {
		vm.Name = name
		vm.SaveState()
	}

	// Ensure config is at least partially populated
	if vm.Config.Name == "" {
		vm.Config.Name = name
		vm.SaveState()
	}

	return vm, nil
}

// ListVMs lists all available VMs
func (m *LocalManager) ListVMs(ctx context.Context) ([]*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Listing VMs")

	entries, err := os.ReadDir(vmsDir())
	if err != nil {
		if os.IsNotExist(err) {
			// VMs directory doesn't exist, return empty list
			return []*VM{}, nil
		}
		return nil, errors.Errorf("reading VMs directory: %w", err)
	}

	vms := make([]*VM, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			vmName := entry.Name()
			vm, err := m.GetVM(ctx, vmName)
			if err != nil {
				logger.Warn().Err(err).Str("name", vmName).Msg("Failed to load VM")
				// Continue with other VMs
				continue
			}
			vms = append(vms, vm)
		}
	}

	return vms, nil
}

// CleanupVMs stops all VM processes and moves their folders to a vms-deleted directory
func (m *LocalManager) CleanupVMs(ctx context.Context) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Cleaning up all VMs")

	// Get all VMs
	vms, err := m.ListVMs(ctx)
	if err != nil {
		return errors.Errorf("listing VMs: %w", err)
	}

	// Create vms-deleted directory if it doesn't exist
	deletedDir := filepath.Join(baseDir(), "vms-deleted")
	if err := os.MkdirAll(deletedDir, 0755); err != nil {
		return errors.Errorf("creating vms-deleted directory: %w", err)
	}

	// Process each VM
	for _, vm := range vms {
		logger.Info().Str("name", vm.Name).Msg("Cleaning up VM")

		// Kill the process if it's running
		if vm.GetStatus() == "running" {
			logger.Info().Str("name", vm.Name).Int("pid", vm.PID).Msg("Stopping VM process")
			if err := vm.Stop(); err != nil {
				logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to stop VM gracefully, killing process")

				// Force kill the process if needed
				if vm.PID > 0 {
					proc, err := os.FindProcess(vm.PID)
					if err == nil {
						if err := proc.Kill(); err != nil {
							logger.Warn().Err(err).Str("name", vm.Name).Int("pid", vm.PID).Msg("Failed to kill VM process")
						}
					}
				}
			}
		}

		// Move the VM directory to vms-deleted
		srcDir := vm.Dir()
		destDir := filepath.Join(deletedDir, vm.Name)

		// Remove destination directory if it already exists
		if _, err := os.Stat(destDir); err == nil {
			logger.Info().Str("name", vm.Name).Msg("Removing existing VM in deleted directory")
			if err := os.RemoveAll(destDir); err != nil {
				logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to remove existing deleted VM directory")
			}
		}

		logger.Info().Str("name", vm.Name).Str("src", srcDir).Str("dest", destDir).Msg("Moving VM directory")
		if err := os.Rename(srcDir, destDir); err != nil {
			logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to move VM directory")
		}
	}

	logger.Info().Int("count", len(vms)).Msg("VM cleanup completed")
	return nil
}

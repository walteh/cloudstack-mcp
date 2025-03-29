package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/crypto/ssh"
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
	baseDir     string
}

// NewLocalManager creates a new LocalManager
func NewLocalManager() (*LocalManager, error) {
	logger := log.Logger.With().Str("component", "vm-manager").Logger()

	// Get VM data directory
	vmsPath := vmsDir()

	// Check if vms directory exists, if not, create it
	if _, err := os.Stat(vmsPath); os.IsNotExist(err) {
		logger.Info().Str("dir", vmsPath).Msg("Creating VMs directory")
		if err := os.MkdirAll(vmsPath, 0755); err != nil {
			return nil, errors.Errorf("creating VMs directory: %w", err)
		}
	}

	// Find qemu-system-aarch64 binary
	qemuPath, err := exec.LookPath("qemu-system-aarch64")
	if err != nil {
		return nil, errors.Errorf("looking for qemu-system-aarch64 binary: %w", err)
	}

	// Find genisoimage or mkisofs binary
	mkisofsPath, err := exec.LookPath("genisoimage")
	if err != nil {
		// Try mkisofs as an alternative
		mkisofsPath, err = exec.LookPath("mkisofs")
		if err != nil {
			return nil, errors.Errorf("looking for genisoimage or mkisofs binary: %w", err)
		}
	}

	// Find qemu-img binary
	qemuImgPath, err := exec.LookPath("qemu-img")
	if err != nil {
		return nil, errors.Errorf("looking for qemu-img binary: %w", err)
	}

	return &LocalManager{
		QemuPath:    qemuPath,
		MkisofsPath: mkisofsPath,
		QemuImgPath: qemuImgPath,
		baseDir:     vmsDir(), // Set the baseDir field here
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

	// Update VM PID
	vm.PID = cmd.Process.Pid

	// Save the state (Note: status is now set by the calling function)
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
						// Check if the buffer contains login prompt which indicates cloud-init is done
						content := string(buffer[:n])
						fmt.Print(content)
						lastReadPos += int64(n)

						// Look for cloud-init logs in QEMU output
						if strings.Contains(content, "cloud-init") {
							logger.Debug().Str("cloud_init_log", content).Msg("Found cloud-init output in QEMU logs")
						}

						// Detect login prompt which indicates initialization is complete
						if strings.Contains(content, "login:") || strings.Contains(content, "Ubuntu") && strings.Contains(content, "tty") {
							logger.Info().Msg("Login prompt detected, system is ready")
						}
					}
				} else {
					logFile.Close()
				}
			}
		}
	}()

	// Display SSH connection info
	fmt.Printf("\nInitializing VM %s (this may take a few minutes)...\n", vm.Name)
	fmt.Printf("SSH Username: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
	fmt.Println("\n--- Cloud-init logs will appear below as they become available ---\n")

	// Set a timeout for the entire operation
	timeout := time.After(10 * time.Minute)

	// Wait for SSH to become available
	var sshClient *ssh.Client
	var sshErr error

	// Start a goroutine to handle SSH connection
	sshConnected := make(chan bool, 1)
	go func() {
		// Try to connect via SSH
		sshClient, sshErr = vm.WaitForSSH(ctx, 300) // 5 minute timeout
		sshConnected <- (sshErr == nil)
	}()

	// Wait for SSH connection or timeout or cancellation
	select {
	case <-timeout:
		logger.Error().Msg("Timed out waiting for VM initialization")
		fmt.Println("\nVM initialization timed out after 10 minutes.")

		// Stop the VM forcefully
		if vm.PID > 0 {
			if proc, err := os.FindProcess(vm.PID); err == nil {
				proc.Kill()
			}
		}

		vm.Status = VMStatusFailed
		vm.LastError = "Initialization timed out"
		vm.SaveState()

		// Wait for log streaming to finish
		<-doneCh

		// Show connection information for manual recovery
		fmt.Printf("\n==== VM INITIALIZATION FAILED ====\n")
		fmt.Printf("VM: %s\n", vm.Name)
		fmt.Printf("Host: %s\n", vm.SSHInfo.Host)
		fmt.Printf("Port: %d\n", vm.SSHInfo.Port)
		fmt.Printf("Username: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
		fmt.Printf("================================\n")

		return errors.Errorf("VM initialization timed out")

	case <-ctx.Done():
		logger.Warn().Msg("VM initialization canceled")

		// Stop the VM forcefully
		if vm.PID > 0 {
			if proc, err := os.FindProcess(vm.PID); err == nil {
				proc.Kill()
			}
		}

		vm.Status = VMStatusFailed
		vm.LastError = "Initialization canceled by user"
		vm.SaveState()

		// Wait for log streaming to finish
		<-doneCh

		// Show connection information for manual recovery
		fmt.Printf("\n==== VM INITIALIZATION CANCELED ====\n")
		fmt.Printf("VM: %s\n", vm.Name)
		fmt.Printf("Host: %s\n", vm.SSHInfo.Host)
		fmt.Printf("Port: %d\n", vm.SSHInfo.Port)
		fmt.Printf("Username: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
		fmt.Printf("================================\n")

		return errors.Errorf("initialization canceled: %w", ctx.Err())

	case success := <-sshConnected:
		if !success {
			// SSH connection failed
			logger.Error().Err(sshErr).Msg("Failed to connect to VM via SSH")

			// Stop the VM forcefully
			if vm.PID > 0 {
				if proc, err := os.FindProcess(vm.PID); err == nil {
					proc.Kill()
				}
			}

			vm.Status = VMStatusFailed
			vm.LastError = fmt.Sprintf("Failed to connect via SSH: %v", sshErr)
			vm.SaveState()

			// Wait for log streaming to finish
			<-doneCh

			// Show connection information for manual recovery
			fmt.Printf("\n==== SSH CONNECTION FAILED ====\n")
			fmt.Printf("VM: %s\n", vm.Name)
			fmt.Printf("Host: %s\n", vm.SSHInfo.Host)
			fmt.Printf("Port: %d\n", vm.SSHInfo.Port)
			fmt.Printf("Username: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
			fmt.Printf("SSH Error: %v\n", sshErr)
			fmt.Printf("================================\n")

			return errors.Errorf("waiting for SSH: %w", sshErr)
		}

		// SSH connected successfully
		defer sshClient.Close()
	}

	// Check if cloud-init has completed
	fmt.Println("\n==== VM is up, checking cloud-init status ====")

	// Try to display cloud-init logs in various ways
	displayCloudInitLogs(ctx, vm, logger)

	// Run cloud-init status command
	cmd := "cloud-init status || echo 'cloud-init-status-not-available'"
	output, err := vm.RunCommand(ctx, cmd)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to check cloud-init status")
		fmt.Println("Failed to check cloud-init status, but VM is running")
	} else {
		fmt.Printf("Cloud-init status: %s\n", strings.TrimSpace(output))
	}

	// At this point, we've successfully connected via SSH, which means the VM is usable
	// so we'll mark it as ready regardless of cloud-init status

	// Stop the VM since we're now fully initialized
	fmt.Println("\nInitialization complete, stopping VM...")

	// Send a shutdown command first to gracefully shutdown
	vm.RunCommand(ctx, "sudo shutdown -h now")

	// Wait a bit for shutdown to take effect
	time.Sleep(5 * time.Second)

	// Then force stop if it's still running
	if err := vm.Stop(); err != nil {
		logger.Warn().Err(err).Msg("Failed to stop VM after initialization")
	}

	// Set status to ready
	vm.Status = VMStatusReady
	vm.SaveState()

	// Wait for log streaming to finish
	<-doneCh

	// Display final connection information with clear formatting
	fmt.Printf("\n==== VM INITIALIZATION COMPLETE ====\n")
	fmt.Printf("VM: %s\n", vm.Name)
	fmt.Printf("Status: READY\n")
	fmt.Printf("\nTo start the VM:\n  vmctl start-vm %s\n", vm.Name)
	fmt.Printf("\nTo connect to the VM:\n  vmctl shell %s\n", vm.Name)
	fmt.Printf("\nCredentials:\n  Username: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
	fmt.Printf("====================================\n")

	return nil
}

// displayCloudInitLogs tries multiple ways to display cloud-init logs
func displayCloudInitLogs(ctx context.Context, vm *VM, logger *zerolog.Logger) {
	// Try multiple ways to get cloud-init logs and display them
	fmt.Println("\n==== Cloud Init Logs ====")

	// 1. Check for cloud-init.log
	cloudInitLog, err := vm.RunCommand(ctx, "cat /var/log/cloud-init.log 2>/dev/null | tail -n 100 || echo 'No cloud-init.log found'")
	if err == nil && !strings.Contains(cloudInitLog, "No cloud-init.log found") {
		fmt.Println("=== From /var/log/cloud-init.log ===")
		fmt.Println(cloudInitLog)
	} else {
		fmt.Println("Could not read /var/log/cloud-init.log")
	}

	// 2. Check for debug logs
	sshDebugLog, err := vm.RunCommand(ctx, "cat /var/log/ssh_debug.log 2>/dev/null || echo 'No SSH debug log found'")
	if err == nil && !strings.Contains(sshDebugLog, "No SSH debug log found") {
		fmt.Println("\n=== SSH Debug Info ===")
		fmt.Println(sshDebugLog)
	}

	// 3. Check cloud-init output log
	cloudInitOutputLog, err := vm.RunCommand(ctx, "cat /var/log/cloud-init-output.log 2>/dev/null | tail -n 100 || echo 'No cloud-init-output.log found'")
	if err == nil && !strings.Contains(cloudInitOutputLog, "No cloud-init-output.log found") {
		fmt.Println("\n=== From /var/log/cloud-init-output.log ===")
		fmt.Println(cloudInitOutputLog)
	} else {
		fmt.Println("Could not read /var/log/cloud-init-output.log")
	}

	// 4. Check journalctl logs for cloud-init
	journalLogs, err := vm.RunCommand(ctx, "journalctl -u cloud-init -u cloud-init-local -u cloud-config -u cloud-final --no-pager | tail -n 100 || echo 'No journal logs found'")
	if err == nil && !strings.Contains(journalLogs, "No journal logs found") {
		fmt.Println("\n=== From systemd journal ===")
		fmt.Println(journalLogs)
	} else {
		fmt.Println("Could not read cloud-init journal logs")
	}

	// 5. If all else fails, show system logs
	if !strings.Contains(cloudInitLog, "cloud-init") && !strings.Contains(journalLogs, "cloud-init") {
		syslog, err := vm.RunCommand(ctx, "cat /var/log/syslog 2>/dev/null | grep -i cloud | tail -n 50 || echo 'No syslog available'")
		if err == nil && !strings.Contains(syslog, "No syslog available") {
			fmt.Println("\n=== From syslog (filtered for cloud-init) ===")
			fmt.Println(syslog)
		}
	}

	fmt.Println("\n==============================")
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
			// Log the error but continue with deletion
			logger.Warn().Err(err).Str("name", vm.Config.Name).Msg("Failed to stop VM cleanly, continuing with deletion")
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

	vmDir := filepath.Join(m.baseDir, name)
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

	entries, err := os.ReadDir(m.baseDir)
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
	deletedDir := filepath.Join(m.baseDir, "vms-deleted")
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

// StartVMVisible starts a VM in a new terminal window to show real-time logs
func (m *LocalManager) StartVMVisible(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM in visible terminal")

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

	// Set up networking based on platform
	var nic string
	switch vm.Config.Network.Type {
	case "vmnet-shared":
		nic = fmt.Sprintf("vmnet-shared,start-address=%s,subnet-mask=%s",
			vm.Config.Network.IPRange,
			vm.Config.Network.Subnet)
	case "tap":
		nic = "tap"
	default:
		// Default to user mode networking with port forwarding
		port, err := findAvailablePort()
		if err != nil {
			return errors.Errorf("finding available port: %w", err)
		}
		nic = fmt.Sprintf("user,hostfwd=tcp::%d-:22", port)
		vm.SSHInfo.Host = "localhost"
		vm.SSHInfo.Port = port
	}

	// Ensure SSH info is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
	}

	// Prepare QEMU command arguments
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

	// Create a full command string
	qemuCommand := m.QemuPath + " " + strings.Join(qemuArgs, " ")

	// Update VM state
	vm.Status = VMStatusInitializing
	vm.SaveState()

	// Write SSH info to a info file for reference
	sshInfoStr := fmt.Sprintf(`SSH Information:
Host: %s
Port: %d
Username: %s
Using SSH key authentication
`, vm.SSHInfo.Host, vm.SSHInfo.Port, vm.SSHInfo.Username)

	infoPath := filepath.Join(vm.Dir(), "ssh-info.txt")
	if err := os.WriteFile(infoPath, []byte(sshInfoStr), 0644); err != nil {
		logger.Warn().Err(err).Msg("Failed to write SSH info file")
	}

	// Determine the terminal command to use based on OS
	var cmd *exec.Cmd
	switch {
	case isAppleSilicon || strings.Contains(os.Getenv("OSTYPE"), "darwin"):
		// macOS uses osascript to open a new Terminal window
		shellScript := fmt.Sprintf(`
echo "Starting QEMU VM: %s"
echo "SSH Information:"
echo "  Host: %s"
echo "  Port: %d"
echo "  Username: %s (using SSH key authentication)"
echo ""
echo "Press Ctrl+C to close this window (VM will continue running in the background)"
echo "========================================================"
echo ""
%s
		`, vm.Name, vm.SSHInfo.Host, vm.SSHInfo.Port, vm.SSHInfo.Username, qemuCommand)

		// Write the shell script to a file
		scriptPath := filepath.Join(vm.Dir(), "start-vm.sh")
		if err := os.WriteFile(scriptPath, []byte(shellScript), 0755); err != nil {
			return errors.Errorf("writing shell script: %w", err)
		}

		// Create AppleScript to open a new Terminal window
		appleScript := fmt.Sprintf(`tell application "Terminal"
			do script "cd %s && ./start-vm.sh"
			activate
		end tell`, vm.Dir())

		cmd = exec.CommandContext(ctx, "osascript", "-e", appleScript)

	case strings.Contains(os.Getenv("OSTYPE"), "linux") || strings.Contains(os.Getenv("TERM_PROGRAM"), "gnome"):
		// For GNOME Terminal on Linux
		cmd = exec.CommandContext(ctx, "gnome-terminal", "--", "bash", "-c",
			fmt.Sprintf("echo 'Starting QEMU VM: %s'; echo 'SSH Info - Host: %s, Port: %d, User: %s'; echo ''; %s; exec bash",
				vm.Name, vm.SSHInfo.Host, vm.SSHInfo.Port, vm.SSHInfo.Username, qemuCommand))

	case strings.Contains(os.Getenv("TERM_PROGRAM"), "xterm"):
		// For xterm
		cmd = exec.CommandContext(ctx, "xterm", "-e",
			fmt.Sprintf("echo 'Starting QEMU VM: %s'; echo 'SSH Info - Host: %s, Port: %d, User: %s'; echo ''; %s; exec bash",
				vm.Name, vm.SSHInfo.Host, vm.SSHInfo.Port, vm.SSHInfo.Username, qemuCommand))

	default:
		// Fallback for other platforms - just try to use "x-terminal-emulator" (common on many Linux distros)
		cmd = exec.CommandContext(ctx, "x-terminal-emulator", "-e",
			fmt.Sprintf("echo 'Starting QEMU VM: %s'; echo 'SSH Info - Host: %s, Port: %d, User: %s'; echo ''; %s; exec bash",
				vm.Name, vm.SSHInfo.Host, vm.SSHInfo.Port, vm.SSHInfo.Username, qemuCommand))
	}

	logger.Info().
		Str("name", vm.Name).
		Str("ssh_host", vm.SSHInfo.Host).
		Int("ssh_port", vm.SSHInfo.Port).
		Msg("Starting VM in a new terminal window...")

	// Execute the command to open a new terminal
	if err := cmd.Start(); err != nil {
		vm.Status = VMStatusFailed
		vm.SaveState()
		return errors.Errorf("starting VM in new terminal: %w", err)
	}

	// Don't wait for the terminal to close - it will be independent
	// Just record the PID for reference (not the actual QEMU PID though)
	vm.PID = cmd.Process.Pid
	vm.SaveState()

	fmt.Println("\n============ VM STARTED IN NEW TERMINAL WINDOW ============")
	fmt.Printf("VM: %s\n", vm.Name)
	fmt.Printf("Status: INITIALIZING\n")
	fmt.Printf("SSH Host: %s\n", vm.SSHInfo.Host)
	fmt.Printf("SSH Port: %d\n", vm.SSHInfo.Port)
	fmt.Printf("SSH User: %s (using SSH key authentication)\n", vm.SSHInfo.Username)
	fmt.Println("You should see a new terminal window with the VM console output")
	fmt.Println("==========================================================\n")

	return nil
}

// StartVMWithConsole starts a VM and then opens an SSH connection to stream logs directly
func (m *LocalManager) StartVMWithConsole(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM with console streaming")

	// First start the VM normally
	if err := m.StartVM(ctx, vm); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("\nVM %s is starting. Waiting for SSH to become available...\n", vm.Name)

	// Create a cancelable context for the console streaming
	consoleCtx, cancelConsole := context.WithCancel(ctx)
	defer cancelConsole()

	// Set up signal handling to gracefully exit
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalCh:
			fmt.Println("\nReceived interrupt signal, stopping console stream...")
			cancelConsole()
		case <-consoleCtx.Done():
			// Context canceled elsewhere
		}
	}()

	// Wait for SSH to become available with retry
	var sshClient *ssh.Client
	var sshErr error

	// Retry loop for SSH connection
	maxRetries := 60
	retryInterval := 5 * time.Second

	fmt.Println("\nWaiting for VM to become accessible via SSH...")

	for i := 0; i < maxRetries; i++ {
		select {
		case <-consoleCtx.Done():
			return errors.Errorf("console streaming canceled")
		default:
			// Try to connect
			sshClient, sshErr = vm.ConnectSSH()
			if sshErr == nil {
				// Successfully connected
				defer sshClient.Close()
				break
			}

			// If we haven't reached max retries, wait and try again
			if i < maxRetries-1 {
				if i%5 == 0 {
					fmt.Printf("SSH connection attempt %d failed: %v. Retrying...\n", i+1, sshErr)
				} else {
					fmt.Printf(".")
				}
				time.Sleep(retryInterval)
			} else {
				return errors.Errorf("failed to connect to VM via SSH after %d attempts: %w", maxRetries, sshErr)
			}
		}
	}

	if sshErr != nil {
		return errors.Errorf("failed to connect to VM via SSH: %w", sshErr)
	}

	fmt.Printf("\nSSH connection established. Streaming console output...\n\n")
	fmt.Println("=== VM Console Output (press Ctrl+C to exit) ===")

	// Stream multiple log files in parallel
	errCh := make(chan error, 5)
	done := make(chan struct{})

	// Function to stream a single log file
	streamLog := func(client *ssh.Client, command, description string) {
		session, err := client.NewSession()
		if err != nil {
			errCh <- errors.Errorf("creating SSH session for %s: %w", description, err)
			return
		}
		defer session.Close()

		// Set up pipes
		session.Stdout = os.Stdout
		session.Stderr = os.Stderr

		fmt.Printf("\n--- Streaming %s ---\n", description)

		if err := session.Start(command); err != nil {
			errCh <- errors.Errorf("starting command for %s: %w", description, err)
			return
		}

		if err := session.Wait(); err != nil {
			if consoleCtx.Err() != nil {
				// Context was canceled, so this is expected
				return
			}
			errCh <- errors.Errorf("waiting for %s command: %w", description, err)
		}
	}

	// Start multiple streams in parallel goroutines
	go func() {
		defer close(done)

		// QEMU console log
		go streamLog(sshClient, "dmesg -w", "kernel messages")

		// Cloud-init logs
		go streamLog(sshClient, "sudo journalctl -f -u cloud-init -u cloud-init-local -u cloud-config -u cloud-final", "cloud-init journalctl")

		// Primary cloud-init log
		go streamLog(sshClient, "sudo tail -f /var/log/cloud-init.log 2>/dev/null || echo 'No cloud-init.log available'", "cloud-init.log")

		// Cloud-init output log
		go streamLog(sshClient, "sudo tail -f /var/log/cloud-init-output.log 2>/dev/null || echo 'No cloud-init-output.log available'", "cloud-init-output.log")

		// Wait for context cancellation
		<-consoleCtx.Done()
	}()

	// Wait for either done signal or an error
	select {
	case <-done:
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StreamQEMULogs streams the QEMU logs for a VM in real-time
func (m *LocalManager) StreamQEMULogs(ctx context.Context, vm *VM) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle signals for graceful exit
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Reset(os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-signalCh:
			fmt.Println("\nReceived cancellation signal, stopping log streaming...")
			cancel()
		case <-ctx.Done():
			// Context canceled elsewhere
		}
	}()

	// Get the log file path
	logFile := filepath.Join(m.baseDir, vm.Name, "qemu.log")

	fmt.Printf("\nStreaming QEMU console output for VM %s (press Ctrl+C to stop)...\n\n", vm.Name)
	fmt.Println("=== VM Console Output (showing cloud-init logs) ===")
	fmt.Println("(Will auto-exit when login prompt is detected, or press Ctrl+C to stop manually)")

	// Check if the log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		fmt.Printf("Log file %s does not exist yet, waiting...\n", logFile)
		// Wait a bit for the log file to be created
		time.Sleep(1 * time.Second)
	}

	// Variables to track terminal location
	var lastPos int64 = 0
	var logBuffer strings.Builder
	var bootComplete bool

	// Loop for reading log file
	for {
		select {
		case <-ctx.Done():
			return bootComplete, ctx.Err()
		default:
			// Check if file exists now
			file, err := os.Open(logFile)
			if err != nil {
				if os.IsNotExist(err) {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				return false, errors.Errorf("opening log file: %w", err)
			}

			// Get file size
			stat, err := file.Stat()
			if err != nil {
				file.Close()
				return false, errors.Errorf("getting file stat: %w", err)
			}

			// If file is new or has grown
			if stat.Size() > lastPos {
				// Seek to last read position
				_, err = file.Seek(lastPos, 0)
				if err != nil {
					file.Close()
					return false, errors.Errorf("seeking in log file: %w", err)
				}

				// Read new content
				newContent := make([]byte, stat.Size()-lastPos)
				n, err := file.Read(newContent)
				if err != nil && err != io.EOF {
					file.Close()
					return false, errors.Errorf("reading log file: %w", err)
				}

				// Update last position
				lastPos = stat.Size()

				// Print new content
				if n > 0 {
					fmt.Print(string(newContent[:n]))
					logBuffer.Write(newContent[:n])

					// Check for signs of login prompt
					currentContent := logBuffer.String()
					if strings.Contains(currentContent, "login:") {
						bootComplete = true
						fmt.Println("=== Login prompt detected, cloud-init process is complete ===")
						fmt.Println("=== Auto-exiting log streaming ===")

						// Update VM status to running/started and ensure it stays that way
						vm.Status = VMStatusStarted
						if err := vm.SaveState(); err != nil {
							fmt.Printf("Warning: Failed to update VM status: %v\n", err)
						} else {
							fmt.Printf("VM %s status updated to %s\n", vm.Name, vm.Status)
						}
						file.Close()

						// Important fix: Keep VM Status as running/started
						// Double-check the status file to ensure it was properly written
						time.Sleep(500 * time.Millisecond) // Brief pause to ensure file operations complete

						// Load the VM again to verify status was saved properly
						updatedVM, err := m.GetVM(ctx, vm.Name)
						if err != nil {
							fmt.Printf("Warning: Could not verify status update: %v\n", err)
						} else if updatedVM.GetStatus() != VMStatusStarted {
							// Fix the status if it didn't stick
							fmt.Printf("Status update didn't persist, fixing VM status to 'started'\n")
							updatedVM.Status = VMStatusStarted
							if err := updatedVM.SaveState(); err != nil {
								fmt.Printf("Warning: Final attempt to update VM status failed: %v\n", err)
							}
						}

						return true, nil
					}
				}
			}

			file.Close()

			// Check VM process still running - exit if not
			if vm.PID > 0 {
				proc, err := vm.Process()
				if err != nil || proc == nil {
					// Process does not exist anymore
					fmt.Println("VM process has terminated.")

					// If we already detected boot completion, maintain "started" status
					// This handles the case where the VM process terminates after successful boot
					if bootComplete {
						// Ensure VM status is "started"
						vm.Status = VMStatusStarted
						if err := vm.SaveState(); err != nil {
							fmt.Printf("Warning: Failed to update VM status: %v\n", err)
						} else {
							fmt.Printf("VM %s status maintained as started after process exit\n", vm.Name)
						}
						return true, nil
					}

					return false, nil
				}
			}

			// Small pause to avoid tight loop
			time.Sleep(100 * time.Millisecond)
		}
	}
}

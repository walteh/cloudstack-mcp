package host

import (
	"context"
	"runtime"
)

type Host interface {
	CreateVMWithConfig(ctx context.Context, config VMConfig) error
	GetVMStatus(ctx context.Context, name string) (Status, error)
	GetVMInfo(ctx context.Context, name string) (*VMInfo, error)
	CreateDisk(ctx context.Context, path string, sizeGB int) error
	ListRunningVMs() ([]string, error)
	InstallDependencies(ctx context.Context) error
}

type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusRunning  Status = "running"
	StatusShutdown Status = "shutdown"
	StatusPaused   Status = "paused"
	StatusSaved    Status = "saved"
)

// VMConfig represents configuration for a QEMU VM
type VMConfig struct {
	Name      string
	CPU       int
	MemoryMB  int
	DiskPath  string
	NetDevice string
	NetBridge string
	CDROM     string
	VGA       string
	UseBIOS   bool
	UseTPM    bool
	KVM       bool
	Headless  bool
	Machine   string // Machine type (especially needed for ARM64)
}

// NewVMConfig creates a new VM configuration with defaults
func NewVMConfig(name string, diskPath string) VMConfig {
	// Default machine type based on architecture
	machine := "q35"
	if runtime.GOARCH == "arm64" {
		machine = "virt"
	}

	return VMConfig{
		Name:      name,
		CPU:       4,
		MemoryMB:  4096,
		DiskPath:  diskPath,
		NetDevice: "virtio-net-pci",
		NetBridge: "virbr0",
		VGA:       "std",
		KVM:       false, // KVM is not available on macOS
		UseBIOS:   false,
		UseTPM:    false,
		Headless:  false,
		Machine:   machine,
	}
}

// VMInfo contains information about a VM
type VMInfo struct {
	CPUs     int
	MemoryMB int
	VNCPort  string
	QMPPort  string
}

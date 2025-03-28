package kvm

// VMConfig represents configuration for a VM managed by libvirt
type VMConfig struct {
	Name     string
	CPU      int
	MemoryMB int
	DiskPath string
	Network  NetworkConfig
	VGA      string
	Machine  string
}

// NetworkConfig represents network configuration for a VM
type NetworkConfig struct {
	Type      string // "bridge" or "user"
	Bridge    string // Bridge name if Type is "bridge"
	Interface string // Network interface name
}

// VMStatus represents the status of a VM
type VMStatus string

const (
	// VMStatusUnknown indicates the VM status cannot be determined
	VMStatusUnknown VMStatus = "unknown"
	// VMStatusRunning indicates the VM is running
	VMStatusRunning VMStatus = "running"
	// VMStatusShutdown indicates the VM is shut down
	VMStatusShutdown VMStatus = "shutdown"
	// VMStatusPaused indicates the VM is paused
	VMStatusPaused VMStatus = "paused"
	// VMStatusSaved indicates the VM state is saved
	VMStatusSaved VMStatus = "saved"
)

// VMInfo contains information about a running VM
type VMInfo struct {
	Name      string
	Status    VMStatus
	CPU       CPUInfo
	Memory    MemoryInfo
	Network   NetworkInfo
	DiskPaths []string
}

// CPUInfo contains CPU information for a VM
type CPUInfo struct {
	Count    int
	Model    string
	Features []string
	HostCPUs []int // CPU cores assigned from host
}

// MemoryInfo contains memory information for a VM
type MemoryInfo struct {
	Total     uint64 // Total memory in bytes
	Used      uint64 // Used memory in bytes
	Available uint64 // Available memory in bytes
}

// NetworkInfo contains network information for a VM
type NetworkInfo struct {
	Interfaces []NetworkInterface
}

// NetworkInterface represents a network interface in a VM
type NetworkInterface struct {
	Name       string
	MACAddress string
	IPAddress  string
	Type       string // "bridge" or "user"
}

package vm

import "os/exec"

type Img struct {
	Name string
	Url  string
}

type CloudInit interface {
	UserData() (string, error)
	NetworkConfig() (string, error)
	MetaData() (string, error)
}

// VMConfig represents the configuration for a virtual machine
type VMConfig struct {
	Name     string
	ID       int
	CPUs     int
	Memory   string // e.g., "2G"
	DiskSize string // e.g., "20G"
	BaseImg  Img
	Network  NetworkConfig
}

// NetworkConfig represents the network configuration for a VM
type NetworkConfig struct {
	Type     string // e.g., "vmnet-shared", "tap"
	MAC      string
	IPRange  string // e.g., "192.168.1.1,192.168.1.20"
	Subnet   string // e.g., "255.255.255.0"
	Hostname string
}

// VM represents a running virtual machine instance
type VM struct {
	Config        VMConfig
	SSHInfo       SSHInfo
	process       *exec.Cmd
	diskOutput    string
	internalState string
}

// SSHInfo contains information needed to SSH into the VM
type SSHInfo struct {
	Username   string
	PrivateKey string
	Host       string
	Port       int
}

package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/digitalocean/go-qemu/hypervisor"
	"github.com/digitalocean/go-qemu/qemu"
	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

// Manager handles VM operations through libvirt inside the guest VM
type Manager struct {
	logger zerolog.Logger
	driver *hypervisor.RPCDriver
	vmIP   string
	vmPort int
}

// NewManager creates a new hypervisor manager
func NewManager(ctx context.Context, logger zerolog.Logger, vmIP string, vmPort int) (*Manager, error) {
	logger.Info().
		Str("vmIP", vmIP).
		Int("vmPort", vmPort).
		Msg("Connecting to libvirt in VM")

	// Create a connection factory for libvirt RPC
	connFactory := func() (net.Conn, error) {
		address := fmt.Sprintf("%s:%d", vmIP, vmPort)
		conn, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			return nil, errors.Errorf("connecting to libvirt at %s: %w", address, err)
		}
		return conn, nil
	}

	// Create the RPC driver
	driver := hypervisor.NewRPCDriver(connFactory)

	// Test the connection
	version, err := driver.Version()
	if err != nil {
		return nil, errors.Errorf("connecting to libvirt: %w", err)
	}

	logger.Info().
		Str("libvirtVersion", version).
		Msg("Connected to libvirt inside VM")

	return &Manager{
		logger: logger,
		driver: driver,
		vmIP:   vmIP,
		vmPort: vmPort,
	}, nil
}

// CreateVM creates a new VM using libvirt
func (m *Manager) CreateVM(ctx context.Context, config VMConfig) error {
	m.logger.Info().
		Str("name", config.Name).
		Int("cpu", config.CPU).
		Int("memoryMB", config.MemoryMB).
		Str("diskPath", config.DiskPath).
		Msg("Creating VM")

	// Convert our config to domain XML
	xml := generateDomainXML(config)

	// Open a connection to libvirt
	monitor, err := m.driver.NewMonitor("system")
	if err != nil {
		return errors.Errorf("connecting to libvirt: %w", err)
	}

	if err := monitor.Connect(); err != nil {
		return errors.Errorf("connecting to monitor: %w", err)
	}
	defer monitor.Disconnect()

	// Create the domain
	defineCmd, err := json.Marshal(map[string]interface{}{
		"execute": "domain-define-xml",
		"arguments": map[string]string{
			"xml": xml,
		},
	})
	if err != nil {
		return errors.Errorf("marshaling domain-define-xml command: %w", err)
	}

	_, err = monitor.Run(defineCmd)
	if err != nil {
		return errors.Errorf("defining domain: %w", err)
	}

	// Start the domain
	startCmd, err := json.Marshal(map[string]interface{}{
		"execute": "domain-start",
		"arguments": map[string]string{
			"name": config.Name,
		},
	})
	if err != nil {
		return errors.Errorf("marshaling domain-start command: %w", err)
	}

	_, err = monitor.Run(startCmd)
	if err != nil {
		return errors.Errorf("starting domain: %w", err)
	}

	m.logger.Info().Str("name", config.Name).Msg("VM created successfully")
	return nil
}

// ListVMs returns a list of all VMs
func (m *Manager) ListVMs(ctx context.Context) ([]VMInfo, error) {
	// Get domain names
	domainNames, err := m.driver.DomainNames()
	if err != nil {
		return nil, errors.Errorf("listing domains: %w", err)
	}

	var vms []VMInfo
	for _, name := range domainNames {
		info, err := m.GetVMInfo(ctx, name)
		if err != nil {
			m.logger.Warn().Err(err).Str("domain", name).Msg("Failed to get VM info")
			continue
		}
		vms = append(vms, *info)
	}

	return vms, nil
}

// GetVMInfo returns information about a specific VM
func (m *Manager) GetVMInfo(ctx context.Context, name string) (*VMInfo, error) {
	// Create a monitor and use it to connect to the domain
	monitor, err := m.driver.NewMonitor(name)
	if err != nil {
		return nil, errors.Errorf("creating monitor for domain: %w", err)
	}

	if err := monitor.Connect(); err != nil {
		return nil, errors.Errorf("connecting to monitor: %w", err)
	}
	defer monitor.Disconnect()

	domain, err := qemu.NewDomain(monitor, name)
	if err != nil {
		return nil, errors.Errorf("creating domain: %w", err)
	}

	// Get status
	statusInt, err := domain.Status()
	if err != nil {
		return nil, errors.Errorf("getting domain status: %w", err)
	}

	// Convert status
	status := convertStatus(int(statusInt))

	// Get CPUs
	cpus, err := domain.CPUs()
	if err != nil {
		return nil, errors.Errorf("getting CPU info: %w", err)
	}

	// Get block devices
	blockDevices, err := domain.BlockDevices()
	if err != nil {
		return nil, errors.Errorf("getting block devices: %w", err)
	}

	// Create info struct
	vmInfo := &VMInfo{
		Name:   name,
		Status: status,
		CPU: CPUInfo{
			Count: len(cpus),
		},
		Memory: MemoryInfo{
			// Can't get actual memory usage through go-qemu easily, so use defaults
			Total:     4096 * 1024 * 1024, // Assume 4GB
			Used:      0,
			Available: 4096 * 1024 * 1024,
		},
		Network: NetworkInfo{
			Interfaces: []NetworkInterface{
				{
					Name:       "default",
					MACAddress: "unknown",
					IPAddress:  "unknown",
					Type:       "bridge",
				},
			},
		},
		DiskPaths: make([]string, 0, len(blockDevices)),
	}

	// Add disk paths
	for _, dev := range blockDevices {
		vmInfo.DiskPaths = append(vmInfo.DiskPaths, dev.Inserted.File)
	}

	return vmInfo, nil
}

// convertStatus converts the domain status to our VMStatus type
func convertStatus(status int) VMStatus {
	switch status {
	case 1: // Running
		return VMStatusRunning
	case 5: // Shutdown
		return VMStatusShutdown
	case 3: // Paused
		return VMStatusPaused
	case 4: // Saved
		return VMStatusSaved
	default:
		return VMStatusUnknown
	}
}

// generateDomainXML generates libvirt domain XML for a VM
func generateDomainXML(config VMConfig) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<domain type="kvm">
  <name>%s</name>
  <memory unit="MiB">%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch="x86_64" machine="%s">hvm</type>
    <boot dev="hd"/>
  </os>
  <devices>
    <disk type="file" device="disk">
      <driver name="qemu" type="qcow2"/>
      <source file="%s"/>
      <target dev="vda" bus="virtio"/>
    </disk>
    <interface type="%s">
      %s
      <model type="virtio"/>
    </interface>
    <graphics type="vnc" port="-1" autoport="yes"/>
    <video>
      <model type="%s"/>
    </video>
  </devices>
</domain>`,
		config.Name,
		config.MemoryMB,
		config.CPU,
		config.Machine,
		config.DiskPath,
		config.Network.Type,
		generateNetworkXML(config.Network),
		config.VGA,
	)
}

// generateNetworkXML generates the network-specific part of the domain XML
func generateNetworkXML(config NetworkConfig) string {
	if config.Type == "bridge" {
		return fmt.Sprintf(`<source bridge="%s"/>`, config.Bridge)
	}
	return "<source network='default'/>"
}

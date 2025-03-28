package qemu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gitlab.com/tozd/go/errors"
)

// NetworkConfig represents QEMU virtual network configuration
type NetworkConfig struct {
	BridgeName    string
	NetworkCIDR   string
	DHCPStart     string
	DHCPEnd       string
	BridgeAddress string
}

// DefaultNetworkConfig returns a sensible default network configuration
func DefaultNetworkConfig() NetworkConfig {
	return NetworkConfig{
		BridgeName:    "virbr0",
		NetworkCIDR:   "192.168.122.0/24",
		DHCPStart:     "192.168.122.100",
		DHCPEnd:       "192.168.122.200",
		BridgeAddress: "192.168.122.1",
	}
}

// SetupNetwork configures networking for QEMU VMs
func (m *Manager) SetupNetwork(ctx context.Context, cfg NetworkConfig) error {
	m.logger.Info().Msg("Setting up QEMU virtual network")

	// We'll use the scripts directory in our work directory
	scriptsDir := filepath.Join(m.workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return errors.Errorf("creating scripts directory: %w", err)
	}

	// Generate network setup script
	scriptPath := filepath.Join(scriptsDir, "setup-network.sh")
	scriptContent := fmt.Sprintf(`#!/bin/bash
set -e

# Check if bridge already exists
if ip link show %s &>/dev/null; then
    echo "Bridge %s already exists"
    exit 0
fi

# Create bridge
sudo ip link add name %s type bridge
sudo ip addr add %s dev %s
sudo ip link set %s up

# Configure IP forwarding
sudo sysctl -w net.ipv4.ip_forward=1

# Configure NAT
sudo iptables -t nat -A POSTROUTING -s %s ! -d %s -j MASQUERADE

# Set up DNS
sudo mkdir -p /etc/qemu
echo "nameserver 8.8.8.8" | sudo tee /etc/qemu/resolv.conf

echo "Virtual network setup complete"
`, cfg.BridgeName, cfg.BridgeName, cfg.BridgeName, cfg.BridgeAddress, cfg.BridgeName,
		cfg.BridgeName, cfg.NetworkCIDR, cfg.NetworkCIDR)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return errors.Errorf("writing network setup script: %w", err)
	}

	// For macOS, we'll create a different script that uses the built-in hypervisor framework
	// since Linux networking commands won't work
	if isMacOS() {
		m.logger.Warn().Msg("Running on macOS - network setup is limited and may require manual configuration")

		// On macOS, let's just create a host-only network
		macOSScriptPath := filepath.Join(scriptsDir, "setup-network-macos.sh")
		macOSScriptContent := `#!/bin/bash
set -e

echo "Setting up macOS host-only networking for QEMU"
echo "Note: On macOS, networking is handled through the hypervisor framework"
echo "You may need to configure port forwarding or VPN manually"

# We use the built-in vmnet framework on macOS
cat > ~/Library/Preferences/SystemConfiguration/com.apple.vmnet.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Shared_Net_Address</key>
    <string>192.168.122.1</string>
    <key>Shared_Net_Mask</key>
    <string>255.255.255.0</string>
</dict>
</plist>
EOF

echo "macOS network configuration complete"
echo "You will need to restart the system for changes to take effect"
`
		if err := os.WriteFile(macOSScriptPath, []byte(macOSScriptContent), 0755); err != nil {
			return errors.Errorf("writing macOS network setup script: %w", err)
		}

		m.logger.Info().Str("path", macOSScriptPath).Msg("Created macOS network setup script - this must be run manually with sudo")
	} else {
		// For Linux, run the script
		cmd := exec.CommandContext(ctx, "bash", scriptPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return errors.Errorf("running network setup script: %w", err)
		}
	}

	m.logger.Info().Msg("Network setup completed")
	return nil
}

// CreateTapInterface creates a tap interface for VM networking
func (m *Manager) CreateTapInterface(ctx context.Context, vmName string, bridgeName string) (string, error) {
	tapName := fmt.Sprintf("tap-%s", vmName)

	m.logger.Info().Str("tap", tapName).Str("bridge", bridgeName).Msg("Creating tap interface")

	// Create tap interface
	cmd := exec.CommandContext(ctx, "sudo", "ip", "tuntap", "add", "dev", tapName, "mode", "tap")
	if err := cmd.Run(); err != nil {
		return "", errors.Errorf("creating tap interface: %w", err)
	}

	// Set tap interface up
	cmd = exec.CommandContext(ctx, "sudo", "ip", "link", "set", tapName, "up")
	if err := cmd.Run(); err != nil {
		return "", errors.Errorf("setting tap interface up: %w", err)
	}

	// Add tap interface to bridge
	cmd = exec.CommandContext(ctx, "sudo", "ip", "link", "set", tapName, "master", bridgeName)
	if err := cmd.Run(); err != nil {
		return "", errors.Errorf("adding tap interface to bridge: %w", err)
	}

	m.logger.Info().Str("tap", tapName).Msg("Tap interface created and configured")
	return tapName, nil
}

package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/crypto/ssh"
)

// Setup configures KVM and libvirt inside a VM
func SetupKVM(ctx context.Context, h VM) (string, error) {
	logger := zerolog.Ctx(ctx).With().Str("component", "kvm-setup").Logger()

	conn, err := h.ConnectionInfo()
	if err != nil {
		return "", errors.Errorf("getting connection info: %w", err)
	}

	logger.Info().
		Str("ip", conn.IP).
		Int("port", conn.Port).
		Str("user", conn.User).
		Msg("Setting up KVM inside VM")

	// Define setup commands
	setupCommands := []string{
		// Update package list
		"sudo apt-get update",

		// Install KVM and libvirt
		"sudo apt-get install -y qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils virtinst",

		// Install supporting tools
		"sudo apt-get install -y virt-manager libguestfs-tools genisoimage",

		// Enable KVM modules
		"sudo modprobe kvm",
		"sudo modprobe kvm_intel || sudo modprobe kvm_amd",

		// Configure libvirt to listen on TCP
		"sudo bash -c 'cat > /etc/libvirt/libvirtd.conf << EOF\nlisten_tls = 0\nlisten_tcp = 1\ntcp_port = \"16509\"\nauth_tcp = \"none\"\nEOF'",

		// Configure libvirt daemon
		"sudo bash -c 'cat > /etc/default/libvirtd << EOF\nlibvirtd_opts=\"--listen\"\nEOF'",

		// Restart libvirt
		"sudo systemctl restart libvirtd",

		// Configure networking
		"sudo virsh net-start default || true",
		"sudo virsh net-autostart default || true",

		// Verify KVM is working
		"sudo virt-host-validate",

		// Get IP address for connection
		"ip addr show | grep 'inet ' | grep -v '127.0.0.1' | awk '{print $2}' | cut -d/ -f1",
	}

	// Create SSH config
	sshConfig := &ssh.ClientConfig{
		User: conn.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(conn.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Connect to VM
	addr := fmt.Sprintf("%s:%d", conn.IP, conn.Port)
	logger.Info().Str("address", addr).Msg("Connecting to VM")

	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return "", errors.Errorf("connecting to VM: %w", err)
	}
	defer client.Close()

	// Run setup commands
	logger.Info().Msg("Running KVM setup commands")

	var ipAddress string

	for _, cmd := range setupCommands {
		logger.Debug().Str("command", cmd).Msg("Running command")

		session, err := client.NewSession()
		if err != nil {
			return "", errors.Errorf("creating SSH session: %w", err)
		}

		output, err := session.CombinedOutput(cmd)
		session.Close()

		if err != nil {
			logger.Error().
				Err(err).
				Str("command", cmd).
				Str("output", string(output)).
				Msg("Command failed")
			// Continue with other commands even if one fails
			continue
		}

		// If this is the IP address command, capture the output
		if strings.Contains(cmd, "ip addr show") {
			ipAddress = strings.TrimSpace(string(output))
		}

		logger.Debug().
			Str("command", cmd).
			Str("output", string(output)).
			Msg("Command succeeded")
	}

	if ipAddress == "" {
		return "", errors.New("couldn't determine VM's IP address")
	}

	logger.Info().
		Str("ip", ipAddress).
		Int("port", 16509).
		Msg("KVM setup completed successfully")

	return ipAddress, nil
}

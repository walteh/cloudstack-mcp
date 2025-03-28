package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	systemVMTemplateURL     = "https://download.cloudstack.org/arm64/systemvmtemplate/4.20/systemvmtemplate-4.20.0.0-kvm-arm64.qcow2.bz2"
	defaultCloudStackDir    = "cloudstack"
	defaultSeconaryStorage  = "secondary"
	defaultPrimaryStorage   = "primary"
	cloudStackPPA           = "ppa:cloudstack/4.20"
	defaultCPUSpeed         = "2400" // Default CPU speed to use for Asahi Linux
	libvirtDefaultTCPPort   = "16509"
	cloudStackManagementURL = "http://localhost:8080/client"
)

// Command represents a shell command to be executed
type Command struct {
	Name string
	Args []string
}

// Execute runs a command and returns its output
func Execute(cmd Command) (string, error) {
	command := exec.Command(cmd.Name, cmd.Args...)
	output, err := command.CombinedOutput()
	return string(output), err
}

// ExecuteWithDir runs a command in a specific directory
func ExecuteWithDir(cmd Command, dir string) (string, error) {
	command := exec.Command(cmd.Name, cmd.Args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	return string(output), err
}

func downloadFile(url string, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func main() {
	// Define command line flags
	action := flag.String("action", "setup", "Action to perform: setup, start, stop")
	setupOnly := flag.Bool("setup-only", false, "Only perform setup steps without installing CloudStack")
	cloudstackDir := flag.String("dir", "", "CloudStack directory (default: ~/cloudstack)")
	cpuSpeed := flag.String("cpu-speed", defaultCPUSpeed, "CPU speed to use for Asahi Linux")
	flag.Parse()

	log.Printf("Running CloudStack KVM %s on %s/%s\n", *action, runtime.GOOS, runtime.GOARCH)

	switch *action {
	case "setup":
		if err := setupKVM(*setupOnly, *cloudstackDir, *cpuSpeed); err != nil {
			log.Fatalf("Failed to setup KVM: %v", err)
		}
	case "start":
		if err := startKVM(); err != nil {
			log.Fatalf("Failed to start KVM: %v", err)
		}
	case "stop":
		if err := stopKVM(); err != nil {
			log.Fatalf("Failed to stop KVM: %v", err)
		}
	default:
		log.Fatalf("Unknown action: %s", *action)
	}
}

func setupKVM(setupOnly bool, cloudstackDir string, cpuSpeed string) error {
	// Check if running on ARM architecture
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("this script is intended for ARM64 (Apple Silicon) machines, detected: %s", runtime.GOARCH)
	}

	// Check if running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("this script must be run as root (sudo)")
	}

	// Create necessary directories
	var baseDir string
	if cloudstackDir == "" {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %v", err)
		}
		baseDir = filepath.Join(homedir, defaultCloudStackDir)
	} else {
		baseDir = cloudstackDir
	}

	secondaryStorageDir := filepath.Join(baseDir, defaultSeconaryStorage)
	primaryStorageDir := filepath.Join(baseDir, defaultPrimaryStorage)

	log.Printf("Creating directories in %s", baseDir)
	for _, dir := range []string{baseDir, secondaryStorageDir, primaryStorageDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	// Set proper ownership (use SUDO_USER if available)
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser != "" {
		log.Printf("Setting ownership to %s", sudoUser)
		cmd := Command{
			Name: "chown",
			Args: []string{"-R", fmt.Sprintf("%s:%s", sudoUser, sudoUser), baseDir},
		}
		if out, err := Execute(cmd); err != nil {
			log.Printf("Warning: failed to set ownership: %v - %s", err, out)
		}
	}

	// Install prerequisites
	log.Println("Installing prerequisites...")
	prereqCmds := []Command{
		{Name: "apt", Args: []string{"update"}},
		{Name: "apt", Args: []string{"install", "-y", "qemu-kvm", "libvirt-daemon-system", "bridge-utils", "nfs-kernel-server", "mysql-server"}},
	}

	for _, cmd := range prereqCmds {
		if out, err := Execute(cmd); err != nil {
			return fmt.Errorf("failed to execute command %s %v: %v - %s", cmd.Name, cmd.Args, err, out)
		}
	}

	// Configure NFS exports
	log.Println("Configuring NFS exports...")
	exportsFile := "/etc/exports"

	// Read current exports
	exports, err := os.ReadFile(exportsFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read exports file: %v", err)
	}

	exportLines := []string{
		fmt.Sprintf("%s *(rw,async,no_root_squash)", primaryStorageDir),
		fmt.Sprintf("%s *(rw,async,no_root_squash)", secondaryStorageDir),
	}

	// Check if export lines already exist
	existingExports := string(exports)
	updatedExports := existingExports
	for _, line := range exportLines {
		if !strings.Contains(existingExports, line) {
			updatedExports += line + "\n"
		}
	}

	if updatedExports != existingExports {
		if err := os.WriteFile(exportsFile, []byte(updatedExports), 0644); err != nil {
			return fmt.Errorf("failed to update exports file: %v", err)
		}
	}

	// Restart NFS server
	if out, err := Execute(Command{Name: "systemctl", Args: []string{"restart", "nfs-kernel-server"}}); err != nil {
		return fmt.Errorf("failed to restart NFS server: %v - %s", err, out)
	}

	// Download SystemVM Template (ARM64 version)
	templateDir := filepath.Join(secondaryStorageDir, "template/tmpl/1/1")
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		return fmt.Errorf("failed to create template directory: %v", err)
	}

	templateArchive := filepath.Join(templateDir, "template.bz2")
	templateFile := filepath.Join(templateDir, "template")

	// Check if template already exists
	if _, err := os.Stat(templateFile); os.IsNotExist(err) {
		log.Printf("Downloading SystemVM template from %s...", systemVMTemplateURL)
		if err := downloadFile(systemVMTemplateURL, templateArchive); err != nil {
			return fmt.Errorf("failed to download template: %v", err)
		}

		log.Println("Extracting template...")
		if out, err := Execute(Command{Name: "bunzip2", Args: []string{templateArchive}}); err != nil {
			return fmt.Errorf("failed to extract template: %v - %s", err, out)
		}
	} else {
		log.Println("SystemVM template already exists, skipping download...")
	}

	if setupOnly {
		log.Println("Setup-only mode, skipping CloudStack installation...")
		return nil
	}

	// Install CloudStack Management Server and Agent
	log.Println("Installing CloudStack Management Server and Agent...")
	cloudstackInstallCmds := []Command{
		{Name: "apt", Args: []string{"install", "-y", "software-properties-common"}},
		{Name: "add-apt-repository", Args: []string{"-y", cloudStackPPA}},
		{Name: "apt", Args: []string{"update"}},
		{Name: "apt", Args: []string{"install", "-y", "cloudstack-management", "cloudstack-agent"}},
	}

	for _, cmd := range cloudstackInstallCmds {
		if out, err := Execute(cmd); err != nil {
			return fmt.Errorf("failed to execute command %s %v: %v - %s", cmd.Name, cmd.Args, err, out)
		}
	}

	// Configure MySQL
	log.Println("Configuring MySQL for CloudStack...")
	mysqlCmds := []Command{
		{Name: "mysql", Args: []string{"-e", "DROP DATABASE IF EXISTS cloud"}},
		{Name: "mysql", Args: []string{"-e", "DROP DATABASE IF EXISTS cloud_usage"}},
		{Name: "mysql", Args: []string{"-e", "CREATE DATABASE cloud"}},
		{Name: "mysql", Args: []string{"-e", "CREATE DATABASE cloud_usage"}},
		{Name: "mysql", Args: []string{"-e", "GRANT ALL ON cloud.* TO 'cloud'@'localhost' IDENTIFIED BY 'cloud'"}},
		{Name: "mysql", Args: []string{"-e", "GRANT ALL ON cloud_usage.* TO 'cloud'@'localhost'"}},
		{Name: "mysql", Args: []string{"-e", "GRANT PROCESS ON *.* TO 'cloud'@'localhost'"}},
		{Name: "mysql", Args: []string{"-e", "GRANT ALL ON cloud.* TO 'cloud'@'%' IDENTIFIED BY 'cloud'"}},
		{Name: "mysql", Args: []string{"-e", "GRANT ALL ON cloud_usage.* TO 'cloud'@'%'"}},
		{Name: "mysql", Args: []string{"-e", "GRANT PROCESS ON *.* TO 'cloud'@'%'"}},
	}

	for _, cmd := range mysqlCmds {
		if out, err := Execute(cmd); err != nil {
			return fmt.Errorf("failed to execute MySQL command: %v - %s", err, out)
		}
	}

	// Configure CloudStack DB
	log.Println("Setting up CloudStack databases...")
	if out, err := Execute(Command{
		Name: "cloudstack-setup-databases",
		Args: []string{"cloud:cloud@localhost", "--deploy-as=root"},
	}); err != nil {
		return fmt.Errorf("failed to setup CloudStack databases: %v - %s", err, out)
	}

	// Initialize CloudStack
	log.Println("Initializing CloudStack management server...")
	if out, err := Execute(Command{Name: "cloudstack-setup-management"}); err != nil {
		return fmt.Errorf("failed to setup CloudStack management: %v - %s", err, out)
	}

	// Configure libvirt
	log.Println("Configuring libvirt...")

	// Stop libvirtd first
	if out, err := Execute(Command{Name: "systemctl", Args: []string{"stop", "libvirtd"}}); err != nil {
		log.Printf("Warning: failed to stop libvirtd: %v - %s", err, out)
	}

	libvirtConf := "/etc/libvirt/libvirtd.conf"
	libvirtConfigs := map[string]string{
		"#listen_tls = 0":                "listen_tls = 0",
		"#listen_tcp = 1":                "listen_tcp = 1",
		"#tcp_port = \"16509\"":          "tcp_port = \"16509\"",
		"#unix_sock_group = \"libvirt\"": "unix_sock_group = \"libvirt\"",
		"#unix_sock_rw_perms = \"0770\"": "unix_sock_rw_perms = \"0770\"",
		"#auth_tcp = \"sasl\"":           "auth_tcp = \"none\"",
	}

	// Read libvirtd.conf
	confContent, err := os.ReadFile(libvirtConf)
	if err != nil {
		return fmt.Errorf("failed to read libvirt config: %v", err)
	}

	// Replace config lines
	lines := strings.Split(string(confContent), "\n")
	for i, line := range lines {
		for oldLine, newLine := range libvirtConfigs {
			if strings.TrimSpace(line) == oldLine {
				lines[i] = newLine
				break
			}
		}
	}

	// Write updated config
	if err := os.WriteFile(libvirtConf, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to update libvirt config: %v", err)
	}

	// Create systemd service for libvirtd
	log.Println("Creating systemd service for libvirtd...")
	serviceContent := `[Unit]
Description=Virtualization daemon
Requires=virtlogd.socket
Requires=virtlockd.socket
Before=libvirt-guests.service
After=network.target
After=dbus.service
After=apparmor.service
After=local-fs.target
Documentation=man:libvirtd(8)
Documentation=https://libvirt.org

[Service]
Type=notify
ExecStart=/usr/sbin/libvirtd --daemon --listen
ExecStartPost=/usr/bin/bash -c "/usr/sbin/iptables -I INPUT -p tcp --dport 16509 -j ACCEPT"
ExecStartPost=/usr/bin/bash -c "/usr/sbin/ip6tables -I INPUT -p tcp --dport 16509 -j ACCEPT"
Restart=on-failure
KillMode=process
EnvironmentFile=-/etc/sysconfig/libvirtd
CapabilityBoundingSet=CAP_AUDIT_CONTROL CAP_AUDIT_READ CAP_AUDIT_WRITE CAP_BLOCK_SUSPEND CAP_CHOWN CAP_CHOWN CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER CAP_FSETID CAP_IPC_LOCK CAP_KILL CAP_LEASE CAP_LINUX_IMMUTABLE CAP_MAC_ADMIN CAP_MAC_OVERRIDE CAP_MKNOD CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_BROADCAST CAP_NET_RAW CAP_SETGID CAP_SETPCAP CAP_SETUID CAP_SYS_ADMIN CAP_SYS_BOOT CAP_SYS_CHROOT CAP_SYS_MODULE CAP_SYS_NICE CAP_SYS_PACCT CAP_SYS_PTRACE CAP_SYS_RAWIO CAP_SYS_RESOURCE CAP_SYS_TIME CAP_SYS_TTY_CONFIG CAP_SYSLOG CAP_WAKE_ALARM
LimitNOFILE=1048576
LimitNPROC=1048576
LimitCORE=infinity
TasksMax=infinity

[Install]
WantedBy=multi-user.target
`

	// Write service file
	if err := os.WriteFile("/etc/systemd/system/libvirtd.service", []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write libvirtd service file: %v", err)
	}

	// Reload systemd, start and enable libvirtd
	systemdCmds := []Command{
		{Name: "systemctl", Args: []string{"daemon-reload"}},
		{Name: "systemctl", Args: []string{"start", "libvirtd"}},
		{Name: "systemctl", Args: []string{"enable", "libvirtd"}},
	}

	for _, cmd := range systemdCmds {
		if out, err := Execute(cmd); err != nil {
			return fmt.Errorf("failed to execute systemd command: %v - %s", err, out)
		}
	}

	// Configure CloudStack Agent
	agentPropertiesFile := "/etc/cloudstack/agent/agent.properties"
	if _, err := os.Stat(agentPropertiesFile); err == nil {
		log.Println("Configuring CloudStack agent...")

		// Update agent properties
		agentProps, err := os.ReadFile(agentPropertiesFile)
		if err != nil {
			return fmt.Errorf("failed to read agent properties: %v", err)
		}

		agentPropsStr := string(agentProps)

		// Set hypervisor to KVM
		agentPropsStr = strings.Replace(agentPropsStr, "# hypervisor=kvm", "hypervisor=kvm", 1)

		// Set CPU speed for Asahi Linux
		if !strings.Contains(agentPropsStr, "host.cpu.speed") {
			agentPropsStr += "\nhost.cpu.speed=" + cpuSpeed + "\n"
		}

		if err := os.WriteFile(agentPropertiesFile, []byte(agentPropsStr), 0644); err != nil {
			return fmt.Errorf("failed to update agent properties: %v", err)
		}

		// Restart agent
		if out, err := Execute(Command{Name: "systemctl", Args: []string{"restart", "cloudstack-agent"}}); err != nil {
			return fmt.Errorf("failed to restart CloudStack agent: %v - %s", err, out)
		}
	} else {
		log.Println("CloudStack agent properties file not found, skipping agent configuration")
	}

	log.Println("====================")
	log.Println("Setup complete! You can now access CloudStack at " + cloudStackManagementURL)
	log.Println("Default credentials: admin / password")
	log.Println("====================")

	return nil
}

func startKVM() error {
	// Start libvirtd service
	output, err := Execute(Command{Name: "sudo", Args: []string{"systemctl", "start", "libvirtd"}})
	if err != nil {
		return fmt.Errorf("failed to start libvirtd: %v - %s", err, output)
	}

	// Start cloudstack-agent service
	output, err = Execute(Command{Name: "sudo", Args: []string{"systemctl", "start", "cloudstack-agent"}})
	if err != nil {
		log.Printf("Warning: failed to start cloudstack-agent: %v - %s", err, output)
	}

	// Start cloudstack-management service
	output, err = Execute(Command{Name: "sudo", Args: []string{"systemctl", "start", "cloudstack-management"}})
	if err != nil {
		log.Printf("Warning: failed to start cloudstack-management: %v - %s", err, output)
	}

	log.Println("KVM and CloudStack services started successfully")
	return nil
}

func stopKVM() error {
	// Stop cloudstack-agent service
	output, err := Execute(Command{Name: "sudo", Args: []string{"systemctl", "stop", "cloudstack-agent"}})
	if err != nil {
		log.Printf("Warning: failed to stop cloudstack-agent: %v - %s", err, output)
	}

	// Stop cloudstack-management service
	output, err = Execute(Command{Name: "sudo", Args: []string{"systemctl", "stop", "cloudstack-management"}})
	if err != nil {
		log.Printf("Warning: failed to stop cloudstack-management: %v - %s", err, output)
	}

	// Stop libvirtd service
	output, err = Execute(Command{Name: "sudo", Args: []string{"systemctl", "stop", "libvirtd"}})
	if err != nil {
		return fmt.Errorf("failed to stop libvirtd: %v - %s", err, output)
	}

	log.Println("KVM and CloudStack services stopped successfully")
	return nil
}

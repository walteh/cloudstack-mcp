# Setting up CloudStack with KVM on Apple Silicon Macs

This guide explains how to set up CloudStack with KVM on Apple Silicon (M1/M2/M3) Macs running Ubuntu Asahi Linux.

## Prerequisites

1. An Apple Silicon Mac (M1, M2, M3, etc.)
2. [Ubuntu Asahi Linux](https://asahilinux.org/) installed (latest version recommended)
3. Root/sudo access

## Automated Installation

We provide a Go-based command-line tool and Taskfile commands to automate the installation and management of CloudStack with KVM:

### Building the Tool

First, build the tool:

```bash
task kvm:build
```

### Setting Up KVM and CloudStack

There are two options for setup:

1. Full CloudStack setup (including management server and agent):

```bash
task kvm:setup
```

2. Basic setup only (creates directories and installs prerequisites):

```bash
task kvm:setup-only
```

You can pass additional parameters to customize the installation:

```bash
# Set a custom CloudStack directory
task kvm:setup -- -dir=/path/to/custom/dir

# Set a custom CPU speed for Asahi Linux
task kvm:setup -- -cpu-speed=3000
```

### Managing Services

Start KVM and CloudStack services:

```bash
task kvm:start
```

Stop KVM and CloudStack services:

```bash
task kvm:stop
```

## Manual Steps

If you prefer to run the commands manually:

```bash
# Build the tool
./go build -o bin/kvmsetup ./cmd/kvmsetup

# Setup with default options
sudo ./bin/kvmsetup -action=setup

# Or customize the setup
sudo ./bin/kvmsetup -action=setup -dir=/path/to/custom/dir -cpu-speed=3000

# Start services
sudo ./bin/kvmsetup -action=start

# Stop services
sudo ./bin/kvmsetup -action=stop
```

## What the Tool Does

The setup process:

1. Creates required directories for CloudStack
2. Installs necessary packages (qemu-kvm, libvirt, NFS server, MySQL)
3. Configures NFS exports for primary and secondary storage
4. Downloads the ARM64 SystemVM template
5. Installs CloudStack management server and agent
6. Configures MySQL for CloudStack
7. Sets up CloudStack databases
8. Configures libvirt for CloudStack
9. Sets the CPU speed in the CloudStack agent properties (required for Asahi Linux)

## Accessing CloudStack

After successful setup, you can access the CloudStack web interface at:

```
http://localhost:8080/client
```

Default credentials:

-   Username: admin
-   Password: password

## Troubleshooting

### Known Issues

1. **CPU Speed Detection**: Asahi Linux may not report CPU speed correctly. The tool sets a default speed of 2400 MHz, but you can customize this with the `-cpu-speed` parameter.

2. **Virtualization Performance**: While KVM works on Apple Silicon, nested virtualization performance may vary.

### Common Errors

If you encounter issues:

1. Check system logs:

    ```bash
    sudo journalctl -u libvirtd
    sudo journalctl -u cloudstack-agent
    sudo journalctl -u cloudstack-management
    ```

2. Verify libvirt is running correctly:

    ```bash
    sudo systemctl status libvirtd
    ```

3. Check if NFS shares are properly exported:
    ```bash
    showmount -e localhost
    ```

## References

-   Based on [Rohit Yadav's guide](https://rohityadav.cloud/blog/cloudstack-rpi4-kvm/) for CloudStack on Raspberry Pi
-   Uses [CloudStack ARM64 SystemVM templates](https://download.cloudstack.org/arm64/systemvmtemplate/4.20/)

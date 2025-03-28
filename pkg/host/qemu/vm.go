package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/digitalocean/go-qemu/qmp"
	"github.com/walteh/cloudstack-mcp/pkg/host"
	"gitlab.com/tozd/go/errors"
)

type QEMUVM struct {
	name     string
	ip       string
	user     string
	password string
	port     int
	process  *exec.Cmd
}

func (v *QEMUVM) Name() string {
	return v.name
}

func (v *QEMUVM) IP() string {
	return v.ip
}

func (v *QEMUVM) User() string {
	return v.user
}

func (v *QEMUVM) Password() string {
	return v.password
}

func (v *QEMUVM) Port() int {
	return v.port
}

func (v *QEMUVM) ConnectionInfo() (*host.VMConnection, error) {
	return nil, errors.Errorf("not implemented")
}

func (v *QEMUVM) Status() host.Status {
	if v.process != nil && v.process.Process != nil {
		if err := v.process.Process.Signal(syscall.SIGCONT); err == nil {
			return host.StatusRunning
		}
	}
	return host.StatusUnknown
}

func (v *QEMUVM) Info() (*host.VMInfo, error) {
	tempDir, err := os.MkdirTemp("", v.name)
	if err != nil {
		return nil, errors.Errorf("creating temp dir: %w", err)
	}
	// Create QMP monitor for detailed info
	socketPath := filepath.Join(tempDir, "sockets", v.name+".sock")
	mon, err := qmp.NewSocketMonitor("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, errors.Errorf("creating QMP monitor: %w", err)
	}

	if err := mon.Connect(); err != nil {
		return nil, errors.Errorf("connecting to QMP monitor: %w", err)
	}
	defer mon.Disconnect()

	// For now, return default values since we need to parse QMP output
	// to get accurate CPU and memory information
	info := &host.VMInfo{
		CPUs:     4,    // Default value
		MemoryMB: 4096, // Default value
	}
	return info, nil
}

type QEMUDisk struct {
	path   string
	sizeGB int
}

func (d *QEMUDisk) Path() string {
	return d.path
}

func (d *QEMUDisk) SizeGB() int {
	return d.sizeGB
}

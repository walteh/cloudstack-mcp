package vm

import (
	"path/filepath"
)

func (vm *VM) Stop() error {
	return vm.process.Process.Kill()
}

func (vm *VM) PID() int {
	return vm.process.Process.Pid
}

func (vm *VM) Wait() error {
	return vm.process.Wait()
}

func (vm *VM) Status() (string, error) {
	if vm.process == nil {
		return "stopped", nil
	}

	if vm.process.ProcessState == nil {
		return "running", nil
	}

	// if vm.process.ProcessState.Exited() {
	// 	return "exited", nil
	// }

	return "exited", nil
}

func (vm *VM) Dir() string {
	return filepath.Join(vmsDir(), vm.Config.Name)
}

func (vm *VM) DiskPath() string {
	return filepath.Join(vm.Dir(), diskName)
}

func (vm *VM) CIDataPath() string {
	return filepath.Join(vm.Dir(), "cidata.iso")
}

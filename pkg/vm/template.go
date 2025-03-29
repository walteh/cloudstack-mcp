package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

// VMTemplate represents a pre-configured VM image template
type VMTemplate struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	BasePath    string            `json:"base_path"` // Original cloud image
	CreatedAt   time.Time         `json:"created_at"`
	Status      string            `json:"status"`       // "creating", "ready", "error"
	Packages    []string          `json:"packages"`     // List of packages installed
	SetupScript string            `json:"setup_script"` // Script used to set up the template
	MetaData    map[string]string `json:"meta_data"`    // Additional metadata
}

// templatesDir returns the directory where templates are stored
func templatesDir() string {
	return filepath.Join(baseDir(), "templates")
}

// Dir returns the template's directory
func (t *VMTemplate) Dir() string {
	return filepath.Join(templatesDir(), t.Name)
}

// ConfigPath returns the path to the template's configuration file
func (t *VMTemplate) ConfigPath() string {
	return filepath.Join(t.Dir(), "template.json")
}

// DiskPath returns the path to the template's disk image
func (t *VMTemplate) DiskPath() string {
	return filepath.Join(t.Dir(), t.Name+".qcow2")
}

// Save persists the template configuration to disk
func (t *VMTemplate) Save() error {
	if err := os.MkdirAll(t.Dir(), 0755); err != nil {
		return errors.Errorf("creating template directory: %w", err)
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return errors.Errorf("marshaling template config: %w", err)
	}

	if err := os.WriteFile(t.ConfigPath(), data, 0644); err != nil {
		return errors.Errorf("writing template config file: %w", err)
	}

	return nil
}

// Create a new template using a base image
func (m *LocalManager) CreateTemplate(ctx context.Context, name, description, baseImage string, packages []string, setupScript string) (*VMTemplate, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", name).Str("base", baseImage).Msg("Creating VM template")

	// Check if template with this name already exists
	templateDirs, err := os.ReadDir(templatesDir())
	if err == nil {
		for _, dir := range templateDirs {
			if dir.IsDir() && dir.Name() == name {
				return nil, errors.Errorf("template %s already exists", name)
			}
		}
	}

	// Ensure templates directory exists
	if err := os.MkdirAll(templatesDir(), 0755); err != nil {
		return nil, errors.Errorf("creating templates directory: %w", err)
	}

	// Get base image path
	baseImagePath := filepath.Join(imagesDir(), baseImage)
	if _, err := os.Stat(baseImagePath); os.IsNotExist(err) {
		return nil, errors.Errorf("base image not found: %s", baseImage)
	}

	// Create new template object
	template := &VMTemplate{
		Name:        name,
		Description: description,
		BasePath:    baseImagePath,
		CreatedAt:   time.Now(),
		Status:      "creating",
		Packages:    packages,
		SetupScript: setupScript,
		MetaData:    make(map[string]string),
	}

	// Save initial template configuration
	if err := template.Save(); err != nil {
		return nil, errors.Errorf("saving template config: %w", err)
	}

	return template, nil
}

// PrepareTemplate creates a VM, installs packages, and saves the result as a template
func (m *LocalManager) PrepareTemplate(ctx context.Context, template *VMTemplate) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", template.Name).Msg("Preparing VM template")

	// Create a temporary VM name for setup
	tempVMName := fmt.Sprintf("template-%s-setup", template.Name)

	// Create custom user-data with package installation
	var userData string
	if len(template.Packages) > 0 {
		userData = fmt.Sprintf(`#cloud-config
package_update: true
package_upgrade: true
packages:
%s

runcmd:
  - echo "Template setup completed" > /etc/template-ready
  - shutdown -h now
`, packageListToYaml(template.Packages))
	}

	if template.SetupScript != "" {
		userData += fmt.Sprintf(`
write_files:
  - path: /tmp/setup.sh
    permissions: '0755'
    content: |
%s

runcmd:
  - /tmp/setup.sh
  - echo "Template setup completed" > /etc/template-ready
  - shutdown -h now
`, indentScript(template.SetupScript))
	}

	// Create a temporary VM config
	vmConfig := VMConfig{
		Name:     tempVMName,
		CPUs:     2,
		Memory:   "2G",
		DiskSize: "20G",
		BaseImg: Img{
			Name: filepath.Base(template.BasePath),
			Url:  "file://" + template.BasePath,
		},
		Network: NetworkConfig{
			Type:     "user",
			MAC:      "52:54:00:12:34:56",
			Hostname: tempVMName,
		},
	}

	// Create a temporary VM
	tempVM, err := m.CreateVM(ctx, vmConfig)
	if err != nil {
		template.Status = "error"
		template.MetaData["error"] = err.Error()
		template.Save()
		return errors.Errorf("creating temporary VM: %w", err)
	}

	// Start the VM and wait for it to complete setup
	err = m.StartVM(ctx, tempVM)
	if err != nil {
		template.Status = "error"
		template.MetaData["error"] = err.Error()
		template.Save()
		return errors.Errorf("starting temporary VM: %w", err)
	}

	// Log that the VM is setting up
	logger.Info().Str("name", tempVMName).Msg("VM is setting up, waiting for completion (this may take several minutes)")

	// TODO: Wait for VM to complete setup and shut down
	// For now, we'll just wait a fixed amount of time
	// In a real implementation, we would monitor the VM status
	logger.Info().Msg("Waiting 5 minutes for VM setup to complete...")
	time.Sleep(5 * time.Minute)

	// Stop the VM if it's still running
	if tempVM.GetStatus() == "running" {
		logger.Info().Msg("Stopping the temporary VM...")
		err := tempVM.Stop()
		if err != nil {
			template.Status = "error"
			template.MetaData["error"] = err.Error()
			template.Save()
			return errors.Errorf("stopping temporary VM: %w", err)
		}

		// Wait for VM to fully stop
		time.Sleep(5 * time.Second)
	}

	// Once VM is set up, convert the disk to a template
	logger.Info().Msg("Creating template disk image from temporary VM...")

	// Create template disk directory
	if err := os.MkdirAll(filepath.Dir(template.DiskPath()), 0755); err != nil {
		template.Status = "error"
		template.MetaData["error"] = err.Error()
		template.Save()
		return errors.Errorf("creating template disk directory: %w", err)
	}

	// Copy the temporary VM's disk to the template location using qemu-img convert
	// This creates a clean, standalone image from the temporary VM
	cmd := exec.Command(
		m.QemuImgPath,
		"convert",
		"-O", "qcow2",
		tempVM.DiskPath(),
		template.DiskPath(),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		template.Status = "error"
		template.MetaData["error"] = fmt.Sprintf("converting disk: %s: %s", err.Error(), string(output))
		template.Save()
		return errors.Errorf("converting disk to template: %s: %w", output, err)
	}

	// Delete the temporary VM
	logger.Info().Msg("Cleaning up temporary VM...")
	if err := m.DeleteVM(ctx, tempVM); err != nil {
		logger.Warn().Err(err).Msg("Failed to clean up temporary VM, continuing anyway")
	}

	// Update the template status
	template.Status = "ready"
	if err := template.Save(); err != nil {
		return errors.Errorf("saving template config: %w", err)
	}

	logger.Info().Str("name", template.Name).Msg("Template prepared successfully")
	return nil
}

// ListTemplates returns a list of all available templates
func (m *LocalManager) ListTemplates(ctx context.Context) ([]*VMTemplate, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Listing VM templates")

	templatesPath := templatesDir()
	entries, err := os.ReadDir(templatesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Templates directory doesn't exist, return empty list
			return []*VMTemplate{}, nil
		}
		return nil, errors.Errorf("reading templates directory: %w", err)
	}

	templates := make([]*VMTemplate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			templatePath := filepath.Join(templatesPath, entry.Name(), "template.json")
			if _, err := os.Stat(templatePath); err == nil {
				// Template configuration exists, load it
				data, err := os.ReadFile(templatePath)
				if err != nil {
					logger.Warn().Err(err).Str("name", entry.Name()).Msg("Failed to read template config")
					continue
				}

				var template VMTemplate
				if err := json.Unmarshal(data, &template); err != nil {
					logger.Warn().Err(err).Str("name", entry.Name()).Msg("Failed to parse template config")
					continue
				}

				templates = append(templates, &template)
			}
		}
	}

	return templates, nil
}

// GetTemplate retrieves a template by name
func (m *LocalManager) GetTemplate(ctx context.Context, name string) (*VMTemplate, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", name).Msg("Getting VM template")

	templatePath := filepath.Join(templatesDir(), name, "template.json")
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return nil, errors.Errorf("template not found: %s", name)
	}

	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, errors.Errorf("reading template config: %w", err)
	}

	var template VMTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, errors.Errorf("parsing template config: %w", err)
	}

	return &template, nil
}

// DeleteTemplate removes a template and its resources
func (m *LocalManager) DeleteTemplate(ctx context.Context, template *VMTemplate) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", template.Name).Msg("Deleting VM template")

	// Remove the template directory
	if err := os.RemoveAll(template.Dir()); err != nil {
		return errors.Errorf("removing template directory: %w", err)
	}

	return nil
}

// CreateVMFromTemplate creates a new VM using a template as the base image
func (m *LocalManager) CreateVMFromTemplate(ctx context.Context, vmName string, template *VMTemplate, userData string) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vmName).Str("template", template.Name).Msg("Creating VM from template")

	// Ensure template is ready
	if template.Status != "ready" {
		return nil, errors.Errorf("template is not ready: %s (status: %s)", template.Name, template.Status)
	}

	// Create VM config
	vmConfig := VMConfig{
		Name:     vmName,
		CPUs:     2,
		Memory:   "2G",
		DiskSize: "20G", // This will be the size of the new layer, not the total size
		BaseImg: Img{
			Name: filepath.Base(template.DiskPath()),
			Url:  "file://" + template.DiskPath(),
		},
		Network: NetworkConfig{
			Type:     "user",
			MAC:      "52:54:00:12:34:56",
			Hostname: vmName,
		},
	}

	// Create VM object
	vm := &VM{
		Name:   vmName,
		Status: "created",
		Config: vmConfig,
		SSHInfo: SSHInfo{
			Username: "ubuntu", // Default for Ubuntu cloud images
			Host:     "localhost",
		},
		MetaData: map[string]string{
			"template": template.Name,
		},
	}

	// Create VM directory
	vmDir := vm.Dir()
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return nil, errors.Errorf("creating VM directory: %w", err)
	}

	// Create VM disk using template as backing file
	cmd := exec.Command(m.QemuImgPath, "create", "-f", "qcow2", "-F", "qcow2", "-b", template.DiskPath(), vm.DiskPath())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Errorf("creating VM disk: %w: %s", err, string(output))
	}

	// Generate cloud-init files based on the provided user data
	metaData, err := vm.BuildMetaData()
	if err != nil {
		return nil, errors.Errorf("generating meta-data: %w", err)
	}

	networkConfig, err := vm.NetworkConfig()
	if err != nil {
		return nil, errors.Errorf("generating network-config: %w", err)
	}

	// Create cloud-init ISO
	if err := createCloudInitISO(vmConfig, userData, metaData, networkConfig, m.MkisofsPath, vmDir); err != nil {
		return nil, errors.Errorf("creating cloud-init ISO: %w", err)
	}

	// Save VM state
	if err := vm.SaveState(); err != nil {
		return nil, errors.Errorf("saving VM state: %w", err)
	}

	return vm, nil
}

// Helper functions

// packageListToYaml converts a slice of package names to YAML format for cloud-init
func packageListToYaml(packages []string) string {
	var result string
	for _, pkg := range packages {
		result += fmt.Sprintf("  - %s\n", pkg)
	}
	return result
}

// indentScript indents each line of a script for YAML formatting
func indentScript(script string) string {
	// TODO: Implement proper indentation
	return script
}

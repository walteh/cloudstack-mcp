package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"gitlab.com/tozd/go/errors"
)

var templateGroup = &cobra.Group{
	ID:    "template",
	Title: "Template Management",
}

func init() {
	rootCmd.AddGroup(templateGroup)
	rootCmd.AddCommand(listTemplatesCmd)
	rootCmd.AddCommand(createTemplateCmd)
	rootCmd.AddCommand(deleteTemplateCmd)
	rootCmd.AddCommand(createVMFromTemplateCmd)
}

// listTemplatesCmd represents the list-templates command
var listTemplatesCmd = &cobra.Command{
	Use:   "list-templates",
	Short: "List available VM templates",
	Long:  `List all available VM templates that can be used to create VMs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTemplates(cmd.Context())
	},
	GroupID: templateGroup.ID,
}

// createTemplateCmd represents the create-template command
var createTemplateCmd = &cobra.Command{
	Use:   "create-template <name> <description> <base-image> [packages...]",
	Short: "Create a new VM template",
	Long:  `Create a new VM template based on a base image with additional packages installed.`,
	Args:  cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return createTemplate(cmd.Context(), args[0], args[1], args[2], args[3:])
	},
	GroupID: templateGroup.ID,
}

// deleteTemplateCmd represents the delete-template command
var deleteTemplateCmd = &cobra.Command{
	Use:   "delete-template <name>",
	Short: "Delete a VM template",
	Long:  `Delete a VM template and its associated resources.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteTemplate(cmd.Context(), args[0])
	},
	GroupID: templateGroup.ID,
}

// createVMFromTemplateCmd represents the create-vm-from-template command
var createVMFromTemplateCmd = &cobra.Command{
	Use:   "create-vm-from-template <vm-name> <template-name>",
	Short: "Create a VM from a template",
	Long:  `Create a new VM from an existing template.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return createVMFromTemplate(cmd.Context(), args[0], args[1])
	},
	GroupID: templateGroup.ID,
}

// Implementation functions

func listTemplates(ctx context.Context) error {
	templates, err := Manager.ListTemplates(ctx)
	if err != nil {
		return errors.Errorf("listing templates: %w", err)
	}

	fmt.Println("Available Templates:")
	for _, tmpl := range templates {
		fmt.Printf("  - %s: %s\n    Status: %s\n    Created: %s\n    Description: %s\n",
			tmpl.Name,
			tmpl.BasePath,
			tmpl.Status,
			tmpl.CreatedAt.Format("2006-01-02 15:04:05"),
			tmpl.Description)
	}

	return nil
}

func createTemplate(ctx context.Context, name, description, baseImage string, packages []string) error {
	// Create setupScript to install KVM
	setupScript := `#!/bin/bash
echo "Setting up KVM environment..."
# Additional setup commands can be added here
echo "Setup completed!"
`

	// Create the template
	template, err := Manager.CreateTemplate(ctx, name, description, baseImage, packages, setupScript)
	if err != nil {
		return errors.Errorf("creating template: %w", err)
	}

	fmt.Printf("Template %s created with status: %s\n", template.Name, template.Status)

	// Prepare the template (creating temporary VM, installing packages, etc.)
	fmt.Printf("Preparing template %s (this may take several minutes)...\n", name)
	if err := Manager.PrepareTemplate(ctx, template); err != nil {
		return errors.Errorf("preparing template: %w", err)
	}

	fmt.Printf("Template %s successfully prepared and ready to use\n", name)
	return nil
}

func deleteTemplate(ctx context.Context, name string) error {
	template, err := Manager.GetTemplate(ctx, name)
	if err != nil {
		return errors.Errorf("getting template: %w", err)
	}

	if err := Manager.DeleteTemplate(ctx, template); err != nil {
		return errors.Errorf("deleting template: %w", err)
	}

	fmt.Printf("Template %s deleted successfully\n", name)
	return nil
}

func createVMFromTemplate(ctx context.Context, vmName, templateName string) error {
	template, err := Manager.GetTemplate(ctx, templateName)
	if err != nil {
		return errors.Errorf("getting template: %w", err)
	}

	if template.Status != "ready" {
		return errors.Errorf("template %s is not ready (status: %s)", templateName, template.Status)
	}

	// Generate user data for the VM
	userData := fmt.Sprintf(`#cloud-config
hostname: %s
`, vmName)

	// Create VM from template
	createdVM, err := Manager.CreateVMFromTemplate(ctx, vmName, template, userData)
	if err != nil {
		return errors.Errorf("creating VM from template: %w", err)
	}

	fmt.Printf("VM %s created successfully from template %s\n", vmName, templateName)

	// Start VM automatically
	if err := Manager.StartVM(ctx, createdVM); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	fmt.Printf("VM %s started successfully\n", vmName)
	return nil
}

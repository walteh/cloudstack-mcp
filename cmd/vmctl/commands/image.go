package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
	"gitlab.com/tozd/go/errors"
)

var imageGroup = &cobra.Group{
	ID:    "image",
	Title: "Image Management",
}

func init() {
	rootCmd.AddGroup(imageGroup)
	rootCmd.AddCommand(listImagesCmd)
	rootCmd.AddCommand(downloadImageCmd)
}

// listImagesCmd represents the list-images command
var listImagesCmd = &cobra.Command{
	Use:   "list-images",
	Short: "List available base images",
	Long:  `List all available base images that can be used to create VMs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listImages(cmd.Context())
	},
	GroupID: imageGroup.ID,
}

func listImages(ctx context.Context) error {
	images, err := vm.ListImages(ctx)
	if err != nil {
		return errors.Errorf("listing images: %w", err)
	}

	fmt.Println("Available Images:")
	for _, img := range images {
		fmt.Printf("  - %s\n", img.Name)
	}

	return nil
}

// downloadImageCmd represents the download-image command
var downloadImageCmd = &cobra.Command{
	Use:   "download-image <name> <url>",
	Short: "Download a base image",
	Long:  `Download a base image from a URL and make it available for VM creation.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return downloadImage(cmd.Context(), args[0], args[1])
	},
	GroupID: imageGroup.ID,
}

// Implementation functions

func downloadImage(ctx context.Context, name, url string) error {
	img := vm.Img{
		Name: name,
		Url:  url,
	}

	if err := vm.DownloadImage(ctx, img, false); err != nil {
		return errors.Errorf("downloading image: %w", err)
	}

	fmt.Printf("Image %s downloaded successfully\n", name)
	return nil
}

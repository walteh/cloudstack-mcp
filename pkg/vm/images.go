package vm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

// DefaultImages contains a list of common cloud images
var DefaultImages = []Img{
	{
		Name: "jammy-server-cloudimg-amd64.img",
		Url:  "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
	},
	{
		Name: "jammy-server-cloudimg-arm64.img",
		Url:  "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-arm64.img",
	},
	{
		Name: "noble-server-cloudimg-arm64.img",
		Url:  "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img",
	},
	{
		Name: "noble-server-cloudimg-amd64.img",
		Url:  "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
	},
}

func (me *Img) Path() string {
	return filepath.Join(imagesDir(), me.Name)
}

func userDownloadsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Errorf("getting user home directory: %w", err)
	}
	return filepath.Join(homeDir, "Downloads"), nil
}

// DownloadImage downloads a VM base image if it doesn't exist
func DownloadImage(ctx context.Context, img Img, ignoreCache bool) error {
	logger := zerolog.Ctx(ctx)

	imgPath := filepath.Join(imagesDir(), img.Name)

	// Check if image already exists
	if _, err := os.Stat(imgPath); err == nil && !ignoreCache {
		logger.Info().Str("name", img.Name).Msg("Image already exists")
		return nil
	}

	// check if the image is already downloaded
	userDownloadsDir, err := userDownloadsDir()
	if err != nil {
		return errors.Errorf("getting user downloads directory: %w", err)
	}
	imgPath = filepath.Join(userDownloadsDir, img.Name)

	if _, err := os.Stat(imgPath); err == nil && !ignoreCache {
		logger.Info().Str("name", img.Name).Msg("Image already exists in user downloads directory")
		if err := os.Rename(imgPath, imgPath); err != nil {
			return errors.Errorf("moving image to cache directory: %w", err)
		}
		return nil
	}

	logger.Info().Str("name", img.Name).Str("url", img.Url).Msg("Downloading image")

	// Create images directory if it doesn't exist
	if err := os.MkdirAll(imagesDir(), 0755); err != nil {
		return errors.Errorf("creating images directory: %w", err)
	}

	// Download image
	resp, err := http.Get(img.Url)
	if err != nil {
		return errors.Errorf("downloading image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status code downloading image: %d", resp.StatusCode)
	}

	// Create temporary file
	tmpPath := fmt.Sprintf("%s.download", imgPath)
	f, err := os.Create(tmpPath)
	if err != nil {
		return errors.Errorf("creating temporary file: %w", err)
	}

	// Copy response body to file
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return errors.Errorf("saving image: %w", err)
	}

	// Rename temporary file to final path
	if err := os.Rename(tmpPath, imgPath); err != nil {
		os.Remove(tmpPath)
		return errors.Errorf("renaming image file: %w", err)
	}

	logger.Info().Str("name", img.Name).Msg("Image downloaded successfully")
	return nil
}

// ListImages lists available images in the images directory
func ListImages(ctx context.Context) ([]Img, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("dir", imagesDir()).Msg("Listing images")

	if _, err := os.Stat(imagesDir()); os.IsNotExist(err) {
		return []Img{}, nil
	}

	entries, err := os.ReadDir(imagesDir())
	if err != nil {
		return nil, errors.Errorf("reading images directory: %w", err)
	}

	var images []Img
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".img" {
			images = append(images, Img{
				Name: entry.Name(),
				Url:  "", // URL unknown for existing images
			})
		}
	}

	return images, nil
}

// DeleteImage deletes an image from the images directory
func DeleteImage(ctx context.Context, imagesDir string, imgName string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", imgName).Msg("Deleting image")

	imgPath := filepath.Join(imagesDir, imgName)
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		return errors.Errorf("image doesn't exist: %s", imgName)
	}

	if err := os.Remove(imgPath); err != nil {
		return errors.Errorf("deleting image: %w", err)
	}

	logger.Info().Str("name", imgName).Msg("Image deleted successfully")
	return nil
}

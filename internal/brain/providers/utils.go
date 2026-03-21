package providers

import (
	"os"
	"path/filepath"
)

func resolveMediaPath(inputPath string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return inputPath
	}

	fileName := filepath.Base(inputPath)
	mediaDir := filepath.Join(cwd, "media")
	fullPath := filepath.Join(mediaDir, fileName)

	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}

	trashPath := filepath.Join(mediaDir, ".trash", fileName)
	if _, err := os.Stat(trashPath); err == nil {
		return trashPath
	}

	return inputPath
}
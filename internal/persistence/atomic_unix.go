//go:build unix

package persistence

import (
	"errors"
	"os"
	"path/filepath"
)

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return syncParentDirectory(target)
}

func syncParentDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func syncRotationMarker(file *os.File) error {
	if err := file.Sync(); err != nil {
		return err
	}
	return syncParentDirectory(file.Name())
}

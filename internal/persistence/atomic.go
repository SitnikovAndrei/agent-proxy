package persistence

import (
	"context"
	"os"
	"path/filepath"
)

func AtomicWrite(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := CanonicalPath(path)
	if err != nil {
		return err
	}
	path = target
	file, err := os.CreateTemp(filepath.Dir(path), ".api-proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replaceFile(file.Name(), path)
}

package persistence

import (
	"errors"
	"os"
	"path/filepath"
)

func CanonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	candidate := absolute
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		suffix = append(suffix, filepath.Base(candidate))
		candidate = parent
	}
}

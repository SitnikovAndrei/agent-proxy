//go:build !unix && !windows

package persistence

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}

func syncRotationMarker(file *os.File) error {
	return os.ErrUnsupported
}

//go:build !unix && !windows

package persistence

import "os"

func tryFileLock(file *os.File) (bool, error) {
	return false, os.ErrUnsupported
}

func unlockFile(file *os.File) error {
	return os.ErrUnsupported
}

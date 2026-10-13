package persistence

import (
	"context"
	"errors"
	"os"
	"time"
)

const credentialLockSuffix = ".api-proxy.lock"

type CredentialLock struct {
	file *os.File
}

func CredentialLockPath(credentialPath string) (string, error) {
	path, err := CanonicalPath(credentialPath)
	if err != nil {
		return "", err
	}
	return path + credentialLockSuffix, nil
}

func LockCredentials(ctx context.Context, credentialPath string) (*CredentialLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := CredentialLockPath(credentialPath)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := waitForFileLock(ctx, file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &CredentialLock{file: file}, nil
}

func waitForFileLock(ctx context.Context, file *os.File) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		acquired, err := tryFileLock(file)
		if err != nil || acquired {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *CredentialLock) Close() error {
	return errors.Join(unlockFile(l.file), l.file.Close())
}

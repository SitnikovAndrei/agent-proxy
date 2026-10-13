package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"ocm/internal/persistence"
)

// Credentials and statistics must never share a file, including aliases.
func ValidateFileSeparation(paths ...string) error {
	type filePath struct {
		absolute string
		info     os.FileInfo
	}
	var resolved []filePath
	for _, path := range paths {
		if path == "" {
			continue
		}
		absolute, err := persistence.CanonicalPath(path)
		if err != nil {
			return fmt.Errorf("resolve configured file path: %w", err)
		}
		info, err := os.Stat(absolute)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect configured file: %w", err)
		}
		for _, previous := range resolved {
			equal := absolute == previous.absolute
			if runtime.GOOS == "windows" {
				equal = strings.EqualFold(absolute, previous.absolute)
			}
			if equal || (info != nil && previous.info != nil && os.SameFile(info, previous.info)) {
				return errors.New("credentials, statistics database, and sidecars must refer to separate files")
			}
		}
		resolved = append(resolved, filePath{absolute: absolute, info: info})
	}
	return nil
}

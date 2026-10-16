//go:build unix

package persistence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := AtomicWrite(context.Background(), path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

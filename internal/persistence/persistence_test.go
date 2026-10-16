package persistence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	for _, content := range []string{"first", "replacement"} {
		if err := AtomicWrite(context.Background(), path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil || string(body) != content {
			t.Fatalf("content=%q err=%v", body, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := AtomicWrite(ctx, path, []byte("canceled"), 0600); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "replacement" {
		t.Fatalf("content=%q err=%v", body, err)
	}
	if err := AtomicWrite(context.Background(), dir, []byte("cannot replace directory"), 0600); err == nil {
		t.Fatal("expected replacement failure")
	}
	temporary, err := filepath.Glob(filepath.Join(dir, ".api-proxy-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary=%v err=%v", temporary, err)
	}
}

func TestCanonicalPathMissingTail(t *testing.T) {
	dir := t.TempDir()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	path, err := CanonicalPath(filepath.Join(dir, "missing", "child"))
	if err != nil || path != filepath.Join(root, "missing", "child") {
		t.Fatalf("path=%q err=%v", path, err)
	}
}

func TestCredentialLockExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	first, err := LockCredentials(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	second, err := LockCredentials(ctx, path)
	if second != nil {
		second.Close()
		t.Fatal("second lock acquired")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	second, err = LockCredentials(ctx2, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRotationMarker(t *testing.T) {
	lock, err := LockCredentials(context.Background(), filepath.Join(t.TempDir(), "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.BeginRotation(context.Background(), "refresh", "access"); err != nil {
		t.Fatal(err)
	}
	if err := lock.BeginRotation(context.Background(), "refresh", "access"); !errors.Is(err, ErrUncertainRotation) {
		t.Fatal(err)
	}
	if err := lock.CompleteRotation(); err != nil {
		t.Fatal(err)
	}
	if err := lock.BeginRotation(context.Background(), "refresh", "access"); err != nil {
		t.Fatal(err)
	}
	if err := lock.file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.file.WriteAt([]byte("invalid"), 0); err != nil {
		t.Fatal(err)
	}
	if err := lock.BeginRotation(context.Background(), "next", "access"); err == nil {
		t.Fatal("invalid marker accepted")
	}
}

func TestCanonicalJSON(t *testing.T) {
	got, err := CanonicalJSON([]byte(` {"z":[{"b":2,"a":1}], "a":{"y":true,"x":null}} `))
	if err != nil || string(got) != `{"a":{"x":null,"y":true},"z":[{"a":1,"b":2}]}` {
		t.Fatalf("json=%s err=%v", got, err)
	}
	for _, invalid := range []string{"", "{", "[1,]"} {
		if _, err := CanonicalJSON([]byte(invalid)); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

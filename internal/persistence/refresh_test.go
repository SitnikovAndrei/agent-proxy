package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testCredentials struct {
	Refresh string
	Access  string
	Fresh   bool
}

func refreshFixture(t *testing.T) (string, RefreshOps[testCredentials]) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	write := func(ctx context.Context, c testCredentials) error {
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return AtomicWrite(ctx, path, data, 0600)
	}
	if err := write(context.Background(), testCredentials{Refresh: "old", Access: "old"}); err != nil {
		t.Fatal(err)
	}
	ops := RefreshOps[testCredentials]{
		Read: func(ctx context.Context) (testCredentials, error) {
			var c testCredentials
			if err := ctx.Err(); err != nil {
				return c, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return c, err
			}
			err = json.Unmarshal(data, &c)
			return c, err
		},
		NeedsRefresh: func(c testCredentials) bool { return !c.Fresh },
		Tokens:       func(c testCredentials) (string, string) { return c.Refresh, c.Access },
		Changed:      func(before, current testCredentials) bool { return before != current },
		Refresh: func(ctx context.Context, c testCredentials) (testCredentials, error) {
			return testCredentials{Refresh: "new", Access: "new", Fresh: true}, nil
		},
		Write: func(ctx context.Context, current, refreshed testCredentials) error { return write(ctx, refreshed) },
	}
	return path, ops
}

func TestRefreshConcurrentCallers(t *testing.T) {
	path, ops := refreshFixture(t)
	var calls atomic.Int32
	refresh := ops.Refresh
	ops.Refresh = func(ctx context.Context, c testCredentials) (testCredentials, error) {
		calls.Add(1)
		return refresh(ctx, c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			got, err := RefreshUnderLock(ctx, path, time.Second, ops)
			if err != nil || !got.Fresh {
				t.Errorf("result=%+v err=%v", got, err)
			}
		}()
	}
	close(start)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
}

func TestRefreshRetriesChangedFile(t *testing.T) {
	path, ops := refreshFixture(t)
	calls := 0
	ops.Refresh = func(ctx context.Context, c testCredentials) (testCredentials, error) {
		calls++
		if calls == 1 {
			changed := testCredentials{Refresh: "external", Access: "external"}
			if err := ops.Write(ctx, c, changed); err != nil {
				return c, err
			}
		} else if c.Refresh != "external" {
			t.Fatalf("retry used stale credentials: %+v", c)
		}
		return testCredentials{Refresh: "new", Access: "new", Fresh: true}, nil
	}
	got, err := RefreshUnderLock(context.Background(), path, time.Second, ops)
	if err != nil || !got.Fresh || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", got, calls, err)
	}
}

func TestRefreshFailureMarker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sent     bool
		rejected bool
	}{
		{name: "pre-send"},
		{name: "post-send", sent: true},
		{name: "rejected", sent: true, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, ops := refreshFixture(t)
			failure := errors.New("refresh failed")
			if tc.rejected {
				failure = fmt.Errorf("refresh failed: %w", ErrRefreshRejected)
			}
			calls := 0
			ops.Refresh = func(ctx context.Context, c testCredentials) (testCredentials, error) {
				calls++
				trace := httptrace.ContextClientTrace(ctx)
				if trace == nil {
					t.Fatal("missing request trace")
				}
				info := httptrace.WroteRequestInfo{}
				if !tc.sent {
					info.Err = failure
				}
				trace.WroteRequest(info)
				return c, failure
			}
			if _, err := RefreshUnderLock(context.Background(), path, time.Second, ops); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			_, err := RefreshUnderLock(context.Background(), path, time.Second, ops)
			if tc.sent && !tc.rejected {
				if !errors.Is(err, ErrUncertainRotation) || calls != 1 {
					t.Fatalf("lost marker: %v", err)
				}
				return
			}
			if !errors.Is(err, failure) || errors.Is(err, ErrUncertainRotation) || calls != 2 {
				t.Fatalf("refresh should be retryable: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRefreshPersistsAfterCancellation(t *testing.T) {
	path, ops := refreshFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refresh := ops.Refresh
	ops.Refresh = func(ctx context.Context, c testCredentials) (testCredentials, error) {
		result, err := refresh(ctx, c)
		cancel()
		return result, err
	}
	got, err := RefreshUnderLock(ctx, path, time.Second, ops)
	if err != nil || !got.Fresh {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	disk, err := ops.Read(context.Background())
	if err != nil || disk != got {
		t.Fatalf("disk=%+v err=%v", disk, err)
	}
	lock, err := LockCredentials(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	pending, err := lock.readRotation()
	if err != nil || pending != nil {
		t.Fatalf("marker=%+v err=%v", pending, err)
	}
}
